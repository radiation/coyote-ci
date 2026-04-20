package source

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

var ErrCredentialSecretMissing = errors.New("source credential secret is missing")
var ErrSSHWriteNotImplemented = errors.New("ssh write-back is not implemented")

type GitWriteBackRequest struct {
	RepositoryURL string
	RepoRoot      string
	PipelinePath  string
	BranchName    string
	CommitMessage string
	Content       []byte

	AuthorName  string
	AuthorEmail string

	Credential domain.SourceCredential
}

type GitWriteBackResult struct {
	BranchName    string
	CommitSHA     string
	RemoteRef     string
	RepositoryURL string
}

type GitWriteBackClient struct{}

func NewGitWriteBackClient() *GitWriteBackClient {
	return &GitWriteBackClient{}
}

func (c *GitWriteBackClient) CommitAndPushPipelineUpdate(ctx context.Context, req GitWriteBackRequest) (GitWriteBackResult, error) {
	repoRoot, err := cleanAbsPath(req.RepoRoot)
	if err != nil {
		return GitWriteBackResult{}, err
	}
	pipelinePath := filepath.Clean(strings.TrimSpace(req.PipelinePath))
	if pipelinePath == "." || strings.HasPrefix(pipelinePath, "..") || filepath.IsAbs(pipelinePath) {
		return GitWriteBackResult{}, fmt.Errorf("invalid pipeline path %q", req.PipelinePath)
	}
	if strings.TrimSpace(req.BranchName) == "" {
		return GitWriteBackResult{}, errors.New("branch name is required")
	}
	if strings.TrimSpace(req.CommitMessage) == "" {
		return GitWriteBackResult{}, errors.New("commit message is required")
	}

	fullPath := filepath.Join(repoRoot, pipelinePath)
	mkdirErr := os.MkdirAll(filepath.Dir(fullPath), 0o755)
	if mkdirErr != nil {
		return GitWriteBackResult{}, mkdirErr
	}
	writeErr := os.WriteFile(fullPath, req.Content, 0o644)
	if writeErr != nil {
		return GitWriteBackResult{}, writeErr
	}

	checkoutErr := runGit(ctx, repoRoot, "checkout", "-B", req.BranchName)
	if checkoutErr != nil {
		return GitWriteBackResult{}, checkoutErr
	}
	addErr := runGit(ctx, repoRoot, "add", "--", pipelinePath)
	if addErr != nil {
		return GitWriteBackResult{}, addErr
	}

	commitEnv := []string{}
	authorName := strings.TrimSpace(req.AuthorName)
	authorEmail := strings.TrimSpace(req.AuthorEmail)
	if authorName != "" {
		commitEnv = append(commitEnv, "GIT_AUTHOR_NAME="+authorName, "GIT_COMMITTER_NAME="+authorName)
	}
	if authorEmail != "" {
		commitEnv = append(commitEnv, "GIT_AUTHOR_EMAIL="+authorEmail, "GIT_COMMITTER_EMAIL="+authorEmail)
	}
	commitErr := runGitWithEnv(ctx, repoRoot, commitEnv, "commit", "-m", req.CommitMessage)
	if commitErr != nil {
		if strings.Contains(commitErr.Error(), "nothing to commit") {
			sha, shaErr := gitRevParseHead(ctx, repoRoot)
			if shaErr != nil {
				return GitWriteBackResult{}, shaErr
			}
			return GitWriteBackResult{BranchName: req.BranchName, CommitSHA: strings.TrimSpace(sha), RemoteRef: "refs/heads/" + req.BranchName, RepositoryURL: req.RepositoryURL}, nil
		}
		return GitWriteBackResult{}, commitErr
	}

	sha, err := gitRevParseHead(ctx, repoRoot)
	if err != nil {
		return GitWriteBackResult{}, err
	}

	pushURL, err := pushURLWithCredential(strings.TrimSpace(req.RepositoryURL), req.Credential)
	if err != nil {
		return GitWriteBackResult{}, err
	}
	pushErr := runGit(ctx, repoRoot, "push", pushURL, "HEAD:refs/heads/"+req.BranchName)
	if pushErr != nil {
		return GitWriteBackResult{}, pushErr
	}

	return GitWriteBackResult{
		BranchName:    req.BranchName,
		CommitSHA:     strings.TrimSpace(sha),
		RemoteRef:     "refs/heads/" + req.BranchName,
		RepositoryURL: req.RepositoryURL,
	}, nil
}

func pushURLWithCredential(repoURL string, credential domain.SourceCredential) (string, error) {
	switch credential.Kind {
	case domain.SourceCredentialKindHTTPSToken:
		secretName := strings.TrimSpace(credential.SecretRef)
		if secretName == "" {
			return "", ErrCredentialSecretMissing
		}
		token := strings.TrimSpace(os.Getenv(secretName))
		if token == "" {
			return "", ErrCredentialSecretMissing
		}
		parsed, err := url.Parse(repoURL)
		if err != nil {
			return "", err
		}
		if parsed.Scheme != "https" {
			return "", fmt.Errorf("https token auth requires https URL")
		}
		username := "x-access-token"
		if credential.Username != nil && strings.TrimSpace(*credential.Username) != "" {
			username = strings.TrimSpace(*credential.Username)
		}
		parsed.User = url.UserPassword(username, token)
		return parsed.String(), nil
	case domain.SourceCredentialKindSSHKey:
		return "", ErrSSHWriteNotImplemented
	default:
		return "", fmt.Errorf("unsupported credential kind %q", credential.Kind)
	}
}

func runGit(ctx context.Context, dir string, args ...string) error {
	return runGitWithEnv(ctx, dir, nil, args...)
}

func runGitWithEnv(ctx context.Context, dir string, env []string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s failed: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func cleanAbsPath(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", errors.New("path is required")
	}
	cleaned := filepath.Clean(trimmed)
	if !filepath.IsAbs(cleaned) {
		return "", errors.New("path must be absolute")
	}
	return cleaned, nil
}
