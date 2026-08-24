package build

import (
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/runner"
)

func TestExecutionFailureKind(t *testing.T) {
	tests := []struct {
		name   string
		result runner.RunStepResult
		want   domain.ExecutionFailureKind
	}{
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
