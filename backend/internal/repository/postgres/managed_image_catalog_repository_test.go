package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

func TestManagedImageCatalogRepository_CreateVersion_ReusesExistingDigest(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}

	repo := NewManagedImageCatalogRepository(db)
	now := time.Date(2026, 4, 27, 17, 12, 33, 0, time.UTC)
	row := []string{"id", "managed_image_id", "version_label", "image_ref", "image_digest", "dependency_fingerprint", "source_repository_url", "created_at"}
	fingerprint := "fingerprint-new"
	repoURL := "https://example.com/repo.git"

	mock.ExpectQuery("INSERT INTO managed_image_versions").
		WithArgs(
			"new-version-id",
			"managed-1",
			"v2",
			"registry.example.com/coyote/go@sha256:abcd",
			"sha256:abcd",
			&fingerprint,
			&repoURL,
			now,
		).
		WillReturnRows(sqlmock.NewRows(row).AddRow(
			"existing-version-id",
			"managed-1",
			"v1",
			"registry.example.com/coyote/go@sha256:abcd",
			"sha256:abcd",
			"fingerprint-old",
			"https://example.com/repo.git",
			now.Add(-time.Hour),
		))

	created, err := repo.CreateVersion(context.Background(), domain.ManagedImageVersion{
		ID:                    "new-version-id",
		ManagedImageID:        "managed-1",
		VersionLabel:          "v2",
		ImageRef:              "registry.example.com/coyote/go@sha256:abcd",
		ImageDigest:           "sha256:abcd",
		DependencyFingerprint: &fingerprint,
		SourceRepositoryURL:   &repoURL,
		CreatedAt:             now,
	})
	if err != nil {
		t.Fatalf("create version failed: %v", err)
	}
	if created.ID != "existing-version-id" {
		t.Fatalf("expected existing version id, got %q", created.ID)
	}
	if created.VersionLabel != "v1" {
		t.Fatalf("expected existing version label, got %q", created.VersionLabel)
	}
	if created.ImageDigest != "sha256:abcd" {
		t.Fatalf("expected sha256:abcd digest, got %q", created.ImageDigest)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}
