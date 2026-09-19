package memory

import (
	"context"
	"errors"
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

func TestCacheEntryRepository_PublishClaimsAreExclusiveAndRecoverAfterExpiry(t *testing.T) {
	repo := NewCacheEntryRepository()
	now := time.Date(2026, time.September, 15, 12, 0, 0, 0, time.UTC)
	first, acquired, firstErr := repo.TryAcquirePublishClaim(context.Background(), "job-1", "go-module", "key", "execution-1", now, time.Minute)
	if firstErr != nil || !acquired {
		t.Fatalf("first claim acquired=%t err=%v", acquired, firstErr)
	}

	if validateErr := repo.ValidatePublishClaim(context.Background(), first, "execution-1", now.Add(30*time.Second)); validateErr != nil {
		t.Fatalf("validate active claim: %v", validateErr)
	}
	if validateErr := repo.ValidatePublishClaim(context.Background(), first, "execution-2", now.Add(30*time.Second)); !errors.Is(validateErr, repository.ErrCachePublishClaimStale) {
		t.Fatalf("validate wrong claimant error=%v, want stale", validateErr)
	}
	_, acquired, busyErr := repo.TryAcquirePublishClaim(context.Background(), "job-1", "go-module", "key", "execution-2", now.Add(30*time.Second), time.Minute)
	if busyErr != nil || acquired {
		t.Fatalf("second claim acquired=%t err=%v", acquired, busyErr)
	}
	if releaseErr := repo.ReleasePublishClaim(context.Background(), domain.CachePublishClaim{JobID: first.JobID, Preset: first.Preset, CacheKey: first.CacheKey, ClaimToken: "wrong"}); releaseErr != nil {
		t.Fatalf("wrong owner release: %v", releaseErr)
	}
	second, acquired, reclaimErr := repo.TryAcquirePublishClaim(context.Background(), "job-1", "go-module", "key", "execution-2", now.Add(2*time.Minute), time.Minute)
	if reclaimErr != nil || !acquired || !second.Reclaimed {
		t.Fatalf("reclaimed claim=%+v acquired=%t err=%v", second, acquired, reclaimErr)
	}
	if validateErr := repo.ValidatePublishClaim(context.Background(), first, "execution-1", now.Add(2*time.Minute)); !errors.Is(validateErr, repository.ErrCachePublishClaimStale) {
		t.Fatalf("validate reclaimed token error=%v, want stale", validateErr)
	}
}

func TestCacheEntryRepository_CompletePublishClaimAllowsUnreclaimedExpiredOwner(t *testing.T) {
	repo := NewCacheEntryRepository()
	now := time.Date(2026, time.September, 15, 12, 0, 0, 0, time.UTC)
	claim, acquired, claimErr := repo.TryAcquirePublishClaim(context.Background(), "job-1", "go-module", "key", "execution-1", now, time.Minute)
	if claimErr != nil || !acquired {
		t.Fatalf("claim=%+v acquired=%t err=%v", claim, acquired, claimErr)
	}
	input := repository.CacheEntryUpsertInput{JobID: "job-1", Preset: "go-module", CacheKey: "key", StorageProvider: domain.StorageProviderFilesystem, ObjectKey: "object", SizeBytes: 1, Checksum: "checksum", ContentDigest: "sha256:checksum", Compression: "tar.gz", Status: domain.CacheEntryStatusReady}
	if _, completeErr := repo.CompletePublishClaim(context.Background(), claim, input, now.Add(2*time.Minute)); completeErr != nil {
		t.Fatalf("complete unreclaimed expired claim: %v", completeErr)
	}
}

func TestCacheEntryRepository_CompletePublishClaimRejectsReplacement(t *testing.T) {
	repo := NewCacheEntryRepository()
	now := time.Date(2026, time.September, 15, 12, 0, 0, 0, time.UTC)
	first, acquired, firstErr := repo.TryAcquirePublishClaim(context.Background(), "job-1", "go-module", "key", "execution-1", now, time.Minute)
	if firstErr != nil || !acquired {
		t.Fatalf("first claim=%+v acquired=%t err=%v", first, acquired, firstErr)
	}
	if _, acquired, replacementErr := repo.TryAcquirePublishClaim(context.Background(), "job-1", "go-module", "key", "execution-2", now.Add(2*time.Minute), time.Minute); replacementErr != nil || !acquired {
		t.Fatalf("replacement acquired=%t err=%v", acquired, replacementErr)
	}
	input := repository.CacheEntryUpsertInput{JobID: "job-1", Preset: "go-module", CacheKey: "key", StorageProvider: domain.StorageProviderFilesystem, ObjectKey: "object", SizeBytes: 1, Checksum: "checksum", ContentDigest: "sha256:checksum", Compression: "tar.gz", Status: domain.CacheEntryStatusReady}
	if _, completeErr := repo.CompletePublishClaim(context.Background(), first, input, now.Add(2*time.Minute)); !errors.Is(completeErr, repository.ErrCachePublishClaimReplaced) {
		t.Fatalf("complete stale claim error=%v, want replaced", completeErr)
	}
}
