package service

import (
	"context"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/runner"
)

type WorkspacePreparer struct {
	service *BuildService
}

func NewWorkspacePreparer(service *BuildService) *WorkspacePreparer {
	return &WorkspacePreparer{service: service}
}

func (p *WorkspacePreparer) Prepare(ctx context.Context, executionContext StepExecutionContext, logManager *ExecutionLogManager) (earlyResult *runner.RunStepResult, earlyErr error, err error) {
	request := executionContext.ExecutionRequest
	buildSource := executionContext.BuildSource

	buildScopedRunner, ok := p.service.runner.(runner.BuildScopedRunner)
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
		logManager.EmitSystemLine(ctx, "Failed to start build container")
		logManager.EmitSystemLine(ctx, formatFailureReasonLine(reason))
		result := failedExecutionResult(reason)
		return &result, prepareErr, nil
	}

	if buildSource.HasSource && executionContext.StepNumber == 1 {
		logManager.EmitSystemLine(ctx, "Resolving source")
		logManager.EmitSystemLine(ctx, "Cloning repository")
		if buildSource.CommitSHA != "" {
			logManager.EmitSystemLine(ctx, "Checking out commit: "+buildSource.CommitSHA)
		} else {
			logManager.EmitSystemLine(ctx, "Checking out ref: "+buildSource.Ref)
		}

		resolvedCommit, sourceErr := p.service.resolveBuildSourceIntoWorkspace(ctx, request.BuildID, buildSource)
		if sourceErr != nil {
			reason := classifySourceFailureReason(sourceErr, buildSource)
			logManager.EmitSystemLine(ctx, "Source checkout failed")
			logManager.EmitSystemLine(ctx, formatFailureReasonLine(reason))
			result := failedExecutionResult(reason)
			return &result, sourceErr, nil
		}

		logManager.EmitSystemLine(ctx, "Resolved commit: "+resolvedCommit)
	}

	logManager.EmitSystemLine(ctx, "Starting build container")
	return nil, nil, nil
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
