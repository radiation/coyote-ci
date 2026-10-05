package execution_test

import (
	"context"
	"testing"

	memoryrepo "github.com/radiation/coyote-ci/backend/internal/repository/memory"
	"github.com/radiation/coyote-ci/backend/internal/runner"
	buildsvc "github.com/radiation/coyote-ci/backend/internal/service/build"
	executionsvc "github.com/radiation/coyote-ci/backend/internal/service/execution"
)

func TestApplicationVersionLifecycle_PersistsBeforeExecutionContextBuild(t *testing.T) {
	ctx := context.Background()
	buildRepo := memoryrepo.NewBuildRepository()
	executionJobRepo := memoryrepo.NewExecutionJobRepository()
	buildService := buildsvc.NewBuildService(buildRepo, nil, nil)
	buildService.SetExecutionJobRepository(executionJobRepo)

	pipelineYAML := `
version: 1
pipeline:
  application_version:
    template: "0.0.{build_number}"
steps:
  - name: verify-version
    run: echo "$COYOTE_APPLICATION_VERSION"
    env:
      COYOTE_APPLICATION_VERSION: user-value
`
	build, createErr := buildService.CreateBuildFromPipeline(ctx, buildsvc.CreatePipelineBuildInput{
		ProjectID:    "project-1",
		PipelineYAML: pipelineYAML,
	})
	if createErr != nil {
		t.Fatalf("create pipeline build: %v", createErr)
	}

	jobs, jobsErr := executionJobRepo.GetJobsByBuildID(ctx, build.ID)
	if jobsErr != nil {
		t.Fatalf("get planned execution jobs: %v", jobsErr)
	}
	if len(jobs) != 1 {
		t.Fatalf("planned jobs=%d, want 1", len(jobs))
	}
	if jobs[0].Environment[executionsvc.ApplicationVersionEnvironmentKey] != "user-value" {
		t.Fatalf("planned job environment=%#v, want pipeline value", jobs[0].Environment)
	}

	prepared, prepareErr := buildService.PrepareBuildExecution(ctx, build.ID)
	if prepareErr != nil {
		t.Fatalf("prepare build execution: %v", prepareErr)
	}
	if prepared.ApplicationVersion == nil || *prepared.ApplicationVersion != "0.0.1" {
		t.Fatalf("prepared application version=%v, want 0.0.1", prepared.ApplicationVersion)
	}
	persisted, getErr := buildRepo.GetByID(ctx, build.ID)
	if getErr != nil {
		t.Fatalf("get persisted build: %v", getErr)
	}
	if persisted.ApplicationVersion == nil || *persisted.ApplicationVersion != "0.0.1" {
		t.Fatalf("persisted application version=%v, want 0.0.1", persisted.ApplicationVersion)
	}

	builder := executionsvc.NewStepExecutionContextBuilder(executionsvc.StepExecutionContextBuilderDeps{
		BuildRepo:        buildRepo,
		ExecutionJobRepo: executionJobRepo,
	})
	executionContext, contextErr := builder.Build(ctx, runner.RunStepRequest{BuildID: build.ID, JobID: jobs[0].ID})
	if contextErr != nil {
		t.Fatalf("build execution context: %v", contextErr)
	}
	if got := executionContext.ExecutionRequest.Env[executionsvc.ApplicationVersionEnvironmentKey]; got != "0.0.1" {
		t.Fatalf("execution request application version=%q, want 0.0.1", got)
	}
	if executionContext.Build.ApplicationVersion == nil || *executionContext.Build.ApplicationVersion != "0.0.1" {
		t.Fatalf("execution context build application version=%v, want 0.0.1", executionContext.Build.ApplicationVersion)
	}
}
