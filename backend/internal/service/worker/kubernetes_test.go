package worker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/runner"
	buildsvc "github.com/radiation/coyote-ci/backend/internal/service/build"
)

type kubernetesBoundary struct {
	*fakeExecutionWorkerBoundary
	job           domain.ExecutionJob
	jobErr        error
	result        runner.RunStepResult
	resultErr     error
	sideEffectErr error
}

func (b *kubernetesBoundary) GetJobByID(context.Context, string) (domain.ExecutionJob, error) {
	return b.job, b.jobErr
}

func (b *kubernetesBoundary) HandleStepResult(_ context.Context, _ runner.RunStepRequest, result runner.RunStepResult) (buildsvc.StepCompletionReport, error) {
	b.result = result
	return buildsvc.StepCompletionReport{SideEffectErr: b.sideEffectErr}, b.resultErr
}

func TestValidateKubernetesRunnableStep(t *testing.T) {
	validSpec := `{"workspace_input":{"mode":"source"}}`
	validJob := domain.ExecutionJob{ID: "job-1", ResolvedSpecJSON: validSpec}
	validBuild := domain.Build{ID: "build-1"}
	validSteps := []domain.BuildStep{{ID: "step-1"}}

	tests := []struct {
		name     string
		step     WorkerRunnableStep
		job      domain.ExecutionJob
		jobErr   error
		build    domain.Build
		steps    []domain.BuildStep
		stepsErr error
		want     string
	}{
		{name: "valid source input", step: WorkerRunnableStep{JobID: "job-1", BuildID: "build-1"}, job: validJob, build: validBuild, steps: validSteps},
		{name: "legacy job", step: WorkerRunnableStep{}, want: "legacy execution jobs"},
		{name: "job lookup error", step: WorkerRunnableStep{JobID: "job-1", BuildID: "build-1"}, jobErr: errors.New("lookup failed"), want: "lookup failed"},
		{name: "invalid workspace plan", step: WorkerRunnableStep{JobID: "job-1", BuildID: "build-1"}, job: domain.ExecutionJob{ResolvedSpecJSON: "{"}, want: "valid workspace input plan"},
		{name: "predecessor workspace", step: WorkerRunnableStep{JobID: "job-1", BuildID: "build-1"}, job: domain.ExecutionJob{ResolvedSpecJSON: `{"workspace_input":{"mode":"predecessor"}}`}, want: "predecessor, fan-out, or fan-in"},
		{name: "repository checkout", step: WorkerRunnableStep{JobID: "job-1", BuildID: "build-1"}, job: domain.ExecutionJob{ResolvedSpecJSON: validSpec, Source: domain.SourceSnapshotRef{RepositoryURL: "https://example.test/repo.git"}}, want: "repository checkout"},
		{name: "step lookup error", step: WorkerRunnableStep{JobID: "job-1", BuildID: "build-1"}, job: validJob, stepsErr: errors.New("steps failed"), want: "steps failed"},
		{name: "multiple steps", step: WorkerRunnableStep{JobID: "job-1", BuildID: "build-1"}, job: validJob, steps: []domain.BuildStep{{}, {}}, want: "multi-step pipelines"},
		{name: "cache", step: WorkerRunnableStep{JobID: "job-1", BuildID: "build-1"}, job: validJob, steps: []domain.BuildStep{{Cache: &domain.StepCacheConfig{}}}, want: "cache restore or save"},
		{name: "step artifacts", step: WorkerRunnableStep{JobID: "job-1", BuildID: "build-1"}, job: validJob, steps: []domain.BuildStep{{ArtifactPaths: []string{"dist"}}}, want: "artifact collection"},
		{name: "artifacts", step: WorkerRunnableStep{JobID: "job-1", BuildID: "build-1"}, job: validJob, build: domain.Build{PipelineConfigYAML: stringPointer("version: 1\nsteps:\n  - name: test\n    run: echo ok\nartifacts:\n  paths: [dist]\n")}, steps: validSteps, want: "artifact collection"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			build := test.build
			if build.ID == "" {
				build.ID = "build-1"
			}
			boundary := &kubernetesBoundary{fakeExecutionWorkerBoundary: &fakeExecutionWorkerBoundary{listBuildsResp: []domain.Build{build}, stepsByBuildID: map[string][]domain.BuildStep{"build-1": test.steps}, getStepsErr: test.stepsErr}, job: test.job, jobErr: test.jobErr}
			service := NewExecutionWorkerService(boundary)
			err := service.ValidateKubernetesRunnableStep(context.Background(), test.step)
			if test.want == "" && err != nil {
				t.Fatalf("validate: %v", err)
			}
			if test.want != "" && (err == nil || !strings.Contains(err.Error(), test.want)) {
				t.Fatalf("expected %q, got %v", test.want, err)
			}
		})
	}
}

func TestIsKubernetesExecutionCapabilityError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "typed", err: &KubernetesExecutionCapabilityError{Feature: "cache restore or save"}, want: true},
		{name: "wrapped typed", err: fmt.Errorf("validation: %w", &KubernetesExecutionCapabilityError{Feature: "artifact collection"}), want: true},
		{name: "unavailable boundary", err: errKubernetesExecutionCapabilityUnavailable, want: true},
		{name: "operational error", err: errors.New("database unavailable")},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if got := IsKubernetesExecutionCapabilityError(testCase.err); got != testCase.want {
				t.Fatalf("IsKubernetesExecutionCapabilityError(%v) = %t, want %t", testCase.err, got, testCase.want)
			}
		})
	}
}

func TestKubernetesExecutionAdapter(t *testing.T) {
	boundary := &kubernetesBoundary{fakeExecutionWorkerBoundary: &fakeExecutionWorkerBoundary{}, job: domain.ExecutionJob{ID: "job-1"}}
	service := NewExecutionWorkerService(boundary)
	step := WorkerRunnableStep{BuildID: "build-1", JobID: "job-1", StepID: "step-1", StepIndex: 2, StepName: "test", WorkerID: "worker-1", ClaimToken: "claim-1"}
	if job, err := service.GetExecutionJob(context.Background(), step.JobID); err != nil || job.ID != step.JobID {
		t.Fatalf("get job=%#v err=%v", job, err)
	}
	if _, err := service.CompleteKubernetesRunnableStep(context.Background(), step, runner.RunStepResult{Status: runner.RunStepStatusSuccess}); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if boundary.result.Status != runner.RunStepStatusSuccess {
		t.Fatalf("result=%#v", boundary.result)
	}
}

func TestKubernetesExecutionAdapterReturnsCompletionSideEffectError(t *testing.T) {
	wantErr := errors.New("execution completion unavailable")
	boundary := &kubernetesBoundary{fakeExecutionWorkerBoundary: &fakeExecutionWorkerBoundary{}, sideEffectErr: wantErr}
	service := NewExecutionWorkerService(boundary)
	step := WorkerRunnableStep{BuildID: "build-1", JobID: "job-1", StepID: "step-1", StepIndex: 2, StepName: "test", WorkerID: "worker-1", ClaimToken: "claim-1"}

	if _, err := service.CompleteKubernetesRunnableStep(context.Background(), step, runner.RunStepResult{}); !errors.Is(err, wantErr) {
		t.Fatalf("expected side effect error %v, got %v", wantErr, err)
	}
}
