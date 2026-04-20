package managedimage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestComputeDependencyFingerprint_DeterministicAndSensitiveToDependencyChange(t *testing.T) {
	repoRoot := t.TempDir()
	mustWrite(t, filepath.Join(repoRoot, "backend", "go.mod"), []byte("module demo\n"))
	mustWrite(t, filepath.Join(repoRoot, ".coyote", "pipeline.yml"), []byte("version: 1\n"))

	fp1, included1, err := ComputeDependencyFingerprint(repoRoot, ".coyote/pipeline.yml")
	if err != nil {
		t.Fatalf("fingerprint failed: %v", err)
	}
	if fp1 == "" {
		t.Fatal("expected non-empty fingerprint")
	}
	if len(included1) == 0 {
		t.Fatal("expected included files")
	}

	fp2, _, err := ComputeDependencyFingerprint(repoRoot, ".coyote/pipeline.yml")
	if err != nil {
		t.Fatalf("fingerprint second pass failed: %v", err)
	}
	if fp1 != fp2 {
		t.Fatalf("expected deterministic fingerprint, got %s vs %s", fp1, fp2)
	}

	mustWrite(t, filepath.Join(repoRoot, "backend", "go.mod"), []byte("module demo\n\nrequire github.com/google/uuid v1.6.0\n"))
	fp3, _, err := ComputeDependencyFingerprint(repoRoot, ".coyote/pipeline.yml")
	if err != nil {
		t.Fatalf("fingerprint third pass failed: %v", err)
	}
	if fp3 == fp1 {
		t.Fatal("expected fingerprint to change after dependency file change")
	}
}

func mustWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
}
