package execution

import (
	"context"
	"errors"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/logs"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/runner"
)

// StepCompletionReport captures lifecycle completion outcome and post-persist side-effect state.
// CompletionOutcome reflects only persisted lifecycle handling.
// SideEffectErr is set only when persistence completed and a non-lifecycle side effect failed.
type StepCompletionReport struct {
	Step              domain.BuildStep
	CompletionOutcome repository.StepCompletionOutcome
	SideEffectErr     error
}

type StepCompletionManagerDeps struct {
	HandleStepResult             func(context.Context, runner.RunStepRequest, runner.RunStepResult, bool) (StepCompletionReport, error)
	RunPostCompletionSideEffects func(context.Context, runner.RunStepRequest, logs.StepLogChunkAppender) error
}

type StepCompletionManager struct {
	deps StepCompletionManagerDeps
}

func NewStepCompletionManager(deps StepCompletionManagerDeps) *StepCompletionManager {
	return &StepCompletionManager{deps: deps}
}

func (m *StepCompletionManager) CompleteEarlyExit(ctx context.Context, executionContext StepExecutionContext, result runner.RunStepResult, logManager *ExecutionLogManager) (StepCompletionReport, error) {
	return m.complete(ctx, executionContext, result, false, nil, logManager)
}

func (m *StepCompletionManager) CompleteExecution(ctx context.Context, executionContext StepExecutionContext, result runner.RunStepResult, logManager *ExecutionLogManager) (StepCompletionReport, error) {
	return m.complete(ctx, executionContext, result, executionContext.HasChunkAppender, executionContext.ChunkAppender, logManager)
}

func (m *StepCompletionManager) complete(
	ctx context.Context,
	executionContext StepExecutionContext,
	result runner.RunStepResult,
	skipLegacyLogWrite bool,
	sideEffectAppender logs.StepLogChunkAppender,
	logManager *ExecutionLogManager,
) (StepCompletionReport, error) {
	report, completionErr := m.deps.HandleStepResult(ctx, executionContext.ExecutionRequest, result, skipLegacyLogWrite)
	if completionErr != nil {
		return report, completionErr
	}

	if sideEffectErr := m.deps.RunPostCompletionSideEffects(ctx, executionContext.ExecutionRequest, sideEffectAppender); sideEffectErr != nil {
		report.SideEffectErr = joinSideEffectErrors(report.SideEffectErr, sideEffectErr)
	}
	logManager.ApplyBufferedErrors(&report)
	return report, nil
}

func joinSideEffectErrors(existing error, additional error) error {
	if additional == nil {
		return existing
	}
	if existing == nil {
		return additional
	}
	return errors.Join(existing, additional)
}
