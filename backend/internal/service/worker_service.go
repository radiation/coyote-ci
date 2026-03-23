package service

import (
	"context"
	"log"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/pkg/contracts"
)

type buildExecutionBoundary interface {
	ListBuilds(ctx context.Context) ([]domain.Build, error)
	StartBuild(ctx context.Context, id string) (domain.Build, error)
	CompleteBuild(ctx context.Context, id string) (domain.Build, error)
	FailBuild(ctx context.Context, id string) (domain.Build, error)
	RunStep(ctx context.Context, request contracts.RunStepRequest) (contracts.RunStepResult, error)
}

type RunnableStep struct {
	BuildID        string
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
		if build.Status != domain.BuildStatusPending {
			continue
		}

		return RunnableStep{
			BuildID:    build.ID,
			StepName:   "default",
			Command:    "sh",
			Args:       []string{"-c", "echo coyote-ci worker default step"},
			WorkingDir: ".",
		}, true, nil
	}

	return RunnableStep{}, false, nil
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

	if _, err := w.builds.StartBuild(ctx, step.BuildID); err != nil {
		log.Printf("claiming error: build_id=%s step=%s error=%v", step.BuildID, step.StepName, err)
		return report, err
	}

	startedAt := time.Now().UTC()
	report.Step.Status = contracts.BuildStepStatusRunning
	report.Step.StartedAt = &startedAt

	result, err := w.builds.RunStep(ctx, contracts.RunStepRequest{
		BuildID:        step.BuildID,
		StepName:       step.StepName,
		Command:        step.Command,
		Args:           step.Args,
		Env:            step.Env,
		WorkingDir:     step.WorkingDir,
		TimeoutSeconds: step.TimeoutSeconds,
	})
	report.Result = result

	completedAt := time.Now().UTC()
	report.Step.EndedAt = &completedAt

	if err != nil {
		log.Printf("execution completed: build_id=%s step=%s status=%s exit_code=%d", step.BuildID, step.StepName, contracts.RunStepStatusFailed, result.ExitCode)
		report.Step.Status = contracts.BuildStepStatusFailed
		_, _ = w.builds.FailBuild(ctx, step.BuildID)
		return report, err
	}

	log.Printf("execution completed: build_id=%s step=%s status=%s exit_code=%d", step.BuildID, step.StepName, result.Status, result.ExitCode)

	if result.Status == contracts.RunStepStatusSuccess {
		report.Step.Status = contracts.BuildStepStatusSuccess
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
