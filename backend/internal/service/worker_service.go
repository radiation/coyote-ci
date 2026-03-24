package service

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/pkg/contracts"
)

type buildExecutionBoundary interface {
	ListBuilds(ctx context.Context) ([]domain.Build, error)
	GetBuildSteps(ctx context.Context, id string) ([]contracts.BuildStep, error)
	ClaimStepIfPending(ctx context.Context, buildID string, stepIndex int, workerID *string, startedAt time.Time) (contracts.BuildStep, bool, error)
	QueueBuild(ctx context.Context, id string) (domain.Build, error)
	StartBuild(ctx context.Context, id string) (domain.Build, error)
	CompleteBuild(ctx context.Context, id string) (domain.Build, error)
	FailBuild(ctx context.Context, id string) (domain.Build, error)
	RunStep(ctx context.Context, request contracts.RunStepRequest) (contracts.RunStepResult, error)
}

type RunnableStep struct {
	BuildID        string
	StepIndex      int
	StepName       string
	Command        string
	Args           []string
	Env            map[string]string
	WorkingDir     string
	TimeoutSeconds int
}

type StepExecutionReport struct {
	BuildID string
	Step    contracts.BuildStep
	Result  contracts.RunStepResult
}

type WorkerService struct {
	builds buildExecutionBoundary
}

func NewWorkerService(builds buildExecutionBoundary) *WorkerService {
	return &WorkerService{builds: builds}
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

		startedAt := time.Now().UTC()
		claimedStep, claimed, err := w.builds.ClaimStepIfPending(ctx, build.ID, nextStep.StepIndex, nil, startedAt)
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
			Command:        defaultString(claimedStep.Command, "sh"),
			Args:           defaultArgs(claimedStep.Args),
			Env:            defaultEnv(claimedStep.Env),
			WorkingDir:     defaultString(claimedStep.WorkingDir, "."),
			TimeoutSeconds: maxInt(claimedStep.TimeoutSeconds, 0),
		}, true, nil
	}

	return RunnableStep{}, false, nil
}

func firstRunnableStep(steps []contracts.BuildStep) (contracts.BuildStep, bool) {
	allPreviousSucceeded := true

	for _, step := range steps {
		switch step.Status {
		case contracts.BuildStepStatusSuccess:
			continue
		case contracts.BuildStepStatusPending:
			if !allPreviousSucceeded {
				return contracts.BuildStep{}, false
			}
			return step, true
		case contracts.BuildStepStatusRunning, contracts.BuildStepStatusFailed:
			allPreviousSucceeded = false
		default:
			allPreviousSucceeded = false
		}
	}

	return contracts.BuildStep{}, false
}

func defaultString(value string, fallback string) string {
	if value == "" {
		return fallback
	}

	return value
}

func defaultArgs(args []string) []string {
	if len(args) == 0 {
		return []string{"-c", "echo coyote-ci worker default step"}
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
		Step: contracts.BuildStep{
			Name:   step.StepName,
			Status: contracts.BuildStepStatusPending,
		},
	}

	startedAt := time.Now().UTC()
	report.Step.Status = contracts.BuildStepStatusRunning
	report.Step.StartedAt = &startedAt

	result, err := w.builds.RunStep(ctx, contracts.RunStepRequest{
		BuildID:        step.BuildID,
		StepIndex:      step.StepIndex,
		StepName:       step.StepName,
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
		log.Printf("execution completed: build_id=%s step=%s status=%s exit_code=%d", step.BuildID, step.StepName, contracts.RunStepStatusFailed, result.ExitCode)
		report.Step.Status = contracts.BuildStepStatusFailed
		_, _ = w.builds.FailBuild(ctx, step.BuildID)
		return report, err
	}

	log.Printf("execution completed: build_id=%s step=%s status=%s exit_code=%d", step.BuildID, step.StepName, result.Status, result.ExitCode)

	if result.Status == contracts.RunStepStatusSuccess {
		report.Step.Status = contracts.BuildStepStatusSuccess

		remaining, err := w.builds.GetBuildSteps(ctx, step.BuildID)
		if err != nil {
			return report, err
		}

		hasPending := false
		for idx := range remaining {
			if remaining[idx].Status == contracts.BuildStepStatusPending {
				hasPending = true
				break
			}
		}

		if hasPending {
			return report, nil
		}

		if _, err := w.builds.CompleteBuild(ctx, step.BuildID); err != nil {
			return report, err
		}
		return report, nil
	}

	report.Step.Status = contracts.BuildStepStatusFailed
	if _, err := w.builds.FailBuild(ctx, step.BuildID); err != nil {
		return report, err
	}

	return report, nil
}
