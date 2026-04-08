package artifact

import (
	"context"
	"strings"
	"testing"
)

func TestNewGCSStore_RequiresClient(t *testing.T) {
	_, err := NewGCSStore(nil, GCSStoreConfig{Bucket: "bucket"})
	if err == nil {
		t.Fatal("expected error for nil client")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "client") {
		t.Fatalf("expected client-related error, got %v", err)
	}
}

func TestGCSStore_ResolveStorageKey_WithPrefix(t *testing.T) {
	store := &GCSStore{bucket: "bucket", prefix: "prefix-root"}

	key := store.ResolveStorageKey("builds/build-1/shared/a.txt")
	if key != "prefix-root/builds/build-1/shared/a.txt" {
		t.Fatalf("unexpected resolved key: %q", key)
	}
}

func TestGCSStore_ResolveStorageKey_DoesNotDoublePrefix(t *testing.T) {
	store := &GCSStore{bucket: "bucket", prefix: "prefix-root"}

	key := store.ResolveStorageKey("prefix-root/builds/build-1/shared/a.txt")
	if key != "prefix-root/builds/build-1/shared/a.txt" {
		t.Fatalf("expected stable prefixed key, got %q", key)
	}
}

func TestGCSStore_ResolveStorageKey_IsIdempotent(t *testing.T) {
	store := &GCSStore{bucket: "bucket", prefix: "prefix-root"}

	first := store.ResolveStorageKey("builds/build-1/shared/a.txt")
	second := store.ResolveStorageKey(first)
	if second != first {
		t.Fatalf("expected idempotent key resolution, first=%q second=%q", first, second)
	}
}

func TestGCSStore_Exists_RejectsInvalidKey(t *testing.T) {
	store := &GCSStore{bucket: "bucket", prefix: "prefix-root"}

	_, err := store.Exists(context.Background(), "../escape")
	if err == nil {
		t.Fatal("expected invalid key error")
	}
	if err != ErrInvalidStorageKey {
		t.Fatalf("expected ErrInvalidStorageKey, got %v", err)
	}
}
