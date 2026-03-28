package service

import (
	"context"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/logs"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/runner"
)

var ErrBuildNotFound = errors.New("build not found")
var ErrBuildStepNotFound = errors.New("build step not found")
var ErrProjectIDRequired = errors.New("project_id is required")
var ErrInvalidBuildStatusTransition = errors.New("invalid build status transition")
var ErrInvalidBuildStepTransition = errors.New("invalid build step transition")
var ErrStaleStepClaim = errors.New("stale step claim")
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

// BuildService coordinates build lifecycle state transitions and delegates step execution to a runner.
type BuildService struct {
	buildRepo repository.BuildRepository
	runner    runner.Runner
	logSink   logs.LogSink
}

func NewBuildService(buildRepo repository.BuildRepository, stepRunner runner.Runner, logSink logs.LogSink) *BuildService {
	if logSink == nil {
		logSink = logs.NewNoopSink()
	}

	return &BuildService{
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

// StepCompletionReport captures lifecycle completion outcome and post-persist side-effect state.
// CompletionOutcome reflects only persisted lifecycle handling.
// SideEffectErr is set only when persistence completed and a non-lifecycle side effect failed.
type StepCompletionReport struct {
	Step              domain.BuildStep
	CompletionOutcome repository.StepCompletionOutcome
	SideEffectErr     error
}

func (s *BuildService) CreateBuild(ctx context.Context, input CreateBuildInput) (domain.Build, error) {
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

		return s.buildRepo.CreateQueuedBuild(ctx, build, steps)
	}

	return s.buildRepo.Create(ctx, build)
}

func (s *BuildService) GetBuild(ctx context.Context, id string) (domain.Build, error) {
	build, err := s.buildRepo.GetByID(ctx, id)
	return build, mapRepoErr(err)
}

func (s *BuildService) ListBuilds(ctx context.Context) ([]domain.Build, error) {
	return s.buildRepo.List(ctx)
}

func (s *BuildService) GetBuildSteps(ctx context.Context, id string) ([]domain.BuildStep, error) {
	steps, err := s.buildRepo.GetStepsByBuildID(ctx, id)
	return steps, mapRepoErr(err)
}

func (s *BuildService) GetBuildLogs(ctx context.Context, id string) ([]logs.BuildLogLine, error) {
	if _, err := s.buildRepo.GetByID(ctx, id); err != nil {
		return nil, mapRepoErr(err)
	}

	reader, ok := s.logSink.(logs.LogReader)
	if !ok {
		return []logs.BuildLogLine{}, nil
	}

	return reader.GetBuildLogs(ctx, id)
}

func (s *BuildService) GetStepLogChunks(ctx context.Context, buildID string, stepIndex int, afterSequence int64, limit int) ([]logs.StepLogChunk, error) {
	if _, err := s.buildRepo.GetByID(ctx, buildID); err != nil {
		return nil, mapRepoErr(err)
	}

	reader, ok := s.logSink.(logs.StepLogChunkReader)
	if !ok {
		return []logs.StepLogChunk{}, nil
	}

	return reader.ListStepLogChunks(ctx, buildID, stepIndex, afterSequence, limit)
}

func (s *BuildService) ClaimStepIfPending(ctx context.Context, buildID string, stepIndex int, workerID *string, startedAt time.Time) (domain.BuildStep, bool, error) {
	step, claimed, err := s.buildRepo.ClaimStepIfPending(ctx, buildID, stepIndex, workerID, startedAt)
	return step, claimed, mapRepoErr(err)
}

func (s *BuildService) ClaimPendingStep(ctx context.Context, buildID string, stepIndex int, claim repository.StepClaim) (domain.BuildStep, bool, error) {
	step, claimed, err := s.buildRepo.ClaimPendingStep(ctx, buildID, stepIndex, claim)
	return step, claimed, mapRepoErr(err)
}

func (s *BuildService) ReclaimExpiredStep(ctx context.Context, buildID string, stepIndex int, reclaimBefore time.Time, claim repository.StepClaim) (domain.BuildStep, bool, error) {
	step, claimed, err := s.buildRepo.ReclaimExpiredStep(ctx, buildID, stepIndex, reclaimBefore, claim)
	return step, claimed, mapRepoErr(err)
}

func (s *BuildService) RenewStepLease(ctx context.Context, buildID string, stepIndex int, claimToken string, leaseExpiresAt time.Time) (domain.BuildStep, bool, error) {
	step, outcome, err := s.buildRepo.RenewStepLease(ctx, buildID, stepIndex, claimToken, leaseExpiresAt)
	if err != nil {
		return domain.BuildStep{}, false, mapRepoErr(err)
	}

	if outcome == repository.StepCompletionCompleted {
		return step, true, nil
	}
	if outcome == repository.StepCompletionStaleClaim {
		return step, false, ErrStaleStepClaim
	}
	if outcome == repository.StepCompletionDuplicateTerminal || outcome == repository.StepCompletionInvalidTransition {
		return domain.BuildStep{}, false, ErrInvalidBuildStepTransition
	}

	return domain.BuildStep{}, false, ErrInvalidBuildStepTransition
}

func (s *BuildService) QueueBuild(ctx context.Context, id string) (domain.Build, error) {
	return s.QueueBuildWithTemplate(ctx, id, "")
}

func (s *BuildService) QueueBuildWithTemplate(ctx context.Context, id string, template string) (domain.Build, error) {
	return s.QueueBuildWithTemplateAndCustomSteps(ctx, id, template, nil)
}

func (s *BuildService) QueueBuildWithTemplateAndCustomSteps(ctx context.Context, id string, template string, customSteps []QueueBuildCustomStepInput) (domain.Build, error) {
	build, err := s.buildRepo.GetByID(ctx, id)
	if err != nil {
		return domain.Build{}, mapRepoErr(err)
	}

	if !domain.CanTransitionBuild(build.Status, domain.BuildStatusQueued) {
		return domain.Build{}, ErrInvalidBuildStatusTransition
	}

	normalizedTemplate := strings.ToLower(strings.TrimSpace(template))
	if normalizedTemplate == BuildTemplateCustom {
		steps, err := buildStepsForCustomTemplate(id, customSteps)
		if err != nil {
			return domain.Build{}, err
		}

		return s.buildRepo.QueueBuild(ctx, id, steps)
	}

	steps := buildStepsForTemplate(id, normalizedTemplate)
	return s.buildRepo.QueueBuild(ctx, id, steps)
}

func (s *BuildService) StartBuild(ctx context.Context, id string) (domain.Build, error) {
	return s.transitionBuildStatus(ctx, id, domain.BuildStatusRunning, nil)
}

func (s *BuildService) CompleteBuild(ctx context.Context, id string) (domain.Build, error) {
	return s.transitionBuildStatus(ctx, id, domain.BuildStatusSuccess, nil)
}

func (s *BuildService) FailBuild(ctx context.Context, id string) (domain.Build, error) {
	return s.transitionBuildStatus(ctx, id, domain.BuildStatusFailed, nil)
}

func (s *BuildService) RunStep(ctx context.Context, request runner.RunStepRequest) (runner.RunStepResult, StepCompletionReport, error) {
	if s.runner == nil {
		return runner.RunStepResult{}, StepCompletionReport{CompletionOutcome: repository.StepCompletionInvalidTransition}, ErrRunnerNotConfigured
	}

	hasChunkAppender := false
	if _, ok := s.logSink.(logs.StepLogChunkAppender); ok {
		hasChunkAppender = true
	}

	var chunkPersistErr error
	var chunkPersistMu sync.Mutex

	persistChunk := func(chunk runner.StepOutputChunk) error {
		if !hasChunkAppender {
			return nil
		}
		appender, ok := s.logSink.(logs.StepLogChunkAppender)
		if !ok {
			return nil
		}

		text := strings.TrimRight(chunk.ChunkText, "\n")
		if strings.TrimSpace(text) == "" {
			return nil
		}

		stream := logs.StepLogStreamSystem
		switch chunk.Stream {
		case runner.StepOutputStreamStdout:
			stream = logs.StepLogStreamStdout
		case runner.StepOutputStreamStderr:
			stream = logs.StepLogStreamStderr
		case runner.StepOutputStreamSystem:
			stream = logs.StepLogStreamSystem
		}

		_, appendErr := appender.AppendStepLogChunk(ctx, logs.StepLogChunk{
			BuildID:   request.BuildID,
			StepID:    request.StepID,
			StepIndex: request.StepIndex,
			StepName:  request.StepName,
			Stream:    stream,
			ChunkText: text,
			CreatedAt: chunk.EmittedAt,
		})
		if appendErr != nil {
			chunkPersistMu.Lock()
			if chunkPersistErr == nil {
				chunkPersistErr = appendErr
			}
			chunkPersistMu.Unlock()
		}
		return nil
	}

	var result runner.RunStepResult
	var err error
	usedStreamingRunner := false
	if streamingRunner, ok := s.runner.(runner.StreamingRunner); ok {
		usedStreamingRunner = true
		result, err = streamingRunner.RunStepStream(ctx, request, persistChunk)
	} else {
		result, err = s.runner.RunStep(ctx, request)
	}

	if hasChunkAppender && !usedStreamingRunner {
		// persistChunk never returns an error; failures are captured in chunkPersistErr
		// via the closure and surfaced as a SideEffectErr after step completion.
		for _, line := range splitLogLines(result.Stdout) {
			_ = persistChunk(runner.StepOutputChunk{Stream: runner.StepOutputStreamStdout, ChunkText: line, EmittedAt: time.Now().UTC()})
		}
		for _, line := range splitLogLines(result.Stderr) {
			_ = persistChunk(runner.StepOutputChunk{Stream: runner.StepOutputStreamStderr, ChunkText: line, EmittedAt: time.Now().UTC()})
		}
	}
	if err != nil {
		now := time.Now().UTC()
		result = runner.RunStepResult{
			Status:     runner.RunStepStatusFailed,
			ExitCode:   -1,
			Stderr:     err.Error(),
			StartedAt:  now,
			FinishedAt: now,
		}
		completionReport, completionErr := s.handleStepResult(ctx, request, result, hasChunkAppender)
		if completionErr != nil {
			return result, completionReport, errors.Join(err, completionErr)
		}
		chunkPersistMu.Lock()
		if completionReport.SideEffectErr == nil && chunkPersistErr != nil {
			completionReport.SideEffectErr = chunkPersistErr
		}
		chunkPersistMu.Unlock()
		return result, completionReport, err
	}

	completionReport, completionErr := s.handleStepResult(ctx, request, result, hasChunkAppender)
	if completionErr != nil {
		return result, completionReport, completionErr
	}
	chunkPersistMu.Lock()
	if completionReport.SideEffectErr == nil && chunkPersistErr != nil {
		completionReport.SideEffectErr = chunkPersistErr
	}
	chunkPersistMu.Unlock()

	return result, completionReport, nil
}

func (s *BuildService) HandleStepResult(ctx context.Context, request runner.RunStepRequest, result runner.RunStepResult) (StepCompletionReport, error) {
	return s.handleStepResult(ctx, request, result, false)
}

func (s *BuildService) handleStepResult(ctx context.Context, request runner.RunStepRequest, result runner.RunStepResult, skipLegacyLogWrite bool) (StepCompletionReport, error) {
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
	completionUpdate := repository.StepUpdate{
		Status:       stepStatus,
		ExitCode:     &exitCode,
		Stdout:       stdout,
		Stderr:       stderr,
		ErrorMessage: stepError,
		StartedAt:    &result.StartedAt,
		FinishedAt:   &result.FinishedAt,
	}

	claimToken := strings.TrimSpace(request.ClaimToken)
	if claimToken == "" {
		return StepCompletionReport{CompletionOutcome: repository.StepCompletionInvalidTransition}, nil
	}

	completionResult, err := s.buildRepo.CompleteStep(ctx, repository.CompleteStepRequest{
		BuildID:      request.BuildID,
		StepIndex:    request.StepIndex,
		ClaimToken:   claimToken,
		RequireClaim: true,
		Update:       completionUpdate,
	})
	if err != nil {
		return StepCompletionReport{CompletionOutcome: repository.StepCompletionInvalidTransition}, mapRepoErr(err)
	}

	if completionResult.Outcome != repository.StepCompletionCompleted {
		return StepCompletionReport{Step: completionResult.Step, CompletionOutcome: completionResult.Outcome}, nil
	}

	report := StepCompletionReport{Step: completionResult.Step, CompletionOutcome: repository.StepCompletionCompleted}
	if skipLegacyLogWrite {
		return report, nil
	}
	if err := writeOutputLogs(ctx, s.logSink, request.BuildID, request.StepName, result.Stdout); err != nil {
		report.SideEffectErr = err
		return report, nil
	}
	if err := writeOutputLogs(ctx, s.logSink, request.BuildID, request.StepName, result.Stderr); err != nil {
		report.SideEffectErr = err
		return report, nil
	}

	return report, nil
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

func (s *BuildService) transitionBuildStatus(ctx context.Context, id string, toStatus domain.BuildStatus, errorMessage *string) (domain.Build, error) {
	build, err := s.buildRepo.GetByID(ctx, id)
	if err != nil {
		return domain.Build{}, mapRepoErr(err)
	}

	if !domain.CanTransitionBuild(build.Status, toStatus) {
		return domain.Build{}, ErrInvalidBuildStatusTransition
	}

	return s.buildRepo.UpdateStatus(ctx, id, toStatus, errorMessage)
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

func mapRepoErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, repository.ErrBuildNotFound) {
		return ErrBuildNotFound
	}
	if errors.Is(err, repository.ErrInvalidBuildStepTransition) {
		return ErrInvalidBuildStepTransition
	}
	return err
}
