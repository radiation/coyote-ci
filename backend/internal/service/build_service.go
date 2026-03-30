package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/artifact"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/logs"
	"github.com/radiation/coyote-ci/backend/internal/pipeline"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/runner"
	"github.com/radiation/coyote-ci/backend/internal/source"
)

var ErrBuildNotFound = errors.New("build not found")
var ErrBuildStepNotFound = errors.New("build step not found")
var ErrProjectIDRequired = errors.New("project_id is required")
var ErrInvalidBuildStatusTransition = errors.New("invalid build status transition")
var ErrInvalidBuildStepTransition = errors.New("invalid build step transition")
var ErrStaleStepClaim = errors.New("stale step claim")
var ErrRunnerNotConfigured = errors.New("runner not configured")
var ErrRunnerWorkspaceNotSupported = errors.New("runner does not support workspace preparation for repo-backed builds")
var ErrCustomTemplateStepsRequired = errors.New("custom template requires at least one step")
var ErrCustomTemplateStepCommandRequired = errors.New("custom template step command is required")
var ErrPipelineYAMLRequired = errors.New("pipeline YAML is required")
var ErrRepoURLRequired = errors.New("repo_url is required")
var ErrRefRequired = errors.New("ref is required")
var ErrRepoFetcherNotConfigured = errors.New("repo fetcher not configured")
var ErrPipelineFileNotFound = errors.New(".coyote/pipeline.yml not found in repository")
var ErrArtifactNotFound = errors.New("artifact not found")

const (
	BuildTemplateDefault = "default"
	BuildTemplateTest    = "test"
	BuildTemplateBuild   = "build"
	BuildTemplateCustom  = "custom"
	BuildTemplateFail    = "fail"
)

