package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
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
var ErrSourceTargetRequired = errors.New("ref or commit_sha is required")
var ErrRepoFetcherNotConfigured = errors.New("repo fetcher not configured")
var ErrPipelineFileNotFound = errors.New("pipeline file not found in repository")
var ErrInvalidPipelinePath = errors.New("invalid pipeline path")
var ErrArtifactNotFound = errors.New("artifact not found")
var ErrArtifactStorageProviderNotConfigured = errors.New("artifact storage provider not configured")
var ErrSourceResolverNotConfigured = errors.New("source resolver not configured")
var ErrExecutionWorkspaceRootNotConfigured = errors.New("execution workspace root not configured")
var ErrExecutionJobRepoNotConfigured = errors.New("execution job repository not configured")
var ErrExecutionJobNotFound = errors.New("execution job not found")
var ErrExecutionJobNotRetryable = errors.New("execution job is not retryable")
var ErrInvalidRerunStepIndex = errors.New("invalid rerun step index")

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

	artifactRepo            repository.ArtifactRepository
	executionOutputRepo     repository.ExecutionJobOutputRepository
	artifactStore           artifact.Store
	artifactStoreResolver   *artifact.StoreResolver
	artifactCollector       *artifact.Collector
	artifactWorkspaceRoot   string
	artifactStorageProvider domain.StorageProvider

	defaultExecutionImage string
}

// BuildServiceConfig groups all optional dependencies for BuildService. Zero
// values are safe — each field is only used when set.
type BuildServiceConfig struct {
	ExecutionJobRepo    repository.ExecutionJobRepository
	ExecutionOutputRepo repository.ExecutionJobOutputRepository
	RepoFetcher         source.RepoFetcher
	SourceResolver      source.WorkspaceSourceResolver
	ArtifactRepo        repository.ArtifactRepository
	ArtifactResolver    *artifact.StoreResolver
	ArtifactWorkspace   string
	ExecutionWorkspace  string
	DefaultImage        string
}

// NewBuildServiceFromConfig creates a fully-wired BuildService in one call.
func NewBuildServiceFromConfig(buildRepo repository.BuildRepository, stepRunner runner.Runner, logSink logs.LogSink, cfg BuildServiceConfig) *BuildService {
	svc := NewBuildService(buildRepo, stepRunner, logSink)
	svc.executionJobRepo = cfg.ExecutionJobRepo
	svc.executionOutputRepo = cfg.ExecutionOutputRepo
	svc.repoFetcher = cfg.RepoFetcher
	if cfg.SourceResolver != nil {
		svc.sourceResolver = cfg.SourceResolver
	}
	svc.defaultExecutionImage = strings.TrimSpace(cfg.DefaultImage)
	svc.executionWorkspaceRoot = normalizeWorkspaceRoot(cfg.ExecutionWorkspace)
	svc.SetArtifactPersistence(cfg.ArtifactRepo, cfg.ArtifactResolver, cfg.ArtifactWorkspace)
	return svc
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
func (s *BuildService) SetArtifactPersistence(repo repository.ArtifactRepository, resolver *artifact.StoreResolver, workspaceRoot string) {
	s.artifactRepo = repo
	s.artifactWorkspaceRoot = normalizeWorkspaceRoot(workspaceRoot)
	if resolver != nil {
		s.artifactStoreResolver = resolver
		s.artifactStore = resolver.Default()
		s.artifactStorageProvider = resolver.DefaultProvider()
		s.artifactCollector = artifact.NewCollector(resolver.Default())
	} else {
		s.artifactStoreResolver = nil
		s.artifactStore = nil
		s.artifactStorageProvider = ""
		s.artifactCollector = nil
	}
	if s.executionWorkspaceRoot == "" {
		s.executionWorkspaceRoot = s.artifactWorkspaceRoot
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
	JobID     *string
	Steps     []CreateBuildStepInput
	Source    *CreateBuildSourceInput
	Trigger   *CreateBuildTriggerInput
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
		JobID:            input.JobID,
		Status:           domain.BuildStatusPending,
		AttemptNumber:    1,
		CreatedAt:        time.Now().UTC(),
		CurrentStepIndex: 0,
		Source:           sourceSpec,
		RepoURL:          optionalStringPtr(sourceRepositoryURL(sourceSpec)),
		Ref:              sourceRef(sourceSpec),
		CommitSHA:        sourceCommitSHA(sourceSpec),
		Trigger:          toDomainBuildTrigger(input.Trigger),
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
			log.Printf("WARNING: durable job creation failed for build_id=%s (build already persisted): %v", queuedBuild.ID, err)
			return domain.Build{}, fmt.Errorf("create execution jobs for build %s: %w", queuedBuild.ID, err)
		}
		return queuedBuild, nil
	}

	return s.buildRepo.Create(ctx, build)
}

