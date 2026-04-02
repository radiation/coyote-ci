package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
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
	"github.com/radiation/coyote-ci/backend/internal/workspace"
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
var ErrSourceTargetRequired = errors.New("ref or commit_sha is required")
var ErrRepoFetcherNotConfigured = errors.New("repo fetcher not configured")
var ErrPipelineFileNotFound = errors.New("pipeline file not found in repository")
var ErrInvalidPipelinePath = errors.New("invalid pipeline path")
var ErrArtifactNotFound = errors.New("artifact not found")
var ErrSourceResolverNotConfigured = errors.New("source resolver not configured")
var ErrExecutionWorkspaceRootNotConfigured = errors.New("execution workspace root not configured")

const (
	BuildTemplateDefault = "default"
	BuildTemplateTest    = "test"
	BuildTemplateBuild   = "build"
	BuildTemplateCustom  = "custom"
	BuildTemplateFail    = "fail"
)

// BuildService coordinates build lifecycle state transitions and delegates step execution to a runner.
type BuildService struct {
	buildRepo              repository.BuildRepository
	executionJobRepo       repository.ExecutionJobRepository
	executionPlanner       *BuildExecutionPlanner
	runner                 runner.Runner
	logSink                logs.LogSink
	repoFetcher            source.RepoFetcher
	sourceResolver         source.WorkspaceSourceResolver
	executionWorkspaceRoot string

	artifactRepo          repository.ArtifactRepository
	executionOutputRepo   repository.ExecutionJobOutputRepository
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
		buildRepo:        buildRepo,
		executionPlanner: NewBuildExecutionPlanner(),
		runner:           stepRunner,
		logSink:          logSink,
		sourceResolver:   source.NewGitWorkspaceSourceResolver(),
	}
}

// SetRepoFetcher attaches a RepoFetcher for repo-backed build creation.
func (s *BuildService) SetRepoFetcher(fetcher source.RepoFetcher) {
	s.repoFetcher = fetcher
}

func (s *BuildService) SetSourceResolver(resolver source.WorkspaceSourceResolver) {
	s.sourceResolver = resolver
}

func (s *BuildService) SetExecutionWorkspaceRoot(root string) {
	s.executionWorkspaceRoot = normalizeWorkspaceRoot(root)
}

// SetDefaultExecutionImage sets the image used when a build-scoped runner requires one.
func (s *BuildService) SetDefaultExecutionImage(image string) {
	s.defaultExecutionImage = strings.TrimSpace(image)
}

// SetArtifactPersistence configures build artifact persistence dependencies.
func (s *BuildService) SetArtifactPersistence(repo repository.ArtifactRepository, store artifact.Store, workspaceRoot string) {
	s.artifactRepo = repo
	s.artifactStore = store
	s.artifactWorkspaceRoot = normalizeWorkspaceRoot(workspaceRoot)
	if s.executionWorkspaceRoot == "" {
		s.executionWorkspaceRoot = s.artifactWorkspaceRoot
	}
	if store != nil {
		s.artifactCollector = artifact.NewCollector(store)
	} else {
		s.artifactCollector = nil
	}
}

func (s *BuildService) SetExecutionJobRepository(repo repository.ExecutionJobRepository) {
	s.executionJobRepo = repo
}

func (s *BuildService) SetExecutionJobOutputRepository(repo repository.ExecutionJobOutputRepository) {
	s.executionOutputRepo = repo
}

type CreateBuildInput struct {
	ProjectID string
	Steps     []CreateBuildStepInput
	Source    *CreateBuildSourceInput
}

