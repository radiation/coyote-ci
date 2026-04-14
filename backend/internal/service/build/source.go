package build

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/service/execution"
	"github.com/radiation/coyote-ci/backend/internal/source"
)

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

	workspaceRoot := buildNormalizeWorkspaceRoot(s.executionWorkspaceRoot)
	if workspaceRoot == "" {
		workspaceRoot = buildNormalizeWorkspaceRoot(s.artifactWorkspaceRoot)
	}
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
