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
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/runner"
)

var ErrProjectIDRequired = errors.New("project_id is required")
var ErrInvalidBuildStatusTransition = errors.New("invalid build status transition")
var ErrRunnerNotConfigured = errors.New("runner not configured")
var ErrCustomTemplateStepsRequired = errors.New("custom template requires at least one step")
var ErrCustomTemplateStepCommandRequired = errors.New("custom template step command is required")

const (
	BuildTemplateDefault = "default"
	BuildTemplateTest    = "test"
	BuildTemplateBuild   = "build"
	BuildTemplateCustom  = "custom"
	BuildTemplateFail    = "fail"
)

// BuildOrchestrator coordinates build lifecycle state transitions and delegates step execution to a runner.
type BuildOrchestrator struct {
	buildRepo repository.BuildRepository
	runner    runner.Runner
	logSink   logs.LogSink
}

func NewBuildOrchestrator(buildRepo repository.BuildRepository, stepRunner runner.Runner, logSink logs.LogSink) *BuildOrchestrator {
	if logSink == nil {
		logSink = logs.NewNoopSink()
	}

	return &BuildOrchestrator{
		buildRepo: buildRepo,
		runner:    stepRunner,
		logSink:   logSink,
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

type QueueBuildCustomStepInput struct {
	Name    string
	Command string
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

		return o.buildRepo.CreateQueuedBuild(ctx, build, steps)
	}

	return o.buildRepo.Create(ctx, build)
}

func (o *BuildOrchestrator) GetBuild(ctx context.Context, id string) (domain.Build, error) {
	return o.buildRepo.GetByID(ctx, id)
}

func (o *BuildOrchestrator) ListBuilds(ctx context.Context) ([]domain.Build, error) {
	return o.buildRepo.List(ctx)
}

func (o *BuildOrchestrator) GetBuildSteps(ctx context.Context, id string) ([]domain.BuildStep, error) {
	return o.buildRepo.GetStepsByBuildID(ctx, id)
}

func (o *BuildOrchestrator) GetBuildLogs(ctx context.Context, id string) ([]logs.BuildLogLine, error) {
	if _, err := o.buildRepo.GetByID(ctx, id); err != nil {
		return nil, err
	}

	reader, ok := o.logSink.(logs.LogReader)
	if !ok {
		return []logs.BuildLogLine{}, nil
	}

	return reader.GetBuildLogs(ctx, id)
}

func (o *BuildOrchestrator) ClaimStepIfPending(ctx context.Context, buildID string, stepIndex int, workerID *string, startedAt time.Time) (domain.BuildStep, bool, error) {
	return o.buildRepo.ClaimStepIfPending(ctx, buildID, stepIndex, workerID, startedAt)
}

func (o *BuildOrchestrator) QueueBuild(ctx context.Context, id string) (domain.Build, error) {
	return o.QueueBuildWithTemplate(ctx, id, "")
}

func (o *BuildOrchestrator) QueueBuildWithTemplate(ctx context.Context, id string, template string) (domain.Build, error) {
	return o.QueueBuildWithTemplateAndCustomSteps(ctx, id, template, nil)
}

func (o *BuildOrchestrator) QueueBuildWithTemplateAndCustomSteps(ctx context.Context, id string, template string, customSteps []QueueBuildCustomStepInput) (domain.Build, error) {
	build, err := o.buildRepo.GetByID(ctx, id)
	if err != nil {
		return domain.Build{}, err
	}

	if !isValidBuildTransition(build.Status, domain.BuildStatusQueued) {
		return domain.Build{}, ErrInvalidBuildStatusTransition
	}

	normalizedTemplate := strings.ToLower(strings.TrimSpace(template))
	if normalizedTemplate == BuildTemplateCustom {
		steps, err := buildStepsForCustomTemplate(id, customSteps)
		if err != nil {
			return domain.Build{}, err
		}

		return o.buildRepo.QueueBuild(ctx, id, steps)
	}

	steps := buildStepsForTemplate(id, normalizedTemplate)
	return o.buildRepo.QueueBuild(ctx, id, steps)
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

func (o *BuildOrchestrator) RunStep(ctx context.Context, request runner.RunStepRequest) (runner.RunStepResult, error) {
	if o.runner == nil {
		return runner.RunStepResult{}, ErrRunnerNotConfigured
	}

	startedAt := time.Now().UTC()
	if _, err := o.persistStepResult(ctx, request.BuildID, request.StepIndex, domain.BuildStepStatusRunning, nil, nil, nil, nil, &startedAt, nil); err != nil {
		return runner.RunStepResult{}, err
	}

	result, err := o.runner.RunStep(ctx, request)
	if err != nil {
		finishedAt := time.Now().UTC()
		message := err.Error()
		if _, persistErr := o.persistStepResult(ctx, request.BuildID, request.StepIndex, domain.BuildStepStatusFailed, nil, nil, nil, &message, nil, &finishedAt); persistErr != nil {
			return runner.RunStepResult{}, errors.Join(err, persistErr)
		}
		return runner.RunStepResult{}, err
	}

	if err := writeOutputLogs(ctx, o.logSink, request.BuildID, request.StepName, result.Stdout); err != nil {
		return runner.RunStepResult{}, err
	}
	if err := writeOutputLogs(ctx, o.logSink, request.BuildID, request.StepName, result.Stderr); err != nil {
		return runner.RunStepResult{}, err
	}

	stepStatus := domain.BuildStepStatusSuccess
	if result.Status == runner.RunStepStatusFailed {
		stepStatus = domain.BuildStepStatusFailed
	}

	var stepError *string
	if stepStatus == domain.BuildStepStatusFailed {
		message := strings.TrimSpace(result.Stderr)
		if message != "" {
			stepError = &message
		}
	}

	var stdout *string
	if result.Stdout != "" {
		stdoutValue := result.Stdout
		stdout = &stdoutValue
	}

	var stderr *string
	if result.Stderr != "" {
		stderrValue := result.Stderr
		stderr = &stderrValue
	}

	exitCode := result.ExitCode
	if _, err := o.persistStepResult(ctx, request.BuildID, request.StepIndex, stepStatus, &exitCode, stdout, stderr, stepError, &result.StartedAt, &result.FinishedAt); err != nil {
		return runner.RunStepResult{}, err
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
	build, err := o.buildRepo.GetByID(ctx, id)
	if err != nil {
		return domain.Build{}, err
	}

	if !isValidBuildTransition(build.Status, toStatus) {
		return domain.Build{}, ErrInvalidBuildStatusTransition
	}

	return o.buildRepo.UpdateStatus(ctx, id, toStatus, errorMessage)
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
			Args:           []string{"-c", "echo coyote-ci worker default step && exit 0"},
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
			Args:       []string{"-c", "echo coyote-ci worker default step && exit 0"},
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
				Args:       []string{"-c", "echo coyote-ci worker default step && exit 0"},
				Env:        map[string]string{},
				WorkingDir: ".",
			},
		}
	case BuildTemplateTest:
		stepInputs = []CreateBuildStepInput{
			{
				Name:       "setup",
				Command:    "sh",
				Args:       []string{"-c", "echo running setup && exit 0"},
				Env:        map[string]string{},
				WorkingDir: ".",
			},
			{
				Name:       "test",
				Command:    "sh",
				Args:       []string{"-c", "echo running tests && exit 0"},
				Env:        map[string]string{},
				WorkingDir: ".",
			},
			{
				Name:       "teardown",
				Command:    "sh",
				Args:       []string{"-c", "echo running teardown && exit 0"},
				Env:        map[string]string{},
				WorkingDir: ".",
			},
		}
	case BuildTemplateBuild:
		stepInputs = []CreateBuildStepInput{
			{
				Name:       "install",
				Command:    "sh",
				Args:       []string{"-c", "echo installing dependencies && exit 0"},
				Env:        map[string]string{},
				WorkingDir: ".",
			},
			{
				Name:       "compile",
				Command:    "sh",
				Args:       []string{"-c", "echo compiling project && exit 0"},
				Env:        map[string]string{},
				WorkingDir: ".",
			},
		}
	case BuildTemplateFail:
		stepInputs = []CreateBuildStepInput{
			{
				Name:       "setup",
				Command:    "sh",
				Args:       []string{"-c", "echo success && exit 0"},
				Env:        map[string]string{},
				WorkingDir: ".",
			},
			{
				Name:       "verify",
				Command:    "sh",
				Args:       []string{"-c", "echo failure 1>&2 && exit 1"},
				Env:        map[string]string{},
				WorkingDir: ".",
			},
		}
	}

	return domainStepsFromInputs(buildID, stepInputs)
}

