package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func TestCacheEntryRepository_FindReadyByKey(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	repo := NewCacheEntryRepository(db)
	now := time.Now().UTC()
	last := now.Add(-time.Minute)
	mock.ExpectQuery("SELECT id, job_id, preset, cache_key").
		WithArgs("job-1", "go", "go:abc").
		WillReturnRows(sqlmock.NewRows([]string{"id", "job_id", "preset", "cache_key", "storage_provider", "object_key", "size_bytes", "checksum", "content_digest", "compression", "status", "created_by_build_id", "created_by_step_id", "created_at", "updated_at", "last_accessed_at"}).
			AddRow("entry-1", "job-1", "go", "go:abc", "filesystem", "obj", int64(10), "sum", "content-sum", "tar.gz", "ready", "build-1", "step-1", now, now, last))

	entry, found, err := repo.FindReadyByKey(context.Background(), "job-1", "go", "go:abc")
	if err != nil {
		t.Fatalf("find ready: %v", err)
	}
	if !found {
		t.Fatal("expected cache entry")
	}
	if entry.ObjectKey != "obj" {
		t.Fatalf("unexpected object key: %s", entry.ObjectKey)
	}
}

func TestCacheEntryRepository_Upsert(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	repo := NewCacheEntryRepository(db)
	now := time.Now().UTC()
	mock.ExpectQuery("INSERT INTO cache_entries").
		WillReturnRows(sqlmock.NewRows([]string{"id", "job_id", "preset", "cache_key", "storage_provider", "object_key", "size_bytes", "checksum", "content_digest", "compression", "status", "created_by_build_id", "created_by_step_id", "created_at", "updated_at", "last_accessed_at"}).
			AddRow("entry-1", "job-1", "go", "go:abc", "filesystem", "obj", int64(42), "sum", "content-sum", "tar.gz", "ready", "build-1", "step-1", now, now, nil))

	entry, err := repo.Upsert(context.Background(), repository.CacheEntryUpsertInput{
		JobID:            "job-1",
		Preset:           "go",
		CacheKey:         "go:abc",
		StorageProvider:  domain.StorageProviderFilesystem,
		ObjectKey:        "obj",
		SizeBytes:        42,
		Checksum:         "sum",
		Compression:      "tar.gz",
		Status:           domain.CacheEntryStatusReady,
		CreatedByBuildID: "build-1",
		CreatedByStepID:  "step-1",
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if entry.SizeBytes != 42 {
		t.Fatalf("unexpected size: %d", entry.SizeBytes)
	}
}

func TestCacheEntryRepository_TryAcquireAndReleasePublishClaim(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	repo := NewCacheEntryRepository(db)
	now := time.Now().UTC()
	mock.ExpectQuery("INSERT INTO cache_publish_claims").
		WillReturnRows(sqlmock.NewRows([]string{"claim_token", "reclaimed"}).AddRow("claim-token", false))
	claim, acquired, claimErr := repo.TryAcquirePublishClaim(context.Background(), "job-1", "go-module", "key", "execution-1", now, time.Minute)
	if claimErr != nil || !acquired || claim.Reclaimed {
		t.Fatalf("claim=%+v acquired=%t err=%v", claim, acquired, claimErr)
	}
	mock.ExpectExec("DELETE FROM cache_publish_claims").
		WithArgs("job-1", "go-module", "key", claim.ClaimToken).
		WillReturnResult(sqlmock.NewResult(0, 1))
	if releaseErr := repo.ReleasePublishClaim(context.Background(), claim); releaseErr != nil {
		t.Fatalf("release claim: %v", releaseErr)
	}
}

func TestCacheEntryRepository_CompletePublishClaim(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	repo := NewCacheEntryRepository(db)
	claim := domain.CachePublishClaim{JobID: "job-1", Preset: "go-module", CacheKey: "key", ClaimToken: "claim-token"}
	now := time.Now().UTC()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT claim_expires_at FROM cache_publish_claims").
		WithArgs(claim.JobID, claim.Preset, claim.CacheKey, claim.ClaimToken).
		WillReturnRows(sqlmock.NewRows([]string{"claim_expires_at"}).AddRow(now.Add(time.Minute)))
	mock.ExpectQuery("INSERT INTO cache_entries").
		WillReturnRows(sqlmock.NewRows([]string{"id", "job_id", "preset", "cache_key", "storage_provider", "object_key", "size_bytes", "checksum", "content_digest", "compression", "status", "created_by_build_id", "created_by_step_id", "created_at", "updated_at", "last_accessed_at"}).
			AddRow("entry-1", "job-1", "go-module", "key", "filesystem", "obj", int64(42), "sum", "content-sum", "tar.gz", "ready", "build-1", "step-1", now, now, nil))
	mock.ExpectExec("DELETE FROM cache_publish_claims").
		WithArgs(claim.JobID, claim.Preset, claim.CacheKey, claim.ClaimToken).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	entry, completeErr := repo.CompletePublishClaim(context.Background(), claim, repository.CacheEntryUpsertInput{JobID: "job-1", Preset: "go-module", CacheKey: "key", StorageProvider: domain.StorageProviderFilesystem, ObjectKey: "obj", SizeBytes: 42, Checksum: "sum", ContentDigest: "content-sum", Compression: "tar.gz", Status: domain.CacheEntryStatusReady, CreatedByBuildID: "build-1", CreatedByStepID: "step-1"}, now)
	if completeErr != nil || entry.ID != "entry-1" {
		t.Fatalf("entry=%+v err=%v", entry, completeErr)
	}
	if expectationsErr := mock.ExpectationsWereMet(); expectationsErr != nil {
		t.Fatalf("SQL expectations: %v", expectationsErr)
	}
}
