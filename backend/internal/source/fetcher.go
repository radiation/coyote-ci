package source

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var refPattern = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)

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
	if err := validateRef(ref); err != nil {
		return "", "", err
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

	err = gitClone(ctx, repoURL, tmpDir)
	if err != nil {
		return "", "", fmt.Errorf("cloning repo %s: %w", repoURL, err)
	}

	resolvedRef, err := resolveRefCommit(ctx, tmpDir, ref)
	if err != nil {
		return "", "", fmt.Errorf("resolving ref %q: %w", ref, err)
	}

	err = gitCheckoutDetach(ctx, tmpDir, resolvedRef)
	if err != nil {
		return "", "", fmt.Errorf("checking out ref %q: %w", ref, err)
	}

	commitSHA, err := gitRevParseHead(ctx, tmpDir)
	if err != nil {
		return "", "", fmt.Errorf("resolving commit SHA: %w", err)
	}

	cleanup = false
	return tmpDir, strings.TrimSpace(commitSHA), nil
}

func gitClone(ctx context.Context, repoURL string, dst string) error {
	cmd := exec.CommandContext(ctx, "git", "clone", "--", repoURL, dst)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func gitCheckoutDetach(ctx context.Context, dir string, commitSHA string) error {
	if !isLikelyCommitSHA(commitSHA) {
		return errors.New("resolved commit is not a full SHA")
	}

	cmd := exec.CommandContext(ctx, "git", "checkout", "--detach", commitSHA)
	if err := setGitDir(cmd, dir); err != nil {
		return err
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func gitRevParseHead(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	if err := setGitDir(cmd, dir); err != nil {
		return "", err
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

func resolveRefCommit(ctx context.Context, dir string, ref string) (string, error) {
	candidates := []string{
		ref + "^{commit}",
		"origin/" + ref + "^{commit}",
		"refs/remotes/origin/" + ref + "^{commit}",
		"refs/tags/" + ref + "^{commit}",
	}

	var lastErr error
	for _, candidate := range candidates {
		out, err := gitRevParseVerify(ctx, dir, candidate)
		if err == nil {
			return strings.TrimSpace(out), nil
		}
		lastErr = err
	}

	if lastErr == nil {
		lastErr = errors.New("unable to resolve ref")
	}
	return "", lastErr
}

func gitRevParseVerify(ctx context.Context, dir string, candidate string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--verify", candidate)
	if err := setGitDir(cmd, dir); err != nil {
		return "", err
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

func setGitDir(cmd *exec.Cmd, dir string) error {
	cleanDir := filepath.Clean(strings.TrimSpace(dir))
	if !filepath.IsAbs(cleanDir) {
		return errors.New("git working directory must be absolute")
	}
	cmd.Dir = cleanDir
	return nil
}

func isLikelyCommitSHA(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, ch := range value {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') && (ch < 'A' || ch > 'F') {
			return false
		}
	}
	return true
}

func validateRef(ref string) error {
	if strings.HasPrefix(ref, "-") {
		return errors.New("ref cannot begin with '-'")
	}
	if strings.Contains(ref, "..") {
		return errors.New("ref contains invalid sequence '..'")
	}
	if strings.Contains(ref, "\\") {
		return errors.New("ref contains invalid character '\\\\'")
	}
	if !refPattern.MatchString(ref) {
		return errors.New("ref contains unsupported characters")
	}
	return nil
}
