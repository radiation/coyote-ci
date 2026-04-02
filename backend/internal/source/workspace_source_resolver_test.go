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

func TestGitWorkspaceSourceResolver_CloneIntoWorkspace_PreservesWorkspaceDirectoryInode(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found in PATH")
	}

	remoteDir := t.TempDir()
	mustRun(t, remoteDir, "git", "init", "--bare")

	seedDir := t.TempDir()
	mustRun(t, seedDir, "git", "clone", remoteDir, ".")
	mustRun(t, seedDir, "git", "config", "user.email", "test@test.com")
	mustRun(t, seedDir, "git", "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(seedDir, "README.md"), []byte("seed"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRun(t, seedDir, "git", "add", "README.md")
	mustRun(t, seedDir, "git", "commit", "-m", "seed")
	mustRun(t, seedDir, "git", "push", "origin", "HEAD")

	workspacePath := filepath.Join(t.TempDir(), "build-keep-inode")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("creating workspace dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, "stale.txt"), []byte("stale"), 0o644); err != nil {
		t.Fatalf("seeding stale file: %v", err)
	}

	beforeInfo, err := os.Stat(workspacePath)
	if err != nil {
		t.Fatalf("stat before clone: %v", err)
	}

	resolver := NewGitWorkspaceSourceResolver()
	if cloneErr := resolver.CloneIntoWorkspace(context.Background(), workspacePath, remoteDir); cloneErr != nil {
		t.Fatalf("clone failed: %v", cloneErr)
	}

	afterInfo, err := os.Stat(workspacePath)
	if err != nil {
		t.Fatalf("stat after clone: %v", err)
	}
	if !os.SameFile(beforeInfo, afterInfo) {
		t.Fatalf("expected workspace directory inode to stay stable across clone")
	}
	if _, err := os.Stat(filepath.Join(workspacePath, "stale.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected stale workspace content to be removed, got err=%v", err)
	}
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
