package source

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

func TestPushURLWithCredential_HTTPSToken(t *testing.T) {
	t.Setenv("COYOTE_GIT_TOKEN", "secret-token")
	cred := domain.SourceCredential{
		Kind:      domain.SourceCredentialKindHTTPSToken,
		SecretRef: "COYOTE_GIT_TOKEN",
	}
	url, env, cleanup, err := pushAuthForCredential("https://github.com/example/repo.git", cred)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer cleanup()

	if strings.Contains(url, "secret-token") {
		t.Fatalf("expected token to be absent from push URL: %s", url)
	}
	if strings.Contains(url, "@github.com") {
		t.Fatalf("expected URL without embedded credentials: %s", url)
	}
	if !strings.Contains(url, "https://github.com/example/repo.git") {
		t.Fatalf("unexpected push URL: %s", url)
	}

	askPassPath := envValue(env, "GIT_ASKPASS")
	if strings.TrimSpace(askPassPath) == "" {
		t.Fatal("expected GIT_ASKPASS to be configured")
	}
	if _, statErr := os.Stat(askPassPath); statErr != nil {
		t.Fatalf("expected askpass script to exist: %v", statErr)
	}
	if strings.Contains(strings.Join(env, "\n"), "secret-token") {
		t.Fatal("expected token to be absent from configured env")
	}
	if envValue(env, "COYOTE_GIT_ASKPASS_SECRET_REF") != "COYOTE_GIT_TOKEN" {
		t.Fatalf("expected secret ref env to be configured, got %q", envValue(env, "COYOTE_GIT_ASKPASS_SECRET_REF"))
	}
}

func TestPushURLWithCredential_SSHNotImplemented(t *testing.T) {
	cred := domain.SourceCredential{Kind: domain.SourceCredentialKindSSHKey, SecretRef: "SSH_KEY"}
	_, _, _, err := pushAuthForCredential("git@github.com:example/repo.git", cred)
	if err == nil || !strings.Contains(err.Error(), ErrSSHWriteNotImplemented.Error()) {
		t.Fatalf("expected ssh not implemented, got: %v", err)
	}
}

func TestPushAuthForCredential_ValidationUsernameAndCleanup(t *testing.T) {
	username := " coyote-bot "
	t.Setenv("COYOTE_GIT_TOKEN", "secret-token")
	pushURL, env, cleanup, err := pushAuthForCredential("https://user:old-secret@github.com/example/repo.git", domain.SourceCredential{
		Kind:      domain.SourceCredentialKindHTTPSToken,
		SecretRef: " COYOTE_GIT_TOKEN ",
		Username:  &username,
	})
	if err != nil {
		t.Fatalf("push auth failed: %v", err)
	}
	askPassPath := envValue(env, "GIT_ASKPASS")
	if askPassPath == "" {
		t.Fatal("expected askpass path")
	}
	if pushURL != "https://github.com/example/repo.git" {
		t.Fatalf("expected sanitized push URL, got %q", pushURL)
	}
	if got := envValue(env, "COYOTE_GIT_ASKPASS_USERNAME"); got != "coyote-bot" {
		t.Fatalf("expected custom username, got %q", got)
	}
	cleanup()
	if _, statErr := os.Stat(askPassPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected cleanup to remove askpass script, got %v", statErr)
	}

	tests := []struct {
		name        string
		repoURL     string
		cred        domain.SourceCredential
		wantErr     error
		wantMessage string
	}{
		{name: "blank secret ref", repoURL: "https://github.com/example/repo.git", cred: domain.SourceCredential{Kind: domain.SourceCredentialKindHTTPSToken}, wantErr: ErrCredentialSecretMissing},
		{name: "missing secret env", repoURL: "https://github.com/example/repo.git", cred: domain.SourceCredential{Kind: domain.SourceCredentialKindHTTPSToken, SecretRef: "MISSING_TOKEN"}, wantErr: ErrCredentialSecretMissing},
		{name: "non https url", repoURL: "http://github.com/example/repo.git", cred: domain.SourceCredential{Kind: domain.SourceCredentialKindHTTPSToken, SecretRef: "COYOTE_GIT_TOKEN"}, wantMessage: "requires https"},
		{name: "unsupported kind", repoURL: "https://github.com/example/repo.git", cred: domain.SourceCredential{Kind: "bearer", SecretRef: "COYOTE_GIT_TOKEN"}, wantMessage: "unsupported credential kind"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, authErr := pushAuthForCredential(tc.repoURL, tc.cred)
			if tc.wantErr != nil && !errors.Is(authErr, tc.wantErr) {
				t.Fatalf("expected %v, got %v", tc.wantErr, authErr)
			}
			if tc.wantMessage != "" && (authErr == nil || !strings.Contains(authErr.Error(), tc.wantMessage)) {
				t.Fatalf("expected error containing %q, got %v", tc.wantMessage, authErr)
			}
		})
	}
}

