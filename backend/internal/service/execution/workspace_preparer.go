package execution

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/runner"
	"github.com/radiation/coyote-ci/backend/internal/source"
)

var ErrRunnerWorkspaceNotSupported = errors.New("runner does not support workspace preparation for repo-backed builds")

type WorkspacePreparerDeps struct {
	Runner                          runner.Runner
	ResolveBuildSourceIntoWorkspace func(context.Context, string, ResolvedBuildSourceSpec) (string, error)
}

type WorkspacePreparer struct {
	deps WorkspacePreparerDeps
}

func NewWorkspacePreparer(deps WorkspacePreparerDeps) *WorkspacePreparer {
	return &WorkspacePreparer{deps: deps}
}

func (p *WorkspacePreparer) Prepare(ctx context.Context, executionContext StepExecutionContext, logManager *ExecutionLogManager) (earlyResult *runner.RunStepResult, earlyErr error, err error) {
	request := executionContext.ExecutionRequest
	buildSource := executionContext.BuildSource

	buildScopedRunner, ok := p.deps.Runner.(runner.BuildScopedRunner)
	if !ok {
		if buildSource.HasSource {
			return nil, nil, ErrRunnerWorkspaceNotSupported
		}
		return nil, nil, nil
	}

	logManager.EmitSystemLine(ctx, "Preparing workspace")
	prepareErr := buildScopedRunner.PrepareBuild(ctx, runner.PrepareBuildRequest{
		BuildID:    request.BuildID,
		RepoURL:    buildSource.RepositoryURL,
		Ref:        buildSource.Ref,
		CommitSHA:  buildSource.CommitSHA,
		Image:      executionContext.ExecutionImage,
		WorkerID:   request.WorkerID,
		ClaimToken: request.ClaimToken,
	})
	if prepareErr != nil {
		_, reason := classifyPrepareFailure(prepareErr)
		logManager.EmitSystemLine(ctx, "Failed to prepare workspace")
		logManager.EmitSystemLine(ctx, formatFailureReasonLine(reason))
		result := failedExecutionResult(reason)
		return &result, prepareErr, nil
	}

	if buildSource.HasSource && executionContext.StepNumber == 1 {
		if p.deps.ResolveBuildSourceIntoWorkspace == nil {
			result := failedExecutionResult("source resolver not configured")
			return &result, errors.New("source resolver not configured"), nil
		}
		logManager.EmitSystemLine(ctx, "Resolving source")
		logManager.EmitSystemLine(ctx, "Cloning repository")
		if buildSource.CommitSHA != "" {
			logManager.EmitSystemLine(ctx, "Checking out commit: "+buildSource.CommitSHA)
		} else {
			logManager.EmitSystemLine(ctx, "Checking out ref: "+buildSource.Ref)
		}

		resolvedCommit, sourceErr := p.deps.ResolveBuildSourceIntoWorkspace(ctx, request.BuildID, buildSource)
		if sourceErr != nil {
			reason := classifySourceFailureReason(sourceErr, buildSource)
			logManager.EmitSystemLine(ctx, "Source checkout failed")
			logManager.EmitSystemLine(ctx, formatFailureReasonLine(reason))
			result := failedExecutionResult(reason)
			return &result, sourceErr, nil
		}

		logManager.EmitSystemLine(ctx, "Resolved commit: "+resolvedCommit)
	}

	logManager.EmitSystemLine(ctx, "Workspace ready")
	return nil, nil, nil
}

func classifySourceFailureReason(err error, sourceSpec ResolvedBuildSourceSpec) string {
	if errors.Is(err, source.ErrRepositoryURLRequired) {
		return "repository URL is required"
	}
	if errors.Is(err, source.ErrCloneFailed) {
		return "repository clone failed"
	}
	if errors.Is(err, source.ErrRefNotFound) {
		return "ref not found: " + strings.TrimSpace(sourceSpec.Ref)
	}
	if errors.Is(err, source.ErrCommitNotFound) {
		return "commit not found: " + strings.TrimSpace(sourceSpec.CommitSHA)
	}
	if errors.Is(err, source.ErrCheckoutTargetRequired) {
		return "ref or commit_sha is required"
	}
	if errors.Is(err, source.ErrCheckoutFailed) {
		return "repository checkout failed"
	}
	if errors.Is(err, source.ErrResolveCommitFailed) {
		return "unable to resolve final commit SHA"
	}
	return "source checkout failed"
}

func failedExecutionResult(reason string) runner.RunStepResult {
	now := time.Now().UTC()
	return runner.RunStepResult{
		Status:     runner.RunStepStatusFailed,
		ExitCode:   -1,
		Stderr:     reason,
		StartedAt:  now,
		FinishedAt: now,
	}
}
