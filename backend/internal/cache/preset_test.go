package cache

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePreset_Supported(t *testing.T) {
	cases := []string{"node", "python-uv", "go"}
	for _, preset := range cases {
		resolved, err := ResolvePreset(preset, "backend")
		if err != nil {
			t.Fatalf("resolve preset %s: %v", preset, err)
		}
		if resolved.Name != preset {
			t.Fatalf("expected preset name %s, got %s", preset, resolved.Name)
		}
		if len(resolved.CachePaths) == 0 {
			t.Fatalf("expected cache paths for %s", preset)
		}
		if len(resolved.FingerprintFiles) == 0 {
			t.Fatalf("expected fingerprint files for %s", preset)
		}
	}
}

func TestComputeFingerprint_UsesLockfileContent(t *testing.T) {
	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, "frontend"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	lockfile := filepath.Join(workspace, "frontend", "package-lock.json")
	if err := os.WriteFile(lockfile, []byte("v1"), 0o644); err != nil {
		t.Fatalf("write lockfile: %v", err)
	}

	fp1, files, err := ComputeFingerprint(workspace, []string{"frontend/package-lock.json"})
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected one seen file, got %d", len(files))
	}

	if writeErr := os.WriteFile(lockfile, []byte("v2"), 0o644); writeErr != nil {
		t.Fatalf("write lockfile v2: %v", writeErr)
	}
	fp2, _, err := ComputeFingerprint(workspace, []string{"frontend/package-lock.json"})
	if err != nil {
		t.Fatalf("fingerprint v2: %v", err)
	}
	if fp1 == fp2 {
		t.Fatal("expected fingerprint change when lockfile changes")
	}
}