type CreateBuildSourceInput struct {
	RepositoryURL string
	Ref           string
	CommitSHA     string
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

type stepFailureKind string

const (
	stepFailureKindNone     stepFailureKind = "none"
	stepFailureKindExitCode stepFailureKind = "exit_code"
	stepFailureKindTimeout  stepFailureKind = "timeout"
	stepFailureKindInternal stepFailureKind = "internal"
)

func (s *BuildService) CreateBuild(ctx context.Context, input CreateBuildInput) (domain.Build, error) {
	if input.ProjectID == "" {
		return domain.Build{}, ErrProjectIDRequired
	}

	sourceSpec, err := toDomainSourceSpec(input.Source)
	if err != nil {
		return domain.Build{}, err
	}

	build := domain.Build{
		ID:               uuid.NewString(),
		ProjectID:        input.ProjectID,
		Status:           domain.BuildStatusPending,
		CreatedAt:        time.Now().UTC(),
		CurrentStepIndex: 0,
		Source:           sourceSpec,
		RepoURL:          optionalStringPtr(sourceRepositoryURL(sourceSpec)),
		Ref:              sourceRef(sourceSpec),
		CommitSHA:        sourceCommitSHA(sourceSpec),
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

		queuedBuild, err := s.buildRepo.CreateQueuedBuild(ctx, build, steps)
		if err != nil {
			return domain.Build{}, err
		}
		if err := s.createDurableJobsForBuild(ctx, queuedBuild, steps); err != nil {
			return domain.Build{}, err
		}
		return queuedBuild, nil
	}

	return s.buildRepo.Create(ctx, build)
}

// CreatePipelineBuildInput is the service-level input for creating a build from pipeline YAML.
type CreatePipelineBuildInput struct {
	ProjectID    string
	PipelineYAML string
	Source       *CreateBuildSourceInput
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

	sourceSpec, err := toDomainSourceSpec(input.Source)
	if err != nil {
		return domain.Build{}, err
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
	pipelineSource := pipelineSourceInline

	build := domain.Build{
		ID:                 buildID,
		ProjectID:          input.ProjectID,
		Status:             domain.BuildStatusQueued,
		CreatedAt:          time.Now().UTC(),
		CurrentStepIndex:   0,
		PipelineConfigYAML: &yamlText,
		PipelineName:       pipelineNamePtr,
		PipelineSource:     &pipelineSource,
		Source:             sourceSpec,
		RepoURL:            optionalStringPtr(sourceRepositoryURL(sourceSpec)),
		Ref:                sourceRef(sourceSpec),
		CommitSHA:          sourceCommitSHA(sourceSpec),
	}

	queuedBuild, err := s.buildRepo.CreateQueuedBuild(ctx, build, steps)
	if err != nil {
		return domain.Build{}, err
	}
	if err := s.createDurableJobsForBuild(ctx, queuedBuild, steps); err != nil {
		return domain.Build{}, err
	}
	return queuedBuild, nil
}

// CreateRepoBuildInput is the service-level input for creating a build from a repository checkout.
type CreateRepoBuildInput struct {
	ProjectID    string
	RepoURL      string
	Ref          string
	CommitSHA    string
	PipelinePath string
}

const pipelineFilePath = ".coyote/pipeline.yml"
const pipelineSourceRepo = "repo"
const pipelineSourceInline = "inline"

// CreateBuildFromRepo clones the repo, loads .coyote/pipeline.yml, parses/validates/resolves
// it, then creates a queued build with canonical build steps and repo source metadata.
func (s *BuildService) CreateBuildFromRepo(ctx context.Context, input CreateRepoBuildInput) (domain.Build, error) {
	if input.ProjectID == "" {
		return domain.Build{}, ErrProjectIDRequired
	}
	if strings.TrimSpace(input.RepoURL) == "" {
		return domain.Build{}, ErrRepoURLRequired
	}
	if strings.TrimSpace(input.Ref) == "" && strings.TrimSpace(input.CommitSHA) == "" {
		return domain.Build{}, ErrSourceTargetRequired
	}
	if s.repoFetcher == nil {
		return domain.Build{}, ErrRepoFetcherNotConfigured
	}

	fetchTarget := strings.TrimSpace(input.CommitSHA)
	if fetchTarget == "" {
		fetchTarget = strings.TrimSpace(input.Ref)
	}

	localPath, commitSHA, err := s.repoFetcher.Fetch(ctx, input.RepoURL, fetchTarget)
	if err != nil {
		return domain.Build{}, fmt.Errorf("fetching repo: %w", err)
	}
	defer func() {
		if localPath != "" {
			_ = os.RemoveAll(localPath)
		}
	}()

	absPipelinePath, effectivePipelinePath, err := resolveRepoPipelinePath(localPath, input.PipelinePath)
	if err != nil {
		return domain.Build{}, err
	}

	src := pipeline.FileSource{Path: absPipelinePath}
	yamlData, _, err := src.Load()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return domain.Build{}, fmt.Errorf("%w: %s", ErrPipelineFileNotFound, effectivePipelinePath)
		}
		return domain.Build{}, fmt.Errorf("loading pipeline file: %w", err)
	}

	resolved, err := pipeline.LoadAndResolve(yamlData)
	if err != nil {
		return domain.Build{}, err
	}

	resolved, err = resolveRepoStepWorkingDirs(effectivePipelinePath, resolved)
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
	pipelineSource := pipelineSourceRepo
	pipelinePath := effectivePipelinePath

	repoURL := strings.TrimSpace(input.RepoURL)
	ref := strings.TrimSpace(input.Ref)
	requestedCommitSHA := strings.TrimSpace(input.CommitSHA)
	var commitSHAPtr *string
	if commitSHA != "" {
		commitSHAPtr = &commitSHA
	}

	sourceCommitValue := requestedCommitSHA
	if commitSHA != "" {
		sourceCommitValue = commitSHA
	}
	domainSource := domain.NewSourceSpec(repoURL, ref, sourceCommitValue)

	build := domain.Build{
		ID:                 buildID,
		ProjectID:          input.ProjectID,
		Status:             domain.BuildStatusQueued,
		CreatedAt:          time.Now().UTC(),
		CurrentStepIndex:   0,
		PipelineConfigYAML: &yamlText,
		PipelineName:       pipelineNamePtr,
		PipelineSource:     &pipelineSource,
		PipelinePath:       &pipelinePath,
		Source:             domainSource,
		RepoURL:            &repoURL,
		Ref:                optionalStringPtr(ref),
		CommitSHA:          commitSHAPtr,
	}

	queuedBuild, err := s.buildRepo.CreateQueuedBuild(ctx, build, steps)
	if err != nil {
		return domain.Build{}, err
	}
	if err := s.createDurableJobsForBuild(ctx, queuedBuild, steps); err != nil {
		return domain.Build{}, err
	}
	return queuedBuild, nil
}

