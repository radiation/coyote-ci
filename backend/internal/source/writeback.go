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

	pushURL, pushEnv, cleanupPushAuth, authErr := pushAuthForCredential(strings.TrimSpace(req.RepositoryURL), req.Credential)
	if authErr != nil {
		return GitWriteBackResult{}, authErr
	}
	defer cleanupPushAuth()

	pushErr := runGitWithEnv(ctx, repoRoot, pushEnv, "push", pushURL, "HEAD:refs/heads/"+req.BranchName)
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

func pushAuthForCredential(repoURL string, credential domain.SourceCredential) (string, []string, func(), error) {
	switch credential.Kind {
	case domain.SourceCredentialKindHTTPSToken:
		secretName := strings.TrimSpace(credential.SecretRef)
		if secretName == "" {
			return "", nil, nil, ErrCredentialSecretMissing
		}
		token := strings.TrimSpace(os.Getenv(secretName))
		if token == "" {
			return "", nil, nil, ErrCredentialSecretMissing
		}
		parsed, err := url.Parse(repoURL)
		if err != nil {
			return "", nil, nil, err
		}
		if parsed.Scheme != "https" {
			return "", nil, nil, fmt.Errorf("https token auth requires https URL")
		}
		parsed.User = nil

		username := "x-access-token"
		if credential.Username != nil && strings.TrimSpace(*credential.Username) != "" {
			username = strings.TrimSpace(*credential.Username)
		}

		askPassPath, askPassErr := createGitAskPassScript()
		if askPassErr != nil {
			return "", nil, nil, askPassErr
		}

		env := []string{
			"GIT_TERMINAL_PROMPT=0",
			"GIT_ASKPASS=" + askPassPath,
			"COYOTE_GIT_ASKPASS_USERNAME=" + username,
			"COYOTE_GIT_ASKPASS_SECRET_REF=" + secretName,
		}
		cleanup := func() {
			_ = os.Remove(askPassPath)
		}

		return parsed.String(), env, cleanup, nil
	case domain.SourceCredentialKindSSHKey:
		return "", nil, nil, ErrSSHWriteNotImplemented
	default:
		return "", nil, nil, fmt.Errorf("unsupported credential kind %q", credential.Kind)
	}
}

func createGitAskPassScript() (string, error) {
	file, err := os.CreateTemp("", "coyote-git-askpass-*")
	if err != nil {
		return "", err
	}

	path := file.Name()
	script := "#!/bin/sh\n" +
		"prompt=\"$1\"\n" +
		"case \"$prompt\" in\n" +
		"  *Username*|*username*)\n" +
		"    printenv COYOTE_GIT_ASKPASS_USERNAME\n" +
		"    ;;\n" +
		"  *)\n" +
		"    secret_ref_name=\"$(printenv COYOTE_GIT_ASKPASS_SECRET_REF)\"\n" +
		"    if [ -n \"$secret_ref_name\" ]; then\n" +
		"      printenv \"$secret_ref_name\"\n" +
		"    fi\n" +
		"    ;;\n" +
		"esac\n"

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

func runGit(ctx context.Context, dir string, args ...string) error {
	return runGitWithEnv(ctx, dir, nil, args...)
}

func runGitWithEnv(ctx context.Context, dir string, env []string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		redactedArgs, redactions := redactGitArgs(args)
		envRedactions := redactSensitiveEnvValues(env)
		redactions = append(redactions, envRedactions...)
		trimmedOut := strings.TrimSpace(string(out))
		for i := range redactions {
			trimmedOut = strings.ReplaceAll(trimmedOut, redactions[i].raw, redactions[i].redacted)
		}
		return fmt.Errorf("git %s failed: %w: %s", strings.Join(redactedArgs, " "), err, trimmedOut)
	}
	return nil
}

type argRedaction struct {
	raw      string
	redacted string
}

func redactGitArgs(args []string) ([]string, []argRedaction) {
	redactedArgs := make([]string, len(args))
	redactions := make([]argRedaction, 0)
	for i, arg := range args {
		redacted := redactSensitiveArg(arg)
		redactedArgs[i] = redacted
		if redacted != arg {
			redactions = append(redactions, argRedaction{raw: arg, redacted: redacted})
		}
	}
	return redactedArgs, redactions
}

func redactSensitiveArg(arg string) string {
	parsed, err := url.Parse(arg)
	if err != nil {
		return arg
	}
	if parsed.Scheme == "" || parsed.User == nil {
		return arg
	}
	username := strings.TrimSpace(parsed.User.Username())
	if username == "" {
		username = "redacted"
	}
	parsed.User = url.User(username)
	return parsed.String()
}

func redactSensitiveEnvValues(env []string) []argRedaction {
	redactions := make([]argRedaction, 0)
	for _, kv := range env {
		idx := strings.Index(kv, "=")
		if idx <= 0 {
			continue
		}
		key := strings.ToUpper(strings.TrimSpace(kv[:idx]))
		value := strings.TrimSpace(kv[idx+1:])
		if value == "" {
			continue
		}
		if strings.Contains(key, "TOKEN") || strings.Contains(key, "SECRET") || strings.Contains(key, "PASSWORD") {
			redactions = append(redactions, argRedaction{raw: value, redacted: "[REDACTED]"})
		}
	}
	return redactions
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
