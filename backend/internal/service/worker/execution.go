package worker

import (
	"context"
	"errors"
	"log"
	"sync/atomic"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/runner"
	buildsvc "github.com/radiation/coyote-ci/backend/internal/service/build"
)

func (w *ExecutionWorkerService) ExecuteRunnableStep(ctx context.Context, step WorkerRunnableStep) (WorkerStepExecutionReport, error) {
	w.recordHeartbeat(ctx, true)

	report := WorkerStepExecutionReport{
		BuildID: step.BuildID,
		Step: domain.BuildStep{
			Name:   step.StepName,
			Status: domain.BuildStepStatusPending,
		},
	}

	build, buildErr := w.builds.GetBuild(ctx, step.BuildID)
	if buildErr == nil && domain.IsTerminalBuildStatus(build.Status) {
		finishedAt := time.Now().UTC()
		report.Step.Status = domain.BuildStepStatusCanceled
		report.Step.FinishedAt = &finishedAt
		log.Printf("skipping execution for terminal build: build_id=%s step=%s status=%s", step.BuildID, step.StepName, build.Status)
		return report, nil
	}
	if buildErr != nil && !errors.Is(buildErr, buildsvc.ErrBuildNotFound) {
		return report, buildErr
	}

	log.Printf("claimed runnable work: build_id=%s step=%s", step.BuildID, step.StepName)
	log.Printf("starting execution: build_id=%s step=%s", step.BuildID, step.StepName)

	startedAt := time.Now().UTC()
	report.Step.Status = domain.BuildStepStatusRunning
	report.Step.StartedAt = &startedAt

	heartbeatCtx, stopHeartbeat := context.WithCancel(ctx)
	heartbeatDone := make(chan struct{})
	heartbeatInterval := w.heartbeatIntervalForStep(step)
	go func() {
		defer close(heartbeatDone)
		ticker := time.NewTicker(heartbeatInterval)
		defer ticker.Stop()

		for {
			select {
			case <-heartbeatCtx.Done():
				return
			case <-ticker.C:
				w.recordHeartbeat(heartbeatCtx, true)
				cont, renewErr := w.renewStepLease(heartbeatCtx, step)
				if renewErr != nil {
					log.Printf("lease renewal error: build_id=%s step=%s err=%v", step.BuildID, step.StepName, renewErr)
					continue
				}
				if !cont {
					return
				}
			}
		}
	}()

	result, completionReport, err := w.builds.RunStep(ctx, runner.RunStepRequest{
		BuildID:        step.BuildID,
		JobID:          step.JobID,
		StepID:         step.StepID,
		StepIndex:      step.StepIndex,
		StepName:       step.StepName,
		WorkerID:       step.WorkerID,
		ClaimToken:     step.ClaimToken,
		Image:          step.Image,
		Command:        step.Command,
		Args:           step.Args,
		Env:            step.Env,
		WorkingDir:     step.WorkingDir,
		TimeoutSeconds: step.TimeoutSeconds,
	})
	stopHeartbeat()
	<-heartbeatDone
	report.Result = result

	completedAt := time.Now().UTC()
	report.Step.FinishedAt = &completedAt
	completionOutcome := completionReport.CompletionOutcome
	if completionReport.SideEffectErr != nil {
		log.Printf("post-persist side-effect failed: build_id=%s step=%s err=%v", step.BuildID, step.StepName, completionReport.SideEffectErr)
		sideEffectMessage := completionReport.SideEffectErr.Error()
		report.SideEffectError = &sideEffectMessage
	}

	if err != nil {
		log.Printf("execution completed: build_id=%s step=%s status=%s exit_code=%d", step.BuildID, step.StepName, runner.RunStepStatusFailed, result.ExitCode)
		report.Step.Status = domain.BuildStepStatusFailed
		return report, err
	}

	if completionOutcome == repository.StepCompletionStaleClaim {
		staleCount := atomic.AddInt64(&w.staleComplete, 1)
		log.Printf("stale completion ignored: build_id=%s step=%s stale_completion_count=%d", step.BuildID, step.StepName, staleCount)
		return report, nil
	}
	if completionOutcome == repository.StepCompletionDuplicateTerminal {
		log.Printf("duplicate terminal completion ignored: build_id=%s step=%s", step.BuildID, step.StepName)
		return report, nil
	}
	if completionOutcome == repository.StepCompletionInvalidTransition {
		log.Printf("invalid transition completion ignored: build_id=%s step=%s", step.BuildID, step.StepName)
		return report, nil
	}

	log.Printf("execution completed: build_id=%s step=%s status=%s exit_code=%d", step.BuildID, step.StepName, result.Status, result.ExitCode)

	if result.Status == runner.RunStepStatusSuccess {
		report.Step.Status = domain.BuildStepStatusSuccess
		return report, nil
	}

	report.Step.Status = domain.BuildStepStatusFailed
	return report, nil
}
