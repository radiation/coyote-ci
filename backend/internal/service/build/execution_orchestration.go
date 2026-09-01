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
	"github.com/radiation/coyote-ci/backend/internal/source"
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
	if recovered, result, report, recoveryErr := s.recoverPublishedWorkspaceRevision(ctx, executionContext, completionManager, logManager); recovered {
		return result, report, recoveryErr
	}
	materializedWorkspace, materializeErr := s.materializeExecutionWorkspace(ctx, executionContext)
	if materializeErr != nil {
		now := time.Now().UTC()
		result := runner.RunStepResult{
			Status:     runner.RunStepStatusFailed,
			ExitCode:   -1,
			Stderr:     "workspace revision: " + materializeErr.Error(),
			StartedAt:  now,
			FinishedAt: now,
		}
		report, completionErr := completionManager.CompleteEarlyExit(ctx, executionContext, result, logManager)
		if completionErr != nil {
			return result, report, errors.Join(materializeErr, completionErr)
		}
		return result, report, materializeErr
	}

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
	if runOutcome.Result.Status == runner.RunStepStatusSuccess {
		if publishErr := s.publishWorkspaceRevision(ctx, executionContext, materializedWorkspace); publishErr != nil {
			now := time.Now().UTC()
			runOutcome.Result = runner.RunStepResult{
				Status: runner.RunStepStatusFailed, ExitCode: -1, Stderr: "workspace revision: " + publishErr.Error(),
				StartedAt: runOutcome.Result.StartedAt, FinishedAt: now,
			}
			runOutcome.ExecutionErr = publishErr
		}
	}
	if runOutcome.Result.Status == runner.RunStepStatusSuccess {
		if commitErr := s.commitExecutionWorkspace(ctx, materializedWorkspace, executionContext.ExecutionRequest.ClaimToken); commitErr != nil {
			now := time.Now().UTC()
			runOutcome.Result = runner.RunStepResult{
				Status:     runner.RunStepStatusFailed,
				ExitCode:   -1,
				Stderr:     commitErr.Error(),
				StartedAt:  runOutcome.Result.StartedAt,
				FinishedAt: now,
			}
			runOutcome.ExecutionErr = commitErr
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

var workspaceRevisionNamespace = uuid.MustParse("80c528d8-d286-5fdd-98ce-5d5239616c2a")

func workspaceRevisionIDForExecutionJob(jobID string) string {
	return uuid.NewSHA1(workspaceRevisionNamespace, []byte(jobID)).String()
}

func (s *BuildService) workspaceRevisionPublicationEnabled() bool {
	return s.workspaceRevisionRepo != nil && s.workspaceRevisionStore != nil
}

func (s *BuildService) publishWorkspaceRevision(ctx context.Context, executionContext StepExecutionContext, materializedWorkspace source.MaterializedWorkspace) error {
	if !s.workspaceRevisionPublicationEnabled() || executionContext.PersistedJob == nil {
		return nil
	}
	job := *executionContext.PersistedJob
	revisionID := workspaceRevisionIDForExecutionJob(job.ID)
	_, createErr := s.workspaceRevisionRepo.CreatePublishing(ctx, domain.WorkspaceRevision{
		ID: revisionID, ProducingExecutionJobID: job.ID, BuildID: job.BuildID, NodeID: job.NodeID,
		AttemptNumber: job.AttemptNumber, Status: domain.WorkspaceRevisionStatusPublishing, CreatedAt: time.Now().UTC(),
	})
	if createErr != nil {
		return fmt.Errorf("creating workspace revision: %w", createErr)
	}
	publication, publishErr := s.workspaceRevisionStore.Publish(ctx, revisionID, materializedWorkspace.Path)
	if publishErr != nil {
		return fmt.Errorf("publishing workspace revision: %w", publishErr)
	}
	if _, markErr := s.workspaceRevisionRepo.MarkPublishedIfClaimed(ctx, revisionID, executionContext.ExecutionRequest.ClaimToken, publication, time.Now().UTC()); markErr != nil {
		return fmt.Errorf("marking workspace revision published: %w", markErr)
	}
	return nil
}

func (s *BuildService) recoverPublishedWorkspaceRevision(ctx context.Context, executionContext StepExecutionContext, completionManager *StepCompletionManager, logManager *ExecutionLogManager) (bool, runner.RunStepResult, StepCompletionReport, error) {
	if !s.workspaceRevisionPublicationEnabled() || executionContext.PersistedJob == nil {
		return false, runner.RunStepResult{}, StepCompletionReport{}, nil
	}
	job := *executionContext.PersistedJob
	revision, revisionErr := s.workspaceRevisionRepo.GetByProducingExecutionJob(ctx, job.ID)
	if errors.Is(revisionErr, repository.ErrWorkspaceRevisionNotFound) {
		return false, runner.RunStepResult{}, StepCompletionReport{}, nil
	}
	if revisionErr != nil {
		return true, runner.RunStepResult{}, StepCompletionReport{}, fmt.Errorf("looking up workspace revision recovery state: %w", revisionErr)
	}
	if revision.ID != workspaceRevisionIDForExecutionJob(job.ID) || revision.Status != domain.WorkspaceRevisionStatusPublished {
		return false, runner.RunStepResult{}, StepCompletionReport{}, nil
	}
	now := time.Now().UTC()
	result := runner.RunStepResult{Status: runner.RunStepStatusSuccess, ExitCode: 0, StartedAt: now, FinishedAt: now}
	logManager.EmitSystemLine(ctx, "Recovered published workspace revision; finalizing execution")
	report, completionErr := completionManager.CompleteExecution(ctx, executionContext, result, logManager)
	return true, result, report, completionErr
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
	build, err := s.buildRepo.GetByID(ctx, buildID)
	if err != nil {
		return fmt.Errorf("fetching build for cleanup check: %w", err)
	}
	if !domain.IsTerminalBuildStatus(build.Status) {
		return nil
	}

	var cleanupErr error
	if buildScopedRunner, ok := s.runner.(runner.BuildScopedRunner); ok {
		cleanupErr = buildScopedRunner.CleanupBuild(ctx, buildID)
	}
	if s.workspaceMaterializer == nil {
		return cleanupErr
	}
	workspaces := s.materializedWorkspacesForRelease(buildID)
	var releaseErr error
	for _, materializedWorkspace := range workspaces {
		releaseErr = errors.Join(releaseErr, s.workspaceMaterializer.Release(ctx, materializedWorkspace))
	}
	if releaseErr == nil {
		s.forgetMaterializedWorkspaces(buildID)
	}
	return errors.Join(cleanupErr, releaseErr)
}

func (s *BuildService) materializeExecutionWorkspace(ctx context.Context, executionContext StepExecutionContext) (source.MaterializedWorkspace, error) {
	if s.workspaceMaterializer == nil {
		return source.MaterializedWorkspace{}, nil
	}
	nodeID := ""
	if executionContext.PersistedJob != nil {
		nodeID = executionContext.PersistedJob.NodeID
	}
	materializedWorkspace, err := s.workspaceMaterializer.Materialize(ctx, source.MaterializeWorkspaceRequest{
		BuildID: executionContext.Build.ID,
		NodeID:  nodeID,
		Input:   executionContext.WorkspaceInput,
	})
	if err != nil {
		return source.MaterializedWorkspace{}, err
	}
	s.rememberMaterializedWorkspace(executionContext.Build.ID, materializedWorkspace)
	return materializedWorkspace, nil
}

func (s *BuildService) commitExecutionWorkspace(ctx context.Context, workspace source.MaterializedWorkspace, claimToken string) error {
	if s.workspaceMaterializer == nil {
		return nil
	}
	return s.workspaceMaterializer.Commit(ctx, workspace, claimToken)
}

func (s *BuildService) rememberMaterializedWorkspace(buildID string, workspace source.MaterializedWorkspace) {
	s.materializedWorkspaceMu.Lock()
	defer s.materializedWorkspaceMu.Unlock()
	if s.materializedWorkspaces == nil {
		s.materializedWorkspaces = make(map[string][]source.MaterializedWorkspace)
	}
	s.materializedWorkspaces[buildID] = append(s.materializedWorkspaces[buildID], workspace)
}

func (s *BuildService) materializedWorkspacesForRelease(buildID string) []source.MaterializedWorkspace {
	s.materializedWorkspaceMu.Lock()
	defer s.materializedWorkspaceMu.Unlock()
	return append([]source.MaterializedWorkspace(nil), s.materializedWorkspaces[buildID]...)
}

func (s *BuildService) forgetMaterializedWorkspaces(buildID string) {
	s.materializedWorkspaceMu.Lock()
	defer s.materializedWorkspaceMu.Unlock()
	delete(s.materializedWorkspaces, buildID)
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
