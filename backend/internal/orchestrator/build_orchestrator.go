package orchestrator

import (
	"context"
	"errors"
	"regexp"
	"strconv"
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

const (
	BuildTemplateDefault = "default"
	BuildTemplateTest    = "test"
	BuildTemplateBuild   = "build"
)

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
	Steps     []CreateBuildStepInput
}

type CreateBuildStepInput struct {
	Name           string
	Command        string
	Args           []string
	Env            map[string]string
	WorkingDir     string
	TimeoutSeconds int
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

	if len(input.Steps) > 0 {
		steps := make([]domain.BuildStep, 0, len(input.Steps))
		for idx, step := range input.Steps {
			normalized := normalizeCreateStepInput(step)
			name := strings.TrimSpace(normalized.Name)
			if name == "" {
				name = "step-" + strconv.Itoa(idx+1)
			}

			steps = append(steps, domain.BuildStep{
				ID:             uuid.NewString(),
				BuildID:        build.ID,
				StepIndex:      idx,
				Name:           name,
				Command:        normalized.Command,
				Args:           normalized.Args,
				Env:            normalized.Env,
				WorkingDir:     normalized.WorkingDir,
				TimeoutSeconds: normalized.TimeoutSeconds,
				Status:         domain.BuildStepStatusPending,
			})
		}

		return o.buildStore.CreateQueuedBuild(ctx, build, steps)
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
			ID:             step.ID,
			BuildID:        step.BuildID,
			StepIndex:      step.StepIndex,
			Name:           step.Name,
			Command:        step.Command,
			Args:           step.Args,
			Env:            step.Env,
			WorkingDir:     step.WorkingDir,
			TimeoutSeconds: step.TimeoutSeconds,
			Status:         contracts.BuildStepStatus(step.Status),
			WorkerID:       step.WorkerID,
			StartedAt:      step.StartedAt,
			FinishedAt:     step.FinishedAt,
			ExitCode:       step.ExitCode,
			ErrorMessage:   step.ErrorMessage,
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
		ID:             step.ID,
		BuildID:        step.BuildID,
		StepIndex:      step.StepIndex,
		Name:           step.Name,
		Command:        step.Command,
		Args:           step.Args,
		Env:            step.Env,
		WorkingDir:     step.WorkingDir,
		TimeoutSeconds: step.TimeoutSeconds,
		Status:         contracts.BuildStepStatus(step.Status),
		WorkerID:       step.WorkerID,
		StartedAt:      step.StartedAt,
		FinishedAt:     step.FinishedAt,
		ExitCode:       step.ExitCode,
		ErrorMessage:   step.ErrorMessage,
	}, true, nil
}

func (o *BuildOrchestrator) QueueBuild(ctx context.Context, id string) (domain.Build, error) {
	return o.QueueBuildWithTemplate(ctx, id, "")
}

func (o *BuildOrchestrator) QueueBuildWithTemplate(ctx context.Context, id string, template string) (domain.Build, error) {
	build, err := o.buildStore.GetByID(ctx, id)
	if err != nil {
		return domain.Build{}, err
	}

	if !isValidBuildTransition(build.Status, domain.BuildStatusQueued) {
		return domain.Build{}, ErrInvalidBuildStatusTransition
	}

	steps := buildStepsForTemplate(id, template)
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
	if _, err := o.persistStepResult(ctx, request.BuildID, request.StepIndex, domain.BuildStepStatusRunning, nil, nil, &startedAt, nil); err != nil {
		return contracts.RunStepResult{}, err
	}

	result, err := o.runner.RunStep(ctx, request)
	if err != nil {
		finishedAt := time.Now().UTC()
		message := err.Error()
		if _, persistErr := o.persistStepResult(ctx, request.BuildID, request.StepIndex, domain.BuildStepStatusFailed, nil, &message, nil, &finishedAt); persistErr != nil {
			return contracts.RunStepResult{}, errors.Join(err, persistErr)
		}
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
	if _, err := o.persistStepResult(ctx, request.BuildID, request.StepIndex, stepStatus, &exitCode, stepError, &result.StartedAt, &result.FinishedAt); err != nil {
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
			ID:             uuid.NewString(),
			BuildID:        buildID,
			StepIndex:      0,
			Name:           "default",
			Command:        "sh",
			Args:           []string{"-c", "echo coyote-ci worker default step"},
			Env:            map[string]string{},
			WorkingDir:     ".",
			TimeoutSeconds: 0,
			Status:         domain.BuildStepStatusPending,
		},
	}
}

