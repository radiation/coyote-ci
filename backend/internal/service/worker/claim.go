package worker

import (
	"context"
	"errors"
	"log"
	"sort"
	"sync/atomic"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	buildsvc "github.com/radiation/coyote-ci/backend/internal/service/build"
)

func (w *ExecutionWorkerService) ClaimRunnableStep(ctx context.Context) (WorkerRunnableStep, bool, error) {
	w.recordHeartbeat(ctx, false)

	builds, err := w.prepareQueuedBuilds(ctx)
	if err != nil {
		return WorkerRunnableStep{}, false, err
	}

	runnable, found, claimJobErr := w.claimRunnableStepFromJobs(ctx)
	if claimJobErr != nil {
		return WorkerRunnableStep{}, false, claimJobErr
	} else if found {
		return runnable, true, nil
	}

	for _, build := range builds {
		if domain.IsTerminalBuildStatus(build.Status) {
			continue
		}
		if build.Status != domain.BuildStatusRunning && build.Status != domain.BuildStatusPending {
			continue
		}

		if build.Status == domain.BuildStatusPending {
			queuedBuild, queueErr := w.builds.QueueBuild(ctx, build.ID)
			if queueErr != nil {
				if !errors.Is(queueErr, buildsvc.ErrInvalidBuildStatusTransition) {
					return WorkerRunnableStep{}, false, queueErr
				}
				continue
			}
			build = queuedBuild
		}

		steps, err := w.builds.GetBuildSteps(ctx, build.ID)
		if err != nil {
			return WorkerRunnableStep{}, false, err
		}

		if len(steps) == 0 {
			continue
		}

		nextStep, runnable := workerFirstRunnableStep(steps)
		if !runnable {
			continue
		}

		claim := w.newStepClaim()
		claimedStep, claimed, err := w.builds.ClaimPendingStep(ctx, build.ID, nextStep.StepIndex, claim)
		if err != nil {
			return WorkerRunnableStep{}, false, err
		}
		if !claimed {
			continue
		}
		claimCount := atomic.AddInt64(&w.claimsWon, 1)
		log.Printf("claim succeeded: build_id=%s step_index=%d worker_id=%s claim_count=%d", build.ID, claimedStep.StepIndex, claim.WorkerID, claimCount)

		runnableStep := WorkerRunnableStep{
			BuildID:        build.ID,
			JobID:          "",
			StepID:         claimedStep.ID,
			StepIndex:      claimedStep.StepIndex,
			StepName:       claimedStep.Name,
			WorkerID:       claim.WorkerID,
			ClaimToken:     claim.ClaimToken,
			Command:        workerDefaultString(claimedStep.Command, "sh"),
			Args:           workerDefaultArgs(claimedStep.Args),
			Env:            workerDefaultEnv(claimedStep.Env),
			WorkingDir:     workerDefaultString(claimedStep.WorkingDir, "."),
			TimeoutSeconds: workerMaxInt(claimedStep.TimeoutSeconds, 0),
		}

		return w.bindRunnableStepFromJob(ctx, runnableStep, claim), true, nil
	}

	for _, build := range builds {
		if domain.IsTerminalBuildStatus(build.Status) {
			continue
		}
		if build.Status != domain.BuildStatusRunning {
			continue
		}

		steps, err := w.builds.GetBuildSteps(ctx, build.ID)
		if err != nil {
			return WorkerRunnableStep{}, false, err
		}

		runningStep, reclaimable := workerFirstReclaimableRunningStep(steps, w.clock().UTC())
		if !reclaimable {
			continue
		}

		claim := w.newStepClaim()
		reclaimedStep, claimed, err := w.builds.ReclaimExpiredStep(ctx, build.ID, runningStep.StepIndex, claim.ClaimedAt, claim)
		if err != nil {
			return WorkerRunnableStep{}, false, err
		}
		if !claimed {
			missCount := atomic.AddInt64(&w.reclaimMisses, 1)
			log.Printf("reclaim miss: build_id=%s step_index=%d miss_count=%d", build.ID, runningStep.StepIndex, missCount)
			continue
		}
		reclaimCount := atomic.AddInt64(&w.reclaimsWon, 1)
		log.Printf("reclaim succeeded: build_id=%s step_index=%d worker_id=%s reclaim_count=%d", build.ID, reclaimedStep.StepIndex, claim.WorkerID, reclaimCount)

		runnableStep := WorkerRunnableStep{
			BuildID:        build.ID,
			JobID:          "",
			StepID:         reclaimedStep.ID,
			StepIndex:      reclaimedStep.StepIndex,
			StepName:       reclaimedStep.Name,
			WorkerID:       claim.WorkerID,
			ClaimToken:     claim.ClaimToken,
			Command:        workerDefaultString(reclaimedStep.Command, "sh"),
			Args:           workerDefaultArgs(reclaimedStep.Args),
			Env:            workerDefaultEnv(reclaimedStep.Env),
			WorkingDir:     workerDefaultString(reclaimedStep.WorkingDir, "."),
			TimeoutSeconds: workerMaxInt(reclaimedStep.TimeoutSeconds, 0),
		}

		return w.bindRunnableStepFromJob(ctx, runnableStep, claim), true, nil
	}

	if len(builds) > 0 {
		missCount := atomic.AddInt64(&w.reclaimMisses, 1)
		log.Printf("reclaim scan no expired running step: miss_count=%d", missCount)
	}

	return WorkerRunnableStep{}, false, nil
}

