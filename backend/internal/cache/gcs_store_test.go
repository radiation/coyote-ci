package cache

import (
	"strings"
	"testing"

	"cloud.google.com/go/storage"
	"github.com/radiation/coyote-ci/backend/internal/domain"
)

func TestNewGCSStore_ValidatesConfigAndTrimsPrefix(t *testing.T) {
	_, nilClientErr := NewGCSStore(nil, GCSStoreConfig{Bucket: "bucket"})
	if nilClientErr == nil {
		t.Fatal("expected nil client error")
	}
	if !strings.Contains(nilClientErr.Error(), "client") {
		t.Fatalf("expected client error, got %v", nilClientErr)
	}

	_, missingBucketErr := NewGCSStore(&storage.Client{}, GCSStoreConfig{Bucket: " "})
	if missingBucketErr == nil {
		t.Fatal("expected missing bucket error")
	}
	if !strings.Contains(missingBucketErr.Error(), "bucket") {
		t.Fatalf("expected bucket error, got %v", missingBucketErr)
	}

	store, createErr := NewGCSStore(&storage.Client{}, GCSStoreConfig{Bucket: " cache-bucket ", Prefix: " /team/cache/ "})
	if createErr != nil {
		t.Fatalf("create gcs cache store: %v", createErr)
	}
	if store.Provider() != domain.StorageProviderGCS {
		t.Fatalf("expected gcs provider, got %q", store.Provider())
	}
	if store.bucket != "cache-bucket" || store.prefix != "team/cache" {
		t.Fatalf("expected trimmed bucket/prefix, got bucket=%q prefix=%q", store.bucket, store.prefix)
	}
}

func TestGCSStore_ObjectKeyAddsPrefixAndArchiveSuffix(t *testing.T) {
	store := &GCSStore{bucket: "bucket", prefix: "team/cache"}
	if got := store.objectKey(" /v1/jobs/job-1/key/ "); got != "team/cache/v1/jobs/job-1/key.tar.gz" {
		t.Fatalf("unexpected prefixed object key: %q", got)
	}

	withoutPrefix := &GCSStore{bucket: "bucket"}
	if got := withoutPrefix.objectKey("v1/jobs/job-1/key"); got != "v1/jobs/job-1/key.tar.gz" {
		t.Fatalf("unexpected unprefixed object key: %q", got)
	}
}
