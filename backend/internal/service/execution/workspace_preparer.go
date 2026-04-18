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

	buildScopedRunner, ok := p.deps.Runner.(runner.BuildScopedRunner)
	if !ok {
		// Runner does not support build-scoped lifecycle; source is assumed already present.
		return nil, nil, nil
	}

	logManager.EmitSystemLine(ctx, "Preparing workspace")
	prepareErr := buildScopedRunner.PrepareBuild(ctx, runner.PrepareBuildRequest{
		BuildID:    request.BuildID,
		RepoURL:    executionContext.BuildSource.RepositoryURL,
		Ref:        executionContext.BuildSource.Ref,
		CommitSHA:  executionContext.BuildSource.CommitSHA,
		Image:      executionContext.ExecutionImage,
		WorkerID:   request.WorkerID,
		ClaimToken: request.ClaimToken,
	})
	if prepareErr != nil {
		_, reason := classifyExecutionPrepareFailure(prepareErr)
		logManager.EmitSystemLine(ctx, "Failed to prepare workspace")
		logManager.EmitSystemLine(ctx, formatFailureReasonLine(reason))
		result := failedExecutionResult(reason)
		return &result, prepareErr, nil
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