func (w *ExecutionWorkerService) claimRunnableStepFromJobs(ctx context.Context) (WorkerRunnableStep, bool, error) {
	claim := w.newStepClaim()
	job, claimed, err := w.builds.ClaimNextRunnableJob(ctx, claim)
	if err != nil {
		return WorkerRunnableStep{}, false, err
	}
	if !claimed {
		return WorkerRunnableStep{}, false, nil
	}

	if stepErr := w.mirrorJobClaimToStep(ctx, job, claim); stepErr != nil {
		return WorkerRunnableStep{}, false, stepErr
	}

	claimCount := atomic.AddInt64(&w.claimsWon, 1)
	log.Printf("job claim succeeded: job_id=%s build_id=%s step_index=%d worker_id=%s claim_count=%d", job.ID, job.BuildID, job.StepIndex, claim.WorkerID, claimCount)

	runnable := WorkerRunnableStep{
		BuildID:        job.BuildID,
		JobID:          job.ID,
		StepID:         job.StepID,
		StepIndex:      job.StepIndex,
		StepName:       job.Name,
		WorkerID:       claim.WorkerID,
		ClaimToken:     claim.ClaimToken,
		Image:          job.Image,
		Command:        workerCommandFromJob(job),
		Args:           workerArgsFromJob(job),
		Env:            workerEnvFromJob(job),
		WorkingDir:     workerDefaultString(job.WorkingDir, "."),
		TimeoutSeconds: workerTimeoutFromJob(job),
	}

	return runnable, true, nil
}

