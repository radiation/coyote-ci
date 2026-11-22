package service

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/artifact"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/repository/memory"
	buildsvc "github.com/radiation/coyote-ci/backend/internal/service/build"
)

type stalledArtifactFetcher struct{ t *testing.T }

func (fetcher stalledArtifactFetcher) Fetch(ctx context.Context, _, _ string) (string, string, error) {
	if _, ok := ctx.Deadline(); !ok {
		fetcher.t.Fatal("artifact fetch has no deadline")
	}
	<-ctx.Done()
	return "", "", errors.New("git clone interrupted")
}

func TestWorkspaceHelperArtifactPublicationRecovery(t *testing.T) {
	ctx := context.Background()
	harness := newWorkspaceHelperArtifactHarness(t)
	jobs, builds := memory.NewJobRepository(), memory.NewBuildRepository()
	deliveries := memory.NewArtifactBuildDeliveryRepository(jobs, builds, memory.NewExecutionJobRepository(), memory.NewExecutionJobOutputRepository())
	publisher := memory.NewPublishingArtifactRepository(deliveries)
	harness.service.artifacts = publisher
	upstream := "upstream"
	producer := domain.Build{ID: harness.build.ID, JobID: &upstream, ProjectID: "project"}
	if _, err := builds.CreateQueuedBuild(ctx, producer, nil); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"first", "second", "disabled", "foreign", "different-path"} {
		consumer := domain.Job{ID: id, Name: id, ProjectID: producer.ProjectID, Enabled: id != "disabled", RepositoryURL: "https://example.invalid", DefaultRef: "main", PipelineYAML: "version: 1\nsteps:\n  - name: consume\n    run: true\n", ArtifactTriggers: []domain.JobArtifactTrigger{{ProducerJobID: upstream, Path: "shared/output.txt"}}}
		if id == "foreign" {
			consumer.ProjectID = "other"
		}
		if id == "different-path" {
			consumer.ArtifactTriggers[0].Path = "shared/*.txt"
		}
		if _, err := jobs.Create(ctx, consumer); err != nil {
			t.Fatal(err)
		}
	}
	upload := func(pod string) error {
		return harness.service.Upload(ctx, "token", harness.job.ID, pod, "", "shared/output.txt", true, strings.NewReader("shared"))
	}
	if err := upload("wrong-pod"); err == nil {
		t.Fatal("unauthorized upload accepted")
	}
	if rows, err := publisher.ListByBuildID(ctx, producer.ID); err != nil || len(rows) != 0 {
		t.Fatalf("unauthorized metadata: %+v %v", rows, err)
	}
	for range 2 {
		if err := upload("pod-uid"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := builds.UpdateStatus(ctx, producer.ID, domain.BuildStatusCanceled, nil); err != nil {
		t.Fatal(err)
	}
	page, err := deliveries.ListPendingAfter(ctx, "", 64)
	if err != nil || len(page) != 2 {
		t.Fatalf("expected two matching consumers: %+v %v", page, err)
	}
	broken, err := jobs.GetByID(ctx, page[0].ConsumerJobID)
	if err != nil {
		t.Fatal(err)
	}
	path := ".coyote/pipeline.yml"
	broken.PipelineYAML, broken.PipelinePath = "", &path
	if _, updateErr := jobs.Update(ctx, broken); updateErr != nil {
		t.Fatal(updateErr)
	}
	buildService := buildsvc.NewBuildService(builds, nil, nil)
	buildService.SetArtifactPersistence(publisher, nil, "")
	server := NewJobService(jobs, buildService).WithArtifactTriggerDeliveryRepository(deliveries)
	server.artifactAttemptTimeout = 100 * time.Millisecond
	buildService.SetRepoFetcher(stalledArtifactFetcher{t})
	if _, pageErr := server.recoverArtifactPage(ctx, "", 64); pageErr != nil {
		t.Fatal(pageErr)
	}
	remaining, scanErr := deliveries.ListPendingAfter(ctx, "", 64)
	if scanErr != nil || len(remaining) != 1 || remaining[0].ID != page[0].ID || !remaining[0].UpdatedAt.Equal(page[0].UpdatedAt) {
		t.Fatalf("stalled delivery not recoverable: %+v %v", remaining, scanErr)
	}
	queued, lookupErr := deliveries.GetByID(ctx, page[1].ID)
	if lookupErr != nil || queued.QueuedBuildID == nil {
		t.Fatalf("later delivery did not queue: %+v %v", queued, lookupErr)
	}
	broken.PipelineYAML = "invalid"
	if _, updateErr := jobs.Update(ctx, broken); updateErr != nil {
		t.Fatal(updateErr)
	}
	if _, pageErr := server.recoverArtifactPage(ctx, "", 64); pageErr != nil {
		t.Fatal(pageErr)
	}
	for index, candidate := range page {
		current, getErr := deliveries.GetByID(ctx, candidate.ID)
		if getErr != nil || index == 0 && current.Status != domain.ArtifactTriggerDeliveryStatusFailed || index == 1 && current.QueuedBuildID == nil {
			t.Fatalf("delivery %d: %+v %v", index, current, getErr)
		}
	}
	linked, processErr := server.processArtifactDelivery(ctx, page[1])
	if processErr != nil || linked.QueuedBuildID == nil {
		t.Fatalf("satisfied delivery: %+v %v", linked, processErr)
	}
	producer.ID = "rerun"
	if _, createErr := builds.CreateQueuedBuild(ctx, producer, nil); createErr != nil {
		t.Fatal(createErr)
	}
	if _, publishErr := publisher.Create(ctx, domain.BuildArtifact{ID: "rerun-artifact", BuildID: producer.ID, LogicalPath: "shared/output.txt", CreatedAt: time.Now()}); publishErr != nil {
		t.Fatal(publishErr)
	}
	if rows, listErr := deliveries.ListByProducerBuildID(ctx, producer.ID); listErr != nil || len(rows) != 2 || rows[0].ArtifactID == page[0].ArtifactID {
		t.Fatalf("rerun obligations: %+v %v", rows, listErr)
	}
}

func TestWorkspaceHelperArtifactServiceTerminalPlanIncludesBuildWideScopes(t *testing.T) {
	harness := newWorkspaceHelperArtifactHarness(t)
	plan, err := harness.service.Plan(context.Background(), "token", harness.job.ID, "pod-uid", true)
	if err != nil || !plan.Collect {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	if len(plan.Scopes) != 1 || !hasArtifactScope(plan.Scopes, "", "shared/*.txt") {
		t.Fatalf("terminal scopes=%#v", plan.Scopes)
	}
}

func TestWorkspaceHelperArtifactServiceFailedTerminalPlanIncludesPriorScopes(t *testing.T) {
	harness := newWorkspaceHelperArtifactHarness(t)
	plan, err := harness.service.Plan(context.Background(), "token", harness.job.ID, "pod-uid", false)
	if err != nil || !plan.Collect || !hasArtifactScope(plan.Scopes, "step-1", "first/*.txt") || !hasArtifactScope(plan.Scopes, "step-2", "second/*.txt") || !hasArtifactScope(plan.Scopes, "", "shared/*.txt") {
		t.Fatalf("failed terminal plan=%#v err=%v", plan, err)
	}
}

func TestWorkspaceHelperArtifactServiceDefersNonterminalSuccessfulCollection(t *testing.T) {
	harness := newWorkspaceHelperArtifactHarness(t)
	harness.builds.steps[0].Status = domain.BuildStepStatusRunning
	plan, err := harness.service.Plan(context.Background(), "token", harness.job.ID, "pod-uid", true)
	if err != nil || plan.Collect {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
}

func TestWorkspaceHelperArtifactServiceDefersNonterminalFailedCollection(t *testing.T) {
	harness := newWorkspaceHelperArtifactHarness(t)
	harness.builds.steps[0].Status = domain.BuildStepStatusRunning
	plan, err := harness.service.Plan(context.Background(), "token", harness.job.ID, "pod-uid", false)
	if err != nil || plan.Collect {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
}

func TestWorkspaceHelperArtifactServiceSuccessfulProducerPlanUsesDurableStepID(t *testing.T) {
	harness := newWorkspaceHelperArtifactHarness(t)
	harness.job.StepID, harness.job.StepIndex = "step-1", 0
	harness.job.NodeID = "node-000"
	harness.service.executionJobs = &workspaceHelperCacheExecutionJobFake{job: harness.job}
	pipelineYAML := "version: 1\nsteps:\n  - name: first\n    run: true\n    artifacts:\n      - name: coyote-server\n        path: first/output.txt\n"
	harness.build.PipelineConfigYAML = &pipelineYAML
	harness.builds.build = harness.build

	plan, err := harness.service.Plan(context.Background(), "token", harness.job.ID, "pod-uid", true)
	if err != nil || !plan.Collect || len(plan.Scopes) != 1 || !hasArtifactScope(plan.Scopes, "step-1", "first/*.txt") {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	if uploadErr := harness.service.Upload(context.Background(), "token", harness.job.ID, "pod-uid", "step-1", "first/output.txt", true, strings.NewReader("first")); uploadErr != nil {
		t.Fatalf("upload producer artifact: %v", uploadErr)
	}
	artifacts, listErr := harness.artifacts.ListByBuildID(context.Background(), harness.build.ID)
	if listErr != nil || len(artifacts) != 1 || !hasPersistedArtifact(artifacts, "first/output.txt", "step-1") {
		t.Fatalf("artifacts=%#v err=%v", artifacts, listErr)
	}
	if artifacts[0].Name != "coyote-server" {
		t.Fatalf("artifact name=%q", artifacts[0].Name)
	}
}

func TestWorkspaceHelperArtifactServiceCollectsNonRootProducerArtifact(t *testing.T) {
	harness := newWorkspaceHelperArtifactHarness(t)
	harness.job.StepID, harness.job.StepIndex, harness.job.NodeID = "step-1", 0, "node-000"
	harness.service.executionJobs = &workspaceHelperCacheExecutionJobFake{job: harness.job}
	harness.builds.steps[0].WorkingDir, harness.builds.steps[0].ArtifactPaths = "backend", []string{"dist/coyote-server"}
	pipelineYAML := "version: 1\nsteps:\n  - name: first\n    working_dir: backend\n    run: true\n    artifacts:\n      - name: coyote-server\n        path: dist/coyote-server\n"
	harness.build.PipelineConfigYAML = &pipelineYAML
	harness.builds.build = harness.build

	plan, planErr := harness.service.Plan(context.Background(), "token", harness.job.ID, "pod-uid", true)
	if planErr != nil || !plan.Collect || len(plan.Scopes) != 1 || !hasArtifactScope(plan.Scopes, "step-1", "backend/dist/coyote-server") {
		t.Fatalf("plan=%#v err=%v", plan, planErr)
	}
	uploadErr := harness.service.Upload(context.Background(), "token", harness.job.ID, "pod-uid", "step-1", "backend/dist/coyote-server", true, strings.NewReader("server"))
	if uploadErr != nil {
		t.Fatalf("upload producer artifact: %v", uploadErr)
	}
	artifacts, listErr := harness.artifacts.ListByBuildID(context.Background(), harness.build.ID)
	if listErr != nil || len(artifacts) != 1 || !hasPersistedArtifact(artifacts, "backend/dist/coyote-server", "step-1") || artifacts[0].Name != "coyote-server" {
		t.Fatalf("artifacts=%#v err=%v", artifacts, listErr)
	}
}

func TestWorkspaceHelperArtifactServiceRejectsArtifactPathEscapingWorkspace(t *testing.T) {
	harness := newWorkspaceHelperArtifactHarness(t)
	harness.builds.steps[0].ArtifactPaths = []string{"../outside"}
	_, planErr := harness.service.Plan(context.Background(), "token", harness.job.ID, "pod-uid", false)
	if planErr == nil {
		t.Fatal("expected escaping artifact path rejection")
	}
}

func TestWorkspaceHelperArtifactServicePersistsBuildWideScopesAndConvergesRetries(t *testing.T) {
	harness := newWorkspaceHelperArtifactHarness(t)
	context := context.Background()
	if err := harness.service.Upload(context, "token", harness.job.ID, "pod-uid", "", "shared/output.txt", true, strings.NewReader("shared")); err != nil {
		t.Fatalf("upload shared artifact: %v", err)
	}
	if err := harness.service.Upload(context, "token", harness.job.ID, "pod-uid", "", "shared/output.txt", true, bytes.NewBufferString("retry")); err != nil {
		t.Fatalf("retry shared artifact: %v", err)
	}
	artifacts, err := harness.artifacts.ListByBuildID(context, harness.build.ID)
	if err != nil || len(artifacts) != 1 {
		t.Fatalf("artifacts=%#v err=%v", artifacts, err)
	}
	if !hasPersistedArtifact(artifacts, "shared/output.txt", "") {
		t.Fatalf("persisted artifacts=%#v", artifacts)
	}
}

func TestWorkspaceHelperArtifactServiceConvergesMetadataConflictAfterCollection(t *testing.T) {
	harness := newWorkspaceHelperArtifactHarness(t)
	harness.artifacts.createErr = repository.ErrArtifactConflict

	uploadErr := harness.service.Upload(context.Background(), "token", harness.job.ID, "pod-uid", "", "shared/output.txt", true, strings.NewReader("shared"))
	if uploadErr != nil {
		t.Fatalf("upload should converge after a metadata conflict: %v", uploadErr)
	}
}

func TestWorkspaceHelperArtifactServiceRejectsUnauthorizedScope(t *testing.T) {
	harness := newWorkspaceHelperArtifactHarness(t)
	if err := harness.service.Upload(context.Background(), "token", harness.job.ID, "pod-uid", "other-step", "first/output.txt", true, strings.NewReader("bad")); err == nil {
		t.Fatal("expected arbitrary step upload rejection")
	}
	harness.capabilities.expectedRole = domain.WorkspaceHelperRoleCacheSave
	if _, err := harness.service.Plan(context.Background(), "token", harness.job.ID, "pod-uid", true); err == nil {
		t.Fatal("expected wrong helper role rejection")
	}
}

type workspaceHelperArtifactHarness struct {
	service      *WorkspaceHelperArtifactService
	capabilities *workspaceHelperCacheCapabilityFake
	builds       *workspaceHelperCacheBuildFake
	artifacts    *workspaceHelperArtifactRepositoryFake
	build        domain.Build
	job          domain.ExecutionJob
}

func newWorkspaceHelperArtifactHarness(t *testing.T) *workspaceHelperArtifactHarness {
	t.Helper()
	pipelineYAML := "version: 1\nsteps:\n  - name: first\n    run: true\n  - name: second\n    run: true\n  - name: final\n    run: true\nartifacts:\n  - shared/*.txt\n"
	build := domain.Build{ID: "build-1", PipelineConfigYAML: &pipelineYAML}
	steps := []domain.BuildStep{{ID: "step-1", BuildID: build.ID, StepIndex: 0, Status: domain.BuildStepStatusSuccess, ArtifactPaths: []string{"first/*.txt"}}, {ID: "step-2", BuildID: build.ID, StepIndex: 1, Status: domain.BuildStepStatusSuccess, ArtifactPaths: []string{"second/*.txt"}}, {ID: "step-final", BuildID: build.ID, StepIndex: 2, Status: domain.BuildStepStatusRunning}}
	job := domain.ExecutionJob{ID: "execution-job", BuildID: build.ID, StepID: "step-final", StepIndex: 2}
	capabilities := &workspaceHelperCacheCapabilityFake{expectedRole: domain.WorkspaceHelperRoleArtifactCollect, expectedJobID: job.ID, expectedPodUID: "pod-uid"}
	builds := &workspaceHelperCacheBuildFake{build: build, steps: steps}
	artifacts := &workspaceHelperArtifactRepositoryFake{}
	service, err := NewWorkspaceHelperArtifactService(WorkspaceHelperArtifactServiceConfig{CapabilityAuthorizer: capabilities, ExecutionJobs: &workspaceHelperCacheExecutionJobFake{job: job}, Builds: builds, Artifacts: artifacts, Collector: artifact.NewCollector(artifact.NewFilesystemStore(t.TempDir()))})
	if err != nil {
		t.Fatalf("new artifact service: %v", err)
	}
	return &workspaceHelperArtifactHarness{service: service, capabilities: capabilities, builds: builds, artifacts: artifacts, build: build, job: job}
}

type workspaceHelperArtifactRepositoryFake struct {
	artifacts []domain.BuildArtifact
	createErr error
}

func (r *workspaceHelperArtifactRepositoryFake) Create(_ context.Context, artifact domain.BuildArtifact) (domain.BuildArtifact, error) {
	if r.createErr != nil {
		return domain.BuildArtifact{}, r.createErr
	}
	for _, existing := range r.artifacts {
		if existing.BuildID != artifact.BuildID || existing.LogicalPath != artifact.LogicalPath {
			continue
		}
		if existing.StepID == nil && artifact.StepID == nil {
			return domain.BuildArtifact{}, repository.ErrArtifactConflict
		}
		if existing.StepID != nil && artifact.StepID != nil && *existing.StepID == *artifact.StepID {
			return domain.BuildArtifact{}, repository.ErrArtifactConflict
		}
	}
	r.artifacts = append(r.artifacts, artifact)
	return artifact, nil
}

func (r *workspaceHelperArtifactRepositoryFake) ListByBuildID(_ context.Context, buildID string) ([]domain.BuildArtifact, error) {
	items := make([]domain.BuildArtifact, 0, len(r.artifacts))
	for _, artifact := range r.artifacts {
		if artifact.BuildID == buildID {
			items = append(items, artifact)
		}
	}
	return items, nil
}

func (r *workspaceHelperArtifactRepositoryFake) Browse(context.Context, repository.BrowseArtifactsParams) ([]domain.ArtifactRecord, error) {
	return nil, nil
}

func (r *workspaceHelperArtifactRepositoryFake) ListCatalog(context.Context, repository.ArtifactCatalogParams) ([]domain.ArtifactRecord, error) {
	return nil, nil
}

func (r *workspaceHelperArtifactRepositoryFake) GetCatalogByID(context.Context, string) (domain.ArtifactRecord, error) {
	return domain.ArtifactRecord{}, repository.ErrArtifactNotFound
}

func (r *workspaceHelperArtifactRepositoryFake) GetByID(_ context.Context, buildID string, artifactID string) (domain.BuildArtifact, error) {
	for _, artifact := range r.artifacts {
		if artifact.BuildID == buildID && artifact.ID == artifactID {
			return artifact, nil
		}
	}
	return domain.BuildArtifact{}, repository.ErrArtifactNotFound
}

func (r *workspaceHelperArtifactRepositoryFake) ListByStepID(_ context.Context, stepID string) ([]domain.BuildArtifact, error) {
	items := make([]domain.BuildArtifact, 0)
	for _, artifact := range r.artifacts {
		if artifact.StepID != nil && *artifact.StepID == stepID {
			items = append(items, artifact)
		}
	}
	return items, nil
}

func hasArtifactScope(scopes []WorkspaceHelperArtifactScope, stepID, pattern string) bool {
	for _, scope := range scopes {
		if scope.StepID == stepID && len(scope.Patterns) == 1 && scope.Patterns[0] == pattern {
			return true
		}
	}
	return false
}

func hasPersistedArtifact(artifacts []domain.BuildArtifact, logicalPath, stepID string) bool {
	for _, item := range artifacts {
		actualStepID := ""
		if item.StepID != nil {
			actualStepID = *item.StepID
		}
		if item.LogicalPath == logicalPath && actualStepID == stepID {
			return true
		}
	}
	return false
}
