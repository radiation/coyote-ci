package artifact

import (
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

func TestResolveStores_DefaultFilesystem(t *testing.T) {
	resolver, err := ResolveStores(StoreConfig{
		Provider:    "filesystem",
		StorageRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("resolve stores failed: %v", err)
	}

	if resolver.DefaultProvider() != domain.StorageProviderFilesystem {
		t.Fatalf("expected filesystem default provider, got %q", resolver.DefaultProvider())
	}
	if _, err := resolver.Resolve(domain.StorageProviderFilesystem); err != nil {
		t.Fatalf("expected filesystem store configured: %v", err)
	}
}

func TestResolveStores_GCSMissingBucketFallbackWhenNotStrict(t *testing.T) {
	resolver, err := ResolveStores(StoreConfig{
		Provider:    "gcs",
		StorageRoot: t.TempDir(),
		Strict:      false,
	})
	if err != nil {
		t.Fatalf("resolve stores failed: %v", err)
	}

	if resolver.DefaultProvider() != domain.StorageProviderFilesystem {
		t.Fatalf("expected filesystem default provider on fallback, got %q", resolver.DefaultProvider())
	}
	if _, err := resolver.Resolve(domain.StorageProviderGCS); err == nil {
		t.Fatal("expected gcs store to be absent when bucket is missing")
	}
}

func TestResolveStores_GCSMissingBucketFailsWhenStrict(t *testing.T) {
	_, err := ResolveStores(StoreConfig{
		Provider:    "gcs",
		StorageRoot: t.TempDir(),
		Strict:      true,
	})
	if err == nil {
		t.Fatal("expected error when strict gcs config is missing bucket")
	}
}
