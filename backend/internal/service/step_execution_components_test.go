package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	memoryrepo "github.com/radiation/coyote-ci/backend/internal/repository/memory"
	"github.com/radiation/coyote-ci/backend/internal/runner"
)

func TestStepExecutionContextBuilder_BindsPersistedJobAsAuthoritativePlan(t *testing.T) {
	claimToken := "claim-token"
	buildRepo := &fakeBuildRepository{
		build: defaultBuild("build-1"),
		steps: []domain.BuildStep{{
			ID:         "step-1",
			BuildID:    "build-1",
			StepIndex:  0,
			Name:       "verify",
			Status:     domain.BuildStepStatusRunning,
			ClaimToken: &claimToken,
		}},
	}

	execRepo := memoryrepo.NewExecutionJobRepository()
	timeout := 90
	now := time.Now().UTC()
	_, err := execRepo.CreateJobsForBuild(context.Background(), []domain.ExecutionJob{{
		ID:             "job-1",
		BuildID:        "build-1",
		StepID:         "step-1",
		Name:           "lint",
		StepIndex:      0,
		Status:         domain.ExecutionJobStatusRunning,
		Image:          "golang:1.24",
		WorkingDir:     "backend",
		Command:        []string{"sh", "-c", "go test ./..."},
		Environment:    map[string]string{"GOFLAGS": "-mod=readonly"},
		TimeoutSeconds: &timeout,
		CreatedAt:      now,
		ClaimToken:     &claimToken,
		Source: domain.SourceSnapshotRef{
			RepositoryURL: "https://github.com/acme/repo.git",
			CommitSHA:     "abc123",
		},
	}})
	if err != nil {
		t.Fatalf("failed to seed execution job: %v", err)
	}

	svc := NewBuildService(buildRepo, &fakeRunner{}, &fakeLogSink{})
	svc.SetExecutionJobRepository(execRepo)
	svc.SetDefaultExecutionImage("alpine:3.20")

	builder := NewStepExecutionContextBuilder(svc)
	executionContext, err := builder.Build(context.Background(), runner.RunStepRequest{
		BuildID:    "build-1",
		StepID:     "step-1",
		ClaimToken: claimToken,
		Command:    "echo",
		Args:       []string{"fallback"},
	})
	if err != nil {
		t.Fatalf("building execution context failed: %v", err)
	}

	if executionContext.ExecutionRequest.Command != "sh" {
		t.Fatalf("expected command from persisted job, got %q", executionContext.ExecutionRequest.Command)
	}
	if len(executionContext.ExecutionRequest.Args) != 2 || executionContext.ExecutionRequest.Args[1] != "go test ./..." {
		t.Fatalf("expected args from persisted job, got %#v", executionContext.ExecutionRequest.Args)
	}
	if executionContext.ExecutionRequest.WorkingDir != "backend" {
		t.Fatalf("expected working dir from persisted job, got %q", executionContext.ExecutionRequest.WorkingDir)
	}
	if executionContext.ExecutionImage != "golang:1.24" {
		t.Fatalf("expected image from persisted job, got %q", executionContext.ExecutionImage)
	}
	if executionContext.BuildSource.CommitSHA != "abc123" {
		t.Fatalf("expected source commit from persisted job, got %q", executionContext.BuildSource.CommitSHA)
	}
	if executionContext.StepNumber != 1 || executionContext.TotalSteps != 1 {
		t.Fatalf("expected normalized step numbering 1/1, got %d/%d", executionContext.StepNumber, executionContext.TotalSteps)
	}
}

func TestStepExecutionContextBuilder_FailsWhenAuthoritativeJobIDLookupFails(t *testing.T) {
	buildRepo := &fakeBuildRepository{
		build: defaultBuild("build-1"),
		steps: []domain.BuildStep{{
			ID:        "step-1",
			BuildID:   "build-1",
			StepIndex: 0,
			Name:      "verify",
			Status:    domain.BuildStepStatusPending,
		}},
	}

	execRepo := memoryrepo.NewExecutionJobRepository()
	svc := NewBuildService(buildRepo, &fakeRunner{}, &fakeLogSink{})
	svc.SetExecutionJobRepository(execRepo)

	builder := NewStepExecutionContextBuilder(svc)
	_, err := builder.Build(context.Background(), runner.RunStepRequest{
		BuildID: "build-1",
		JobID:   "missing-job-id",
		StepID:  "step-1",
	})
	if !errors.Is(err, ErrExecutionJobNotFound) {
		t.Fatalf("expected ErrExecutionJobNotFound, got %v", err)
	}
}

func TestWorkspacePreparer_ReturnsEarlyFailureResultOnPrepareError(t *testing.T) {
	runnerWithPrepareFailure := &fakeBuildScopedRunner{prepareErr: errors.New("docker create failed")}
	svc := NewBuildService(&fakeBuildRepository{}, runnerWithPrepareFailure, &fakeLogSink{})

	executionContext := StepExecutionContext{
		ExecutionImage: "golang:1.24",
		BuildSource:    resolvedBuildSourceSpec{},
		ExecutionRequest: runner.RunStepRequest{
			BuildID:    "build-1",
			StepID:     "step-1",
			StepIndex:  0,
			StepName:   "test",
			ClaimToken: "claim-token",
		},
		StepNumber: 1,
		TotalSteps: 1,
	}

	preparer := NewWorkspacePreparer(svc)
	logManager := NewExecutionLogManager(svc, executionContext)
	earlyResult, earlyErr, err := preparer.Prepare(context.Background(), executionContext, logManager)
	if err != nil {
		t.Fatalf("expected no hard prepare error, got %v", err)
	}
	if earlyErr == nil || earlyErr.Error() != "docker create failed" {
		t.Fatalf("expected prepare error to be returned as early error, got %v", earlyErr)
	}
	if earlyResult == nil {
		t.Fatal("expected early result")
	}
	if earlyResult.Status != runner.RunStepStatusFailed || earlyResult.ExitCode != -1 {
		t.Fatalf("expected failed early result, got status=%q exit=%d", earlyResult.Status, earlyResult.ExitCode)
	}
	if earlyResult.Stderr != "docker create failed" {
		t.Fatalf("expected normalized prepare failure reason, got %q", earlyResult.Stderr)
	}
}
