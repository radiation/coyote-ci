package source

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// RepoFetcher abstracts fetching a repository to a local filesystem path.
type RepoFetcher interface {
	// Fetch clones the repo at the given URL and checks out the requested ref.
	// Returns the local path to the cloned repo, the resolved commit SHA, and any error.
	// The caller is responsible for cleaning up the returned path.
	Fetch(ctx context.Context, repoURL string, ref string) (localPath string, commitSHA string, err error)
}

// GitFetcher implements RepoFetcher using the git CLI.
type GitFetcher struct{}

func NewGitFetcher() *GitFetcher {
	return &GitFetcher{}
}

func (g *GitFetcher) Fetch(ctx context.Context, repoURL string, ref string) (string, string, error) {
	repoURL = strings.TrimSpace(repoURL)
	if repoURL == "" {
		return "", "", errors.New("repo URL is required")
	}
	if strings.HasPrefix(repoURL, "-") {
		return "", "", errors.New("repo URL cannot begin with '-'")
	}

	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", "", errors.New("ref is required")
	}
	if strings.HasPrefix(ref, "-") {
		return "", "", errors.New("ref cannot begin with '-'")
	}

	tmpDir, err := os.MkdirTemp("", "coyote-repo-*")
	if err != nil {
		return "", "", fmt.Errorf("creating temp dir: %w", err)
	}

	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(tmpDir)
		}
	}()

	err = runGit(ctx, "", "clone", "--", repoURL, tmpDir)
	if err != nil {
		return "", "", fmt.Errorf("cloning repo %s: %w", repoURL, err)
	}

	err = runGit(ctx, tmpDir, "checkout", "--detach", ref)
	if err != nil {
		return "", "", fmt.Errorf("checking out ref %q: %w", ref, err)
	}

	commitSHA, err := gitOutput(ctx, tmpDir, "rev-parse", "HEAD")
	if err != nil {
		return "", "", fmt.Errorf("resolving commit SHA: %w", err)
	}

	cleanup = false
	return tmpDir, strings.TrimSpace(commitSHA), nil
}

func runGit(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cleanDir := filepath.Clean(dir)
		if !filepath.IsAbs(cleanDir) {
			return errors.New("git working directory must be absolute")
		}
		cmd.Dir = cleanDir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cleanDir := filepath.Clean(dir)
		if !filepath.IsAbs(cleanDir) {
			return "", errors.New("git working directory must be absolute")
		}
		cmd.Dir = cleanDir
	}
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", err
	}
	return string(out), nil
}