func buildStepsForTemplate(buildID string, template string) []domain.BuildStep {
	normalizedTemplate := strings.ToLower(strings.TrimSpace(template))

	stepInputs := []CreateBuildStepInput{
		{
			Name:       "default",
			Command:    "sh",
			Args:       []string{"-c", "echo coyote-ci worker default step"},
			Env:        map[string]string{},
			WorkingDir: ".",
		},
	}

	switch normalizedTemplate {
	case "", BuildTemplateDefault:
		stepInputs = []CreateBuildStepInput{
			{
				Name:       "default",
				Command:    "sh",
				Args:       []string{"-c", "echo coyote-ci worker default step"},
				Env:        map[string]string{},
				WorkingDir: ".",
			},
		}
	case BuildTemplateTest:
		stepInputs = []CreateBuildStepInput{
			{
				Name:       "setup",
				Command:    "sh",
				Args:       []string{"-c", "echo running setup"},
				Env:        map[string]string{},
				WorkingDir: ".",
			},
			{
				Name:       "test",
				Command:    "sh",
				Args:       []string{"-c", "echo running tests"},
				Env:        map[string]string{},
				WorkingDir: ".",
			},
			{
				Name:       "teardown",
				Command:    "sh",
				Args:       []string{"-c", "echo running teardown"},
				Env:        map[string]string{},
				WorkingDir: ".",
			},
		}
	case BuildTemplateBuild:
		stepInputs = []CreateBuildStepInput{
			{
				Name:       "install",
				Command:    "sh",
				Args:       []string{"-c", "echo installing dependencies"},
				Env:        map[string]string{},
				WorkingDir: ".",
			},
			{
				Name:       "compile",
				Command:    "sh",
				Args:       []string{"-c", "echo compiling project"},
				Env:        map[string]string{},
				WorkingDir: ".",
			},
		}
	}

	steps := make([]domain.BuildStep, 0, len(stepInputs))
	for idx, input := range stepInputs {
		normalized := normalizeCreateStepInput(input)
		steps = append(steps, domain.BuildStep{
			ID:             uuid.NewString(),
			BuildID:        buildID,
			StepIndex:      idx,
			Name:           normalized.Name,
			Command:        normalized.Command,
			Args:           normalized.Args,
			Env:            normalized.Env,
			WorkingDir:     normalized.WorkingDir,
			TimeoutSeconds: normalized.TimeoutSeconds,
			Status:         domain.BuildStepStatusPending,
		})
	}

	return steps
}

func normalizeCreateStepInput(in CreateBuildStepInput) CreateBuildStepInput {
	out := in

	if strings.TrimSpace(out.Command) == "" {
		out.Command = "sh"
	}
	if len(out.Args) == 0 {
		out.Args = []string{"-c", "echo coyote-ci worker default step"}
	}
	if out.Env == nil {
		out.Env = map[string]string{}
	}
	if strings.TrimSpace(out.WorkingDir) == "" {
		out.WorkingDir = "."
	}
	if out.TimeoutSeconds < 0 {
		out.TimeoutSeconds = 0
	}

	return out
}

func (o *BuildOrchestrator) persistStepResult(ctx context.Context, buildID string, stepIndex int, stepStatus domain.BuildStepStatus, exitCode *int, errorMessage *string, startedAt *time.Time, finishedAt *time.Time) (domain.BuildStep, error) {
	persistedStep, err := o.buildStore.UpdateStepByIndex(ctx, buildID, stepIndex, stepStatus, nil, exitCode, errorMessage, startedAt, finishedAt)
	if err != nil {
		return domain.BuildStep{}, err
	}

	if stepStatus == domain.BuildStepStatusSuccess {
		build, err := o.buildStore.GetByID(ctx, buildID)
		if err != nil {
			return domain.BuildStep{}, err
		}

		if stepIndex == build.CurrentStepIndex {
			_, err = o.buildStore.UpdateCurrentStepIndex(ctx, buildID, build.CurrentStepIndex+1)
			if err != nil {
				return domain.BuildStep{}, err
			}
		}
	}

	return persistedStep, nil
}
