package postgres

import (
	"context"
	"database/sql"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func TestVersionTagRepository_CreateForTargets_WithManagedImageVersionTarget(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	repo := NewVersionTagRepository(db)
	now := time.Now().UTC()
	rows := sqlmock.NewRows([]string{"id", "job_id", "version_text", "target_type", "artifact_id", "managed_image_version_id", "created_at"}).
		AddRow("tag-1", "job-1", "v1.2.3", string(domain.VersionTagTargetManagedImageVersion), nil, "managed-version-1", now)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT managed_image_versions.id
		FROM managed_image_versions
		JOIN managed_images ON managed_images.id = managed_image_versions.managed_image_id
		JOIN jobs ON jobs.project_id = managed_images.project_id
		WHERE managed_image_versions.id IN ($1) AND jobs.id = $2
	`)).
		WithArgs("managed-version-1", "job-1").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("managed-version-1"))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id FROM managed_image_versions WHERE id IN ($1)`)).
		WithArgs("managed-version-1").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("managed-version-1"))
	mock.ExpectQuery(regexp.QuoteMeta(`
			SELECT 1
			FROM version_tags
			WHERE job_id = $1 AND version_text = $2 AND managed_image_version_id IN ($3)
			LIMIT 1
		`)).
		WithArgs("job-1", "v1.2.3", "managed-version-1").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(regexp.QuoteMeta(`
		INSERT INTO version_tags (
			id,
			job_id,
			version_text,
			target_type,
			artifact_id,
			managed_image_version_id
		)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, job_id, version_text, target_type, artifact_id, managed_image_version_id, created_at`)).
		WithArgs(sqlmock.AnyArg(), "job-1", "v1.2.3", string(domain.VersionTagTargetManagedImageVersion), nil, versionTagStringPtr("managed-version-1")).
		WillReturnRows(rows)
	mock.ExpectCommit()

	created, err := repo.CreateForTargets(context.Background(), repository.CreateVersionTagsParams{
		JobID:                  "job-1",
		Version:                "v1.2.3",
		ManagedImageVersionIDs: []string{"managed-version-1"},
	})
	if err != nil {
		t.Fatalf("CreateForTargets returned error: %v", err)
	}
	if len(created) != 1 {
		t.Fatalf("expected 1 created tag, got %d", len(created))
	}
	if created[0].ManagedImageVersionID == nil || *created[0].ManagedImageVersionID != "managed-version-1" {
		t.Fatalf("expected managed image version tag target, got %#v", created[0].ManagedImageVersionID)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}
