package artifact

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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

func TestCollector_RejectsSymlinkArtifacts(t *testing.T) {
	workspace := t.TempDir()
	storeRoot := t.TempDir()
	outsideRoot := t.TempDir()

	outsideFile := filepath.Join(outsideRoot, "secret.txt")
	mustWriteFile(t, outsideFile, []byte("do-not-collect"))

	if err := os.MkdirAll(filepath.Join(workspace, "dist"), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	symlinkPath := filepath.Join(workspace, "dist", "secret.txt")
	if err := os.Symlink(outsideFile, symlinkPath); err != nil {
		t.Skipf("symlink not supported in this environment: %v", err)
	}

	collector := NewCollector(NewFilesystemStore(storeRoot))
	result, err := collector.Collect(context.Background(), CollectRequest{
		BuildID:       "build-1",
		WorkspacePath: workspace,
		Patterns:      []string{"dist/**"},
	})
	if err == nil {
		t.Fatal("expected symlink artifact to be rejected")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "symlink") {
		t.Fatalf("expected symlink-related error, got %v", err)
	}
	if len(result.Artifacts) != 0 {
		t.Fatalf("expected no persisted artifacts, got %d", len(result.Artifacts))
	}
}

func TestCollector_CollectResultArtifacts_AreSortedByLogicalPath(t *testing.T) {
	workspace := t.TempDir()
	storeRoot := t.TempDir()

	mustWriteFile(t, filepath.Join(workspace, "reports", "z.xml"), []byte("z"))
	mustWriteFile(t, filepath.Join(workspace, "dist", "a.txt"), []byte("a"))
	mustWriteFile(t, filepath.Join(workspace, "dist", "m.txt"), []byte("m"))

	collector := NewCollector(NewFilesystemStore(storeRoot))
	result, err := collector.Collect(context.Background(), CollectRequest{
		BuildID:       "build-1",
		WorkspacePath: workspace,
		Patterns:      []string{"dist/**", "reports/*.xml"},
	})
	if err != nil {
		t.Fatalf("collect failed: %v", err)
	}
	if len(result.Artifacts) != 3 {
		t.Fatalf("expected 3 artifacts, got %d", len(result.Artifacts))
	}

	got := []string{
		result.Artifacts[0].LogicalPath,
		result.Artifacts[1].LogicalPath,
		result.Artifacts[2].LogicalPath,
	}
	want := []string{"dist/a.txt", "dist/m.txt", "reports/z.xml"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected sorted artifacts %v, got %v", want, got)
		}
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
