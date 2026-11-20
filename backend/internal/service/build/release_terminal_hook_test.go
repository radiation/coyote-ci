package build

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/logs"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/repository/memory"
	"github.com/radiation/coyote-ci/backend/internal/runner"
)

func TestBuildService_LocalCompletionTagsChannelsOnce(t *testing.T) {
	for _, atomicCompletion := range []bool{false, true} {
		for _, collection := range []string{"existing", "collect", "fail"} {
			t.Run(fmt.Sprintf("atomic=%t/%s", atomicCompletion, collection), func(t *testing.T) {
				testLocalCompletionTagsChannelsOnce(t, atomicCompletion, collection)
			})
		}
	}
}

func testLocalCompletionTagsChannelsOnce(t *testing.T, atomicCompletion bool, collection string) {
	t.Helper()
	ctx := context.Background()
	claimToken := "claim-1"
	jobID := "job-1"
	pipelineYAML := "version: 1\nsteps:\n  - name: package\n    run: true\nartifacts:\n  - path: dist/*.tgz\n    version:\n      template: '1.2.3'\n      channel: latest\n"
	build := domain.Build{ID: "build-local", JobID: &jobID, PipelineConfigYAML: &pipelineYAML}
	repo := memory.NewBuildRepository()
	if _, createErr := repo.CreateQueuedBuild(ctx, build, []domain.BuildStep{{ID: "step-local", BuildID: build.ID, StepIndex: 0, Name: "package", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken}}); createErr != nil {
		t.Fatal(createErr)
	}
	if _, startErr := repo.UpdateStatus(ctx, build.ID, domain.BuildStatusRunning, nil); startErr != nil {
		t.Fatal(startErr)
	}
	tagger := &fakeBuildVersionTagger{}
	artifactRepo := &fakeArtifactRepository{artifacts: map[string][]domain.BuildArtifact{build.ID: {{ID: "artifact-1", BuildID: build.ID, LogicalPath: "dist/app.tgz"}}}}
	jobs := memory.NewExecutionJobRepository()
	jobs.SetBuildRepository(repo)
	claimExpiresAt := time.Now().UTC().Add(time.Minute)
	if _, createErr := jobs.CreateJobsForBuild(ctx, []domain.ExecutionJob{{ID: "execution-local", BuildID: build.ID, StepID: "step-local", StepIndex: 0, Status: domain.ExecutionJobStatusRunning, ClaimToken: &claimToken, ClaimExpiresAt: &claimExpiresAt}}); createErr != nil {
		t.Fatal(createErr)
	}
	service := NewBuildServiceFromConfig(repo, nil, logs.NewMemorySink(), BuildServiceConfig{
		ArtifactRepo: artifactRepo, VersionTagger: tagger, ExecutionJobRepo: jobs,
	})
	collectionErr := errors.New("artifact store unavailable")
	if collection != "existing" {
		workspaceRoot := t.TempDir()
		artifactDirectory := filepath.Join(workspaceRoot, build.ID, "dist")
		if mkdirErr := os.MkdirAll(artifactDirectory, 0o755); mkdirErr != nil {
			t.Fatal(mkdirErr)
		}
		if writeErr := os.WriteFile(filepath.Join(artifactDirectory, "extra.tgz"), []byte("artifact"), 0o644); writeErr != nil {
			t.Fatal(writeErr)
		}
		if collection == "fail" {
			service.SetArtifactPersistence(artifactRepo, testStoreResolver(&failingStore{err: collectionErr}), workspaceRoot)
		} else {
			artifactRepo.artifacts[build.ID] = nil
			events := []string{}
			service.SetArtifactPersistence(artifactRepo, testStoreResolver(&recordingStore{events: &events}), workspaceRoot)
		}
	}
	executionContext := StepExecutionContext{ExecutionRequest: runner.RunStepRequest{BuildID: build.ID, JobID: "execution-local", StepIndex: 0, StepName: "package", ClaimToken: claimToken, RequireAtomicExecutionCompletion: atomicCompletion}}
	manager := NewStepCompletionManager(service)
	logManager := NewExecutionLogManager(service, executionContext)
	result := runner.RunStepResult{Status: runner.RunStepStatusSuccess, StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC()}
	report, completeErr := manager.CompleteExecution(ctx, executionContext, result, logManager)
	if completeErr != nil || report.CompletionOutcome != repository.StepCompletionCompleted || !report.BuildBecameSuccessful {
		t.Fatalf("completion: %+v err=%v", report, completeErr)
	}
	if collection == "fail" {
		if !errors.Is(report.SideEffectErr, collectionErr) || tagger.calls != 0 {
			t.Fatalf("collection failure must prevent all tagging: report=%+v calls=%d", report, tagger.calls)
		}
		return
	}
	if report.SideEffectErr != nil {
		t.Fatal(report.SideEffectErr)
	}
	if tagger.calls != 2 || tagger.input.Kind != string(domain.VersionTagKindChannel) {
		t.Fatalf("want one version and one channel operation after collection, got calls=%d input=%+v", tagger.calls, tagger.input)
	}
	report, completeErr = manager.CompleteExecution(ctx, executionContext, result, logManager)
	if completeErr != nil || report.SideEffectErr != nil || report.CompletionOutcome != repository.StepCompletionDuplicateTerminal || report.BuildBecameSuccessful {
		t.Fatalf("duplicate completion: %+v err=%v", report, completeErr)
	}
	if tagger.calls != 3 || tagger.input.Kind != string(domain.VersionTagKindVersion) {
		t.Fatalf("duplicate must be version-only: calls=%d input=%+v", tagger.calls, tagger.input)
	}
}

