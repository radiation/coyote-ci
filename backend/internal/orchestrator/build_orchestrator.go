package orchestrator

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/logs"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/runner"
	"github.com/radiation/coyote-ci/backend/internal/store"
	"github.com/radiation/coyote-ci/backend/pkg/contracts"
)

var ErrProjectIDRequired = errors.New("project_id is required")
var ErrInvalidBuildStatusTransition = errors.New("invalid build status transition")
var ErrRunnerNotConfigured = errors.New("runner not configured")

// BuildOrchestrator coordinates build lifecycle state transitions and delegates step execution to a runner.
type BuildOrchestrator struct {
	buildStore store.BuildStore
	runner     runner.Runner
	logSink    logs.LogSink
}

func NewBuildOrchestrator(buildStore store.BuildStore, stepRunner runner.Runner, logSink logs.LogSink) *BuildOrchestrator {
	if logSink == nil {
		logSink = logs.NewNoopSink()
	}

	return &BuildOrchestrator{
		buildStore: buildStore,
		runner:     stepRunner,
		logSink:    logSink,
	}
}

type CreateBuildInput struct {
	ProjectID string
}

func (o *BuildOrchestrator) CreateBuild(ctx context.Context, input CreateBuildInput) (domain.Build, error) {
	if input.ProjectID == "" {
		return domain.Build{}, ErrProjectIDRequired
	}

	build := domain.Build{
		ID:               uuid.NewString(),
		ProjectID:        input.ProjectID,
		Status:           domain.BuildStatusPending,
		CreatedAt:        time.Now().UTC(),
		CurrentStepIndex: 0,
	}

	return o.buildStore.Create(ctx, build)
}

func (o *BuildOrchestrator) GetBuild(ctx context.Context, id string) (domain.Build, error) {
	return o.buildStore.GetByID(ctx, id)
}

func (o *BuildOrchestrator) ListBuilds(ctx context.Context) ([]domain.Build, error) {
	return o.buildStore.List(ctx)
}

func (o *BuildOrchestrator) GetBuildSteps(ctx context.Context, id string) ([]contracts.BuildStep, error) {
	steps, err := o.buildStore.GetStepsByBuildID(ctx, id)
	if err != nil {
		return nil, err
	}

	result := make([]contracts.BuildStep, 0, len(steps))
	for _, step := range steps {
		result = append(result, contracts.BuildStep{
			StepIndex: step.StepIndex,
			Name:      step.Name,
			Status:    contracts.BuildStepStatus(step.Status),
			StartedAt: step.StartedAt,
			EndedAt:   step.FinishedAt,
		})
	}

	return result, nil
}

func (o *BuildOrchestrator) GetBuildLogs(ctx context.Context, id string) ([]contracts.BuildLogLine, error) {
	if _, err := o.buildStore.GetByID(ctx, id); err != nil {
		return nil, err
	}

	reader, ok := o.logSink.(logs.LogReader)
	if !ok {
		return []contracts.BuildLogLine{}, nil
	}

	return reader.GetBuildLogs(ctx, id)
}

func (o *BuildOrchestrator) ClaimStepIfPending(ctx context.Context, buildID string, stepIndex int, workerID *string, startedAt time.Time) (contracts.BuildStep, bool, error) {
	step, claimed, err := o.buildStore.ClaimStepIfPending(ctx, buildID, stepIndex, workerID, startedAt)
	if err != nil {
		return contracts.BuildStep{}, false, err
	}
	if !claimed {
		return contracts.BuildStep{}, false, nil
	}

	return contracts.BuildStep{
		StepIndex: step.StepIndex,
		Name:      step.Name,
		Status:    contracts.BuildStepStatus(step.Status),
		StartedAt: step.StartedAt,
		EndedAt:   step.FinishedAt,
	}, true, nil
}

func (o *BuildOrchestrator) QueueBuild(ctx context.Context, id string) (domain.Build, error) {
	build, err := o.buildStore.GetByID(ctx, id)
	if err != nil {
		return domain.Build{}, err
	}

	if !isValidBuildTransition(build.Status, domain.BuildStatusQueued) {
		return domain.Build{}, ErrInvalidBuildStatusTransition
	}

	steps := defaultBuildSteps(id)
	return o.buildStore.QueueBuild(ctx, id, steps)
}

func (o *BuildOrchestrator) StartBuild(ctx context.Context, id string) (domain.Build, error) {
	return o.transitionBuildStatus(ctx, id, domain.BuildStatusRunning, nil)
}

func (o *BuildOrchestrator) CompleteBuild(ctx context.Context, id string) (domain.Build, error) {
	return o.transitionBuildStatus(ctx, id, domain.BuildStatusSuccess, nil)
}

func (o *BuildOrchestrator) FailBuild(ctx context.Context, id string) (domain.Build, error) {
	return o.transitionBuildStatus(ctx, id, domain.BuildStatusFailed, nil)
}

