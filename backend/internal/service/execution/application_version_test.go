package execution

import (
	"context"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	memoryrepo "github.com/radiation/coyote-ci/backend/internal/repository/memory"
	"github.com/radiation/coyote-ci/backend/internal/runner"
)

func TestStepExecutionContextBuilder_ApplicationVersionOverridesUserEnvironment(t *testing.T) {
	ctx := context.Background()
	buildRepo := memoryrepo.NewBuildRepository()
	version := "1.2.3"
	build, createErr := buildRepo.CreateQueuedBuild(ctx, domain.Build{
		ID:                 "build-versioned",
		ApplicationVersion: &version,
	}, []domain.BuildStep{{ID: "step-versioned", StepIndex: 0, Name: "test"}})
	if createErr != nil {
		t.Fatalf("create queued build: %v", createErr)
	}
	executionJobRepo := memoryrepo.NewExecutionJobRepository()
	_, createJobsErr := executionJobRepo.CreateJobsForBuild(ctx, []domain.ExecutionJob{{
		ID:          "job-versioned",
		BuildID:     build.ID,
		StepID:      "step-versioned",
		Name:        "test",
		StepIndex:   0,
		Status:      domain.ExecutionJobStatusQueued,
		Environment: map[string]string{ApplicationVersionEnvironmentKey: "user-value"},
	}})
	if createJobsErr != nil {
		t.Fatalf("create execution job: %v", createJobsErr)
	}
	builder := NewStepExecutionContextBuilder(StepExecutionContextBuilderDeps{
		BuildRepo:        buildRepo,
		ExecutionJobRepo: executionJobRepo,
		LogSink:          &recordingLogSink{},
	})
	executionContext, buildErr := builder.Build(ctx, runner.RunStepRequest{BuildID: build.ID, JobID: "job-versioned"})
	if buildErr != nil {
		t.Fatalf("build execution context: %v", buildErr)
	}
	if got := executionContext.ExecutionRequest.Env[ApplicationVersionEnvironmentKey]; got != version {
		t.Fatalf("application version environment=%q, want %q", got, version)
	}
}
