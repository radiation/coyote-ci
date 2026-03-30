package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func TestArtifactRepository_Create(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	repo := NewArtifactRepository(db)
	now := time.Now().UTC()
	contentType := "application/zip"
	checksum := "abc123"

	mock.ExpectQuery("INSERT INTO build_artifacts").
		WithArgs("artifact-1", "build-1", "dist/output.zip", "build-1/dist/output.zip", int64(10), &contentType, &checksum, now).
		WillReturnRows(sqlmock.NewRows([]string{"id", "build_id", "logical_path", "storage_key", "size_bytes", "content_type", "checksum_sha256", "created_at"}).
			AddRow("artifact-1", "build-1", "dist/output.zip", "build-1/dist/output.zip", int64(10), contentType, checksum, now))

	artifact, err := repo.Create(context.Background(), domain.BuildArtifact{
		ID:             "artifact-1",
		BuildID:        "build-1",
		LogicalPath:    "dist/output.zip",
		StorageKey:     "build-1/dist/output.zip",
		SizeBytes:      10,
		ContentType:    &contentType,
		ChecksumSHA256: &checksum,
		CreatedAt:      now,
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if artifact.ID != "artifact-1" {
		t.Fatalf("unexpected artifact id: %q", artifact.ID)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations failed: %v", err)
	}
}

func TestArtifactRepository_Create_ZeroCreatedAtUsesDatabaseNow(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	repo := NewArtifactRepository(db)
	now := time.Now().UTC()

	mock.ExpectQuery("INSERT INTO build_artifacts").
		WithArgs("artifact-1", "build-1", "dist/output.zip", "build-1/dist/output.zip", int64(10), nil, nil, nil).
		WillReturnRows(sqlmock.NewRows([]string{"id", "build_id", "logical_path", "storage_key", "size_bytes", "content_type", "checksum_sha256", "created_at"}).
			AddRow("artifact-1", "build-1", "dist/output.zip", "build-1/dist/output.zip", int64(10), nil, nil, now))

	artifact, err := repo.Create(context.Background(), domain.BuildArtifact{
		ID:          "artifact-1",
		BuildID:     "build-1",
		LogicalPath: "dist/output.zip",
		StorageKey:  "build-1/dist/output.zip",
		SizeBytes:   10,
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if artifact.CreatedAt.IsZero() {
		t.Fatal("expected created_at returned from database")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations failed: %v", err)
	}
}

func TestArtifactRepository_GetByID_NotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	repo := NewArtifactRepository(db)

	mock.ExpectQuery("SELECT id, build_id, logical_path, storage_key, size_bytes, content_type, checksum_sha256, created_at").
		WithArgs("build-1", "missing").
		WillReturnRows(sqlmock.NewRows([]string{"id", "build_id", "logical_path", "storage_key", "size_bytes", "content_type", "checksum_sha256", "created_at"}))

	_, err = repo.GetByID(context.Background(), "build-1", "missing")
	if err == nil {
		t.Fatal("expected not found error")
	}
	if err != repository.ErrArtifactNotFound {
		t.Fatalf("expected ErrArtifactNotFound, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations failed: %v", err)
	}
}
