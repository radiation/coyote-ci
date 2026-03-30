package artifact

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCollector_CollectsMatchingFiles(t *testing.T) {
	workspace := t.TempDir()
	storeRoot := t.TempDir()

	mustWriteFile(t, filepath.Join(workspace, "dist", "app"), []byte("binary"))
	mustWriteFile(t, filepath.Join(workspace, "reports", "junit.xml"), []byte("xml"))
	mustWriteFile(t, filepath.Join(workspace, "notes", "skip.txt"), []byte("skip"))

	collector := NewCollector(NewFilesystemStore(storeRoot))
	result, err := collector.Collect(context.Background(), CollectRequest{
		BuildID:       "build-1",
		WorkspacePath: workspace,
		Patterns:      []string{"dist/**", "reports/*.xml", "missing/*.txt"},
	})
	if err != nil {
		t.Fatalf("collect failed: %v", err)
	}
	if len(result.Artifacts) != 2 {
		t.Fatalf("expected 2 artifacts, got %d", len(result.Artifacts))
	}
	if len(result.Warnings) != 1 {
		t.Fatalf("expected 1 warning for unmatched pattern, got %d", len(result.Warnings))
	}

	for _, artifact := range result.Artifacts {
		if artifact.SizeBytes <= 0 {
			t.Fatalf("expected positive size for artifact %q", artifact.LogicalPath)
		}
		if artifact.ChecksumSHA256 == nil || *artifact.ChecksumSHA256 == "" {
			t.Fatalf("expected checksum for artifact %q", artifact.LogicalPath)
		}
	}
}

func TestCollector_MissingWorkspaceDoesNotFail(t *testing.T) {
	collector := NewCollector(NewFilesystemStore(t.TempDir()))
	result, err := collector.Collect(context.Background(), CollectRequest{
		BuildID:       "build-1",
		WorkspacePath: filepath.Join(t.TempDir(), "missing"),
		Patterns:      []string{"dist/**"},
	})
	if err != nil {
		t.Fatalf("expected nil error for missing workspace, got %v", err)
	}
	if len(result.Artifacts) != 0 {
		t.Fatalf("expected no artifacts, got %d", len(result.Artifacts))
	}
	if len(result.Warnings) == 0 {
		t.Fatal("expected warning when workspace is missing")
	}
}

func mustWriteFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}
}
