package cache

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
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

func TestFilesystemStore_ValidationAndExtractionEdges(t *testing.T) {
	ctx := context.Background()
	store := NewFilesystemStore(t.TempDir())

	if _, err := NewFilesystemStore(" ").Restore(ctx, "key", t.TempDir()); err == nil || !strings.Contains(err.Error(), "storage root") {
		t.Fatalf("expected blank root restore error, got %v", err)
	}
	if _, err := store.Restore(ctx, "../escape", t.TempDir()); !errors.Is(err, ErrInvalidCacheKey) {
		t.Fatalf("expected invalid cache key from restore, got %v", err)
	}
	if _, err := store.Restore(ctx, "key", " "); err == nil || !strings.Contains(err.Error(), "destination root") {
		t.Fatalf("expected destination root error, got %v", err)
	}
	if _, err := store.Save(ctx, "key", " "); err == nil || !strings.Contains(err.Error(), "source root") {
		t.Fatalf("expected source root error, got %v", err)
	}
	if _, err := store.Save(ctx, "/absolute", t.TempDir()); !errors.Is(err, ErrInvalidCacheKey) {
		t.Fatalf("expected invalid cache key from save, got %v", err)
	}

	archiveDir := filepath.Join(store.root, "dir-cache.tar.gz")
	if mkdirErr := os.MkdirAll(archiveDir, 0o755); mkdirErr != nil {
		t.Fatalf("mkdir archive dir: %v", mkdirErr)
	}
	if _, err := store.Restore(ctx, "dir-cache", t.TempDir()); err == nil || !strings.Contains(err.Error(), "not an archive file") {
		t.Fatalf("expected archive directory error, got %v", err)
	}

	unsupportedPath := filepath.Join(store.root, "unsupported.tar.gz")
	writeUnsupportedTarArchive(t, unsupportedPath)
	if _, err := store.Restore(ctx, "unsupported", t.TempDir()); err == nil || !strings.Contains(err.Error(), "unsupported tar entry type") {
		t.Fatalf("expected unsupported tar entry error, got %v", err)
	}

	root := t.TempDir()
	for _, entry := range []string{"", ".", "nested/file.txt"} {
		path, err := safeExtractPath(root, entry)
		if err != nil {
			t.Fatalf("safe extract path %q failed: %v", entry, err)
		}
		if !strings.HasPrefix(path, filepath.Clean(root)) {
			t.Fatalf("expected %q to stay under %q", path, root)
		}
	}
	for _, entry := range []string{"../escape", `..\escape`} {
		if _, err := safeExtractPath(root, entry); err == nil || !strings.Contains(err.Error(), "escapes destination") {
			t.Fatalf("expected escape error for %q, got %v", entry, err)
		}
	}
}

func TestFilesystemStore_EvictsOldArchives(t *testing.T) {
	root := t.TempDir()
	store := NewFilesystemStoreWithMaxSize(root, 10)
	oldArchive := filepath.Join(root, "old.tar.gz")
	newArchive := filepath.Join(root, "new.tar.gz")
	if writeErr := os.WriteFile(oldArchive, []byte("old-cache"), 0o644); writeErr != nil {
		t.Fatalf("write old archive: %v", writeErr)
	}
	if writeErr := os.WriteFile(newArchive, []byte("new-cache"), 0o644); writeErr != nil {
		t.Fatalf("write new archive: %v", writeErr)
	}
	oldTime := time.Unix(100, 0)
	newTime := time.Unix(200, 0)
	if chtimesErr := os.Chtimes(oldArchive, oldTime, oldTime); chtimesErr != nil {
		t.Fatalf("chtimes old archive: %v", chtimesErr)
	}
	if chtimesErr := os.Chtimes(newArchive, newTime, newTime); chtimesErr != nil {
		t.Fatalf("chtimes new archive: %v", chtimesErr)
	}

	if err := store.evictIfNeeded(); err != nil {
		t.Fatalf("evict cache archives: %v", err)
	}
	if _, err := os.Stat(oldArchive); !os.IsNotExist(err) {
		t.Fatalf("expected old archive to be evicted, got %v", err)
	}
	if _, err := os.Stat(newArchive); err != nil {
		t.Fatalf("expected new archive to remain, got %v", err)
	}
}

func writeUnsupportedTarArchive(t *testing.T, archivePath string) {
	t.Helper()
	if mkdirErr := os.MkdirAll(filepath.Dir(archivePath), 0o755); mkdirErr != nil {
		t.Fatalf("mkdir archive dir: %v", mkdirErr)
	}
	file, createErr := os.Create(archivePath)
	if createErr != nil {
		t.Fatalf("create archive: %v", createErr)
	}
	gz := gzip.NewWriter(file)
	writer := tar.NewWriter(gz)
	writeErr := writer.WriteHeader(&tar.Header{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "target"})
	closeWriterErr := writer.Close()
	closeGzipErr := gz.Close()
	closeFileErr := file.Close()
	if writeErr != nil {
		t.Fatalf("write tar header: %v", writeErr)
	}
	if closeWriterErr != nil {
		t.Fatalf("close tar writer: %v", closeWriterErr)
	}
	if closeGzipErr != nil {
		t.Fatalf("close gzip writer: %v", closeGzipErr)
	}
	if closeFileErr != nil {
		t.Fatalf("close archive file: %v", closeFileErr)
	}
}