func (o *BuildOrchestrator) RunStep(ctx context.Context, request contracts.RunStepRequest) (contracts.RunStepResult, error) {
	if o.runner == nil {
		return contracts.RunStepResult{}, ErrRunnerNotConfigured
	}

	startedAt := time.Now().UTC()
	if _, err := o.persistStepResult(ctx, request.BuildID, request.StepName, domain.BuildStepStatusRunning, nil, nil, &startedAt, nil); err != nil {
		return contracts.RunStepResult{}, err
	}

	result, err := o.runner.RunStep(ctx, request)
	if err != nil {
		finishedAt := time.Now().UTC()
		message := err.Error()
		_, _ = o.persistStepResult(ctx, request.BuildID, request.StepName, domain.BuildStepStatusFailed, nil, &message, nil, &finishedAt)
		return contracts.RunStepResult{}, err
	}

	if err := writeOutputLogs(ctx, o.logSink, request.BuildID, request.StepName, result.Stdout); err != nil {
		return contracts.RunStepResult{}, err
	}
	if err := writeOutputLogs(ctx, o.logSink, request.BuildID, request.StepName, result.Stderr); err != nil {
		return contracts.RunStepResult{}, err
	}

	stepStatus := domain.BuildStepStatusSuccess
	if result.Status == contracts.RunStepStatusFailed {
		stepStatus = domain.BuildStepStatusFailed
	}

	var stepError *string
	if stepStatus == domain.BuildStepStatusFailed {
		message := strings.TrimSpace(result.Stderr)
		if message != "" {
			stepError = &message
		}
	}

	exitCode := result.ExitCode
	if _, err := o.persistStepResult(ctx, request.BuildID, request.StepName, stepStatus, &exitCode, stepError, &result.StartedAt, &result.FinishedAt); err != nil {
		return contracts.RunStepResult{}, err
	}

	return result, nil
}

func writeOutputLogs(ctx context.Context, sink logs.LogSink, buildID string, stepName string, output string) error {
	for _, line := range splitLogLines(output) {
		if err := sink.WriteStepLog(ctx, buildID, stepName, line); err != nil {
			return err
		}
	}

	return nil
}

var lineBreakSplitter = regexp.MustCompile(`\r?\n`)

func splitLogLines(output string) []string {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return nil
	}

	return lineBreakSplitter.Split(trimmed, -1)
}

func (o *BuildOrchestrator) transitionBuildStatus(ctx context.Context, id string, toStatus domain.BuildStatus, errorMessage *string) (domain.Build, error) {
	build, err := o.buildStore.GetByID(ctx, id)
	if err != nil {
		return domain.Build{}, err
	}

	if !isValidBuildTransition(build.Status, toStatus) {
		return domain.Build{}, ErrInvalidBuildStatusTransition
	}

	return o.buildStore.UpdateStatus(ctx, id, toStatus, errorMessage)
}

func isValidBuildTransition(fromStatus, toStatus domain.BuildStatus) bool {
	switch fromStatus {
	case domain.BuildStatusPending:
		return toStatus == domain.BuildStatusQueued || toStatus == domain.BuildStatusRunning
	case domain.BuildStatusQueued:
		return toStatus == domain.BuildStatusRunning
	case domain.BuildStatusRunning:
		return toStatus == domain.BuildStatusSuccess || toStatus == domain.BuildStatusFailed
	default:
		return false
	}
}

func defaultBuildSteps(buildID string) []domain.BuildStep {
	return []domain.BuildStep{
		{
			ID:        uuid.NewString(),
			BuildID:   buildID,
			StepIndex: 0,
			Name:      "default",
			Status:    domain.BuildStepStatusPending,
		},
	}
}

func (o *BuildOrchestrator) persistStepResult(ctx context.Context, buildID string, stepName string, stepStatus domain.BuildStepStatus, exitCode *int, errorMessage *string, startedAt *time.Time, finishedAt *time.Time) (domain.BuildStep, error) {
	build, err := o.buildStore.GetByID(ctx, buildID)
	if err != nil {
		return domain.BuildStep{}, err
	}

	steps, err := o.buildStore.GetStepsByBuildID(ctx, buildID)
	if err != nil {
		return domain.BuildStep{}, err
	}

	for _, step := range steps {
		if step.Name != stepName {
			continue
		}

		persistedStep, err := o.buildStore.UpdateStepByIndex(ctx, buildID, step.StepIndex, stepStatus, nil, exitCode, errorMessage, startedAt, finishedAt)
		if err != nil {
			return domain.BuildStep{}, err
		}

		if stepStatus == domain.BuildStepStatusSuccess && step.StepIndex == build.CurrentStepIndex {
			_, err = o.buildStore.UpdateCurrentStepIndex(ctx, buildID, build.CurrentStepIndex+1)
			if err != nil {
				return domain.BuildStep{}, err
			}
		}

		return persistedStep, nil
	}

	if build.CurrentStepIndex >= 0 && build.CurrentStepIndex < len(steps) {
		step := steps[build.CurrentStepIndex]
		persistedStep, err := o.buildStore.UpdateStepByIndex(ctx, buildID, step.StepIndex, stepStatus, nil, exitCode, errorMessage, startedAt, finishedAt)
		if err != nil {
			return domain.BuildStep{}, err
		}

		if stepStatus == domain.BuildStepStatusSuccess {
			_, err = o.buildStore.UpdateCurrentStepIndex(ctx, buildID, build.CurrentStepIndex+1)
			if err != nil {
				return domain.BuildStep{}, err
			}
		}

		return persistedStep, nil
	}

	return domain.BuildStep{}, repository.ErrBuildNotFound
}
