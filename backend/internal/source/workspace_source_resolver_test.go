package source

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestGitWorkspaceSourceResolver_CloneAndCheckout(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found in PATH")
	}

	remoteDir := t.TempDir()
	mustRun(t, remoteDir, "git", "init", "--bare")

	workDir := t.TempDir()
	mustRun(t, workDir, "git", "clone", remoteDir, ".")
	mustRun(t, workDir, "git", "config", "user.email", "test@test.com")
	mustRun(t, workDir, "git", "config", "user.name", "Test")

	if err := os.MkdirAll(filepath.Join(workDir, ".coyote"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workDir, ".coyote", "pipeline.yml"), []byte("version: 1\nsteps:\n  - name: test\n    run: echo ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRun(t, workDir, "git", "add", ".")
	mustRun(t, workDir, "git", "commit", "-m", "init")
	mustRun(t, workDir, "git", "push", "origin", "HEAD")
	mainSHA := mustOutput(t, workDir, "git", "rev-parse", "HEAD")

	mustRun(t, workDir, "git", "checkout", "-b", "feature/source-phase")
	if err := os.WriteFile(filepath.Join(workDir, "README.md"), []byte("feature"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRun(t, workDir, "git", "add", "README.md")
	mustRun(t, workDir, "git", "commit", "-m", "feature")
	mustRun(t, workDir, "git", "push", "origin", "feature/source-phase")
	featureSHA := mustOutput(t, workDir, "git", "rev-parse", "HEAD")

	resolver := NewGitWorkspaceSourceResolver()
	workspacePath := filepath.Join(t.TempDir(), "build-1")

	t.Run("ref checkout", func(t *testing.T) {
		if err := resolver.CloneIntoWorkspace(context.Background(), workspacePath, remoteDir); err != nil {
			t.Fatalf("clone failed: %v", err)
		}
		resolved, err := resolver.CheckoutWorkspaceSource(context.Background(), workspacePath, WorkspaceSourceSpec{RepositoryURL: remoteDir, Ref: "feature/source-phase"})
		if err != nil {
			t.Fatalf("checkout failed: %v", err)
		}
		if resolved != featureSHA {
			t.Fatalf("expected feature sha %q, got %q", featureSHA, resolved)
		}
	})

	t.Run("commit takes precedence", func(t *testing.T) {
		if err := resolver.CloneIntoWorkspace(context.Background(), workspacePath, remoteDir); err != nil {
			t.Fatalf("clone failed: %v", err)
		}
		resolved, err := resolver.CheckoutWorkspaceSource(context.Background(), workspacePath, WorkspaceSourceSpec{RepositoryURL: remoteDir, Ref: "feature/source-phase", CommitSHA: mainSHA})
		if err != nil {
			t.Fatalf("checkout failed: %v", err)
		}
		if resolved != mainSHA {
			t.Fatalf("expected main sha %q, got %q", mainSHA, resolved)
		}
	})
}

func TestGitWorkspaceSourceResolver_Failures(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found in PATH")
	}

	resolver := NewGitWorkspaceSourceResolver()
	workspacePath := filepath.Join(t.TempDir(), "build-2")

	if err := resolver.CloneIntoWorkspace(context.Background(), workspacePath, ""); !errors.Is(err, ErrRepositoryURLRequired) {
		t.Fatalf("expected ErrRepositoryURLRequired, got %v", err)
	}

	if err := resolver.CloneIntoWorkspace(context.Background(), workspacePath, "/no/such/repo"); !errors.Is(err, ErrCloneFailed) {
		t.Fatalf("expected ErrCloneFailed, got %v", err)
	}
}