func (s *BuildService) createDurableJobsForBuild(ctx context.Context, build domain.Build, steps []domain.BuildStep) error {
	if s.executionJobRepo == nil || s.executionPlanner == nil || len(steps) == 0 {
		return nil
	}

	jobs, err := s.executionPlanner.Plan(build, steps, s.resolveExecutionImage(build))
	if err != nil {
		return err
	}
	_, err = s.executionJobRepo.CreateJobsForBuild(ctx, jobs)
	if err != nil {
		return err
	}

	if s.executionOutputRepo == nil || len(jobs) == 0 {
		return nil
	}

	declaredOutputs, outputErr := s.declaredOutputsForBuild(build, jobs)
	if outputErr != nil {
		return outputErr
	}
	if len(declaredOutputs) == 0 {
		return nil
	}
	_, outputErr = s.executionOutputRepo.CreateMany(ctx, declaredOutputs)
	return outputErr
}

func (s *BuildService) declaredOutputsForBuild(build domain.Build, jobs []domain.ExecutionJob) ([]domain.ExecutionJobOutput, error) {
	if build.PipelineConfigYAML == nil || strings.TrimSpace(*build.PipelineConfigYAML) == "" {
		return []domain.ExecutionJobOutput{}, nil
	}

	resolved, err := pipeline.LoadAndResolve([]byte(strings.TrimSpace(*build.PipelineConfigYAML)))
	if err != nil {
		return []domain.ExecutionJobOutput{}, nil
	}
	if len(resolved.Artifacts.Paths) == 0 {
		return []domain.ExecutionJobOutput{}, nil
	}

	// Build-level artifacts are declared against the final execution job in the current sequential model.
	lastJob := jobs[len(jobs)-1]
	outputs := make([]domain.ExecutionJobOutput, 0, len(resolved.Artifacts.Paths))
	for idx, item := range resolved.Artifacts.Paths {
		name := strings.TrimSpace(item)
		if name == "" {
			continue
		}
		outputs = append(outputs, domain.ExecutionJobOutput{
			ID:           uuid.NewString(),
			JobID:        lastJob.ID,
			BuildID:      build.ID,
			Name:         "output-" + strconv.Itoa(idx+1),
			Kind:         "artifact",
			DeclaredPath: name,
			Status:       domain.ExecutionJobOutputStatusDeclared,
			CreatedAt:    time.Now().UTC(),
		})
	}

	return outputs, nil
}

