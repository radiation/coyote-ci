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
		ID:        uuid.NewString(),
		ProjectID: input.ProjectID,
		Status:    domain.BuildStatusPending,
		CreatedAt: time.Now().UTC(),
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
	if _, err := o.buildStore.GetByID(ctx, id); err != nil {
		return nil, err
	}

	return []contracts.BuildStep{}, nil
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

func (o *BuildOrchestrator) QueueBuild(ctx context.Context, id string) (domain.Build, error) {
	return o.transitionBuildStatus(ctx, id, domain.BuildStatusQueued)
}

func (o *BuildOrchestrator) StartBuild(ctx context.Context, id string) (domain.Build, error) {
	return o.transitionBuildStatus(ctx, id, domain.BuildStatusRunning)
}

func (o *BuildOrchestrator) CompleteBuild(ctx context.Context, id string) (domain.Build, error) {
	return o.transitionBuildStatus(ctx, id, domain.BuildStatusSuccess)
}

func (o *BuildOrchestrator) FailBuild(ctx context.Context, id string) (domain.Build, error) {
	return o.transitionBuildStatus(ctx, id, domain.BuildStatusFailed)
}

func (o *BuildOrchestrator) RunStep(ctx context.Context, request contracts.RunStepRequest) (contracts.RunStepResult, error) {
	if o.runner == nil {
		return contracts.RunStepResult{}, ErrRunnerNotConfigured
	}

	result, err := o.runner.RunStep(ctx, request)
	if err != nil {
		return contracts.RunStepResult{}, err
	}

	if err := writeOutputLogs(ctx, o.logSink, request.BuildID, request.StepName, result.Stdout); err != nil {
		return contracts.RunStepResult{}, err
	}
	if err := writeOutputLogs(ctx, o.logSink, request.BuildID, request.StepName, result.Stderr); err != nil {
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

func (o *BuildOrchestrator) transitionBuildStatus(ctx context.Context, id string, toStatus domain.BuildStatus) (domain.Build, error) {
	build, err := o.buildStore.GetByID(ctx, id)
	if err != nil {
		return domain.Build{}, err
	}

	if !isValidBuildTransition(build.Status, toStatus) {
		return domain.Build{}, ErrInvalidBuildStatusTransition
	}

	return o.buildStore.UpdateStatus(ctx, id, toStatus)
}

func isValidBuildTransition(fromStatus, toStatus domain.BuildStatus) bool {
	switch fromStatus {
	case domain.BuildStatusPending:
		return toStatus == domain.BuildStatusQueued
	case domain.BuildStatusQueued:
		return toStatus == domain.BuildStatusRunning
	case domain.BuildStatusRunning:
		return toStatus == domain.BuildStatusSuccess || toStatus == domain.BuildStatusFailed
	default:
		return false
	}
}
