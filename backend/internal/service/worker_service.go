package service

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/runner"
)

type buildExecutionBoundary interface {
	ListBuilds(ctx context.Context) ([]domain.Build, error)
	GetBuildSteps(ctx context.Context, id string) ([]domain.BuildStep, error)
	ClaimPendingStep(ctx context.Context, buildID string, stepIndex int, claim repository.StepClaim) (domain.BuildStep, bool, error)
	ReclaimExpiredStep(ctx context.Context, buildID string, stepIndex int, reclaimBefore time.Time, claim repository.StepClaim) (domain.BuildStep, bool, error)
	QueueBuild(ctx context.Context, id string) (domain.Build, error)
	StartBuild(ctx context.Context, id string) (domain.Build, error)
	CompleteBuild(ctx context.Context, id string) (domain.Build, error)
	FailBuild(ctx context.Context, id string) (domain.Build, error)
	RunStep(ctx context.Context, request runner.RunStepRequest) (runner.RunStepResult, error)
}

type RunnableStep struct {
	BuildID        string
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
	BuildID string
	Step    domain.BuildStep
	Result  runner.RunStepResult
}

type WorkerService struct {
	builds        buildExecutionBoundary
	workerID      string
	leaseDuration time.Duration
	clock         func() time.Time
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
	builds, err := w.builds.ListBuilds(ctx)
	if err != nil {
		return RunnableStep{}, false, err
	}

	for _, build := range builds {
		if build.Status != domain.BuildStatusQueued && build.Status != domain.BuildStatusRunning && build.Status != domain.BuildStatusPending {
			continue
		}

		steps, err := w.builds.GetBuildSteps(ctx, build.ID)
		if err != nil {
			return RunnableStep{}, false, err
		}

		if len(steps) == 0 {
			if build.Status == domain.BuildStatusPending {
				_, err = w.builds.QueueBuild(ctx, build.ID)
				if err != nil {
					if !errors.Is(err, ErrInvalidBuildStatusTransition) {
						return RunnableStep{}, false, err
					}
					continue
				}

				steps, err = w.builds.GetBuildSteps(ctx, build.ID)
				if err != nil {
					return RunnableStep{}, false, err
				}
				if len(steps) == 0 {
					continue
				}
			} else {
				continue
			}
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

		if build.Status == domain.BuildStatusPending || build.Status == domain.BuildStatusQueued {
			if _, err := w.builds.StartBuild(ctx, build.ID); err != nil && !errors.Is(err, ErrInvalidBuildStatusTransition) {
				return RunnableStep{}, false, err
			}
		}

		return RunnableStep{
			BuildID:        build.ID,
			StepIndex:      claimedStep.StepIndex,
			StepName:       claimedStep.Name,
			WorkerID:       claim.WorkerID,
			ClaimToken:     claim.ClaimToken,
			Command:        defaultString(claimedStep.Command, "sh"),
			Args:           defaultArgs(claimedStep.Args),
			Env:            defaultEnv(claimedStep.Env),
			WorkingDir:     defaultString(claimedStep.WorkingDir, "."),
			TimeoutSeconds: maxInt(claimedStep.TimeoutSeconds, 0),
		}, true, nil
	}

	for _, build := range builds {
		if build.Status != domain.BuildStatusQueued && build.Status != domain.BuildStatusRunning && build.Status != domain.BuildStatusPending {
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
			continue
		}

		if build.Status == domain.BuildStatusPending || build.Status == domain.BuildStatusQueued {
			if _, err := w.builds.StartBuild(ctx, build.ID); err != nil && !errors.Is(err, ErrInvalidBuildStatusTransition) {
				return RunnableStep{}, false, err
			}
		}

		return RunnableStep{
			BuildID:        build.ID,
			StepIndex:      reclaimedStep.StepIndex,
			StepName:       reclaimedStep.Name,
			WorkerID:       claim.WorkerID,
			ClaimToken:     claim.ClaimToken,
			Command:        defaultString(reclaimedStep.Command, "sh"),
			Args:           defaultArgs(reclaimedStep.Args),
			Env:            defaultEnv(reclaimedStep.Env),
			WorkingDir:     defaultString(reclaimedStep.WorkingDir, "."),
			TimeoutSeconds: maxInt(reclaimedStep.TimeoutSeconds, 0),
		}, true, nil
	}

	return RunnableStep{}, false, nil
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

	result, err := w.builds.RunStep(ctx, runner.RunStepRequest{
		BuildID:        step.BuildID,
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
	report.Result = result

	completedAt := time.Now().UTC()
	report.Step.FinishedAt = &completedAt

	if err != nil {
		if errors.Is(err, ErrStaleStepClaim) {
			log.Printf("stale completion ignored: build_id=%s step=%s", step.BuildID, step.StepName)
			return report, nil
		}
		log.Printf("execution completed: build_id=%s step=%s status=%s exit_code=%d", step.BuildID, step.StepName, runner.RunStepStatusFailed, result.ExitCode)
		report.Step.Status = domain.BuildStepStatusFailed
		return report, err
	}

	log.Printf("execution completed: build_id=%s step=%s status=%s exit_code=%d", step.BuildID, step.StepName, result.Status, result.ExitCode)

	if result.Status == runner.RunStepStatusSuccess {
		report.Step.Status = domain.BuildStepStatusSuccess
		return report, nil
	}

	report.Step.Status = domain.BuildStepStatusFailed
	return report, nil
}
