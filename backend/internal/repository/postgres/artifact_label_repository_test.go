package postgres

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func TestArtifactLabelRepository_CreateForArtifacts_VersionIdempotentForSameArtifact(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	repo := NewArtifactLabelRepository(db)
	now := time.Now().UTC()

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT a.id, a.package_id, a.created_at, b.job_id
		FROM build_artifacts a
		JOIN builds b ON b.id = a.build_id
		WHERE a.id IN ($1)
		ORDER BY a.package_id ASC, a.created_at DESC, a.id DESC
	`)).
		WithArgs("artifact-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "package_id", "created_at", "job_id"}).AddRow("artifact-1", "package-1", now, "job-1"))
	mock.ExpectQuery(regexp.QuoteMeta(`
		INSERT INTO artifact_versions (id, package_id, artifact_id, version_text, created_at)
		VALUES ($1, $2, $3, $4, NOW())
		RETURNING id, artifact_id, created_at
	`)).
		WithArgs(sqlmock.AnyArg(), "package-1", "artifact-1", "1.2.3").
		WillReturnError(errors.New("duplicate key value violates unique constraint artifact_versions_package_id_version_text_key"))
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, artifact_id, created_at
		FROM artifact_versions
		WHERE package_id = $1 AND version_text = $2
	`)).
		WithArgs("package-1", "1.2.3").
		WillReturnRows(sqlmock.NewRows([]string{"id", "artifact_id", "created_at"}).AddRow("version-1", "artifact-1", now))
	mock.ExpectCommit()

	created, err := repo.CreateForArtifacts(context.Background(), repository.CreateArtifactLabelsParams{
		JobID:       "job-1",
		Value:       "1.2.3",
		Kind:        domain.VersionTagKindVersion,
		ArtifactIDs: []string{"artifact-1"},
	})
	if err != nil {
		t.Fatalf("CreateForArtifacts returned error: %v", err)
	}
	if len(created) != 1 {
		t.Fatalf("expected 1 created tag, got %d", len(created))
	}
	if created[0].ID != "version-1" {
		t.Fatalf("expected existing version tag id, got %q", created[0].ID)
	}
	if created[0].ArtifactID == nil || *created[0].ArtifactID != "artifact-1" {
		t.Fatalf("expected existing artifact target, got %#v", created[0].ArtifactID)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestArtifactLabelRepository_CreateForArtifacts_VersionConflictsAcrossArtifacts(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	repo := NewArtifactLabelRepository(db)
	now := time.Now().UTC()

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT a.id, a.package_id, a.created_at, b.job_id
		FROM build_artifacts a
		JOIN builds b ON b.id = a.build_id
		WHERE a.id IN ($1)
		ORDER BY a.package_id ASC, a.created_at DESC, a.id DESC
	`)).
		WithArgs("artifact-2").
		WillReturnRows(sqlmock.NewRows([]string{"id", "package_id", "created_at", "job_id"}).AddRow("artifact-2", "package-1", now, "job-1"))
	mock.ExpectQuery(regexp.QuoteMeta(`
		INSERT INTO artifact_versions (id, package_id, artifact_id, version_text, created_at)
		VALUES ($1, $2, $3, $4, NOW())
		RETURNING id, artifact_id, created_at
	`)).
		WithArgs(sqlmock.AnyArg(), "package-1", "artifact-2", "1.2.3").
		WillReturnError(errors.New("duplicate key value violates unique constraint artifact_versions_package_id_version_text_key"))
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, artifact_id, created_at
		FROM artifact_versions
		WHERE package_id = $1 AND version_text = $2
	`)).
		WithArgs("package-1", "1.2.3").
		WillReturnRows(sqlmock.NewRows([]string{"id", "artifact_id", "created_at"}).AddRow("version-1", "artifact-1", now))
	mock.ExpectRollback()

	_, err = repo.CreateForArtifacts(context.Background(), repository.CreateArtifactLabelsParams{
		JobID:       "job-1",
		Value:       "1.2.3",
		Kind:        domain.VersionTagKindVersion,
		ArtifactIDs: []string{"artifact-2"},
	})
	if !errors.Is(err, repository.ErrVersionTagConflict) {
		t.Fatalf("expected ErrVersionTagConflict, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestArtifactLabelRepository_CreateForArtifacts_ChannelMovesCurrentArtifact(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	repo := NewArtifactLabelRepository(db)
	now := time.Now().UTC()
	updatedAt := now.Add(time.Minute)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT a.id, a.package_id, a.created_at, b.job_id
		FROM build_artifacts a
		JOIN builds b ON b.id = a.build_id
		WHERE a.id IN ($1)
		ORDER BY a.package_id ASC, a.created_at DESC, a.id DESC
	`)).
		WithArgs("artifact-2").
		WillReturnRows(sqlmock.NewRows([]string{"id", "package_id", "created_at", "job_id"}).AddRow("artifact-2", "package-1", now, "job-1"))
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, current_artifact_id, created_at, updated_at
		FROM artifact_channels
		WHERE package_id = $1 AND channel_name = $2
		FOR UPDATE
	`)).
		WithArgs("package-1", "prod").
		WillReturnRows(sqlmock.NewRows([]string{"id", "current_artifact_id", "created_at", "updated_at"}).AddRow("channel-1", "artifact-1", now, now))
	mock.ExpectQuery(regexp.QuoteMeta(`
		UPDATE artifact_channels
		SET current_artifact_id = $3,
		    updated_at = NOW()
		WHERE package_id = $1 AND channel_name = $2
		RETURNING id, current_artifact_id, updated_at
	`)).
		WithArgs("package-1", "prod", "artifact-2").
		WillReturnRows(sqlmock.NewRows([]string{"id", "current_artifact_id", "updated_at"}).AddRow("channel-1", "artifact-2", updatedAt))
	mock.ExpectExec(regexp.QuoteMeta(`
		INSERT INTO artifact_channel_events (id, package_id, channel_name, previous_artifact_id, new_artifact_id, created_at)
		VALUES ($1, $2, $3, $4, $5, NOW())
	`)).
		WithArgs(sqlmock.AnyArg(), "package-1", "prod", "artifact-1", "artifact-2").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	created, err := repo.CreateForArtifacts(context.Background(), repository.CreateArtifactLabelsParams{
		JobID:       "job-1",
		Value:       "prod",
		Kind:        domain.VersionTagKindChannel,
		ArtifactIDs: []string{"artifact-2"},
	})
	if err != nil {
		t.Fatalf("CreateForArtifacts returned error: %v", err)
	}
	if len(created) != 1 {
		t.Fatalf("expected 1 created tag, got %d", len(created))
	}
	if created[0].Kind != domain.VersionTagKindChannel {
		t.Fatalf("expected channel kind, got %q", created[0].Kind)
	}
	if created[0].ArtifactID == nil || *created[0].ArtifactID != "artifact-2" {
		t.Fatalf("expected updated current artifact, got %#v", created[0].ArtifactID)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}
