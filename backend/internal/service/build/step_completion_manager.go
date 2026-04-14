package build

import (
	"context"

	"github.com/radiation/coyote-ci/backend/internal/logs"
	"github.com/radiation/coyote-ci/backend/internal/runner"
)

type StepCompletionManager struct {
	service *BuildService
}

func NewStepCompletionManager(service *BuildService) *StepCompletionManager {
	return &StepCompletionManager{service: service}
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
	report, completionErr := m.service.handleStepResult(ctx, executionContext.ExecutionRequest, result, skipLegacyLogWrite)
	if completionErr != nil {
		return report, completionErr
	}

	if sideEffectErr := m.service.runPostCompletionSideEffects(ctx, executionContext.ExecutionRequest, sideEffectAppender); sideEffectErr != nil {
		report.SideEffectErr = joinSideEffectErrors(report.SideEffectErr, sideEffectErr)
	}
	logManager.ApplyBufferedErrors(&report)
	return report, nil
}
