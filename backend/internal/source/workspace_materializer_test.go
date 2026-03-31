package source

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestHostWorkspaceMaterializer_PrepareWorkspace_CreatesWorkspace(t *testing.T) {
	root := t.TempDir()
	m := NewHostWorkspaceMaterializer(root)

	workspacePath, err := m.PrepareWorkspace(context.Background(), WorkspacePrepareRequest{BuildID: "build-1"})
	if err != nil {
		t.Fatalf("prepare workspace failed: %v", err)
	}

	expected := filepath.Join(root, "build-1")
	if workspacePath != expected {
		t.Fatalf("expected workspace %q, got %q", expected, workspacePath)
	}
	if _, err := os.Stat(workspacePath); err != nil {
		t.Fatalf("expected workspace path to exist, got error %v", err)
	}
}

func TestHostWorkspaceMaterializer_PrepareWorkspace_ReusesExistingDirectory(t *testing.T) {
	root := t.TempDir()
	m := NewHostWorkspaceMaterializer(root)

	workspacePath, err := m.PrepareWorkspace(context.Background(), WorkspacePrepareRequest{BuildID: "build-2"})
	if err != nil {
		t.Fatalf("prepare workspace failed: %v", err)
	}

	contentPath := filepath.Join(workspacePath, "README.md")
	if writeErr := os.WriteFile(contentPath, []byte("ok"), 0o644); writeErr != nil {
		t.Fatalf("write workspace content: %v", writeErr)
	}

	workspacePathAgain, err := m.PrepareWorkspace(context.Background(), WorkspacePrepareRequest{BuildID: "build-2", RepoURL: "https://example.com/repo.git", Ref: "main"})
	if err != nil {
		t.Fatalf("prepare workspace failed on reuse: %v", err)
	}
	if workspacePathAgain != workspacePath {
		t.Fatalf("expected same workspace path %q, got %q", workspacePath, workspacePathAgain)
	}
	if _, statErr := os.Stat(contentPath); statErr != nil {
		t.Fatalf("expected workspace content to remain on reuse, got %v", statErr)
	}
}

func TestHostWorkspaceMaterializer_CleanupWorkspace(t *testing.T) {
	root := t.TempDir()
	m := NewHostWorkspaceMaterializer(root)
	workspacePath := filepath.Join(root, "build-3")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	if err := m.CleanupWorkspace(context.Background(), "build-3"); err != nil {
		t.Fatalf("cleanup workspace failed: %v", err)
	}

	if _, err := os.Stat(workspacePath); !os.IsNotExist(err) {
		t.Fatalf("expected workspace to be removed, stat err=%v", err)
	}
}
