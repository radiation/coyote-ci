package build

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/service/execution"
	"github.com/radiation/coyote-ci/backend/internal/source"
)

const buildPreparationLogStepName = "build-prep"

func (s *BuildService) emitBuildPreparationLog(ctx context.Context, buildID string, line string) {
	if s.logSink == nil {
		return
	}
	trimmedBuildID := strings.TrimSpace(buildID)
	trimmedLine := strings.TrimSpace(line)
	if trimmedBuildID == "" || trimmedLine == "" {
		return
	}
	if err := s.logSink.WriteStepLog(ctx, trimmedBuildID, buildPreparationLogStepName, trimmedLine); err != nil {
		log.Printf("build prep log write failed: build_id=%s error=%v", trimmedBuildID, err)
	}
}

func buildSourceSpecFromBuild(build domain.Build) execution.ResolvedBuildSourceSpec {
	if build.Source != nil {
		result := execution.ResolvedBuildSourceSpec{
			RepositoryURL: strings.TrimSpace(build.Source.RepositoryURL),
			Ref:           buildReadOptionalString(build.Source.Ref),
			CommitSHA:     buildReadOptionalString(build.Source.CommitSHA),
		}
		result.HasSource = result.RepositoryURL != ""
		return result
	}

	result := execution.ResolvedBuildSourceSpec{
		RepositoryURL: buildReadOptionalString(build.RepoURL),
		Ref:           buildReadOptionalString(build.Ref),
		CommitSHA:     buildReadOptionalString(build.CommitSHA),
	}
	result.HasSource = result.RepositoryURL != ""
	return result
}

func buildSourceSpecFromInput(input *CreateBuildSourceInput) (*domain.SourceSpec, error) {
	if input == nil {
		return nil, nil
	}

	repoURL := strings.TrimSpace(input.RepositoryURL)
	if repoURL == "" {
		return nil, ErrRepoURLRequired
	}

	ref := strings.TrimSpace(input.Ref)
	commitSHA := strings.TrimSpace(input.CommitSHA)
	if ref == "" && commitSHA == "" {
		return nil, ErrSourceTargetRequired
	}

	return domain.NewSourceSpec(repoURL, ref, commitSHA), nil
}

func buildSourceRepositoryURL(spec *domain.SourceSpec) string {
	if spec == nil {
		return ""
	}
	return strings.TrimSpace(spec.RepositoryURL)
}