// CreatePipelineBuildInput is the service-level input for creating a build from pipeline YAML.
type CreatePipelineBuildInput struct {
	ProjectID    string
	JobID        *string
	PipelineYAML string
	Source       *CreateBuildSourceInput
	Trigger      *CreateBuildTriggerInput
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
		JobID:              input.JobID,
		Status:             domain.BuildStatusQueued,
		AttemptNumber:      1,
		CreatedAt:          time.Now().UTC(),
		CurrentStepIndex:   0,
		PipelineConfigYAML: &yamlText,
		PipelineName:       pipelineNamePtr,
		PipelineSource:     &pipelineSource,
		Source:             sourceSpec,
		RepoURL:            optionalStringPtr(sourceRepositoryURL(sourceSpec)),
		Ref:                sourceRef(sourceSpec),
		CommitSHA:          sourceCommitSHA(sourceSpec),
		Trigger:            toDomainBuildTrigger(input.Trigger),
	}

	queuedBuild, err := s.buildRepo.CreateQueuedBuild(ctx, build, steps)
	if err != nil {
		return domain.Build{}, err
	}
	if err := s.createDurableJobsForBuild(ctx, queuedBuild, steps); err != nil {
		log.Printf("WARNING: durable job creation failed for build_id=%s (build already persisted): %v", queuedBuild.ID, err)
		return domain.Build{}, fmt.Errorf("create execution jobs for build %s: %w", queuedBuild.ID, err)
	}
	return queuedBuild, nil
}

// CreateRepoBuildInput is the service-level input for creating a build from a repository checkout.
type CreateRepoBuildInput struct {
	ProjectID    string
	JobID        *string
	RepoURL      string
	Ref          string
	CommitSHA    string
	PipelinePath string
	Trigger      *CreateBuildTriggerInput
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
		JobID:              input.JobID,
		Status:             domain.BuildStatusQueued,
		AttemptNumber:      1,
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
		Trigger:            toDomainBuildTrigger(input.Trigger),
	}

	queuedBuild, err := s.buildRepo.CreateQueuedBuild(ctx, build, steps)
	if err != nil {
		return domain.Build{}, err
	}
	if err := s.createDurableJobsForBuild(ctx, queuedBuild, steps); err != nil {
		log.Printf("WARNING: durable job creation failed for build_id=%s (build already persisted): %v", queuedBuild.ID, err)
		return domain.Build{}, fmt.Errorf("create execution jobs for build %s: %w", queuedBuild.ID, err)
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

	builder := NewStepExecutionContextBuilder(s)
	executionContext, err := builder.Build(ctx, request)
	if err != nil {
		return runner.RunStepResult{}, StepCompletionReport{}, err
	}

	logManager := NewExecutionLogManager(s, executionContext)
	workspacePreparer := NewWorkspacePreparer(s)
	completionManager := NewStepCompletionManager(s)
	stepRunner := NewStepRunner(s.runner)

	earlyResult, earlyErr, prepareErr := workspacePreparer.Prepare(ctx, executionContext, logManager)
	if prepareErr != nil {
		return runner.RunStepResult{}, StepCompletionReport{}, prepareErr
	}
	if earlyResult != nil {
		report, completionErr := completionManager.CompleteEarlyExit(ctx, executionContext, *earlyResult, logManager)
		if completionErr != nil {
			return *earlyResult, report, errors.Join(earlyErr, completionErr)
		}
		return *earlyResult, report, earlyErr
	}

	logManager.EmitExecutionStart(ctx)
	runOutcome := stepRunner.Run(ctx, executionContext, logManager)
	logManager.EmitExecutionEnd(ctx, runOutcome.Result)

	report, completionErr := completionManager.CompleteExecution(ctx, executionContext, runOutcome.Result, logManager)
	if completionErr != nil {
		if runOutcome.ExecutionErr != nil {
			return runOutcome.Result, report, errors.Join(runOutcome.ExecutionErr, completionErr)
		}
		return runOutcome.Result, report, completionErr
	}

	if runOutcome.ExecutionErr != nil {
		return runOutcome.Result, report, runOutcome.ExecutionErr
	}

	return runOutcome.Result, report, nil
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
