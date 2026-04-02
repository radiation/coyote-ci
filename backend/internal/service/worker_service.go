package service

import (
	"context"
	"errors"
	"hash/fnv"
	"log"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/runner"
)

type buildExecutionBoundary interface {
	ClaimNextRunnableJob(ctx context.Context, claim repository.StepClaim) (domain.ExecutionJob, bool, error)
	ListBuilds(ctx context.Context) ([]domain.Build, error)
	GetBuildSteps(ctx context.Context, id string) ([]domain.BuildStep, error)
	GetJobByStepID(ctx context.Context, stepID string) (domain.ExecutionJob, error)
	ClaimJobByStepID(ctx context.Context, stepID string, claim repository.StepClaim) (domain.ExecutionJob, bool, error)
	RenewJobLease(ctx context.Context, jobID string, claimToken string, leaseExpiresAt time.Time) (domain.ExecutionJob, bool, error)
	ClaimPendingStep(ctx context.Context, buildID string, stepIndex int, claim repository.StepClaim) (domain.BuildStep, bool, error)
	ReclaimExpiredStep(ctx context.Context, buildID string, stepIndex int, reclaimBefore time.Time, claim repository.StepClaim) (domain.BuildStep, bool, error)
	RenewStepLease(ctx context.Context, buildID string, stepIndex int, claimToken string, leaseExpiresAt time.Time) (domain.BuildStep, bool, error)
	QueueBuild(ctx context.Context, id string) (domain.Build, error)
	StartBuild(ctx context.Context, id string) (domain.Build, error)
	CompleteBuild(ctx context.Context, id string) (domain.Build, error)
	FailBuild(ctx context.Context, id string) (domain.Build, error)
	RunStep(ctx context.Context, request runner.RunStepRequest) (runner.RunStepResult, StepCompletionReport, error)
}

type RunnableStep struct {
	BuildID        string
	JobID          string
	StepID         string
	StepIndex      int
	StepName       string
	WorkerID       string
	ClaimToken     string
	Command        string
	Args           []string
	Env            map[string]string
	WorkingDir     string
	TimeoutSeconds int
}

type StepExecutionReport struct {
	BuildID         string
	Step            domain.BuildStep
	Result          runner.RunStepResult
	SideEffectError *string
}

type WorkerRecoveryStats struct {
	ClaimsWon     int64 `json:"claims_won"`
	ReclaimsWon   int64 `json:"reclaims_won"`
	RenewalsWon   int64 `json:"renewals_won"`
	RenewalsStale int64 `json:"renewals_stale"`
	StaleComplete int64 `json:"stale_completion_rejected"`
	ReclaimMisses int64 `json:"reclaim_misses"`
}

type WorkerService struct {
	builds        buildExecutionBoundary
	workerID      string
	leaseDuration time.Duration
	clock         func() time.Time
	claimsWon     int64
	reclaimsWon   int64
	renewalsWon   int64
	renewalsStale int64
	staleComplete int64
	reclaimMisses int64
}

func NewWorkerService(builds buildExecutionBoundary) *WorkerService {
	return NewWorkerServiceWithLease(builds, "", 45*time.Second)
}

func NewWorkerServiceWithLease(builds buildExecutionBoundary, workerID string, leaseDuration time.Duration) *WorkerService {
	resolvedWorkerID := strings.TrimSpace(workerID)
	if resolvedWorkerID == "" {
		resolvedWorkerID = uuid.NewString()
	}
	if leaseDuration <= 0 {
		leaseDuration = 45 * time.Second
	}

	return &WorkerService{
		builds:        builds,
		workerID:      resolvedWorkerID,
		leaseDuration: leaseDuration,
		clock:         time.Now,
	}
}

