package build

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/pipeline"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/runner"
	"github.com/radiation/coyote-ci/backend/internal/workspace"
)

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

func (s *BuildService) RunStep(ctx context.Context, request runner.RunStepRequest) (runner.RunStepResult, StepCompletionReport, error) {
	if s.runner == nil {
		return runner.RunStepResult{}, StepCompletionReport{CompletionOutcome: repository.StepCompletionInvalidTransition}, ErrRunnerNotConfigured
	}

	builder := NewStepExecutionContextBuilder(s)
	executionContext, err := builder.Build(ctx, request)
	if err != nil {
		return runner.RunStepResult{}, StepCompletionReport{}, mapExecutionErr(err)
	}
	executionContext = s.enrichExecutionContextForRunner(executionContext)

	logManager := NewExecutionLogManager(s, executionContext)
	completionManager := NewStepCompletionManager(s)
	stepRunner := NewStepRunner(s.runner)

	logManager.EmitExecutionStart(ctx)

	workspacePreparer := NewWorkspacePreparer(s)
	earlyResult, earlyErr, prepareErr := workspacePreparer.Prepare(ctx, executionContext, logManager)
	if prepareErr != nil {
		return runner.RunStepResult{}, StepCompletionReport{}, mapExecutionErr(prepareErr)
	}
	if earlyResult != nil {
		report, completionErr := completionManager.CompleteEarlyExit(ctx, executionContext, *earlyResult, logManager)
		if completionErr != nil {
			return *earlyResult, report, errors.Join(earlyErr, completionErr)
		}
		return *earlyResult, report, earlyErr
	}

	cacheSideEffectErr := error(nil)
	preparedCache := preparedStepCache{}
	if s.stepCacheManager != nil {
		cacheState, cacheErr := s.stepCacheManager.Prepare(ctx, executionContext, logManager)
		if cacheErr != nil {
			logManager.EmitSystemLine(ctx, "Cache: restore failed")
			logManager.EmitSystemLine(ctx, formatFailureReasonLine(cacheErr.Error()))
			cacheSideEffectErr = joinSideEffectErrors(cacheSideEffectErr, cacheErr)
		} else if cacheState.Enabled {
			preparedCache = cacheState
			executionContext.ExecutionRequest.CacheMounts = append([]runner.CacheMount(nil), cacheState.Mounts...)
		}
	}
	runOutcome := stepRunner.Run(ctx, executionContext, logManager)
	if s.stepCacheManager != nil && preparedCache.Enabled {
		saveErr := s.stepCacheManager.Save(ctx, executionContext, logManager, preparedCache, runOutcome.Result)
		if saveErr != nil {
			logManager.EmitSystemLine(ctx, "Cache: save failed")
			logManager.EmitSystemLine(ctx, formatFailureReasonLine(saveErr.Error()))
			cacheSideEffectErr = joinSideEffectErrors(cacheSideEffectErr, saveErr)
		}
	}
	logManager.EmitExecutionEnd(ctx, runOutcome.Result)

	report, completionErr := completionManager.CompleteExecution(ctx, executionContext, runOutcome.Result, logManager)
	report.SideEffectErr = joinSideEffectErrors(report.SideEffectErr, cacheSideEffectErr)
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

func (s *BuildService) enrichExecutionContextForRunner(executionContext StepExecutionContext) StepExecutionContext {
	visibleWorkspace := workspace.DefaultContainerRoot
	if provider, ok := s.runner.(runner.WorkspacePathProvider); ok {
		if root, found := provider.StepVisibleWorkspaceRoot(executionContext.Build.ID); found && strings.TrimSpace(root) != "" {
			visibleWorkspace = root
		}
	}

	executionContext.StepWorkingDir = workspace.ResolveVisibleWorkingDir(visibleWorkspace, executionContext.ExecutionRequest.WorkingDir)
	env := cloneEnv(executionContext.ExecutionRequest.Env)
	env[runner.EnvWorkspace] = visibleWorkspace
	if executionContext.ExecutionRequest.BuildID != "" {
		env[runner.EnvBuildID] = executionContext.ExecutionRequest.BuildID
	}
	if executionContext.ExecutionRequest.StepID != "" {
		env[runner.EnvStepID] = executionContext.ExecutionRequest.StepID
	}

	trigger := domain.NormalizeBuildTrigger(executionContext.Build.Trigger)
	if trigger.Kind == domain.BuildTriggerKindArtifact {
		relativePath, relativeErr := workspace.TriggerArtifactRelativePath(readOptionalString(trigger.ArtifactPath))
		if relativeErr == nil {
			env[runner.EnvTriggerArtifactLocalRelative] = relativePath
			env[runner.EnvTriggerArtifactLocalDir] = workspace.ResolveVisiblePath(visibleWorkspace, workspace.TriggerArtifactsRelativeRoot)
			if path.Clean(strings.ReplaceAll(visibleWorkspace, "\\", "/")) == workspace.DefaultContainerRoot {
				env[runner.EnvTriggerArtifactLocalPath] = path.Join(visibleWorkspace, relativePath)
			} else {
				env[runner.EnvTriggerArtifactLocalPath] = workspace.ResolveVisiblePath(visibleWorkspace, relativePath)
			}
		}
	}

	executionContext.ExecutionRequest.Env = env
	return executionContext
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