func buildStepsForCustomTemplate(buildID string, customSteps []QueueBuildCustomStepInput) ([]domain.BuildStep, error) {
	if len(customSteps) == 0 {
		return nil, ErrCustomTemplateStepsRequired
	}

	stepInputs := make([]CreateBuildStepInput, 0, len(customSteps))
	for idx, step := range customSteps {
		command := strings.TrimSpace(step.Command)
		if command == "" {
			return nil, ErrCustomTemplateStepCommandRequired
		}

		name := strings.TrimSpace(step.Name)
		if name == "" {
			name = "step-" + strconv.Itoa(idx+1)
		}

		stepInputs = append(stepInputs, CreateBuildStepInput{
			Name:       name,
			Command:    "sh",
			Args:       []string{"-c", command},
			Env:        map[string]string{},
			WorkingDir: ".",
		})
	}

	return domainStepsFromInputs(buildID, stepInputs), nil
}

func domainStepsFromInputs(buildID string, stepInputs []CreateBuildStepInput) []domain.BuildStep {
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
		out.Args = []string{"-c", "echo coyote-ci worker default step && exit 0"}
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

func (o *BuildOrchestrator) persistStepResult(ctx context.Context, buildID string, stepIndex int, stepStatus domain.BuildStepStatus, exitCode *int, stdout *string, stderr *string, errorMessage *string, startedAt *time.Time, finishedAt *time.Time) (domain.BuildStep, error) {
	persistedStep, err := o.buildRepo.UpdateStepByIndex(ctx, buildID, stepIndex, stepStatus, nil, exitCode, stdout, stderr, errorMessage, startedAt, finishedAt)
	if err != nil {
		return domain.BuildStep{}, err
	}

	if stepStatus == domain.BuildStepStatusSuccess {
		build, err := o.buildRepo.GetByID(ctx, buildID)
		if err != nil {
			return domain.BuildStep{}, err
		}

		if stepIndex == build.CurrentStepIndex {
			_, err = o.buildRepo.UpdateCurrentStepIndex(ctx, buildID, build.CurrentStepIndex+1)
			if err != nil {
				return domain.BuildStep{}, err
			}
		}
	}

	return persistedStep, nil
}