func resolveRepoPipelinePath(repoRoot string, requestedPath string) (string, string, error) {
	trimmed := strings.TrimSpace(requestedPath)
	if trimmed == "" {
		trimmed = pipelineFilePath
	}

	if filepath.IsAbs(trimmed) {
		return "", "", fmt.Errorf("%w: must be a relative path", ErrInvalidPipelinePath)
	}

	cleaned := filepath.Clean(trimmed)
	if cleaned == "." {
		return "", "", fmt.Errorf("%w: must point to a file", ErrInvalidPipelinePath)
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(os.PathSeparator)) {
		return "", "", fmt.Errorf("%w: must stay within repository root", ErrInvalidPipelinePath)
	}
	if filepath.VolumeName(cleaned) != "" {
		return "", "", fmt.Errorf("%w: must not include a volume prefix", ErrInvalidPipelinePath)
	}

	abs := filepath.Join(repoRoot, cleaned)
	rel, err := filepath.Rel(repoRoot, abs)
	if err != nil {
		return "", "", fmt.Errorf("%w: unable to resolve path", ErrInvalidPipelinePath)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", "", fmt.Errorf("%w: must stay within repository root", ErrInvalidPipelinePath)
	}

	normalized := filepath.ToSlash(cleaned)
	return abs, normalized, nil
}

func resolveRepoStepWorkingDirs(pipelinePath string, resolved *pipeline.ResolvedPipeline) (*pipeline.ResolvedPipeline, error) {
	if resolved == nil {
		return nil, fmt.Errorf("%w: pipeline is required", ErrInvalidPipelinePath)
	}

	normalizedPipelinePath := path.Clean(filepath.ToSlash(strings.TrimSpace(pipelinePath)))
	pipelineDir := "."
	if normalizedPipelinePath != pipelineFilePath {
		pipelineDir = path.Clean(path.Dir(normalizedPipelinePath))
		if pipelineDir == "" {
			pipelineDir = "."
		}
	}

	for i := range resolved.Steps {
		stepDir := strings.TrimSpace(resolved.Steps[i].WorkingDir)
		if stepDir == "" || stepDir == "." {
			resolved.Steps[i].WorkingDir = pipelineDir
			continue
		}

		if path.IsAbs(stepDir) {
			return nil, fmt.Errorf("%w: steps[%d].working_dir must be relative", ErrInvalidPipelinePath, i)
		}

		normalizedStepDir := path.Clean(strings.ReplaceAll(stepDir, "\\", "/"))
		if normalizedStepDir == ".." || strings.HasPrefix(normalizedStepDir, "../") {
			return nil, fmt.Errorf("%w: steps[%d].working_dir escapes repository root", ErrInvalidPipelinePath, i)
		}

		combined := path.Clean(path.Join(pipelineDir, normalizedStepDir))
		if combined == ".." || strings.HasPrefix(combined, "../") {
			return nil, fmt.Errorf("%w: steps[%d].working_dir escapes repository root", ErrInvalidPipelinePath, i)
		}

		resolved.Steps[i].WorkingDir = combined
	}

	return resolved, nil
}

