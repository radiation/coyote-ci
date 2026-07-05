package atomicfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReplaceFileAtomicReplacesExistingDestination(t *testing.T) {
	tempDir := t.TempDir()
	source := filepath.Join(tempDir, "source.txt")
	destination := filepath.Join(tempDir, "destination.txt")

	if err := os.WriteFile(source, []byte("new"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if err := os.WriteFile(destination, []byte("old"), 0o644); err != nil {
		t.Fatalf("write destination: %v", err)
	}

	if err := ReplaceFileAtomic(source, destination); err != nil {
		t.Fatalf("ReplaceFileAtomic failed: %v", err)
	}
	body, err := os.ReadFile(destination)
	if err != nil {
		t.Fatalf("read destination: %v", err)
	}
	if string(body) != "new" {
		t.Fatalf("unexpected destination body: %q", string(body))
	}
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Fatalf("expected source to be removed, got err=%v", err)
	}
}
