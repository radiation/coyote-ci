package build

import (
	"context"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/runner"
)

type StepRunOutcome struct {
	Result       runner.RunStepResult
	ExecutionErr error
}

type StepRunner struct {
	runner runner.Runner
}

func NewStepRunner(stepRunner runner.Runner) *StepRunner {
	return &StepRunner{runner: stepRunner}
}

func (r *StepRunner) Run(ctx context.Context, executionContext StepExecutionContext, logManager *ExecutionLogManager) StepRunOutcome {
	request := executionContext.ExecutionRequest
	result, runErr := r.execute(ctx, request, logManager)
	if runErr != nil {
		now := time.Now().UTC()
		return StepRunOutcome{
			Result: runner.RunStepResult{
				Status:     runner.RunStepStatusFailed,
				ExitCode:   -1,
				Stderr:     runErr.Error(),
				StartedAt:  now,
				FinishedAt: now,
			},
			ExecutionErr: runErr,
		}
	}
	return StepRunOutcome{Result: result}
}

func (r *StepRunner) execute(ctx context.Context, request runner.RunStepRequest, logManager *ExecutionLogManager) (runner.RunStepResult, error) {
	if streamingRunner, ok := r.runner.(runner.StreamingRunner); ok {
		return streamingRunner.RunStepStream(ctx, request, func(chunk runner.StepOutputChunk) error {
			return logManager.PersistRunnerChunk(ctx, chunk)
		})
	}

	result, err := r.runner.RunStep(ctx, request)
	if err != nil {
		return runner.RunStepResult{}, err
	}
	logManager.BackfillNonStreamingOutput(ctx, result)
	return result, nil
}
