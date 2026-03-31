package service

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/source"
)

type resolvedBuildSourceSpec struct {
	RepositoryURL string
	Ref           string
	CommitSHA     string
	HasSource     bool
}

func sourceSpecFromBuild(build domain.Build) resolvedBuildSourceSpec {
	if build.Source != nil {
		result := resolvedBuildSourceSpec{
			RepositoryURL: strings.TrimSpace(build.Source.RepositoryURL),
			Ref:           readOptionalString(build.Source.Ref),
			CommitSHA:     readOptionalString(build.Source.CommitSHA),
		}
		result.HasSource = result.RepositoryURL != ""
		return result
	}

	result := resolvedBuildSourceSpec{
		RepositoryURL: readOptionalString(build.RepoURL),
		Ref:           readOptionalString(build.Ref),
		CommitSHA:     readOptionalString(build.CommitSHA),
	}
	result.HasSource = result.RepositoryURL != ""
	return result
}

func toDomainSourceSpec(input *CreateBuildSourceInput) (*domain.SourceSpec, error) {
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

func sourceRepositoryURL(spec *domain.SourceSpec) string {
	if spec == nil {
		return ""
	}
	return strings.TrimSpace(spec.RepositoryURL)
}

func sourceRef(spec *domain.SourceSpec) *string {
	if spec == nil || spec.Ref == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*spec.Ref)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func sourceCommitSHA(spec *domain.SourceSpec) *string {
	if spec == nil || spec.CommitSHA == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*spec.CommitSHA)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func optionalStringPtr(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func normalizeWorkspaceRoot(root string) string {
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

func (s *BuildService) resolveBuildSourceIntoWorkspace(ctx context.Context, buildID string, sourceSpec resolvedBuildSourceSpec) (string, error) {
	if s.sourceResolver == nil {
		return "", ErrSourceResolverNotConfigured
	}

	workspaceRoot := normalizeWorkspaceRoot(s.executionWorkspaceRoot)
	if workspaceRoot == "" {
		workspaceRoot = normalizeWorkspaceRoot(s.artifactWorkspaceRoot)
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

func classifySourceFailureReason(err error, sourceSpec resolvedBuildSourceSpec) string {
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

func readOptionalString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
