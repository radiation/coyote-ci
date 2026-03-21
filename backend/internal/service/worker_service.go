package service

import (
	"context"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/pkg/contracts"
)

type buildExecutionBoundary interface {
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

func (w *WorkerService) ExecuteRunnableStep(ctx context.Context, step RunnableStep) (StepExecutionReport, error) {
	report := StepExecutionReport{
		BuildID: step.BuildID,
		Step: contracts.BuildStep{
			Name:   step.StepName,
			Status: contracts.BuildStepStatusPending,
		},
	}

	if _, err := w.builds.StartBuild(ctx, step.BuildID); err != nil {
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
		report.Step.Status = contracts.BuildStepStatusFailed
		_, _ = w.builds.FailBuild(ctx, step.BuildID)
		return report, err
	}

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
