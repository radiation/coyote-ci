package service

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/artifact"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func TestWorkspaceHelperArtifactServiceTerminalPlanIncludesAllBuildScopes(t *testing.T) {
	harness := newWorkspaceHelperArtifactHarness(t)
	plan, err := harness.service.Plan(context.Background(), "token", harness.job.ID, "pod-uid", true)
	if err != nil || !plan.Collect {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	if len(plan.Scopes) != 3 || !hasArtifactScope(plan.Scopes, "step-1", "first/*.txt") || !hasArtifactScope(plan.Scopes, "step-2", "second/*.txt") || !hasArtifactScope(plan.Scopes, "", "shared/*.txt") {
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

func TestWorkspaceHelperArtifactServicePersistsBuildWideScopesAndConvergesRetries(t *testing.T) {
	harness := newWorkspaceHelperArtifactHarness(t)
	context := context.Background()
	if err := harness.service.Upload(context, "token", harness.job.ID, "pod-uid", "step-1", "first/output.txt", true, strings.NewReader("first")); err != nil {
		t.Fatalf("upload first step artifact: %v", err)
	}
	if err := harness.service.Upload(context, "token", harness.job.ID, "pod-uid", "step-2", "second/output.txt", true, strings.NewReader("second")); err != nil {
		t.Fatalf("upload prior step artifact: %v", err)
	}
	if err := harness.service.Upload(context, "token", harness.job.ID, "pod-uid", "", "shared/output.txt", true, strings.NewReader("shared")); err != nil {
		t.Fatalf("upload shared artifact: %v", err)
	}
	if err := harness.service.Upload(context, "token", harness.job.ID, "pod-uid", "step-1", "first/output.txt", true, bytes.NewBufferString("retry")); err != nil {
		t.Fatalf("retry first step artifact: %v", err)
	}
	artifacts, err := harness.artifacts.ListByBuildID(context, harness.build.ID)
	if err != nil || len(artifacts) != 3 {
		t.Fatalf("artifacts=%#v err=%v", artifacts, err)
	}
	if !hasPersistedArtifact(artifacts, "first/output.txt", "step-1") || !hasPersistedArtifact(artifacts, "second/output.txt", "step-2") || !hasPersistedArtifact(artifacts, "shared/output.txt", "") {
		t.Fatalf("persisted artifacts=%#v", artifacts)
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
}

func (r *workspaceHelperArtifactRepositoryFake) Create(_ context.Context, artifact domain.BuildArtifact) (domain.BuildArtifact, error) {
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