func (s *BuildService) RunStep(ctx context.Context, request runner.RunStepRequest) (runner.RunStepResult, StepCompletionReport, error) {
	if s.runner == nil {
		return runner.RunStepResult{}, StepCompletionReport{CompletionOutcome: repository.StepCompletionInvalidTransition}, ErrRunnerNotConfigured
	}

	request, persistedJob := s.bindRequestToPersistedJob(ctx, request)

	build, err := s.buildRepo.GetByID(ctx, request.BuildID)
	if err != nil {
		if errors.Is(err, repository.ErrBuildNotFound) {
			return runner.RunStepResult{}, StepCompletionReport{}, ErrBuildNotFound
		}
		return runner.RunStepResult{}, StepCompletionReport{}, fmt.Errorf("fetching build for step execution: %w", err)
	}
	executionImage := s.resolveExecutionImage(build)
	buildSource := sourceSpecFromBuild(build)
	if persistedJob != nil {
		executionImage = persistedJob.Image
		buildSource = sourceSpecFromJob(*persistedJob)
	}

	steps, err := s.buildRepo.GetStepsByBuildID(ctx, request.BuildID)
	if err != nil {
		return runner.RunStepResult{}, StepCompletionReport{}, fmt.Errorf("fetching build steps for step execution: %w", mapRepoErr(err))
	}
	totalSteps := len(steps)
	if totalSteps <= 0 {
		totalSteps = request.StepIndex + 1
	}
	if totalSteps <= 0 {
		totalSteps = 1
	}
	stepNumber := request.StepIndex + 1
	if stepNumber <= 0 {
		stepNumber = 1
	}

	stepWorkingDir := workspace.New(request.BuildID, "").ContainerWorkingDir(request.WorkingDir)
	stepCommand := runner.RenderStepCommand(request.Command, request.Args)

	var chunkAppender logs.StepLogChunkAppender
	if appender, ok := s.logSink.(logs.StepLogChunkAppender); ok {
		chunkAppender = appender
	}

	var visibilityLogErr error
	emitVisibilityLine := func(line string) {
		if err := s.writeSystemExecutionLogLine(ctx, request, chunkAppender, line); err != nil && visibilityLogErr == nil {
			visibilityLogErr = err
		}
	}
	emitVisibilityLines := func(lines []string) {
		for _, line := range lines {
			emitVisibilityLine(line)
		}
	}

	if buildScopedRunner, ok := s.runner.(runner.BuildScopedRunner); ok {
		emitVisibilityLine("Preparing workspace")
		prepareErr := buildScopedRunner.PrepareBuild(ctx, runner.PrepareBuildRequest{
			BuildID:    request.BuildID,
			RepoURL:    buildSource.RepositoryURL,
			Ref:        buildSource.Ref,
			CommitSHA:  buildSource.CommitSHA,
			Image:      executionImage,
			WorkerID:   request.WorkerID,
			ClaimToken: request.ClaimToken,
		})
		if prepareErr != nil {
			_, reason := classifyPrepareFailure(prepareErr)
			emitVisibilityLine("Failed to start build container")
			emitVisibilityLine(formatFailureReasonLine(reason))

			now := time.Now().UTC()
			result := runner.RunStepResult{
				Status:     runner.RunStepStatusFailed,
				ExitCode:   -1,
				Stderr:     reason,
				StartedAt:  now,
				FinishedAt: now,
			}

			completionReport, completionErr := s.handleStepResult(ctx, request, result, false)
			if completionErr != nil {
				return result, completionReport, errors.Join(prepareErr, completionErr)
			}

			if sideEffectErr := s.runPostCompletionSideEffects(ctx, request, nil); sideEffectErr != nil {
				completionReport.SideEffectErr = joinSideEffectErrors(completionReport.SideEffectErr, sideEffectErr)
			}
			if visibilityLogErr != nil {
				completionReport.SideEffectErr = joinSideEffectErrors(completionReport.SideEffectErr, visibilityLogErr)
			}

			return result, completionReport, prepareErr
		}

		if buildSource.HasSource && stepNumber == 1 {
			emitVisibilityLine("Resolving source")
			emitVisibilityLine("Cloning repository")
			if buildSource.CommitSHA != "" {
				emitVisibilityLine("Checking out commit: " + buildSource.CommitSHA)
			} else {
				emitVisibilityLine("Checking out ref: " + buildSource.Ref)
			}

			resolvedCommit, sourceErr := s.resolveBuildSourceIntoWorkspace(ctx, request.BuildID, buildSource)
			if sourceErr != nil {
				reason := classifySourceFailureReason(sourceErr, buildSource)
				emitVisibilityLine("Source checkout failed")
				emitVisibilityLine(formatFailureReasonLine(reason))

				now := time.Now().UTC()
				result := runner.RunStepResult{
					Status:     runner.RunStepStatusFailed,
					ExitCode:   -1,
					Stderr:     reason,
					StartedAt:  now,
					FinishedAt: now,
				}

				completionReport, completionErr := s.handleStepResult(ctx, request, result, false)
				if completionErr != nil {
					return result, completionReport, errors.Join(sourceErr, completionErr)
				}

				if sideEffectErr := s.runPostCompletionSideEffects(ctx, request, nil); sideEffectErr != nil {
					completionReport.SideEffectErr = joinSideEffectErrors(completionReport.SideEffectErr, sideEffectErr)
				}
				if visibilityLogErr != nil {
					completionReport.SideEffectErr = joinSideEffectErrors(completionReport.SideEffectErr, visibilityLogErr)
				}

				return result, completionReport, sourceErr
			}

			emitVisibilityLine("Resolved commit: " + resolvedCommit)
		}

		emitVisibilityLine("Starting build container")
	} else if buildSource.HasSource {
		// Non-build-scoped runners cannot prepare a workspace for repo-backed builds.
		return runner.RunStepResult{}, StepCompletionReport{}, ErrRunnerWorkspaceNotSupported
	}

	hasChunkAppender := chunkAppender != nil

	var chunkPersistErr error
	var chunkPersistMu sync.Mutex

	persistChunk := func(chunk runner.StepOutputChunk) error {
		if !hasChunkAppender {
			return nil
		}
		if chunkAppender == nil {
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

		_, appendErr := chunkAppender.AppendStepLogChunk(ctx, logs.StepLogChunk{
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
	applyBufferedLogErrors := func(report *StepCompletionReport) {
		chunkPersistMu.Lock()
		capturedChunkErr := chunkPersistErr
		chunkPersistMu.Unlock()
		if capturedChunkErr != nil {
			report.SideEffectErr = joinSideEffectErrors(report.SideEffectErr, capturedChunkErr)
		}
		if visibilityLogErr != nil {
			report.SideEffectErr = joinSideEffectErrors(report.SideEffectErr, visibilityLogErr)
		}
	}

	if stepNumber == 1 && (build.StartedAt == nil || build.StartedAt.IsZero()) {
		emitVisibilityLines(formatBuildStartLines(executionImage, workspace.DefaultContainerRoot, totalSteps))
	}
	if stepNumber == 1 {
		emitVisibilityLine("Executing pipeline steps")
	}
	emitVisibilityLines(formatStepStartLines(stepNumber, totalSteps, request.StepName, executionImage, stepWorkingDir, stepCommand))

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
		failureKind, failureReason := classifyStepFailure(result)
		emitVisibilityLine(formatFailureStepEndLine(stepNumber, totalSteps, request.StepName, result.FinishedAt.Sub(result.StartedAt), result.ExitCode, failureKind))
		emitVisibilityLine(formatFailureReasonLine(failureReason))
		completionReport, completionErr := s.handleStepResult(ctx, request, result, hasChunkAppender)
		if completionErr != nil {
			return result, completionReport, errors.Join(runErr, completionErr)
		}
		if sideEffectErr := s.runPostCompletionSideEffects(ctx, request, chunkAppender); sideEffectErr != nil {
			completionReport.SideEffectErr = joinSideEffectErrors(completionReport.SideEffectErr, sideEffectErr)
		}
		applyBufferedLogErrors(&completionReport)
		return result, completionReport, runErr
	}

	stepStatus := "succeeded"
	if result.Status == runner.RunStepStatusFailed {
		stepStatus = "failed"
	}
	if stepStatus == "failed" {
		failureKind, failureReason := classifyStepFailure(result)
		emitVisibilityLine(formatFailureStepEndLine(stepNumber, totalSteps, request.StepName, result.FinishedAt.Sub(result.StartedAt), result.ExitCode, failureKind))
		emitVisibilityLine(formatFailureReasonLine(failureReason))
	} else {
		emitVisibilityLine(formatStepEndLine(stepNumber, totalSteps, request.StepName, stepStatus, result.FinishedAt.Sub(result.StartedAt), result.ExitCode))
	}

	completionReport, completionErr := s.handleStepResult(ctx, request, result, hasChunkAppender)
	if completionErr != nil {
		return result, completionReport, completionErr
	}
	if sideEffectErr := s.runPostCompletionSideEffects(ctx, request, chunkAppender); sideEffectErr != nil {
		completionReport.SideEffectErr = joinSideEffectErrors(completionReport.SideEffectErr, sideEffectErr)
	}
	applyBufferedLogErrors(&completionReport)

	return result, completionReport, nil
}

func (s *BuildService) bindRequestToPersistedJob(ctx context.Context, request runner.RunStepRequest) (runner.RunStepRequest, *domain.ExecutionJob) {
	if s.executionJobRepo == nil {
		return request, nil
	}

	var (
		job domain.ExecutionJob
		err error
	)

	jobID := strings.TrimSpace(request.JobID)
	if jobID != "" {
		job, err = s.executionJobRepo.GetJobByID(ctx, jobID)
	} else if strings.TrimSpace(request.StepID) != "" {
		job, err = s.executionJobRepo.GetJobByStepID(ctx, request.StepID)
	}
	if err != nil {
		return request, nil
	}

	request.JobID = job.ID
	request.StepID = defaultString(request.StepID, job.StepID)
	request.StepIndex = job.StepIndex
	request.StepName = defaultString(job.Name, request.StepName)
	if len(job.Command) > 0 {
		request.Command = defaultString(job.Command[0], "sh")
		if len(job.Command) > 1 {
			request.Args = append([]string(nil), job.Command[1:]...)
		} else {
			request.Args = []string{}
		}
	}
	request.Env = cloneEnv(job.Environment)
	request.WorkingDir = defaultString(job.WorkingDir, ".")
	if job.TimeoutSeconds != nil {
		request.TimeoutSeconds = maxInt(*job.TimeoutSeconds, 0)
	}

	if job.ClaimToken != nil && strings.TrimSpace(request.ClaimToken) == "" {
		request.ClaimToken = *job.ClaimToken
	}

	return request, &job
}

func sourceSpecFromJob(job domain.ExecutionJob) resolvedBuildSourceSpec {
	return resolvedBuildSourceSpec{
		HasSource:     strings.TrimSpace(job.Source.RepositoryURL) != "",
		RepositoryURL: strings.TrimSpace(job.Source.RepositoryURL),
		Ref:           optionalStringValue(job.Source.RefName),
		CommitSHA:     strings.TrimSpace(job.Source.CommitSHA),
	}
}

func optionalStringValue(value *string) string {
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