func (w *WorkerService) ClaimRunnableStep(ctx context.Context) (RunnableStep, bool, error) {
	if runnable, found, err := w.claimRunnableStepFromJobs(ctx); err != nil {
		return RunnableStep{}, false, err
	} else if found {
		return runnable, true, nil
	}

	// Transitional fallback for builds without durable jobs.
	builds, err := w.builds.ListBuilds(ctx)
	if err != nil {
		return RunnableStep{}, false, err
	}

	for _, build := range builds {
		if domain.IsTerminalBuildStatus(build.Status) {
			continue
		}
		if build.Status != domain.BuildStatusQueued && build.Status != domain.BuildStatusRunning && build.Status != domain.BuildStatusPending {
			continue
		}

		if build.Status == domain.BuildStatusPending {
			queuedBuild, queueErr := w.builds.QueueBuild(ctx, build.ID)
			if queueErr != nil {
				if !errors.Is(queueErr, ErrInvalidBuildStatusTransition) {
					return RunnableStep{}, false, queueErr
				}
				continue
			}
			build = queuedBuild
		}

		steps, err := w.builds.GetBuildSteps(ctx, build.ID)
		if err != nil {
			return RunnableStep{}, false, err
		}

		if len(steps) == 0 {
			continue
		}

		nextStep, runnable := firstRunnableStep(steps)
		if !runnable {
			continue
		}

		claim := w.newStepClaim()
		claimedStep, claimed, err := w.builds.ClaimPendingStep(ctx, build.ID, nextStep.StepIndex, claim)
		if err != nil {
			return RunnableStep{}, false, err
		}
		if !claimed {
			continue
		}
		claimCount := atomic.AddInt64(&w.claimsWon, 1)
		log.Printf("claim succeeded: build_id=%s step_index=%d worker_id=%s claim_count=%d", build.ID, claimedStep.StepIndex, claim.WorkerID, claimCount)

		if build.Status == domain.BuildStatusQueued {
			if _, err := w.builds.StartBuild(ctx, build.ID); err != nil && !errors.Is(err, ErrInvalidBuildStatusTransition) {
				return RunnableStep{}, false, err
			}
		}

		runnableStep := RunnableStep{
			BuildID:        build.ID,
			JobID:          "",
			StepID:         claimedStep.ID,
			StepIndex:      claimedStep.StepIndex,
			StepName:       claimedStep.Name,
			WorkerID:       claim.WorkerID,
			ClaimToken:     claim.ClaimToken,
			Command:        defaultString(claimedStep.Command, "sh"),
			Args:           defaultArgs(claimedStep.Args),
			Env:            defaultEnv(claimedStep.Env),
			WorkingDir:     defaultString(claimedStep.WorkingDir, "."),
			TimeoutSeconds: maxInt(claimedStep.TimeoutSeconds, 0),
		}

		return w.bindRunnableStepFromJob(ctx, runnableStep, claim), true, nil
	}

	for _, build := range builds {
		if domain.IsTerminalBuildStatus(build.Status) {
			continue
		}
		if build.Status != domain.BuildStatusQueued && build.Status != domain.BuildStatusRunning {
			continue
		}

		steps, err := w.builds.GetBuildSteps(ctx, build.ID)
		if err != nil {
			return RunnableStep{}, false, err
		}

		runningStep, reclaimable := firstReclaimableRunningStep(steps, w.clock().UTC())
		if !reclaimable {
			continue
		}

		claim := w.newStepClaim()
		reclaimedStep, claimed, err := w.builds.ReclaimExpiredStep(ctx, build.ID, runningStep.StepIndex, claim.ClaimedAt, claim)
		if err != nil {
			return RunnableStep{}, false, err
		}
		if !claimed {
			missCount := atomic.AddInt64(&w.reclaimMisses, 1)
			log.Printf("reclaim miss: build_id=%s step_index=%d miss_count=%d", build.ID, runningStep.StepIndex, missCount)
			continue
		}
		reclaimCount := atomic.AddInt64(&w.reclaimsWon, 1)
		log.Printf("reclaim succeeded: build_id=%s step_index=%d worker_id=%s reclaim_count=%d", build.ID, reclaimedStep.StepIndex, claim.WorkerID, reclaimCount)

		if build.Status == domain.BuildStatusQueued {
			if _, err := w.builds.StartBuild(ctx, build.ID); err != nil && !errors.Is(err, ErrInvalidBuildStatusTransition) {
				return RunnableStep{}, false, err
			}
		}

		runnableStep := RunnableStep{
			BuildID:        build.ID,
			JobID:          "",
			StepID:         reclaimedStep.ID,
			StepIndex:      reclaimedStep.StepIndex,
			StepName:       reclaimedStep.Name,
			WorkerID:       claim.WorkerID,
			ClaimToken:     claim.ClaimToken,
			Command:        defaultString(reclaimedStep.Command, "sh"),
			Args:           defaultArgs(reclaimedStep.Args),
			Env:            defaultEnv(reclaimedStep.Env),
			WorkingDir:     defaultString(reclaimedStep.WorkingDir, "."),
			TimeoutSeconds: maxInt(reclaimedStep.TimeoutSeconds, 0),
		}

		return w.bindRunnableStepFromJob(ctx, runnableStep, claim), true, nil
	}

	if len(builds) > 0 {
		missCount := atomic.AddInt64(&w.reclaimMisses, 1)
		log.Printf("reclaim scan no expired running step: miss_count=%d", missCount)
	}

	return RunnableStep{}, false, nil
}

