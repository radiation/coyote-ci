package build

import (
	"context"
	"errors"
	"log"
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
	if stepStatus == domain.BuildStepStatusSuccess && strings.TrimSpace(request.JobID) != "" && (request.RequireAtomicExecutionCompletion || s.workspaceRevisionPublicationEnabled()) {
		atomicRepo, ok := s.executionJobRepo.(interface {
			CompleteSuccessfulStepAndJob(context.Context, repository.CompleteSuccessfulStepAndJobRequest) (repository.CompleteStepResult, domain.ExecutionJob, repository.StepCompletionOutcome, error)
		})
		if !ok {
			return StepCompletionReport{CompletionOutcome: repository.StepCompletionInvalidTransition}, errors.New("execution job repository does not support atomic successful completion")
		}
		completed, _, outcome, completeErr := atomicRepo.CompleteSuccessfulStepAndJob(ctx, repository.CompleteSuccessfulStepAndJobRequest{
			JobID: request.JobID, ClaimToken: claimToken, FinishedAt: result.FinishedAt, ExitCode: result.ExitCode,
			StepRequest: repository.CompleteStepRequest{BuildID: request.BuildID, StepIndex: request.StepIndex, ClaimToken: claimToken, RequireClaim: true, Update: completionUpdate},
		})
		if completeErr != nil {
			return StepCompletionReport{CompletionOutcome: repository.StepCompletionInvalidTransition}, mapRepoErr(completeErr)
		}
		report := StepCompletionReport{Step: completed.Step, CompletionOutcome: outcome}
		if s.buildNotifier != nil {
			build, buildErr := s.buildRepo.GetByID(ctx, request.BuildID)
			if buildErr != nil {
				log.Printf("WARNING: build notification skipped: build_id=%s reason=build_lookup_failed err=%v", request.BuildID, buildErr)
			} else {
				s.notifyTerminalBuild(ctx, build)
			}
		}
		if skipLegacyLogWrite {
			return report, nil
		}
		if writeErr := writeExecutionOutputLogs(ctx, s.logSink, request.BuildID, request.StepName, result.Stdout); writeErr != nil {
			report.SideEffectErr = writeErr
			return report, nil
		}
		if writeErr := writeExecutionOutputLogs(ctx, s.logSink, request.BuildID, request.StepName, result.Stderr); writeErr != nil {
			report.SideEffectErr = writeErr
		}
		return report, nil
	}

	if stepStatus == domain.BuildStepStatusFailed && strings.TrimSpace(request.JobID) != "" && request.RequireAtomicExecutionCompletion {
		atomicRepo, ok := s.executionJobRepo.(interface {
			CompleteFailedStepAndJob(context.Context, repository.CompleteFailedStepAndJobRequest) (repository.CompleteStepResult, domain.ExecutionJob, repository.StepCompletionOutcome, error)
		})
		if !ok {
			return StepCompletionReport{CompletionOutcome: repository.StepCompletionInvalidTransition}, errors.New("execution job repository does not support atomic failed completion")
		}
		message := "step execution failed"
		if stepError != nil {
			message = *stepError
		}
		completed, _, outcome, completeErr := atomicRepo.CompleteFailedStepAndJob(ctx, repository.CompleteFailedStepAndJobRequest{
			JobID: request.JobID, ClaimToken: claimToken, FinishedAt: result.FinishedAt, ErrorMessage: message, FailureKind: executionFailureKind(result), ExitCode: &exitCode,
			StepRequest: repository.CompleteStepRequest{BuildID: request.BuildID, StepIndex: request.StepIndex, ClaimToken: claimToken, RequireClaim: true, Update: completionUpdate},
		})
		if completeErr != nil {
			return StepCompletionReport{CompletionOutcome: repository.StepCompletionInvalidTransition}, mapRepoErr(completeErr)
		}
		report := StepCompletionReport{Step: completed.Step, CompletionOutcome: outcome}
		if s.buildNotifier != nil {
			build, buildErr := s.buildRepo.GetByID(ctx, request.BuildID)
			if buildErr != nil {
				log.Printf("WARNING: build notification skipped: build_id=%s reason=build_lookup_failed err=%v", request.BuildID, buildErr)
			} else {
				s.notifyTerminalBuild(ctx, build)
			}
		}
		if skipLegacyLogWrite {
			return report, nil
		}
		if writeErr := writeExecutionOutputLogs(ctx, s.logSink, request.BuildID, request.StepName, result.Stdout); writeErr != nil {
			report.SideEffectErr = writeErr
			return report, nil
		}
		if writeErr := writeExecutionOutputLogs(ctx, s.logSink, request.BuildID, request.StepName, result.Stderr); writeErr != nil {
			report.SideEffectErr = writeErr
		}
		return report, nil
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
			failureKind := executionFailureKind(result)
			_, _, jobErr = s.executionJobRepo.CompleteJobFailure(ctx, request.JobID, claimToken, result.FinishedAt, message, failureKind, &exitCode, nil)
		}
		if jobErr != nil {
			report.SideEffectErr = jobErr
		}
	}

	if s.buildNotifier != nil {
		build, buildErr := s.buildRepo.GetByID(ctx, request.BuildID)
		if buildErr != nil {
			log.Printf("WARNING: build notification skipped: build_id=%s reason=build_lookup_failed err=%v", request.BuildID, buildErr)
		} else {
			s.notifyTerminalBuild(ctx, build)
		}
	}

	if skipLegacyLogWrite {
		return report, nil
	}
	if err := writeExecutionOutputLogs(ctx, s.logSink, request.BuildID, request.StepName, result.Stdout); err != nil {
		report.SideEffectErr = err
		return report, nil
	}
	if err := writeExecutionOutputLogs(ctx, s.logSink, request.BuildID, request.StepName, result.Stderr); err != nil {
		report.SideEffectErr = err
		return report, nil
	}

	return report, nil
}

func executionFailureKind(result runner.RunStepResult) domain.ExecutionFailureKind {
	if strings.HasPrefix(result.Stderr, "workspace revision: ") {
		return domain.ExecutionFailureKindWorkspace
	}
	if result.TimedOut {
		return domain.ExecutionFailureKindTimeout
	}
	return domain.ExecutionFailureKindExecution
}
