package cache

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestComputeFingerprint_SortsSkipsMissingAndRejectsEmptyInputs(t *testing.T) {
	root := t.TempDir()
	writeCacheFixture(t, root, "b.lock", "b")
	writeCacheFixture(t, root, "a.lock", "a")

	fingerprint, seen, err := ComputeFingerprint(root, []string{" missing.lock ", "../escape", "b.lock", "", "a.lock"})
	if err != nil {
		t.Fatalf("compute fingerprint failed: %v", err)
	}
	if fingerprint == "" {
		t.Fatal("expected non-empty fingerprint")
	}
	wantSeen := []string{"a.lock", "b.lock"}
	for idx := range wantSeen {
		if seen[idx] != wantSeen[idx] {
			t.Fatalf("seen[%d]: expected %q, got %q", idx, wantSeen[idx], seen[idx])
		}
	}

	if _, _, err := ComputeFingerprint(" ", []string{"a.lock"}); err == nil || err.Error() != "workspace root is required" {
		t.Fatalf("expected workspace root error, got %v", err)
	}
	if _, _, err := ComputeFingerprint(root, []string{"missing.lock", "../escape"}); !errors.Is(err, ErrNoFingerprintFilesFound) {
		t.Fatalf("expected ErrNoFingerprintFilesFound, got %v", err)
	}
}

func TestSecureJoin(t *testing.T) {
	root := t.TempDir()
	joined, err := secureJoin(root, `nested\lock.file`)
	if err != nil {
		t.Fatalf("secure join failed: %v", err)
	}
	want := filepath.Join(root, "nested", "lock.file")
	if joined != want {
		t.Fatalf("expected %q, got %q", want, joined)
	}

	for _, rel := range []string{"/absolute", "../escape", `..\escape`} {
		if _, err := secureJoin(root, rel); err == nil {
			t.Fatalf("expected secure join to reject %q", rel)
		}
	}
}

func writeCacheFixture(t *testing.T, root string, relPath string, content string) {
	t.Helper()
	path := filepath.Join(root, relPath)
	if mkdirErr := os.MkdirAll(filepath.Dir(path), 0o755); mkdirErr != nil {
		t.Fatalf("mkdir fixture: %v", mkdirErr)
	}
	if writeErr := os.WriteFile(path, []byte(content), 0o644); writeErr != nil {
		t.Fatalf("write fixture: %v", writeErr)
	}
}
