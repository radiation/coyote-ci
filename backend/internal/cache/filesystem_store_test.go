package cache

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestFilesystemStore_EvictsOldestEntriesWhenOverLimit(t *testing.T) {
	ctx := context.Background()
	store := NewFilesystemStoreWithMaxSize(t.TempDir(), 1024)

	sourceA := filepath.Join(t.TempDir(), "source-a")
	if err := os.MkdirAll(filepath.Join(sourceA, "paths", "000"), 0o755); err != nil {
		t.Fatalf("mkdir source-a: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceA, "paths", "000", "cache.bin"), make([]byte, 800), 0o644); err != nil {
		t.Fatalf("write source-a: %v", err)
	}
	if err := store.Save(ctx, "v1/job/key-a", sourceA); err != nil {
		t.Fatalf("save key-a: %v", err)
	}

	entryAPath, err := store.resolvePathForKey("v1/job/key-a")
	if err != nil {
		t.Fatalf("resolve key-a path: %v", err)
	}
	old := time.Now().UTC().Add(-2 * time.Hour)
	if chtimesErr := os.Chtimes(entryAPath, old, old); chtimesErr != nil {
		t.Fatalf("touch key-a old: %v", chtimesErr)
	}

	sourceB := filepath.Join(t.TempDir(), "source-b")
	if mkdirErr := os.MkdirAll(filepath.Join(sourceB, "paths", "000"), 0o755); mkdirErr != nil {
		t.Fatalf("mkdir source-b: %v", mkdirErr)
	}
	if writeErr := os.WriteFile(filepath.Join(sourceB, "paths", "000", "cache.bin"), make([]byte, 800), 0o644); writeErr != nil {
		t.Fatalf("write source-b: %v", writeErr)
	}
	if saveErr := store.Save(ctx, "v1/job/key-b", sourceB); saveErr != nil {
		t.Fatalf("save key-b: %v", saveErr)
	}

	hitA, err := store.Restore(ctx, "v1/job/key-a", filepath.Join(t.TempDir(), "dest-a"))
	if err != nil {
		t.Fatalf("restore key-a: %v", err)
	}
	if hitA {
		t.Fatal("expected key-a to be evicted")
	}

	hitB, err := store.Restore(ctx, "v1/job/key-b", filepath.Join(t.TempDir(), "dest-b"))
	if err != nil {
		t.Fatalf("restore key-b: %v", err)
	}
	if !hitB {
		t.Fatal("expected key-b to remain after eviction")
	}
}

func TestFilesystemStore_SaveRejectsSymlinkContent(t *testing.T) {
	ctx := context.Background()
	store := NewFilesystemStore(t.TempDir())

	source := filepath.Join(t.TempDir(), "source")
	pathsDir := filepath.Join(source, "paths", "000")
	if err := os.MkdirAll(pathsDir, 0o755); err != nil {
		t.Fatalf("mkdir source paths: %v", err)
	}
	if err := os.Symlink("/etc/hosts", filepath.Join(pathsDir, "hosts-link")); err != nil {
		t.Fatalf("create symlink fixture: %v", err)
	}

	err := store.Save(ctx, "v1/job/symlink-save", source)
	if err == nil {
		t.Fatal("expected save to fail when cache content includes a symlink")
	}
	if !strings.Contains(err.Error(), "symlinks are not allowed") {
		t.Fatalf("expected symlink rejection error, got %v", err)
	}
}

func TestFilesystemStore_RestoreRejectsSymlinkContent(t *testing.T) {
	ctx := context.Background()
	store := NewFilesystemStore(t.TempDir())

	entryPath, err := store.resolvePathForKey("v1/job/symlink-restore")
	if err != nil {
		t.Fatalf("resolve key path: %v", err)
	}
	pathsDir := filepath.Join(entryPath, "paths", "000")
	err = os.MkdirAll(pathsDir, 0o755)
	if err != nil {
		t.Fatalf("mkdir cache entry paths: %v", err)
	}
	err = os.Symlink("/etc/hosts", filepath.Join(pathsDir, "hosts-link"))
	if err != nil {
		t.Fatalf("create symlink fixture: %v", err)
	}

	_, err = store.Restore(ctx, "v1/job/symlink-restore", filepath.Join(t.TempDir(), "dest"))
	if err == nil {
		t.Fatal("expected restore to fail when cache entry includes a symlink")
	}
	if !strings.Contains(err.Error(), "symlinks are not allowed") {
		t.Fatalf("expected symlink rejection error, got %v", err)
	}
}

func TestFilesystemStore_SaveSkipsRewriteWhenSnapshotUnchanged(t *testing.T) {
	ctx := context.Background()
	store := NewFilesystemStore(t.TempDir())

	source := filepath.Join(t.TempDir(), "source")
	cacheFile := filepath.Join(source, "paths", "000", "mod.cache")
	if err := os.MkdirAll(filepath.Dir(cacheFile), 0o755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	if err := os.WriteFile(cacheFile, []byte("cached-data"), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	if err := store.Save(ctx, "v1/job/unchanged", source); err != nil {
		t.Fatalf("initial save: %v", err)
	}

	entryPath, err := store.resolvePathForKey("v1/job/unchanged")
	if err != nil {
		t.Fatalf("resolve key path: %v", err)
	}
	entryFile := filepath.Join(entryPath, "paths", "000", "mod.cache")

	sentinel := time.Now().UTC().Add(-1 * time.Hour).Round(time.Second)
	err = os.Chtimes(entryFile, sentinel, sentinel)
	if err != nil {
		t.Fatalf("set sentinel mtime: %v", err)
	}

	err = store.Save(ctx, "v1/job/unchanged", source)
	if err != nil {
		t.Fatalf("second save: %v", err)
	}

	info, err := os.Stat(entryFile)
	if err != nil {
		t.Fatalf("stat entry file: %v", err)
	}
	if !info.ModTime().Equal(sentinel) {
		t.Fatalf("expected unchanged snapshot save to preserve entry file mtime, want=%s got=%s", sentinel, info.ModTime())
	}
}