type delayedLocalCompletionRepository struct {
	repository.BuildRepository
	afterCompletion func()
}

func (repo *delayedLocalCompletionRepository) CompleteStep(ctx context.Context, request repository.CompleteStepRequest) (repository.CompleteStepResult, error) {
	result, completeErr := repo.BuildRepository.CompleteStep(ctx, request)
	if completeErr == nil && result.Outcome == repository.StepCompletionCompleted {
		repo.afterCompletion()
	}
	return result, completeErr
}

func TestBuildService_DelayedLocalCompletionDoesNotReplayChannel(t *testing.T) {
	ctx := context.Background()
	claimToken := "claim-1"
	jobID := "job-1"
	pipelineYAML := "version: 1\nsteps:\n  - name: first\n    run: true\n  - name: last\n    run: true\nartifacts:\n  - path: dist/app.tgz\n    version:\n      template: '1.2.3'\n      channel: latest\n"
	build := domain.Build{ID: "build-local", JobID: &jobID, PipelineConfigYAML: &pipelineYAML}
	repo := memory.NewBuildRepository()
	steps := []domain.BuildStep{
		{ID: "step-first", BuildID: build.ID, StepIndex: 0, Name: "first", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken},
		{ID: "step-last", BuildID: build.ID, StepIndex: 1, Name: "last", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken},
	}
	if _, createErr := repo.CreateQueuedBuild(ctx, build, steps); createErr != nil {
		t.Fatal(createErr)
	}
	if _, startErr := repo.UpdateStatus(ctx, build.ID, domain.BuildStatusRunning, nil); startErr != nil {
		t.Fatal(startErr)
	}
	tagger := &fakeBuildVersionTagger{}
	config := BuildServiceConfig{
		ArtifactRepo:  &fakeArtifactRepository{artifacts: map[string][]domain.BuildArtifact{build.ID: {{ID: "artifact-1", BuildID: build.ID, LogicalPath: "dist/app.tgz"}}}},
		VersionTagger: tagger,
	}
	finalService := NewBuildServiceFromConfig(repo, nil, logs.NewMemorySink(), config)
	result := runner.RunStepResult{Status: runner.RunStepStatusSuccess, StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC()}
	delayedRepo := &delayedLocalCompletionRepository{BuildRepository: repo, afterCompletion: func() {
		executionContext := StepExecutionContext{ExecutionRequest: runner.RunStepRequest{BuildID: build.ID, StepIndex: 1, StepName: "last", ClaimToken: claimToken}}
		report, completeErr := NewStepCompletionManager(finalService).CompleteExecution(ctx, executionContext, result, NewExecutionLogManager(finalService, executionContext))
		if completeErr != nil || report.SideEffectErr != nil || !report.BuildBecameSuccessful || tagger.calls != 2 {
			t.Fatalf("final completion: %+v calls=%d err=%v", report, tagger.calls, completeErr)
		}
	}}
	delayedService := NewBuildServiceFromConfig(delayedRepo, nil, logs.NewMemorySink(), config)
	executionContext := StepExecutionContext{ExecutionRequest: runner.RunStepRequest{BuildID: build.ID, StepIndex: 0, StepName: "first", ClaimToken: claimToken}}
	report, completeErr := NewStepCompletionManager(delayedService).CompleteExecution(ctx, executionContext, result, NewExecutionLogManager(delayedService, executionContext))
	if completeErr != nil || report.SideEffectErr != nil || report.CompletionOutcome != repository.StepCompletionCompleted || report.BuildBecameSuccessful {
		t.Fatalf("delayed completion: %+v err=%v", report, completeErr)
	}
	if tagger.calls != 3 || tagger.input.Kind != string(domain.VersionTagKindVersion) {
		t.Fatalf("delayed nonfinal completion replayed channel: calls=%d input=%+v", tagger.calls, tagger.input)
	}
}

