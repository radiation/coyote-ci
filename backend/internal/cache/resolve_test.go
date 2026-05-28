package cache

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveStore_FilesystemDefaults(t *testing.T) {
	store, resolveErr := ResolveStore(StoreConfig{Provider: " filesystem ", StorageRoot: t.TempDir(), MaxSizeMB: 1})
	if resolveErr != nil {
		t.Fatalf("resolve filesystem cache store: %v", resolveErr)
	}
	if store.Provider() != "filesystem" {
		t.Fatalf("expected filesystem provider, got %q", store.Provider())
	}

	defaultStore, defaultErr := ResolveStore(StoreConfig{StorageRoot: t.TempDir()})
	if defaultErr != nil {
		t.Fatalf("resolve default cache store: %v", defaultErr)
	}
	if defaultStore.Provider() != "filesystem" {
		t.Fatalf("expected default filesystem provider, got %q", defaultStore.Provider())
	}
}

func TestResolveStore_GCSMissingBucketFallbackAndStrictError(t *testing.T) {
	store, resolveErr := ResolveStore(StoreConfig{Provider: "gcs", StorageRoot: t.TempDir(), Strict: false})
	if resolveErr != nil {
		t.Fatalf("resolve gcs fallback cache store: %v", resolveErr)
	}
	if store.Provider() != "filesystem" {
		t.Fatalf("expected filesystem fallback provider, got %q", store.Provider())
	}

	_, strictErr := ResolveStore(StoreConfig{Provider: "gcs", Strict: true})
	if strictErr == nil {
		t.Fatal("expected strict missing bucket error")
	}
	if !strings.Contains(strictErr.Error(), "WORKER_CACHE_GCS_BUCKET") {
		t.Fatalf("expected bucket error, got %v", strictErr)
	}
}

func TestResolveStore_UnsupportedProvider(t *testing.T) {
	_, resolveErr := ResolveStore(StoreConfig{Provider: "redis"})
	if resolveErr == nil {
		t.Fatal("expected unsupported provider error")
	}
	if !strings.Contains(resolveErr.Error(), "unsupported cache storage provider") {
		t.Fatalf("unexpected unsupported provider error: %v", resolveErr)
	}
}

func TestFilesystemStore_TotalSizeBytesCountsArchives(t *testing.T) {
	store := NewFilesystemStore(t.TempDir())
	source := t.TempDir()
	writeCacheTestFile(t, source+"/paths/000/cache.txt", "cached")

	if _, saveErr := store.Save(context.Background(), "v1/job/key", source); saveErr != nil {
		t.Fatalf("save cache: %v", saveErr)
	}

	total, sizeErr := store.TotalSizeBytes()
	if sizeErr != nil {
		t.Fatalf("total size bytes: %v", sizeErr)
	}
	if total <= 0 {
		t.Fatalf("expected positive cache size, got %d", total)
	}

	_, blankErr := NewFilesystemStore("").TotalSizeBytes()
	if blankErr == nil {
		t.Fatal("expected blank root error")
	}
	if errors.Is(blankErr, ErrInvalidCacheKey) {
		t.Fatalf("expected root configuration error, got %v", blankErr)
	}
}

func writeCacheTestFile(t *testing.T, filePath string, content string) {
	t.Helper()
	if mkdirErr := os.MkdirAll(filepath.Dir(filePath), 0o755); mkdirErr != nil {
		t.Fatalf("create cache test directory: %v", mkdirErr)
	}
	if writeErr := os.WriteFile(filePath, []byte(content), 0o644); writeErr != nil {
		t.Fatalf("write cache test file: %v", writeErr)
	}
}
