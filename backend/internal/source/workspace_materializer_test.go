package source

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

type fakeWorkspaceFetcher struct {
	calls      int
	lastRepo   string
	lastRef    string
	localPath  string
	commitSHA  string
	fetchError error
}

func (f *fakeWorkspaceFetcher) Fetch(_ context.Context, repoURL string, ref string) (string, string, error) {
	f.calls++
	f.lastRepo = repoURL
	f.lastRef = ref
	if f.fetchError != nil {
		return "", "", f.fetchError
	}
	return f.localPath, f.commitSHA, nil
}

func TestHostWorkspaceMaterializer_PrepareWorkspace_CreatesEmptyWorkspace(t *testing.T) {
	root := t.TempDir()
	m := NewHostWorkspaceMaterializer(nil, root)

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

func TestHostWorkspaceMaterializer_PrepareWorkspace_RepoUsesCommitSHAWhenPresent(t *testing.T) {
	root := t.TempDir()
	fetched := t.TempDir()
	if err := os.MkdirAll(filepath.Join(fetched, ".git"), 0o755); err != nil {
		t.Fatalf("create fake .git dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(fetched, "README.md"), []byte("ok"), 0o644); err != nil {
		t.Fatalf("write fetched file: %v", err)
	}

	fetcher := &fakeWorkspaceFetcher{localPath: fetched, commitSHA: "abc123"}
	m := NewHostWorkspaceMaterializer(fetcher, root)

	workspacePath, err := m.PrepareWorkspace(context.Background(), WorkspacePrepareRequest{
		BuildID:   "build-2",
		RepoURL:   "https://example.com/repo.git",
		Ref:       "main",
		CommitSHA: "deadbeef",
	})
	if err != nil {
		t.Fatalf("prepare workspace failed: %v", err)
	}
	if fetcher.calls != 1 {
		t.Fatalf("expected one fetch call, got %d", fetcher.calls)
	}
	if fetcher.lastRef != "deadbeef" {
		t.Fatalf("expected commit sha to be preferred, got %q", fetcher.lastRef)
	}
	if _, statErr := os.Stat(filepath.Join(workspacePath, "README.md")); statErr != nil {
		t.Fatalf("expected materialized file, got %v", statErr)
	}

	// Reuse the same workspace for subsequent steps.
	_, err = m.PrepareWorkspace(context.Background(), WorkspacePrepareRequest{
		BuildID: "build-2",
		RepoURL: "https://example.com/repo.git",
		Ref:     "main",
	})
	if err != nil {
		t.Fatalf("expected workspace reuse, got %v", err)
	}
	if fetcher.calls != 1 {
		t.Fatalf("expected workspace reuse with one fetch call, got %d", fetcher.calls)
	}
}

func TestHostWorkspaceMaterializer_PrepareWorkspace_RepoWithoutPreparedMarkerRematerializes(t *testing.T) {
	root := t.TempDir()
	buildID := "build-4"
	workspacePath := filepath.Join(root, buildID)
	if err := os.MkdirAll(filepath.Join(workspacePath, ".git"), 0o755); err != nil {
		t.Fatalf("create workspace .git: %v", err)
	}

	fetched := t.TempDir()
	if err := os.MkdirAll(filepath.Join(fetched, ".git"), 0o755); err != nil {
		t.Fatalf("create fetched .git: %v", err)
	}
	if err := os.WriteFile(filepath.Join(fetched, "README.md"), []byte("ok"), 0o644); err != nil {
		t.Fatalf("write fetched file: %v", err)
	}

	fetcher := &fakeWorkspaceFetcher{localPath: fetched, commitSHA: "abc123"}
	m := NewHostWorkspaceMaterializer(fetcher, root)

	_, err := m.PrepareWorkspace(context.Background(), WorkspacePrepareRequest{
		BuildID: buildID,
		RepoURL: "https://example.com/repo.git",
		Ref:     "main",
	})
	if err != nil {
		t.Fatalf("prepare workspace failed: %v", err)
	}
	if fetcher.calls != 1 {
		t.Fatalf("expected one fetch call when marker missing, got %d", fetcher.calls)
	}
}

func TestHostWorkspaceMaterializer_CleanupWorkspace(t *testing.T) {
	root := t.TempDir()
	m := NewHostWorkspaceMaterializer(nil, root)
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
