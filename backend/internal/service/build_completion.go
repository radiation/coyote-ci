package service

import (
	"context"
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/runner"
)

func (s *BuildService) HandleStepResult(ctx context.Context, request runner.RunStepRequest, result runner.RunStepResult) (StepCompletionReport, error) {
	return s.handleStepResult(ctx, request, result, false)
}

func (s *BuildService) handleStepResult(ctx context.Context, request runner.RunStepRequest, result runner.RunStepResult, skipLegacyLogWrite bool) (StepCompletionReport, error) {
	stepStatus := domain.BuildStepStatusSuccess
	if result.Status == runner.RunStepStatusFailed {
		stepStatus = domain.BuildStepStatusFailed
	}

	var stepError *string
	if stepStatus == domain.BuildStepStatusFailed {
		message := strings.TrimSpace(result.Stderr)
		if message != "" {
			stepError = &message
		}
	}

	var stdout *string
	if result.Stdout != "" {
		stdoutValue := result.Stdout
		stdout = &stdoutValue
	}

	var stderr *string
	if result.Stderr != "" {
		stderrValue := result.Stderr
		stderr = &stderrValue
	}

	exitCode := result.ExitCode
	completionUpdate := repository.StepUpdate{
		Status:       stepStatus,
		ExitCode:     &exitCode,
		Stdout:       stdout,
		Stderr:       stderr,
		ErrorMessage: stepError,
		StartedAt:    &result.StartedAt,
		FinishedAt:   &result.FinishedAt,
	}

	claimToken := strings.TrimSpace(request.ClaimToken)
	if claimToken == "" {
		return StepCompletionReport{CompletionOutcome: repository.StepCompletionInvalidTransition}, nil
	}

	completionResult, err := s.buildRepo.CompleteStep(ctx, repository.CompleteStepRequest{
		BuildID:      request.BuildID,
		StepIndex:    request.StepIndex,
		ClaimToken:   claimToken,
		RequireClaim: true,
		Update:       completionUpdate,
	})
	if err != nil {
		return StepCompletionReport{CompletionOutcome: repository.StepCompletionInvalidTransition}, mapRepoErr(err)
	}

	if completionResult.Outcome != repository.StepCompletionCompleted {
		return StepCompletionReport{Step: completionResult.Step, CompletionOutcome: completionResult.Outcome}, nil
	}

	report := StepCompletionReport{Step: completionResult.Step, CompletionOutcome: repository.StepCompletionCompleted}
	if s.executionJobRepo != nil && strings.TrimSpace(request.JobID) != "" {
		var jobErr error
		if stepStatus == domain.BuildStepStatusSuccess {
			_, _, jobErr = s.executionJobRepo.CompleteJobSuccess(ctx, request.JobID, claimToken, result.FinishedAt, result.ExitCode, nil)
		} else {
			message := "step execution failed"
			if stepError != nil {
				message = *stepError
			}
			_, _, jobErr = s.executionJobRepo.CompleteJobFailure(ctx, request.JobID, claimToken, result.FinishedAt, message, &exitCode, nil)
		}
		if jobErr != nil {
			report.SideEffectErr = jobErr
		}
	}

	if skipLegacyLogWrite {
		return report, nil
	}
	if err := writeOutputLogs(ctx, s.logSink, request.BuildID, request.StepName, result.Stdout); err != nil {
		report.SideEffectErr = err
		return report, nil
	}
	if err := writeOutputLogs(ctx, s.logSink, request.BuildID, request.StepName, result.Stderr); err != nil {
		report.SideEffectErr = err
		return report, nil
	}

	return report, nil
}
