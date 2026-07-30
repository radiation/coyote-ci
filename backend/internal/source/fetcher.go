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

// AuthenticatedRepoFetcher is an optional extension for authenticated HTTPS repository fetches.
type AuthenticatedRepoFetcher interface {
	RepoFetcher
	FetchWithHTTPSCredential(ctx context.Context, repoURL string, ref string, credential HTTPSCredential) (localPath string, commitSHA string, err error)
}

// GitFetcher implements RepoFetcher using the git CLI.
type GitFetcher struct{}

func NewGitFetcher() *GitFetcher {
	return &GitFetcher{}
}

func (g *GitFetcher) Fetch(ctx context.Context, repoURL string, ref string) (string, string, error) {
	return g.fetch(ctx, repoURL, ref, nil)
}

func (g *GitFetcher) FetchWithHTTPSCredential(ctx context.Context, repoURL string, ref string, credential HTTPSCredential) (string, string, error) {
	return g.fetch(ctx, repoURL, ref, &credential)
}

func (g *GitFetcher) fetch(ctx context.Context, repoURL string, ref string, credential *HTTPSCredential) (string, string, error) {
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

	if credential != nil {
		err = gitCloneWithHTTPSCredential(ctx, repoURL, tmpDir, *credential)
	} else {
		err = gitClone(ctx, repoURL, tmpDir)
	}
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

func gitCloneWithHTTPSCredential(ctx context.Context, repoURL string, dst string, credential HTTPSCredential) error {
	username := strings.TrimSpace(credential.Username)
	password := strings.TrimSpace(credential.Password)
	if username == "" || password == "" {
		return errors.New("https git credential is required")
	}
	askPassPath, err := createGitTokenAskPassScript()
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(askPassPath) }()

	cmd := exec.CommandContext(ctx, "git", "clone", "--", repoURL, dst)
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ASKPASS="+askPassPath,
		"COYOTE_GIT_ASKPASS_USERNAME="+username,
		"COYOTE_GIT_ASKPASS_TOKEN="+password,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, redactGitSecret(string(out), password))
	}
	return nil
}

func createGitTokenAskPassScript() (string, error) {
	file, err := os.CreateTemp("", "coyote-git-askpass-*")
	if err != nil {
		return "", err
	}
	path := file.Name()
	script := "#!/bin/sh\ncase \"$1\" in\n  *Username*|*username*) printenv COYOTE_GIT_ASKPASS_USERNAME ;;\n  *) printenv COYOTE_GIT_ASKPASS_TOKEN ;;\nesac\n"
	if _, writeErr := file.WriteString(script); writeErr != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return "", writeErr
	}
	if closeErr := file.Close(); closeErr != nil {
		_ = os.Remove(path)
		return "", closeErr
	}
	if chmodErr := os.Chmod(path, 0o700); chmodErr != nil {
		_ = os.Remove(path)
		return "", chmodErr
	}
	return path, nil
}

func redactGitSecret(value string, secret string) string {
	return strings.ReplaceAll(value, strings.TrimSpace(secret), "[REDACTED]")
}

// IsAuthenticationFailure permits a credential refresh only for explicit,
// sanitized Git authentication rejections. Ambiguous failures remain terminal.
func IsAuthenticationFailure(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, indicator := range []string{
		"repository not found", "http 404", "404 not found", "permission denied",
		"access denied", "repository access", "invalid ref", "invalid commit",
		"malformed url", "no such host", "network is unreachable", "timeout",
		"rate limit", "service unavailable",
	} {
		if strings.Contains(message, indicator) {
			return false
		}
	}
	for _, indicator := range []string{
		"authentication failed", "bad credentials", "invalid credentials",
		"credential rejected", "http 401", "401 unauthorized",
	} {
		if strings.Contains(message, indicator) {
			return true
		}
	}
	return false
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