func (w *WorkerService) claimRunnableStepFromJobs(ctx context.Context) (RunnableStep, bool, error) {
	claim := w.newStepClaim()
	job, claimed, err := w.builds.ClaimNextRunnableJob(ctx, claim)
	if err != nil {
		return RunnableStep{}, false, err
	}
	if !claimed {
		return RunnableStep{}, false, nil
	}

	if stepErr := w.mirrorJobClaimToStep(ctx, job, claim); stepErr != nil {
		return RunnableStep{}, false, stepErr
	}

	if _, startErr := w.builds.StartBuild(ctx, job.BuildID); startErr != nil && !errors.Is(startErr, ErrInvalidBuildStatusTransition) {
		return RunnableStep{}, false, startErr
	}

	claimCount := atomic.AddInt64(&w.claimsWon, 1)
	log.Printf("job claim succeeded: job_id=%s build_id=%s step_index=%d worker_id=%s claim_count=%d", job.ID, job.BuildID, job.StepIndex, claim.WorkerID, claimCount)

	runnable := RunnableStep{
		BuildID:        job.BuildID,
		JobID:          job.ID,
		StepID:         job.StepID,
		StepIndex:      job.StepIndex,
		StepName:       job.Name,
		WorkerID:       claim.WorkerID,
		ClaimToken:     claim.ClaimToken,
		Command:        commandFromJob(job),
		Args:           argsFromJob(job),
		Env:            envFromJob(job),
		WorkingDir:     defaultString(job.WorkingDir, "."),
		TimeoutSeconds: timeoutFromJob(job),
	}

	return runnable, true, nil
}

func (w *WorkerService) mirrorJobClaimToStep(ctx context.Context, job domain.ExecutionJob, claim repository.StepClaim) error {
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

	return ErrInvalidBuildStepTransition
}

func timeoutFromJob(job domain.ExecutionJob) int {
	if job.TimeoutSeconds == nil {
		return 0
	}
	return maxInt(*job.TimeoutSeconds, 0)
}

func (w *WorkerService) heartbeatInterval() time.Duration {
	interval := w.leaseDuration / 3
	if interval <= 0 {
		return time.Second
	}
	return interval
}

func (w *WorkerService) heartbeatIntervalForStep(step RunnableStep) time.Duration {
	base := w.heartbeatInterval()
	window := base / 5
	if window <= 0 {
		return base
	}

	h := fnv.New32a()
	_, _ = h.Write([]byte(step.WorkerID))
	_, _ = h.Write([]byte(step.ClaimToken))

	spread := int64((2 * window) + 1)
	offset := time.Duration(int64(h.Sum32())%spread - int64(window))
	interval := base + offset

	minInterval := 100 * time.Millisecond
	if interval < minInterval {
		interval = minInterval
	}

	maxInterval := w.leaseDuration - (w.leaseDuration / 10)
	if maxInterval > minInterval && interval > maxInterval {
		interval = maxInterval
	}

	return interval
}

func (w *WorkerService) RecoveryStats() WorkerRecoveryStats {
	return WorkerRecoveryStats{
		ClaimsWon:     atomic.LoadInt64(&w.claimsWon),
		ReclaimsWon:   atomic.LoadInt64(&w.reclaimsWon),
		RenewalsWon:   atomic.LoadInt64(&w.renewalsWon),
		RenewalsStale: atomic.LoadInt64(&w.renewalsStale),
		StaleComplete: atomic.LoadInt64(&w.staleComplete),
		ReclaimMisses: atomic.LoadInt64(&w.reclaimMisses),
	}
}

func (w *WorkerService) renewStepLease(ctx context.Context, step RunnableStep) (bool, error) {
	leaseExpiresAt := w.clock().UTC().Add(w.leaseDuration)
	if step.JobID != "" {
		_, renewed, renewErr := w.builds.RenewJobLease(ctx, step.JobID, step.ClaimToken, leaseExpiresAt)
		if renewErr != nil {
			if errors.Is(renewErr, ErrStaleStepClaim) {
				staleCount := atomic.AddInt64(&w.renewalsStale, 1)
				log.Printf("job lease renewal rejected as stale: job_id=%s build_id=%s step=%s stale_count=%d", step.JobID, step.BuildID, step.StepName, staleCount)
				return false, nil
			}
			return false, renewErr
		}
		if !renewed {
			staleCount := atomic.AddInt64(&w.renewalsStale, 1)
			log.Printf("job lease renewal rejected: job_id=%s build_id=%s step=%s stale_count=%d", step.JobID, step.BuildID, step.StepName, staleCount)
			return false, nil
		}
	}

	_, renewedStep, stepErr := w.builds.RenewStepLease(ctx, step.BuildID, step.StepIndex, step.ClaimToken, leaseExpiresAt)
	if stepErr != nil {
		if errors.Is(stepErr, ErrStaleStepClaim) {
			staleCount := atomic.AddInt64(&w.renewalsStale, 1)
			log.Printf("step lease renewal rejected as stale: build_id=%s step=%s stale_count=%d", step.BuildID, step.StepName, staleCount)
			return false, nil
		}
		return false, stepErr
	}
	if !renewedStep {
		staleCount := atomic.AddInt64(&w.renewalsStale, 1)
		log.Printf("step lease renewal rejected: build_id=%s step=%s stale_count=%d", step.BuildID, step.StepName, staleCount)
		return false, nil
	}

	renewCount := atomic.AddInt64(&w.renewalsWon, 1)
	log.Printf("lease renewal succeeded: build_id=%s step=%s renewal_count=%d", step.BuildID, step.StepName, renewCount)

	return true, nil
}

