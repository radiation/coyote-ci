package cache

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFilesystemStore_SaveAndRestore(t *testing.T) {
	ctx := context.Background()
	store := NewFilesystemStore(t.TempDir())

	source := filepath.Join(t.TempDir(), "source")
	if err := os.MkdirAll(filepath.Join(source, "paths", "000"), 0o755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	if err := os.WriteFile(filepath.Join(source, "paths", "000", "mod.cache"), []byte("cached-data"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	if err := store.Save(ctx, "v1/job/key", source); err != nil {
		t.Fatalf("save: %v", err)
	}

	destination := filepath.Join(t.TempDir(), "destination")
	hit, err := store.Restore(ctx, "v1/job/key", destination)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if !hit {
		t.Fatal("expected cache hit after save")
	}

	data, err := os.ReadFile(filepath.Join(destination, "paths", "000", "mod.cache"))
	if err != nil {
		t.Fatalf("read restored file: %v", err)
	}
	if string(data) != "cached-data" {
		t.Fatalf("unexpected restored content: %q", string(data))
	}
}

func TestFilesystemStore_RestoreMiss(t *testing.T) {
	ctx := context.Background()
	store := NewFilesystemStore(t.TempDir())
	hit, err := store.Restore(ctx, "v1/job/missing", t.TempDir())
	if err != nil {
		t.Fatalf("restore miss: %v", err)
	}
	if hit {
		t.Fatal("expected miss for missing cache key")
	}
}