func (w *ExecutionWorkerService) prepareQueuedBuilds(ctx context.Context) ([]domain.Build, error) {
	builds, err := w.builds.ListBuilds(ctx)
	if err != nil {
		return nil, err
	}

	sort.SliceStable(builds, func(i, j int) bool {
		left := builds[i]
		right := builds[j]
		leftQueued := left.Status == domain.BuildStatusPending || left.Status == domain.BuildStatusQueued
		rightQueued := right.Status == domain.BuildStatusPending || right.Status == domain.BuildStatusQueued
		if leftQueued != rightQueued {
			return leftQueued
		}
		if leftQueued {
			if left.Priority != right.Priority {
				return left.Priority > right.Priority
			}
			leftQueuedAt := left.CreatedAt
			if left.QueuedAt != nil {
				leftQueuedAt = *left.QueuedAt
			}
			rightQueuedAt := right.CreatedAt
			if right.QueuedAt != nil {
				rightQueuedAt = *right.QueuedAt
			}
			if !leftQueuedAt.Equal(rightQueuedAt) {
				return leftQueuedAt.Before(rightQueuedAt)
			}
		}
		if !left.CreatedAt.Equal(right.CreatedAt) {
			return left.CreatedAt.Before(right.CreatedAt)
		}
		return left.ID < right.ID
	})

	for i, build := range builds {
		if domain.IsTerminalBuildStatus(build.Status) {
			continue
		}

		if build.Status == domain.BuildStatusPending {
			queuedBuild, queueErr := w.builds.QueueBuild(ctx, build.ID)
			if queueErr != nil {
				if errors.Is(queueErr, buildsvc.ErrInvalidBuildStatusTransition) {
					continue
				}
				return nil, queueErr
			}
			build = queuedBuild
			builds[i] = build
		}

		if build.Status != domain.BuildStatusQueued {
			continue
		}

		preparedBuild, prepErr := w.builds.PrepareBuildExecution(ctx, build.ID)
		if prepErr != nil {
			if errors.Is(prepErr, buildsvc.ErrInvalidBuildStatusTransition) {
				continue
			}
			return nil, prepErr
		}
		builds[i] = preparedBuild
	}

	return builds, nil
}

func (w *ExecutionWorkerService) mirrorJobClaimToStep(ctx context.Context, job domain.ExecutionJob, claim repository.StepClaim) error {
	if job.StepID == "" {
		return nil
	}

	if _, claimed, err := w.builds.ClaimPendingStep(ctx, job.BuildID, job.StepIndex, claim); err != nil {
		return err
	} else if claimed {
		return nil
	}

	if _, reclaimed, err := w.builds.ReclaimExpiredStep(ctx, job.BuildID, job.StepIndex, claim.ClaimedAt, claim); err != nil {
		return err
	} else if reclaimed {
		reclaimCount := atomic.AddInt64(&w.reclaimsWon, 1)
		log.Printf("step reclaim mirrored from job claim: build_id=%s step_index=%d reclaim_count=%d", job.BuildID, job.StepIndex, reclaimCount)
		return nil
	}

	return buildsvc.ErrInvalidBuildStepTransition
}

func (w *ExecutionWorkerService) bindRunnableStepFromJob(ctx context.Context, step WorkerRunnableStep, claim repository.StepClaim) WorkerRunnableStep {
	if step.StepID == "" {
		return step
	}

	job, claimed, err := w.builds.ClaimJobByStepID(ctx, step.StepID, claim)
	if err != nil || !claimed {
		return step
	}

	step.JobID = job.ID
	step.Image = job.Image
	step.Command = workerCommandFromJob(job)
	step.Args = workerArgsFromJob(job)
	step.Env = workerEnvFromJob(job)
	step.WorkingDir = workerDefaultString(job.WorkingDir, ".")
	if job.TimeoutSeconds != nil {
		step.TimeoutSeconds = workerMaxInt(*job.TimeoutSeconds, 0)
	}

	return step
}

func (w *ExecutionWorkerService) ensureBuildRunning(ctx context.Context, buildID string) error {
	build, err := w.builds.GetBuild(ctx, buildID)
	if err != nil {
		return err
	}

	if build.Status == domain.BuildStatusQueued {
		_, startErr := w.builds.StartBuild(ctx, buildID)
		if startErr != nil {
			if !errors.Is(startErr, buildsvc.ErrInvalidBuildStatusTransition) {
				return startErr
			}
			refreshed, refreshErr := w.builds.GetBuild(ctx, buildID)
			if refreshErr != nil {
				return refreshErr
			}
			if refreshed.Status != domain.BuildStatusRunning {
				return buildsvc.ErrInvalidBuildStatusTransition
			}
			return nil
		}

		log.Printf("build transition to running accepted: build_id=%s", buildID)
		return nil
	}

	if build.Status == domain.BuildStatusRunning {
		return nil
	}

	return buildsvc.ErrInvalidBuildStatusTransition
}