func (w *WorkerService) newStepClaim() repository.StepClaim {
	now := w.clock().UTC()
	return repository.StepClaim{
		WorkerID:       w.workerID,
		ClaimToken:     uuid.NewString(),
		ClaimedAt:      now,
		LeaseExpiresAt: now.Add(w.leaseDuration),
	}
}

func firstRunnableStep(steps []domain.BuildStep) (domain.BuildStep, bool) {
	allPreviousSucceeded := true

	for _, step := range steps {
		switch step.Status {
		case domain.BuildStepStatusSuccess:
			continue
		case domain.BuildStepStatusPending:
			if !allPreviousSucceeded {
				return domain.BuildStep{}, false
			}
			return step, true
		case domain.BuildStepStatusRunning, domain.BuildStepStatusFailed:
			allPreviousSucceeded = false
		default:
			allPreviousSucceeded = false
		}
	}

	return domain.BuildStep{}, false
}

func firstReclaimableRunningStep(steps []domain.BuildStep, now time.Time) (domain.BuildStep, bool) {
	for _, step := range steps {
		if step.Status == domain.BuildStepStatusSuccess {
			continue
		}

		if step.Status != domain.BuildStepStatusRunning {
			return domain.BuildStep{}, false
		}
		if step.LeaseExpiresAt == nil || step.LeaseExpiresAt.After(now) {
			return domain.BuildStep{}, false
		}

		return step, true
	}

	return domain.BuildStep{}, false
}

func defaultString(value string, fallback string) string {
	if value == "" {
		return fallback
	}

	return value
}

func defaultArgs(args []string) []string {
	if len(args) == 0 {
		return []string{"-c", "echo coyote-ci worker default step && exit 0"}
	}

	return args
}

func defaultEnv(env map[string]string) map[string]string {
	if env == nil {
		return map[string]string{}
	}

	return env
}

func maxInt(value int, minimum int) int {
	if value < minimum {
		return minimum
	}

	return value
}

func (w *WorkerService) ExecuteRunnableStep(ctx context.Context, step RunnableStep) (StepExecutionReport, error) {
	log.Printf("claimed runnable work: build_id=%s step=%s", step.BuildID, step.StepName)
	log.Printf("starting execution: build_id=%s step=%s", step.BuildID, step.StepName)

	report := StepExecutionReport{
		BuildID: step.BuildID,
		Step: domain.BuildStep{
			Name:   step.StepName,
			Status: domain.BuildStepStatusPending,
		},
	}

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

func (w *WorkerService) bindRunnableStepFromJob(ctx context.Context, step RunnableStep, claim repository.StepClaim) RunnableStep {
	if step.StepID == "" {
		return step
	}

	job, err := w.builds.GetJobByStepID(ctx, step.StepID)
	if err != nil {
		return step
	}

	step.JobID = job.ID
	step.Command = commandFromJob(job)
	step.Args = argsFromJob(job)
	step.Env = envFromJob(job)
	step.WorkingDir = defaultString(job.WorkingDir, ".")
	if job.TimeoutSeconds != nil {
		step.TimeoutSeconds = maxInt(*job.TimeoutSeconds, 0)
	}

	if _, claimed, claimErr := w.builds.ClaimJobByStepID(ctx, step.StepID, claim); claimErr == nil && !claimed {
		return step
	}

	return step
}

func commandFromJob(job domain.ExecutionJob) string {
	if len(job.Command) > 0 {
		return defaultString(job.Command[0], "sh")
	}
	return "sh"
}

func argsFromJob(job domain.ExecutionJob) []string {
	if len(job.Command) <= 1 {
		return defaultArgs(nil)
	}
	args := make([]string, len(job.Command)-1)
	copy(args, job.Command[1:])
	return args
}

func envFromJob(job domain.ExecutionJob) map[string]string {
	if job.Environment == nil {
		return map[string]string{}
	}
	env := make(map[string]string, len(job.Environment))
	for key, value := range job.Environment {
		env[key] = value
	}
	return env
}
