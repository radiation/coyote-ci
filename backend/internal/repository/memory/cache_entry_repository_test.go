package memory

import (
	"context"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func TestCacheEntryRepository_UpsertAndFindReady(t *testing.T) {
	repo := NewCacheEntryRepository()
	ctx := context.Background()

	_, err := repo.Upsert(ctx, repository.CacheEntryUpsertInput{
		JobID:            "job-1",
		Preset:           "go",
		CacheKey:         "go:abc",
		StorageProvider:  domain.StorageProviderFilesystem,
		ObjectKey:        "obj-1",
		SizeBytes:        10,
		Checksum:         "sum1",
		Compression:      "tar.gz",
		Status:           domain.CacheEntryStatusReady,
		CreatedByBuildID: "build-1",
		CreatedByStepID:  "step-1",
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	entry, found, err := repo.FindReadyByKey(ctx, "job-1", "go", "go:abc")
	if err != nil {
		t.Fatalf("find ready: %v", err)
	}
	if !found {
		t.Fatal("expected ready cache entry")
	}
	if entry.ObjectKey != "obj-1" {
		t.Fatalf("expected object key obj-1, got %s", entry.ObjectKey)
	}
}

func TestCacheEntryRepository_LastWriterWins(t *testing.T) {
	repo := NewCacheEntryRepository()
	ctx := context.Background()

	_, err := repo.Upsert(ctx, repository.CacheEntryUpsertInput{
		JobID:            "job-1",
		Preset:           "go",
		CacheKey:         "go:abc",
		StorageProvider:  domain.StorageProviderFilesystem,
		ObjectKey:        "obj-old",
		Status:           domain.CacheEntryStatusReady,
		Compression:      "tar.gz",
		CreatedByBuildID: "build-1",
		CreatedByStepID:  "step-1",
	})
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	_, err = repo.Upsert(ctx, repository.CacheEntryUpsertInput{
		JobID:            "job-1",
		Preset:           "go",
		CacheKey:         "go:abc",
		StorageProvider:  domain.StorageProviderFilesystem,
		ObjectKey:        "obj-new",
		Status:           domain.CacheEntryStatusReady,
		Compression:      "tar.gz",
		CreatedByBuildID: "build-2",
		CreatedByStepID:  "step-2",
	})
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	entry, found, err := repo.FindReadyByKey(ctx, "job-1", "go", "go:abc")
	if err != nil {
		t.Fatalf("find ready after second upsert: %v", err)
	}
	if !found {
		t.Fatal("expected ready cache entry")
	}
	if entry.ObjectKey != "obj-new" {
		t.Fatalf("expected latest object key obj-new, got %s", entry.ObjectKey)
	}
}

func TestCacheEntryRepository_MarkAccessed(t *testing.T) {
	repo := NewCacheEntryRepository()
	ctx := context.Background()
	entry, err := repo.Upsert(ctx, repository.CacheEntryUpsertInput{
		JobID:            "job-1",
		Preset:           "go",
		CacheKey:         "go:abc",
		StorageProvider:  domain.StorageProviderFilesystem,
		ObjectKey:        "obj-1",
		Status:           domain.CacheEntryStatusReady,
		Compression:      "tar.gz",
		CreatedByBuildID: "build-1",
		CreatedByStepID:  "step-1",
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	at := time.Now().UTC()
	if markErr := repo.MarkAccessed(ctx, entry.ID, at); markErr != nil {
		t.Fatalf("mark accessed: %v", markErr)
	}

	fetched, found, err := repo.FindReadyByKey(ctx, "job-1", "go", "go:abc")
	if err != nil {
		t.Fatalf("find ready: %v", err)
	}
	if !found || fetched.LastAccessedAt == nil {
		t.Fatal("expected last_accessed_at to be set")
	}
}
