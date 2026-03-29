package source

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestGitFetcher_Fetch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found in PATH")
	}

	remoteDir := t.TempDir()
	mustRun(t, remoteDir, "git", "init", "--bare")

	workDir := t.TempDir()
	mustRun(t, workDir, "git", "clone", remoteDir, ".")
	mustRun(t, workDir, "git", "config", "user.email", "test@test.com")
	mustRun(t, workDir, "git", "config", "user.name", "Test")

	pipelineDir := filepath.Join(workDir, ".coyote")
	if err := os.MkdirAll(pipelineDir, 0o755); err != nil {
		t.Fatal(err)
	}
	pipelineContent := []byte("version: 1\nsteps:\n  - name: hello\n    run: echo hi\n")
	if err := os.WriteFile(filepath.Join(pipelineDir, "pipeline.yml"), pipelineContent, 0o644); err != nil {
		t.Fatal(err)
	}

	mustRun(t, workDir, "git", "add", ".")
	mustRun(t, workDir, "git", "commit", "-m", "init")
	mustRun(t, workDir, "git", "push", "origin", "HEAD")

	expectedSHA := mustOutput(t, workDir, "git", "rev-parse", "HEAD")

	fetcher := NewGitFetcher()

	t.Run("fetch by branch", func(t *testing.T) {
		localPath, commitSHA, err := fetcher.Fetch(context.Background(), remoteDir, "master")
		if err != nil {
			localPath, commitSHA, err = fetcher.Fetch(context.Background(), remoteDir, "main")
		}
		if err != nil {
			t.Fatalf("fetch failed: %v", err)
		}
		defer func() { _ = os.RemoveAll(localPath) }()

		if commitSHA != expectedSHA {
			t.Fatalf("expected SHA %q, got %q", expectedSHA, commitSHA)
		}

		pipelinePath := filepath.Join(localPath, ".coyote", "pipeline.yml")
		if _, err := os.Stat(pipelinePath); err != nil {
			t.Fatalf("pipeline file not found at %s: %v", pipelinePath, err)
		}
	})

	t.Run("fetch by commit SHA", func(t *testing.T) {
		localPath, commitSHA, err := fetcher.Fetch(context.Background(), remoteDir, expectedSHA)
		if err != nil {
			t.Fatalf("fetch failed: %v", err)
		}
		defer func() { _ = os.RemoveAll(localPath) }()

		if commitSHA != expectedSHA {
			t.Fatalf("expected SHA %q, got %q", expectedSHA, commitSHA)
		}
	})

	t.Run("invalid ref", func(t *testing.T) {
		_, _, err := fetcher.Fetch(context.Background(), remoteDir, "nonexistent-branch")
		if err == nil {
			t.Fatal("expected error for invalid ref")
		}
	})

	t.Run("invalid repo URL", func(t *testing.T) {
		_, _, err := fetcher.Fetch(context.Background(), "/nonexistent/repo", "main")
		if err == nil {
			t.Fatal("expected error for invalid repo")
		}
	})
}

func mustRun(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, args, err, out)
	}
}

func mustOutput(t *testing.T, dir string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s %v failed: %v", name, args, err)
	}
	result := string(out)
	if len(result) > 0 && result[len(result)-1] == '\n' {
		result = result[:len(result)-1]
	}
	return result
}