func buildSourceRef(spec *domain.SourceSpec) *string {
	if spec == nil || spec.Ref == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*spec.Ref)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func buildSourceCommitSHA(spec *domain.SourceSpec) *string {
	if spec == nil || spec.CommitSHA == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*spec.CommitSHA)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func buildOptionalStringPtr(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func buildNormalizeWorkspaceRoot(root string) string {
	trimmed := strings.TrimSpace(root)
	if trimmed == "" {
		return ""
	}

	absRoot, err := filepath.Abs(trimmed)
	if err != nil {
		return trimmed
	}

	return absRoot
}

func (s *BuildService) resolveBuildSourceInWorkspace(ctx context.Context, buildID string, sourceSpec execution.ResolvedBuildSourceSpec) (string, error) {
	if s.sourceResolver == nil {
		return "", ErrSourceResolverNotConfigured
	}

	workspaceRoot := s.currentWorkspaceRoot()
	if workspaceRoot == "" {
		return "", ErrExecutionWorkspaceRootNotConfigured
	}

	workspacePath := filepath.Join(workspaceRoot, strings.TrimSpace(buildID))
	if err := s.sourceResolver.CloneIntoWorkspace(ctx, workspacePath, sourceSpec.RepositoryURL); err != nil {
		return "", err
	}

	resolvedCommit, err := s.sourceResolver.CheckoutWorkspaceSource(ctx, workspacePath, source.WorkspaceSourceSpec{
		RepositoryURL: sourceSpec.RepositoryURL,
		Ref:           sourceSpec.Ref,
		CommitSHA:     sourceSpec.CommitSHA,
	})
	if err != nil {
		return "", err
	}

	trimmedResolvedCommit := strings.TrimSpace(resolvedCommit)
	if trimmedResolvedCommit == "" {
		return "", source.ErrResolveCommitFailed
	}

	build, err := s.buildRepo.UpdateSourceCommitSHA(ctx, strings.TrimSpace(buildID), trimmedResolvedCommit)
	if err != nil {
		return "", fmt.Errorf("persisting resolved commit SHA: %w", err)
	}
	_ = build

	return trimmedResolvedCommit, nil
}

func (s *BuildService) currentWorkspaceRoot() string {
	workspaceRoot := buildNormalizeWorkspaceRoot(s.executionWorkspaceRoot)
	if workspaceRoot != "" {
		return workspaceRoot
	}
	return buildNormalizeWorkspaceRoot(s.artifactWorkspaceRoot)
}

func (s *BuildService) prepareBuildWorkspace(ctx context.Context, buildID string) error {
	workspaceRoot := s.currentWorkspaceRoot()
	if workspaceRoot == "" {
		// No workspace root configured: the runner manages its own workspace (e.g. inprocess).
		// Skip host-side directory creation; source resolution below will still gate on HasSource.
		return nil
	}

	materializer := source.NewHostWorkspaceMaterializer(workspaceRoot)
	_, err := materializer.PrepareWorkspace(ctx, source.WorkspacePrepareRequest{BuildID: strings.TrimSpace(buildID)})
	return err
}

func (s *BuildService) cleanupPreparedWorkspace(ctx context.Context, buildID string) error {
	workspaceRoot := s.currentWorkspaceRoot()
	if workspaceRoot == "" {
		return nil
	}
	materializer := source.NewHostWorkspaceMaterializer(workspaceRoot)
	return materializer.CleanupWorkspace(ctx, strings.TrimSpace(buildID))
}

func (s *BuildService) PrepareBuildExecution(ctx context.Context, id string) (domain.Build, error) {
	prepStartedAt := time.Now().UTC()
	buildID := strings.TrimSpace(id)

	build, err := s.buildRepo.GetByID(ctx, buildID)
	if err != nil {
		return domain.Build{}, mapRepoErr(err)
	}

	switch build.Status {
	case domain.BuildStatusRunning:
		return build, nil
	case domain.BuildStatusPreparing:
		return build, nil
	case domain.BuildStatusQueued:
	default:
		return domain.Build{}, ErrInvalidBuildStatusTransition
	}

	build, err = s.transitionBuildStatus(ctx, buildID, domain.BuildStatusPreparing, nil)
	if err != nil {
		return domain.Build{}, err
	}
	s.emitBuildPreparationLog(ctx, buildID, "Preparing build workspace")

	if prepErr := s.prepareBuildWorkspace(ctx, buildID); prepErr != nil {
		message := prepErr.Error()
		failed, updateErr := s.buildRepo.UpdateStatus(ctx, buildID, domain.BuildStatusFailed, &message)
		if updateErr != nil {
			return domain.Build{}, mapRepoErr(updateErr)
		}
		log.Printf("build preparation failed: build_id=%s duration_ms=%d reason=%q", buildID, time.Since(prepStartedAt).Milliseconds(), message)
		return failed, nil
	}

	sourceSpec := buildSourceSpecFromBuild(build)
	if sourceSpec.HasSource {
		s.emitBuildPreparationLog(ctx, buildID, "Checking out source")
		if _, sourceErr := s.resolveBuildSourceInWorkspace(ctx, buildID, sourceSpec); sourceErr != nil {
			reason := classifyBuildSourceFailureReason(sourceErr, sourceSpec)
			_ = s.cleanupPreparedWorkspace(ctx, buildID)
			failed, updateErr := s.buildRepo.UpdateStatus(ctx, buildID, domain.BuildStatusFailed, &reason)
			if updateErr != nil {
				return domain.Build{}, mapRepoErr(updateErr)
			}
			log.Printf("build preparation failed: build_id=%s duration_ms=%d reason=%q", buildID, time.Since(prepStartedAt).Milliseconds(), reason)
			return failed, nil
		}
		s.emitBuildPreparationLog(ctx, buildID, "Source checkout complete")
	}
	s.emitBuildPreparationLog(ctx, buildID, "Build workspace ready")

	runningBuild, transitionErr := s.transitionBuildStatus(ctx, buildID, domain.BuildStatusRunning, nil)
	if transitionErr != nil {
		return domain.Build{}, transitionErr
	}
	log.Printf("build preparation completed: build_id=%s duration_ms=%d", buildID, time.Since(prepStartedAt).Milliseconds())
	return runningBuild, nil
}

func classifyBuildSourceFailureReason(err error, sourceSpec execution.ResolvedBuildSourceSpec) string {
	if errors.Is(err, source.ErrRepositoryURLRequired) || errors.Is(err, ErrRepoURLRequired) {
		return "repository URL is required"
	}
	if errors.Is(err, source.ErrCloneFailed) {
		return "repository clone failed"
	}
	if errors.Is(err, source.ErrRefNotFound) {
		return "ref not found: " + sourceSpec.Ref
	}
	if errors.Is(err, source.ErrCommitNotFound) {
		return "commit not found: " + sourceSpec.CommitSHA
	}
	if errors.Is(err, source.ErrCheckoutTargetRequired) || errors.Is(err, ErrSourceTargetRequired) {
		return "ref or commit_sha is required"
	}
	if errors.Is(err, source.ErrCheckoutFailed) {
		return "repository checkout failed"
	}
	if errors.Is(err, source.ErrResolveCommitFailed) {
		return "unable to resolve final commit SHA"
	}
	if errors.Is(err, ErrSourceResolverNotConfigured) {
		return "source resolver not configured"
	}
	if errors.Is(err, ErrExecutionWorkspaceRootNotConfigured) {
		return "execution workspace root not configured"
	}
	return "source checkout failed"
}

func buildReadOptionalString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
