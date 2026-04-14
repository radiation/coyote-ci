package build

import (
	"context"
	"errors"
	"time"

	cachepkg "github.com/radiation/coyote-ci/backend/internal/cache"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/logs"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/runner"
	"github.com/radiation/coyote-ci/backend/internal/service/execution"
)

type StepExecutionContext = execution.StepExecutionContext
type ExecutionLogManager = execution.ExecutionLogManager
type WorkspacePreparer = execution.WorkspacePreparer
type StepCompletionManager = execution.StepCompletionManager
type StepRunner = execution.StepRunner
type preparedStepCache = execution.PreparedStepCache
type StepCacheManager = execution.StepCacheManager

type StepExecutionContextBuilder struct {
	inner *execution.StepExecutionContextBuilder
}

func NewStepExecutionContextBuilder(service *BuildService) *StepExecutionContextBuilder {
	inner := execution.NewStepExecutionContextBuilder(execution.StepExecutionContextBuilderDeps{
		BuildRepo:             service.buildRepo,
		ExecutionJobRepo:      service.executionJobRepo,
		ResolveExecutionImage: service.resolveExecutionImage,
		LogSink:               service.logSink,
	})
	return &StepExecutionContextBuilder{inner: inner}
}

func (b *StepExecutionContextBuilder) Build(ctx context.Context, request runner.RunStepRequest) (StepExecutionContext, error) {
	executionContext, err := b.inner.Build(ctx, request)
	if err != nil {
		return StepExecutionContext{}, mapExecutionErr(err)
	}
	return executionContext, nil
}

func NewExecutionLogManager(service *BuildService, executionContext StepExecutionContext) *ExecutionLogManager {
	return execution.NewExecutionLogManager(service.logSink, executionContext)
}

func NewWorkspacePreparer(service *BuildService) *WorkspacePreparer {
	return execution.NewWorkspacePreparer(execution.WorkspacePreparerDeps{
		Runner: service.runner,
		ResolveBuildSourceIntoWorkspace: func(ctx context.Context, buildID string, spec execution.ResolvedBuildSourceSpec) (string, error) {
			return service.resolveBuildSourceInWorkspace(ctx, buildID, spec)
		},
	})
}

func NewStepCompletionManager(service *BuildService) *StepCompletionManager {
	return execution.NewStepCompletionManager(execution.StepCompletionManagerDeps{
		HandleStepResult:             service.handleStepResult,
		RunPostCompletionSideEffects: service.runPostCompletionSideEffects,
	})
}

func NewStepRunner(stepRunner runner.Runner) *StepRunner {
	return execution.NewStepRunner(stepRunner)
}

func NewStepCacheManager(store cachepkg.Store, entryRepo repository.CacheEntryRepository, executionRootPath string) *StepCacheManager {
	return execution.NewStepCacheManager(store, entryRepo, executionRootPath)
}

func (s *BuildService) writeSystemExecutionLogLine(ctx context.Context, request runner.RunStepRequest, appender logs.StepLogChunkAppender, line string) error {
	return execution.WriteSystemExecutionLogLine(ctx, s.logSink, request, appender, line)
}

func formatFailureReasonLine(reason string) string {
	return execution.FormatFailureReasonLine(reason)
}

func formatBuildSummaryLines(status domain.BuildStatus, duration time.Duration, completedSteps int, totalSteps int, artifactPaths []string) []string {
	return execution.FormatBuildSummaryLines(status, duration, completedSteps, totalSteps, artifactPaths)
}

func writeOutputLogs(ctx context.Context, sink logs.LogSink, buildID string, stepName string, output string) error {
	return execution.WriteOutputLogs(ctx, sink, buildID, stepName, output)
}

func presetMounts(runtimeDir string, cachePaths []string) ([]runner.CacheMount, error) {
	return execution.PresetMounts(runtimeDir, cachePaths)
}

func effectiveJobID(executionContext StepExecutionContext) string {
	return execution.EffectiveJobID(executionContext)
}

func mapExecutionErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, execution.ErrBuildNotFound) {
		return ErrBuildNotFound
	}
	if errors.Is(err, execution.ErrExecutionJobNotFound) {
		return ErrExecutionJobNotFound
	}
	if errors.Is(err, execution.ErrRunnerWorkspaceNotSupported) {
		return ErrRunnerWorkspaceNotSupported
	}
	if errors.Is(err, execution.ErrExecutionWorkspaceRootNotConfigured) {
		return ErrExecutionWorkspaceRootNotConfigured
	}
	return err
}