func TestWritebackRedactionAndPathHelpers(t *testing.T) {
	if isMissingRemoteBranchError(nil) {
		t.Fatal("nil error should not be a missing remote branch")
	}
	for _, message := range []string{"fatal: couldn't find remote ref refs/heads/missing", "fatal: not our ref abc"} {
		if !isMissingRemoteBranchError(errors.New(message)) {
			t.Fatalf("expected missing branch error for %q", message)
		}
	}
	if isMissingRemoteBranchError(errors.New("permission denied")) {
		t.Fatal("permission errors should not be treated as missing remote branch")
	}

	redactedArgs, redactions := redactGitArgs([]string{"push", "https://token:secret@github.com/example/repo.git", "https://:secret@github.com/example/other.git", "origin"})
	if strings.Contains(strings.Join(redactedArgs, " "), "secret") {
		t.Fatalf("expected secrets to be redacted from args: %#v", redactedArgs)
	}
	if len(redactions) != 2 {
		t.Fatalf("expected 2 arg redactions, got %#v", redactions)
	}

	envRedactions := redactSensitiveEnvValues([]string{"TOKEN=secret-token", "PASSWORD=hunter2", "EMPTY_SECRET=", "NORMAL=value", "MALFORMED"})
	if len(envRedactions) != 2 {
		t.Fatalf("expected 2 env redactions, got %#v", envRedactions)
	}

	abs, err := cleanAbsPath(" " + t.TempDir() + " ")
	if err != nil {
		t.Fatalf("clean absolute path failed: %v", err)
	}
	if !filepath.IsAbs(abs) {
		t.Fatalf("expected absolute path, got %q", abs)
	}
	for _, value := range []string{" ", "relative/path"} {
		if _, err := cleanAbsPath(value); err == nil {
			t.Fatalf("expected cleanAbsPath to reject %q", value)
		}
	}
}

func TestCommitAndPushPipelineUpdate_UsesBranchStrategy(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required for write-back test")
	}

	ctx := context.Background()
	baseDir := t.TempDir()
	remoteDir := filepath.Join(baseDir, "remote.git")
	localDir := filepath.Join(baseDir, "local")

	mustRunGit(t, baseDir, "init", "--bare", remoteDir)
	mustRunGit(t, baseDir, "clone", remoteDir, localDir)
	mustRunGit(t, localDir, "config", "user.name", "test")
	mustRunGit(t, localDir, "config", "user.email", "test@example.com")
	mustWriteFile(t, filepath.Join(localDir, ".coyote", "pipeline.yml"), []byte("version: 1\npipeline:\n  image: golang:1.27.1\n"))
	mustRunGit(t, localDir, "add", ".")
	mustRunGit(t, localDir, "commit", "-m", "initial")
	mustRunGit(t, localDir, "push", "origin", "HEAD:main")

	t.Setenv("COYOTE_GIT_TOKEN", "unused-local-test")

	client := NewGitWriteBackClient()
	credential := domain.SourceCredential{
		Kind:      domain.SourceCredentialKindHTTPSToken,
		SecretRef: "COYOTE_GIT_TOKEN",
	}

	result, err := client.CommitAndPushPipelineUpdate(ctx, GitWriteBackRequest{
		RepositoryURL: "https://example.invalid/repo.git",
		RepoRoot:      localDir,
		PipelinePath:  ".coyote/pipeline.yml",
		BranchName:    "coyote/managed-image-refresh/fp-abc123",
		CommitMessage: "chore(coyote): refresh managed build image to immutable digest",
		Content:       []byte("version: 1\npipeline:\n  image: registry.example.com/coyote/go@sha256:1234\n"),
		AuthorName:    "Coyote CI Bot",
		AuthorEmail:   "bot@coyote-ci.local",
		Credential:    credential,
	})
	if err == nil {
		// We expect push to https URL to fail in this local-only test.
		t.Fatalf("expected push failure due to remote URL, got success: %+v", result)
	}

	branchOut := mustGitOutput(t, localDir, "rev-parse", "--abbrev-ref", "HEAD")
	if strings.TrimSpace(branchOut) != "coyote/managed-image-refresh/fp-abc123" {
		t.Fatalf("expected bot branch checkout, got %q", strings.TrimSpace(branchOut))
	}

	content, readErr := os.ReadFile(filepath.Join(localDir, ".coyote", "pipeline.yml"))
	if readErr != nil {
		t.Fatalf("read updated pipeline: %v", readErr)
	}
	if !strings.Contains(string(content), "@sha256:1234") {
		t.Fatalf("expected immutable digest pin in updated pipeline: %s", string(content))
	}
}