// BuildService coordinates build lifecycle state transitions and delegates step execution to a runner.
type BuildService struct {
	buildRepo   repository.BuildRepository
	runner      runner.Runner
	logSink     logs.LogSink
	repoFetcher source.RepoFetcher

	artifactRepo          repository.ArtifactRepository
	artifactStore         artifact.Store
	artifactCollector     *artifact.Collector
	artifactWorkspaceRoot string

	defaultExecutionImage string
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

// SetRepoFetcher attaches a RepoFetcher for repo-backed build creation.
func (s *BuildService) SetRepoFetcher(fetcher source.RepoFetcher) {
	s.repoFetcher = fetcher
}

// SetDefaultExecutionImage sets the image used when a build-scoped runner requires one.
func (s *BuildService) SetDefaultExecutionImage(image string) {
	s.defaultExecutionImage = strings.TrimSpace(image)
}

// SetArtifactPersistence configures build artifact persistence dependencies.
func (s *BuildService) SetArtifactPersistence(repo repository.ArtifactRepository, store artifact.Store, workspaceRoot string) {
	s.artifactRepo = repo
	s.artifactStore = store
	s.artifactWorkspaceRoot = strings.TrimSpace(workspaceRoot)
	if store != nil {
		s.artifactCollector = artifact.NewCollector(store)
	} else {
		s.artifactCollector = nil
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

// CreatePipelineBuildInput is the service-level input for creating a build from pipeline YAML.
type CreatePipelineBuildInput struct {
	ProjectID    string
	PipelineYAML string
	SourcePath   string // e.g. ".coyote/pipeline.yml"
}

// CreateBuildFromPipeline parses, validates, and resolves pipeline YAML, then creates
// a queued build with canonical build steps. The raw YAML is snapshot-persisted on the build.
func (s *BuildService) CreateBuildFromPipeline(ctx context.Context, input CreatePipelineBuildInput) (domain.Build, error) {
	if input.ProjectID == "" {
		return domain.Build{}, ErrProjectIDRequired
	}
	yamlText := strings.TrimSpace(input.PipelineYAML)
	if yamlText == "" {
		return domain.Build{}, ErrPipelineYAMLRequired
	}

	resolved, err := pipeline.LoadAndResolve([]byte(yamlText))
	if err != nil {
		return domain.Build{}, err
	}

	buildID := uuid.NewString()
	steps := pipelineStepsToDomain(buildID, resolved.Steps)

	pipelineName := resolved.Name
	var pipelineNamePtr *string
	if pipelineName != "" {
		pipelineNamePtr = &pipelineName
	}
	var sourcePtr *string
	if input.SourcePath != "" {
		sourcePtr = &input.SourcePath
	}

	build := domain.Build{
		ID:                 buildID,
		ProjectID:          input.ProjectID,
		Status:             domain.BuildStatusQueued,
		CreatedAt:          time.Now().UTC(),
		CurrentStepIndex:   0,
		PipelineConfigYAML: &yamlText,
		PipelineName:       pipelineNamePtr,
		PipelineSource:     sourcePtr,
	}

	return s.buildRepo.CreateQueuedBuild(ctx, build, steps)
}

// pipelineStepsToDomain converts resolved pipeline steps into canonical domain build steps.
func pipelineStepsToDomain(buildID string, steps []pipeline.ResolvedStep) []domain.BuildStep {
	out := make([]domain.BuildStep, 0, len(steps))
	for idx, rs := range steps {
		env := rs.Env
		if env == nil {
			env = map[string]string{}
		}
		workingDir := rs.WorkingDir
		if workingDir == "" {
			workingDir = "."
		}
		out = append(out, domain.BuildStep{
			ID:             uuid.NewString(),
			BuildID:        buildID,
			StepIndex:      idx,
			Name:           rs.Name,
			Command:        "sh",
			Args:           []string{"-c", rs.Run},
			Env:            env,
			WorkingDir:     workingDir,
			TimeoutSeconds: rs.TimeoutSeconds,
			Status:         domain.BuildStepStatusPending,
		})
	}
	return out
}

// CreateRepoBuildInput is the service-level input for creating a build from a repository checkout.
type CreateRepoBuildInput struct {
	ProjectID string
	RepoURL   string
	Ref       string
}

const pipelineFilePath = ".coyote/pipeline.yml"

// CreateBuildFromRepo clones the repo, loads .coyote/pipeline.yml, parses/validates/resolves
// it, then creates a queued build with canonical build steps and repo source metadata.
func (s *BuildService) CreateBuildFromRepo(ctx context.Context, input CreateRepoBuildInput) (domain.Build, error) {
	if input.ProjectID == "" {
		return domain.Build{}, ErrProjectIDRequired
	}
	if strings.TrimSpace(input.RepoURL) == "" {
		return domain.Build{}, ErrRepoURLRequired
	}
	if strings.TrimSpace(input.Ref) == "" {
		return domain.Build{}, ErrRefRequired
	}
	if s.repoFetcher == nil {
		return domain.Build{}, ErrRepoFetcherNotConfigured
	}

	localPath, commitSHA, err := s.repoFetcher.Fetch(ctx, input.RepoURL, input.Ref)
	if err != nil {
		return domain.Build{}, fmt.Errorf("fetching repo: %w", err)
	}
	defer func() {
		if localPath != "" {
			_ = os.RemoveAll(localPath)
		}
	}()

	src := pipeline.FileSource{Path: filepath.Join(localPath, pipelineFilePath)}
	yamlData, _, err := src.Load()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return domain.Build{}, ErrPipelineFileNotFound
		}
		return domain.Build{}, fmt.Errorf("loading pipeline file: %w", err)
	}

	resolved, err := pipeline.LoadAndResolve(yamlData)
	if err != nil {
		return domain.Build{}, err
	}

	buildID := uuid.NewString()
	steps := pipelineStepsToDomain(buildID, resolved.Steps)

	yamlText := string(yamlData)
	var pipelineNamePtr *string
	if resolved.Name != "" {
		pipelineNamePtr = &resolved.Name
	}
	pipelineSource := pipelineFilePath

	repoURL := strings.TrimSpace(input.RepoURL)
	ref := strings.TrimSpace(input.Ref)
	var commitSHAPtr *string
	if commitSHA != "" {
		commitSHAPtr = &commitSHA
	}

	build := domain.Build{
		ID:                 buildID,
		ProjectID:          input.ProjectID,
		Status:             domain.BuildStatusQueued,
		CreatedAt:          time.Now().UTC(),
		CurrentStepIndex:   0,
		PipelineConfigYAML: &yamlText,
		PipelineName:       pipelineNamePtr,
		PipelineSource:     &pipelineSource,
		RepoURL:            &repoURL,
		Ref:                &ref,
		CommitSHA:          commitSHAPtr,
	}

	return s.buildRepo.CreateQueuedBuild(ctx, build, steps)
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

func (s *BuildService) GetBuildArtifacts(ctx context.Context, buildID string) ([]domain.BuildArtifact, error) {
	if _, err := s.buildRepo.GetByID(ctx, buildID); err != nil {
		return nil, mapRepoErr(err)
	}
	if s.artifactRepo == nil {
		return []domain.BuildArtifact{}, nil
	}

	artifacts, err := s.artifactRepo.ListByBuildID(ctx, buildID)
	if err != nil {
		return nil, err
	}

	return artifacts, nil
}

func (s *BuildService) OpenBuildArtifact(ctx context.Context, buildID string, artifactID string) (domain.BuildArtifact, io.ReadCloser, error) {
	if _, err := s.buildRepo.GetByID(ctx, buildID); err != nil {
		return domain.BuildArtifact{}, nil, mapRepoErr(err)
	}
	if s.artifactRepo == nil || s.artifactStore == nil {
		return domain.BuildArtifact{}, nil, ErrArtifactNotFound
	}

	meta, err := s.artifactRepo.GetByID(ctx, buildID, artifactID)
	if err != nil {
		if errors.Is(err, repository.ErrArtifactNotFound) {
			return domain.BuildArtifact{}, nil, ErrArtifactNotFound
		}
		return domain.BuildArtifact{}, nil, err
	}

	stream, err := s.artifactStore.Open(ctx, meta.StorageKey)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return domain.BuildArtifact{}, nil, ErrArtifactNotFound
		}
		return domain.BuildArtifact{}, nil, err
	}

	return meta, stream, nil
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

	build, err := s.buildRepo.GetByID(ctx, request.BuildID)
	if err != nil {
		if errors.Is(err, repository.ErrBuildNotFound) {
			return runner.RunStepResult{}, StepCompletionReport{}, ErrBuildNotFound
		}
		return runner.RunStepResult{}, StepCompletionReport{}, fmt.Errorf("fetching build for step execution: %w", err)
	}

	if buildScopedRunner, ok := s.runner.(runner.BuildScopedRunner); ok {
		executionImage := s.resolveExecutionImage(build)
		prepareErr := buildScopedRunner.PrepareBuild(ctx, runner.PrepareBuildRequest{
			BuildID:    request.BuildID,
			RepoURL:    readOptionalString(build.RepoURL),
			Ref:        readOptionalString(build.Ref),
			CommitSHA:  readOptionalString(build.CommitSHA),
			Image:      executionImage,
			WorkerID:   request.WorkerID,
			ClaimToken: request.ClaimToken,
		})
		if prepareErr != nil {
			now := time.Now().UTC()
			result := runner.RunStepResult{
				Status:     runner.RunStepStatusFailed,
				ExitCode:   -1,
				Stderr:     prepareErr.Error(),
				StartedAt:  now,
				FinishedAt: now,
			}

			completionReport, completionErr := s.handleStepResult(ctx, request, result, false)
			if completionErr != nil {
				return result, completionReport, errors.Join(prepareErr, completionErr)
			}

			if sideEffectErr := s.runPostCompletionSideEffects(ctx, request.BuildID); sideEffectErr != nil {
				completionReport.SideEffectErr = joinSideEffectErrors(completionReport.SideEffectErr, sideEffectErr)
			}

			return result, completionReport, prepareErr
		}
	} else if readOptionalString(build.RepoURL) != "" {
		// Non-build-scoped runners cannot prepare a workspace for repo-backed builds.
		return runner.RunStepResult{}, StepCompletionReport{}, ErrRunnerWorkspaceNotSupported
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
	var runErr error
	usedStreamingRunner := false
	if streamingRunner, ok := s.runner.(runner.StreamingRunner); ok {
		usedStreamingRunner = true
		result, runErr = streamingRunner.RunStepStream(ctx, request, persistChunk)
	} else {
		result, runErr = s.runner.RunStep(ctx, request)
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
	if runErr != nil {
		now := time.Now().UTC()
		result = runner.RunStepResult{
			Status:     runner.RunStepStatusFailed,
			ExitCode:   -1,
			Stderr:     runErr.Error(),
			StartedAt:  now,
			FinishedAt: now,
		}
		completionReport, completionErr := s.handleStepResult(ctx, request, result, hasChunkAppender)
		if completionErr != nil {
			return result, completionReport, errors.Join(runErr, completionErr)
		}
		if sideEffectErr := s.runPostCompletionSideEffects(ctx, request.BuildID); sideEffectErr != nil {
			completionReport.SideEffectErr = joinSideEffectErrors(completionReport.SideEffectErr, sideEffectErr)
		}
		chunkPersistMu.Lock()
		if completionReport.SideEffectErr == nil && chunkPersistErr != nil {
			completionReport.SideEffectErr = chunkPersistErr
		}
		chunkPersistMu.Unlock()
		return result, completionReport, runErr
	}

	completionReport, completionErr := s.handleStepResult(ctx, request, result, hasChunkAppender)
	if completionErr != nil {
		return result, completionReport, completionErr
	}
	if sideEffectErr := s.runPostCompletionSideEffects(ctx, request.BuildID); sideEffectErr != nil {
		completionReport.SideEffectErr = joinSideEffectErrors(completionReport.SideEffectErr, sideEffectErr)
	}
	chunkPersistMu.Lock()
	if completionReport.SideEffectErr == nil && chunkPersistErr != nil {
		completionReport.SideEffectErr = chunkPersistErr
	}
	chunkPersistMu.Unlock()

	return result, completionReport, nil
}

func (s *BuildService) runPostCompletionSideEffects(ctx context.Context, buildID string) error {
	artifactErr := s.collectArtifactsIfTerminal(ctx, buildID)
	if artifactErr != nil {
		return artifactErr
	}

	return s.cleanupExecutionIfTerminal(ctx, buildID)
}

func (s *BuildService) collectArtifactsIfTerminal(ctx context.Context, buildID string) error {
	if s.artifactRepo == nil || s.artifactCollector == nil || strings.TrimSpace(s.artifactWorkspaceRoot) == "" {
		return nil
	}

	build, err := s.buildRepo.GetByID(ctx, buildID)
	if err != nil {
		return fmt.Errorf("fetching build for artifact collection: %w", err)
	}
	if !domain.IsTerminalBuildStatus(build.Status) {
		return nil
	}

	existing, err := s.artifactRepo.ListByBuildID(ctx, buildID)
	if err != nil {
		return fmt.Errorf("checking existing artifacts: %w", err)
	}
	existingPaths := make(map[string]struct{}, len(existing))
	for _, item := range existing {
		existingPaths[item.LogicalPath] = struct{}{}
	}

	patterns, err := artifactPatternsFromBuild(build)
	if err != nil {
		return fmt.Errorf("resolving build artifact declarations: %w", err)
	}
	if len(patterns) == 0 {
		return nil
	}

	workspacePath := filepath.Join(s.artifactWorkspaceRoot, strings.TrimSpace(buildID))
	log.Printf("artifact collection start: build_id=%s workspace_path=%s declared_patterns=%q storage_root=%s", buildID, workspacePath, patterns, artifactStoreRootForLog(s.artifactStore))
	collectResult, err := s.artifactCollector.Collect(ctx, artifact.CollectRequest{
		BuildID:          buildID,
		WorkspacePath:    workspacePath,
		Patterns:         patterns,
		SkipLogicalPaths: existingPaths,
	})
	if err != nil {
		log.Printf("artifact collection error: build_id=%s workspace_path=%s err=%v", buildID, workspacePath, err)
		return err
	}
	for _, warning := range collectResult.Warnings {
		log.Printf("artifact collection warning: build_id=%s %s", buildID, warning)
	}
	log.Printf("artifact metadata persistence start: build_id=%s artifacts_to_persist=%d", buildID, len(collectResult.Artifacts))

	for _, item := range collectResult.Artifacts {
		log.Printf("artifact metadata persist: build_id=%s logical_path=%s storage_key=%s size_bytes=%d", buildID, item.LogicalPath, item.StorageKey, item.SizeBytes)
		_, err := s.artifactRepo.Create(ctx, domain.BuildArtifact{
			ID:             uuid.NewString(),
			BuildID:        buildID,
			LogicalPath:    item.LogicalPath,
			StorageKey:     item.StorageKey,
			SizeBytes:      item.SizeBytes,
			ContentType:    item.ContentType,
			ChecksumSHA256: item.ChecksumSHA256,
			CreatedAt:      time.Now().UTC(),
		})
		if err != nil {
			log.Printf("artifact metadata persistence error: build_id=%s logical_path=%s err=%v", buildID, item.LogicalPath, err)
			return fmt.Errorf("persisting artifact metadata: %w", err)
		}
	}
	log.Printf("artifact metadata persistence complete: build_id=%s persisted=%d", buildID, len(collectResult.Artifacts))

	return nil
}

func artifactStoreRootForLog(store artifact.Store) string {
	if store == nil {
		return ""
	}
	if reporter, ok := store.(interface{ RootPath() string }); ok {
		return strings.TrimSpace(reporter.RootPath())
	}
	return ""
}

func artifactPatternsFromBuild(build domain.Build) ([]string, error) {
	if build.PipelineConfigYAML == nil {
		return nil, nil
	}
	trimmed := strings.TrimSpace(*build.PipelineConfigYAML)
	if trimmed == "" {
		return nil, nil
	}

	pipelineFile, err := pipeline.ParseAndValidate([]byte(trimmed))
	if err != nil {
		return nil, err
	}

	return pipelineFile.Artifacts.Paths, nil
}

func readOptionalString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func (s *BuildService) resolveExecutionImage(build domain.Build) string {
	defaultImage := strings.TrimSpace(s.defaultExecutionImage)
	if build.PipelineConfigYAML == nil {
		return defaultImage
	}

	yamlText := strings.TrimSpace(*build.PipelineConfigYAML)
	if yamlText == "" {
		return defaultImage
	}

	resolved, err := pipeline.LoadAndResolve([]byte(yamlText))
	if err != nil {
		return defaultImage
	}

	if resolved.Image != "" {
		return resolved.Image
	}

	return defaultImage
}

func (s *BuildService) cleanupExecutionIfTerminal(ctx context.Context, buildID string) error {
	buildScopedRunner, ok := s.runner.(runner.BuildScopedRunner)
	if !ok {
		return nil
	}

	build, err := s.buildRepo.GetByID(ctx, buildID)
	if err != nil {
		return fmt.Errorf("fetching build for cleanup check: %w", err)
	}
	if !domain.IsTerminalBuildStatus(build.Status) {
		return nil
	}

	return buildScopedRunner.CleanupBuild(ctx, buildID)
}

func joinSideEffectErrors(existing error, additional error) error {
	if additional == nil {
		return existing
	}
	if existing == nil {
		return additional
	}
	return errors.Join(existing, additional)
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
