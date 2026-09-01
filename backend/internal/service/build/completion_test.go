package build

import (
	"context"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/runner"
)

func TestExecutionFailureKind(t *testing.T) {
	tests := []struct {
		name   string
		result runner.RunStepResult
		want   domain.ExecutionFailureKind
	}{
		{
			name: "workspace publication failure",
			result: runner.RunStepResult{
				ExitCode: -1,
				Stderr:   "workspace revision: store unavailable",
			},
			want: domain.ExecutionFailureKindWorkspace,
		},
		{
			name: "timeout",
			result: runner.RunStepResult{
				ExitCode: -1,
				TimedOut: true,
			},
			want: domain.ExecutionFailureKindTimeout,
		},
		{
			name: "timeout-like stderr without typed timeout",
			result: runner.RunStepResult{
				ExitCode: -1,
				Stderr:   "step timed out after 30s",
			},
			want: domain.ExecutionFailureKindExecution,
		},
		{
			name: "ordinary execution failure",
			result: runner.RunStepResult{
				ExitCode: 1,
				Stderr:   "command exited with code 1",
			},
			want: domain.ExecutionFailureKindExecution,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := executionFailureKind(test.result); got != test.want {
				t.Fatalf("executionFailureKind() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestBuildService_HandleStepResult_RequiresAtomicRepositoryForRevisionSuccess(t *testing.T) {
	claimToken := "claim-active"
	service := NewBuildService(&fakeBuildRepository{}, &fakeRunner{}, &fakeLogSink{})
	service.SetWorkspaceRevisionPublication(failingWorkspaceRevisionRepository{}, &recordingWorkspaceRevisionStore{})

	report, handleErr := service.HandleStepResult(context.Background(), runner.RunStepRequest{BuildID: "build-1", JobID: "job-1", StepIndex: 0, ClaimToken: claimToken}, runner.RunStepResult{Status: runner.RunStepStatusSuccess})
	if handleErr == nil || handleErr.Error() != "execution job repository does not support atomic successful completion" {
		t.Fatalf("expected atomic repository requirement error, got %v", handleErr)
	}
	if report.CompletionOutcome != repository.StepCompletionInvalidTransition {
		t.Fatalf("expected invalid transition report, got %q", report.CompletionOutcome)
	}
}