func TestCommitAndPushPipelineUpdate_BasesOnExistingRemoteBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required for write-back test")
	}

	ctx := context.Background()
	baseDir := t.TempDir()
	remoteDir := filepath.Join(baseDir, "remote.git")
	seedDir := filepath.Join(baseDir, "seed")
	localDir := filepath.Join(baseDir, "local")
	branchName := "coyote/managed-image-refresh/fp-abc123"

	mustRunGit(t, baseDir, "init", "--bare", remoteDir)
	mustRunGit(t, baseDir, "clone", remoteDir, seedDir)
	mustRunGit(t, seedDir, "config", "user.name", "test")
	mustRunGit(t, seedDir, "config", "user.email", "test@example.com")
	mustWriteFile(t, filepath.Join(seedDir, ".coyote", "pipeline.yml"), []byte("version: 1\npipeline:\n  image: golang:1.27.1\n"))
	mustRunGit(t, seedDir, "add", ".")
	mustRunGit(t, seedDir, "commit", "-m", "initial")
	mustRunGit(t, seedDir, "push", "origin", "HEAD:main")
	mustRunGit(t, seedDir, "checkout", "-B", branchName)
	mustWriteFile(t, filepath.Join(seedDir, "branch-marker.txt"), []byte("remote branch content\n"))
	mustRunGit(t, seedDir, "add", ".")
	mustRunGit(t, seedDir, "commit", "-m", "existing bot branch")
	mustRunGit(t, seedDir, "push", "origin", "HEAD:refs/heads/"+branchName)

	mustRunGit(t, baseDir, "clone", remoteDir, localDir)
	mustRunGit(t, localDir, "config", "user.name", "test")
	mustRunGit(t, localDir, "config", "user.email", "test@example.com")

	t.Setenv("COYOTE_GIT_TOKEN", "unused-local-test")

	client := NewGitWriteBackClient()
	credential := domain.SourceCredential{
		Kind:      domain.SourceCredentialKindHTTPSToken,
		SecretRef: "COYOTE_GIT_TOKEN",
	}

	_, err := client.CommitAndPushPipelineUpdate(ctx, GitWriteBackRequest{
		RepositoryURL: "https://example.invalid/repo.git",
		RepoRoot:      localDir,
		PipelinePath:  ".coyote/pipeline.yml",
		BranchName:    branchName,
		CommitMessage: "chore(coyote): refresh managed build image to immutable digest",
		Content:       []byte("version: 1\npipeline:\n  image: registry.example.com/coyote/go@sha256:1234\n"),
		AuthorName:    "Coyote CI Bot",
		AuthorEmail:   "bot@coyote-ci.local",
		Credential:    credential,
	})
	if err == nil {
		t.Fatal("expected push failure due to invalid https remote")
	}

	marker, readErr := os.ReadFile(filepath.Join(localDir, "branch-marker.txt"))
	if readErr != nil {
		t.Fatalf("expected fetched remote branch content to exist locally: %v", readErr)
	}
	if strings.TrimSpace(string(marker)) != "remote branch content" {
		t.Fatalf("unexpected marker content: %q", string(marker))
	}
}

func mustRunGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v: %s", strings.Join(args, " "), err, string(out))
	}
}

func mustGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v: %s", strings.Join(args, " "), err, string(out))
	}
	return string(out)
}

func mustWriteFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

func envValue(env []string, key string) string {
	prefix := key + "="
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			return strings.TrimPrefix(kv, prefix)
		}
	}
	return ""
}
