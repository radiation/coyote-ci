package service

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/runner"
)

type buildExecutionBoundary interface {
	ListBuilds(ctx context.Context) ([]domain.Build, error)
	GetBuildSteps(ctx context.Context, id string) ([]domain.BuildStep, error)
	ClaimStepIfPending(ctx context.Context, buildID string, stepIndex int, workerID *string, startedAt time.Time) (domain.BuildStep, bool, error)
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
