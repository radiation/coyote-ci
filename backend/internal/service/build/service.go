package build

import (
	"context"
	"errors"
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/artifact"
	cachepkg "github.com/radiation/coyote-ci/backend/internal/cache"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/logs"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/runner"
	"github.com/radiation/coyote-ci/backend/internal/service/execution"
	versiontagsvc "github.com/radiation/coyote-ci/backend/internal/service/versiontag"
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
var ErrBuildRerunUnavailable = errors.New("build rerun context is unavailable")
var ErrBuildPriorityOutOfRange = errors.New("priority must be between 1 and 10")

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
	managedImageRefresher  ManagedImageRefresher
	sourceResolver         source.WorkspaceSourceResolver
	executionWorkspaceRoot string

	artifactRepo            repository.ArtifactRepository
	executionOutputRepo     repository.ExecutionJobOutputRepository
	artifactStore           artifact.Store
	artifactStoreResolver   *artifact.StoreResolver
	artifactCollector       *artifact.Collector
	artifactWorkspaceRoot   string
	artifactStorageProvider domain.StorageProvider
	stepCacheManager        *StepCacheManager
	versionTagger           BuildVersionTagger
	buildNotifier           BuildLifecycleNotifier

	defaultExecutionImage string
}

type BuildVersionTagger interface {
	CreateVersionTags(ctx context.Context, jobID string, input versiontagsvc.CreateVersionTagsInput) ([]domain.VersionTag, error)
}

type BuildLifecycleNotifier interface {
	NotifyTerminalBuild(ctx context.Context, build domain.Build) error
}

// BuildServiceConfig groups all optional dependencies for BuildService. Zero
// values are safe — each field is only used when set.
type BuildServiceConfig struct {
	ExecutionJobRepo      repository.ExecutionJobRepository
	ExecutionOutputRepo   repository.ExecutionJobOutputRepository
	RepoFetcher           source.RepoFetcher
	ManagedImageRefresher ManagedImageRefresher
	SourceResolver        source.WorkspaceSourceResolver
	ArtifactRepo          repository.ArtifactRepository
	ArtifactLabelRepo     repository.ArtifactLabelRepository
	ArtifactResolver      *artifact.StoreResolver
	ArtifactWorkspace     string
	ExecutionWorkspace    string
	DefaultImage          string
	CacheStore            cachepkg.Store
	CacheEntryRepo        repository.CacheEntryRepository
	VersionTagger         BuildVersionTagger
	BuildNotifier         BuildLifecycleNotifier
}

// NewBuildServiceFromConfig creates a fully-wired BuildService in one call.
func NewBuildServiceFromConfig(buildRepo repository.BuildRepository, stepRunner runner.Runner, logSink logs.LogSink, cfg BuildServiceConfig) *BuildService {
	svc := NewBuildService(buildRepo, stepRunner, logSink)
	svc.executionJobRepo = cfg.ExecutionJobRepo
	svc.executionOutputRepo = cfg.ExecutionOutputRepo
	svc.repoFetcher = cfg.RepoFetcher
	svc.managedImageRefresher = cfg.ManagedImageRefresher
	if cfg.SourceResolver != nil {
		svc.sourceResolver = cfg.SourceResolver
	}
	svc.defaultExecutionImage = strings.TrimSpace(cfg.DefaultImage)
	svc.executionWorkspaceRoot = buildNormalizeWorkspaceRoot(cfg.ExecutionWorkspace)
	svc.versionTagger = cfg.VersionTagger
	svc.buildNotifier = cfg.BuildNotifier
	if cfg.ArtifactLabelRepo != nil {
		type artifactLabelRepoAware interface {
			SetArtifactLabelRepository(repository.ArtifactLabelRepository)
		}
		if aware, ok := svc.versionTagger.(artifactLabelRepoAware); ok {
			aware.SetArtifactLabelRepository(cfg.ArtifactLabelRepo)
		}
	}
	svc.SetArtifactPersistence(cfg.ArtifactRepo, cfg.ArtifactResolver, cfg.ArtifactWorkspace)
	svc.SetStepCacheStore(cfg.CacheStore, cfg.CacheEntryRepo)
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

func validateRequestedBuildPriority(priority int) error {
	if priority == 0 {
		return nil
	}
	if !domain.ValidPriority(priority) {
		return ErrBuildPriorityOutOfRange
	}
	return nil
}

// SetRepoFetcher attaches a RepoFetcher for repo-backed build creation.
func (s *BuildService) SetRepoFetcher(fetcher source.RepoFetcher) {
	s.repoFetcher = fetcher
}

func (s *BuildService) SetSourceResolver(resolver source.WorkspaceSourceResolver) {
	s.sourceResolver = resolver
}

func (s *BuildService) SetExecutionWorkspaceRoot(root string) {
	s.executionWorkspaceRoot = buildNormalizeWorkspaceRoot(root)
	if s.stepCacheManager != nil {
		s.stepCacheManager = NewStepCacheManager(s.stepCacheManager.Store(), s.stepCacheManager.EntryRepo(), s.executionWorkspaceRoot)
	}
}

// SetDefaultExecutionImage sets the image used when a build-scoped runner requires one.
func (s *BuildService) SetDefaultExecutionImage(image string) {
	s.defaultExecutionImage = strings.TrimSpace(image)
}

// SetArtifactPersistence configures build artifact persistence dependencies.
func (s *BuildService) SetArtifactPersistence(repo repository.ArtifactRepository, resolver *artifact.StoreResolver, workspaceRoot string) {
	s.artifactRepo = repo
	s.artifactWorkspaceRoot = buildNormalizeWorkspaceRoot(workspaceRoot)
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
	type buildRepoAware interface {
		SetBuildRepository(repository.BuildRepository)
	}
	if aware, ok := repo.(buildRepoAware); ok {
		aware.SetBuildRepository(s.buildRepo)
	}
}

func (s *BuildService) SetExecutionJobOutputRepository(repo repository.ExecutionJobOutputRepository) {
	s.executionOutputRepo = repo
}

func (s *BuildService) SetStepCacheStore(store cachepkg.Store, entryRepo repository.CacheEntryRepository) {
	if store == nil || entryRepo == nil {
		s.stepCacheManager = nil
		return
	}
	s.stepCacheManager = NewStepCacheManager(store, entryRepo, s.executionWorkspaceRoot)
}

type StepCompletionReport = execution.StepCompletionReport

type stepFailureKind string

const (
	stepFailureKindNone     stepFailureKind = "none"
	stepFailureKindExitCode stepFailureKind = "exit_code"
	stepFailureKindTimeout  stepFailureKind = "timeout"
	stepFailureKindInternal stepFailureKind = "internal"
)
