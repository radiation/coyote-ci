package source

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

func TestHostWorkspaceMaterializer_PrepareWorkspace_CreatesWorkspace(t *testing.T) {
	root := t.TempDir()
	m := NewHostWorkspaceMaterializer(root)

	workspacePath, err := m.PrepareWorkspace(context.Background(), WorkspacePrepareRequest{BuildID: "build-1"})
	if err != nil {
		t.Fatalf("prepare workspace failed: %v", err)
	}

	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		canonicalRoot = root
	}
	expected := filepath.Join(canonicalRoot, "build-1")
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

func TestHostWorkspaceMaterializer_PrepareWorkspace_ReturnsCanonicalWorkspacePath(t *testing.T) {
	realRoot := filepath.Join(t.TempDir(), "real-root")
	if err := os.MkdirAll(realRoot, 0o755); err != nil {
		t.Fatalf("mkdir real root: %v", err)
	}
	symlinkRoot := filepath.Join(t.TempDir(), "root-link")
	if err := os.Symlink(realRoot, symlinkRoot); err != nil {
		t.Fatalf("creating root symlink: %v", err)
	}

	m := NewHostWorkspaceMaterializer(symlinkRoot)
	workspacePath, err := m.PrepareWorkspace(context.Background(), WorkspacePrepareRequest{BuildID: "build-canonical"})
	if err != nil {
		t.Fatalf("prepare workspace failed: %v", err)
	}

	canonicalRoot, err := filepath.EvalSymlinks(realRoot)
	if err != nil {
		t.Fatalf("eval real root symlink: %v", err)
	}
	expected := filepath.Join(canonicalRoot, "build-canonical")
	if workspacePath != expected {
		t.Fatalf("expected canonical workspace %q, got %q", expected, workspacePath)
	}
}

func TestHostWorkspaceMaterializer_MaterializeReusesLinearPredecessorWorkspace(t *testing.T) {
	materializer := NewHostWorkspaceMaterializer(t.TempDir())
	ctx := context.Background()

	sourceWorkspace, err := materializer.Materialize(ctx, MaterializeWorkspaceRequest{
		BuildID: "build-1",
		Input:   domain.WorkspaceInputPlan{Mode: domain.WorkspaceInputModeSource},
	})
	if err != nil {
		t.Fatalf("materialize source workspace: %v", err)
	}
	marker := filepath.Join(sourceWorkspace.Path, "generated.txt")
	if writeErr := os.WriteFile(marker, []byte("generated"), 0o644); writeErr != nil {
		t.Fatalf("write source workspace marker: %v", writeErr)
	}

	predecessorWorkspace, err := materializer.Materialize(ctx, MaterializeWorkspaceRequest{
		BuildID: "build-1",
		Input: domain.WorkspaceInputPlan{
			Mode:           domain.WorkspaceInputModePredecessor,
			ProducerNodeID: "generate",
		},
	})
	if err != nil {
		t.Fatalf("materialize predecessor workspace: %v", err)
	}
	if predecessorWorkspace.Path != sourceWorkspace.Path {
		t.Fatalf("expected predecessor to reuse %q, got %q", sourceWorkspace.Path, predecessorWorkspace.Path)
	}
	if _, statErr := os.Stat(marker); statErr != nil {
		t.Fatalf("expected predecessor to retain source workspace contents: %v", statErr)
	}
}

func TestHostWorkspaceMaterializer_MaterializeRetainsLegacyDirectoryForFanOutAndFanIn(t *testing.T) {
	materializer := NewHostWorkspaceMaterializer(t.TempDir())
	ctx := context.Background()

	sourceWorkspace, err := materializer.Materialize(ctx, MaterializeWorkspaceRequest{
		BuildID: "build-1",
		Input:   domain.WorkspaceInputPlan{Mode: domain.WorkspaceInputModeSource},
	})
	if err != nil {
		t.Fatalf("materialize source workspace: %v", err)
	}

	for _, input := range []domain.WorkspaceInputPlan{
		{
			Mode:                       domain.WorkspaceInputModePredecessor,
			ProducerNodeID:             "compile",
			IsolatedWritableDescendant: true,
		},
		{
			Mode:                 domain.WorkspaceInputModeFanIn,
			CommonAncestorNodeID: "compile",
		},
	} {
		workspace, materializeErr := materializer.Materialize(ctx, MaterializeWorkspaceRequest{BuildID: "build-1", Input: input})
		if materializeErr != nil {
			t.Fatalf("materialize legacy-compatible input %#v: %v", input, materializeErr)
		}
		if workspace.Path != sourceWorkspace.Path {
			t.Fatalf("expected legacy-compatible input %#v to reuse %q, got %q", input, sourceWorkspace.Path, workspace.Path)
		}
	}
}

func TestHostWorkspaceMaterializer_CommitAndRelease(t *testing.T) {
	materializer := NewHostWorkspaceMaterializer(t.TempDir())
	workspace, err := materializer.Materialize(context.Background(), MaterializeWorkspaceRequest{
		BuildID: "build-1",
		Input:   domain.WorkspaceInputPlan{Mode: domain.WorkspaceInputModeSource},
	})
	if err != nil {
		t.Fatalf("materialize workspace: %v", err)
	}
	if commitErr := materializer.Commit(context.Background(), workspace, "claim-1"); commitErr != nil {
		t.Fatalf("commit workspace: %v", commitErr)
	}
	if releaseErr := materializer.Release(context.Background(), workspace); releaseErr != nil {
		t.Fatalf("release workspace: %v", releaseErr)
	}
	if _, statErr := os.Stat(workspace.Path); !os.IsNotExist(statErr) {
		t.Fatalf("expected released workspace to be removed, stat err=%v", statErr)
	}
}
