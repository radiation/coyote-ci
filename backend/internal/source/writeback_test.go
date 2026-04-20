package source

import (
	"context"
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
	url, err := pushURLWithCredential("https://github.com/example/repo.git", cred)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(url, "x-access-token:secret-token@github.com") {
		t.Fatalf("unexpected push URL: %s", url)
	}
}

func TestPushURLWithCredential_SSHNotImplemented(t *testing.T) {
	cred := domain.SourceCredential{Kind: domain.SourceCredentialKindSSHKey, SecretRef: "SSH_KEY"}
	_, err := pushURLWithCredential("git@github.com:example/repo.git", cred)
	if err == nil || !strings.Contains(err.Error(), ErrSSHWriteNotImplemented.Error()) {
		t.Fatalf("expected ssh not implemented, got: %v", err)
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
	mustWriteFile(t, filepath.Join(localDir, ".coyote", "pipeline.yml"), []byte("version: 1\npipeline:\n  image: golang:1.26.2\n"))
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
