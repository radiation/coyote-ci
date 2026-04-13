package cache

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

func TestFilesystemStore_SaveAndRestore(t *testing.T) {
	store := NewFilesystemStore(t.TempDir())
	source := filepath.Join(t.TempDir(), "src")
	cacheFile := filepath.Join(source, "paths", "000", "mod.cache")
	if err := os.MkdirAll(filepath.Dir(cacheFile), 0o755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	if err := os.WriteFile(cacheFile, []byte("cached-data"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	saveResult, err := store.Save(context.Background(), "v1/job/key", source)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if saveResult.SizeBytes <= 0 {
		t.Fatalf("expected save size > 0, got %d", saveResult.SizeBytes)
	}
	if saveResult.Compression != "tar.gz" {
		t.Fatalf("expected tar.gz compression, got %q", saveResult.Compression)
	}

	destination := filepath.Join(t.TempDir(), "dest")
	restoreResult, err := store.Restore(context.Background(), "v1/job/key", destination)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if !restoreResult.Hit {
		t.Fatal("expected cache hit")
	}

	restored, err := os.ReadFile(filepath.Join(destination, "paths", "000", "mod.cache"))
	if err != nil {
		t.Fatalf("read restored cache file: %v", err)
	}
	if string(restored) != "cached-data" {
		t.Fatalf("unexpected restored content: %q", string(restored))
	}
}

func TestFilesystemStore_RestoreMiss(t *testing.T) {
	store := NewFilesystemStore(t.TempDir())
	result, err := store.Restore(context.Background(), "v1/job/missing", t.TempDir())
	if err != nil {
		t.Fatalf("restore miss: %v", err)
	}
	if result.Hit {
		t.Fatal("expected miss for missing cache")
	}
}

func TestFilesystemStore_SaveRejectsSymlinkContent(t *testing.T) {
	store := NewFilesystemStore(t.TempDir())
	source := filepath.Join(t.TempDir(), "source")
	pathsDir := filepath.Join(source, "paths", "000")
	if err := os.MkdirAll(pathsDir, 0o755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	if err := os.Symlink("/etc/hosts", filepath.Join(pathsDir, "hosts-link")); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	_, err := store.Save(context.Background(), "v1/job/key", source)
	if err == nil {
		t.Fatal("expected symlink save failure")
	}
	if !strings.Contains(err.Error(), "symlinks are not allowed") {
		t.Fatalf("expected symlink error, got %v", err)
	}
}

func TestFilesystemStore_SaveRejectsFIFOContent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mkfifo is not supported on windows")
	}

	store := NewFilesystemStore(t.TempDir())
	source := filepath.Join(t.TempDir(), "source")
	pathsDir := filepath.Join(source, "paths", "000")
	if err := os.MkdirAll(pathsDir, 0o755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	fifoPath := filepath.Join(pathsDir, "pipe")
	if mkfifoErr := syscall.Mkfifo(fifoPath, 0o644); mkfifoErr != nil {
		t.Fatalf("create fifo: %v", mkfifoErr)
	}

	_, saveErr := store.Save(context.Background(), "v1/job/key", source)
	if saveErr == nil {
		t.Fatal("expected fifo save failure")
	}
	if !strings.Contains(saveErr.Error(), "unsupported file type") {
		t.Fatalf("expected unsupported file type error, got %v", saveErr)
	}
}
