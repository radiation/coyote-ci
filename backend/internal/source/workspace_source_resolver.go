package source

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var ErrWorkspacePathRequired = errors.New("workspace path is required")
var ErrRepositoryURLRequired = errors.New("repository URL is required")
var ErrCheckoutTargetRequired = errors.New("ref or commit SHA is required")
var ErrCloneFailed = errors.New("repository clone failed")
var ErrRefNotFound = errors.New("ref not found")
var ErrCommitNotFound = errors.New("commit not found")
var ErrCheckoutFailed = errors.New("repository checkout failed")
var ErrResolveCommitFailed = errors.New("unable to resolve final commit SHA")

type WorkspaceSourceSpec struct {
	RepositoryURL string
	Ref           string
	CommitSHA     string
}

// WorkspaceSourceResolver materializes source into an existing host workspace.
type WorkspaceSourceResolver interface {
	CloneIntoWorkspace(ctx context.Context, workspacePath string, repositoryURL string) error
	CheckoutWorkspaceSource(ctx context.Context, workspacePath string, spec WorkspaceSourceSpec) (string, error)
}

// GitWorkspaceSourceResolver uses git CLI to populate and pin workspace source.
type GitWorkspaceSourceResolver struct{}

func NewGitWorkspaceSourceResolver() *GitWorkspaceSourceResolver {
	return &GitWorkspaceSourceResolver{}
}

func (r *GitWorkspaceSourceResolver) CloneIntoWorkspace(ctx context.Context, workspacePath string, repositoryURL string) error {
	cleanWorkspacePath, repoURL, err := normalizeCloneInputs(workspacePath, repositoryURL)
	if err != nil {
		return err
	}

	if err := os.RemoveAll(cleanWorkspacePath); err != nil {
		return fmt.Errorf("%w: resetting workspace path: %v", ErrCloneFailed, err)
	}
	if err := os.MkdirAll(filepath.Dir(cleanWorkspacePath), 0o755); err != nil {
		return fmt.Errorf("%w: creating workspace parent: %v", ErrCloneFailed, err)
	}
	if err := gitClone(ctx, repoURL, cleanWorkspacePath); err != nil {
		return fmt.Errorf("%w: %v", ErrCloneFailed, err)
	}

	return nil
}

func (r *GitWorkspaceSourceResolver) CheckoutWorkspaceSource(ctx context.Context, workspacePath string, spec WorkspaceSourceSpec) (string, error) {
	cleanWorkspacePath := filepath.Clean(strings.TrimSpace(workspacePath))
	if !filepath.IsAbs(cleanWorkspacePath) {
		return "", ErrWorkspacePathRequired
	}

	commitCandidate := strings.TrimSpace(spec.CommitSHA)
	if commitCandidate != "" {
		if _, err := gitRevParseVerify(ctx, cleanWorkspacePath, commitCandidate+"^{commit}"); err != nil {
			return "", fmt.Errorf("%w: %s", ErrCommitNotFound, commitCandidate)
		}
		if err := gitCheckoutDetach(ctx, cleanWorkspacePath, commitCandidate); err != nil {
			return "", fmt.Errorf("%w: %v", ErrCheckoutFailed, err)
		}

		resolvedCommit, err := gitRevParseHead(ctx, cleanWorkspacePath)
		if err != nil {
			return "", fmt.Errorf("%w: %v", ErrResolveCommitFailed, err)
		}
		return strings.TrimSpace(resolvedCommit), nil
	}

	ref := strings.TrimSpace(spec.Ref)
	if ref == "" {
		return "", ErrCheckoutTargetRequired
	}

	resolvedRef, err := resolveRefCommit(ctx, cleanWorkspacePath, ref)
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrRefNotFound, ref)
	}
	if checkoutErr := gitCheckoutDetach(ctx, cleanWorkspacePath, resolvedRef); checkoutErr != nil {
		return "", fmt.Errorf("%w: %v", ErrCheckoutFailed, checkoutErr)
	}

	resolvedCommit, err := gitRevParseHead(ctx, cleanWorkspacePath)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrResolveCommitFailed, err)
	}

	return strings.TrimSpace(resolvedCommit), nil
}

func normalizeCloneInputs(workspacePath string, repositoryURL string) (string, string, error) {
	cleanWorkspacePath := filepath.Clean(strings.TrimSpace(workspacePath))
	if cleanWorkspacePath == "" || !filepath.IsAbs(cleanWorkspacePath) {
		return "", "", ErrWorkspacePathRequired
	}

	repoURL := strings.TrimSpace(repositoryURL)
	if repoURL == "" {
		return "", "", ErrRepositoryURLRequired
	}
	if strings.HasPrefix(repoURL, "-") {
		return "", "", ErrRepositoryURLRequired
	}

	return cleanWorkspacePath, repoURL, nil
}