func TestBuildService_ReleaseFailureHookRunsAfterFailedStepCompletion(t *testing.T) {
	claimToken := "claim-1"
	repo := &fakeBuildRepository{
		build: domain.Build{ID: "build-1", Status: domain.BuildStatusRunning},
		steps: []domain.BuildStep{{ID: "step-1", BuildID: "build-1", StepIndex: 0, Name: "test", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken}},
	}
	snapshotter := &recordingReleaseSnapshotter{}
	service := NewBuildServiceFromConfig(repo, nil, logs.NewMemorySink(), BuildServiceConfig{ReleaseSnapshotter: snapshotter})

	_, completionErr := service.HandleStepResult(context.Background(), runner.RunStepRequest{BuildID: "build-1", StepIndex: 0, StepName: "test", ClaimToken: claimToken}, runner.RunStepResult{Status: runner.RunStepStatusFailed, ExitCode: 1, Stderr: "test failed", StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC()})
	if completionErr != nil {
		t.Fatalf("complete failed step: %v", completionErr)
	}
	if len(snapshotter.failedBuilds) != 1 || snapshotter.failedBuilds[0].Status != domain.BuildStatusFailed {
		t.Fatalf("release failure calls=%+v, want failed build", snapshotter.failedBuilds)
	}
}

func TestBuildService_ReleaseFailureHookRunsAfterAtomicCancel(t *testing.T) {
	base := &fakeBuildRepository{build: domain.Build{ID: "build-1", Status: domain.BuildStatusRunning}}
	repo := &fakeAtomicCancelBuildRepository{fakeBuildRepository: base}
	snapshotter := &recordingReleaseSnapshotter{}
	service := NewBuildServiceFromConfig(repo, nil, logs.NewMemorySink(), BuildServiceConfig{ReleaseSnapshotter: snapshotter})

	_, cancelErr := service.CancelBuild(context.Background(), "build-1")
	if cancelErr != nil {
		t.Fatalf("cancel build: %v", cancelErr)
	}
	if len(snapshotter.failedBuilds) != 1 || snapshotter.failedBuilds[0].Status != domain.BuildStatusCanceled {
		t.Fatalf("release failure calls=%+v, want canceled build", snapshotter.failedBuilds)
	}
}

type recordingReleaseSnapshotter struct {
	failedBuilds []domain.Build
}

func TestBuildService_ReconcileArtifactVersions(t *testing.T) {
	for _, status := range []domain.BuildStatus{domain.BuildStatusRunning, domain.BuildStatusFailed, domain.BuildStatusCanceled, domain.BuildStatusSuccess} {
		t.Run(string(status), func(t *testing.T) {
			jobID := "job-1"
			pipelineYAML := "version: 1\nsteps:\n  - name: package\n    run: true\nartifacts:\n  - path: dist/app.tgz\n    version:\n      template: '1.2.3'\n      channel: latest\n"
			build := domain.Build{ID: "build-1", JobID: &jobID, Status: status, PipelineConfigYAML: &pipelineYAML}
			tagger := &fakeBuildVersionTagger{}
			service := NewBuildServiceFromConfig(&fakeBuildRepository{build: build}, nil, nil, BuildServiceConfig{
				ArtifactRepo:  &fakeArtifactRepository{artifacts: map[string][]domain.BuildArtifact{build.ID: {{ID: "artifact-1", BuildID: build.ID, LogicalPath: "dist/app.tgz"}}}},
				VersionTagger: tagger,
			})
			if reconcileErr := service.ReconcileArtifactVersions(context.Background(), build.ID); reconcileErr != nil {
				t.Fatal(reconcileErr)
			}
			wantCalls := 0
			if status == domain.BuildStatusSuccess {
				wantCalls = 1
				if tagger.input.Kind != string(domain.VersionTagKindVersion) || tagger.input.Version != "1.2.3" {
					t.Fatalf("recovery moved configured channel: %+v", tagger.input)
				}
			}
			if tagger.calls != wantCalls {
				t.Fatalf("version calls=%d, want %d", tagger.calls, wantCalls)
			}
		})
	}
	service := NewBuildService(&fakeBuildRepository{getErr: repository.ErrBuildNotFound}, nil, nil)
	if reconcileErr := service.ReconcileArtifactVersions(context.Background(), "missing"); !errors.Is(reconcileErr, repository.ErrBuildNotFound) {
		t.Fatalf("build lookup failure not propagated: %v", reconcileErr)
	}
}

func (*recordingReleaseSnapshotter) EnsureSnapshot(context.Context, domain.Build) error {
	return nil
}

func (s *recordingReleaseSnapshotter) FailBuildRelease(_ context.Context, build domain.Build) error {
	s.failedBuilds = append(s.failedBuilds, build)
	return nil
}
