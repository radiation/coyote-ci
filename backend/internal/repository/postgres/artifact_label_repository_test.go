package postgres

import (
	"context"
	"database/sql/driver"
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

func TestArtifactLabelRepository_ListByArtifactID_ReturnsVersionsAndChannels(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	repo := NewArtifactLabelRepository(db)
	now := time.Now().UTC()

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, job_id, label_value, label_kind, artifact_id, created_at
		FROM (
			SELECT
				av.id,
				COALESCE(ap.job_id::text, '') AS job_id,
				av.version_text AS label_value,
				'version' AS label_kind,
				av.artifact_id,
				av.created_at
			FROM artifact_versions av
			JOIN artifact_packages ap ON ap.id = av.package_id
			WHERE av.artifact_id = $1

			UNION ALL

			SELECT
				ac.id,
				COALESCE(ap.job_id::text, '') AS job_id,
				ac.channel_name AS label_value,
				'channel' AS label_kind,
				ac.current_artifact_id AS artifact_id,
				ac.updated_at AS created_at
			FROM artifact_channels ac
			JOIN artifact_packages ap ON ap.id = ac.package_id
			WHERE ac.current_artifact_id = $1
		) labels
		ORDER BY created_at ASC, id ASC
	`)).
		WithArgs("artifact-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "job_id", "label_value", "label_kind", "artifact_id", "created_at"}).
			AddRow("version-1", "job-1", "1.2.3", "version", "artifact-1", now).
			AddRow("channel-1", "job-1", "prod", "channel", "artifact-1", now.Add(time.Minute)))

	tags, err := repo.ListByArtifactID(context.Background(), "artifact-1")
	if err != nil {
		t.Fatalf("ListByArtifactID returned error: %v", err)
	}
	if len(tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(tags))
	}
	if tags[0].Kind != domain.VersionTagKindVersion || tags[1].Kind != domain.VersionTagKindChannel {
		t.Fatalf("expected version then channel tags, got %#v", tags)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestArtifactLabelRepository_ListByArtifactIDs_Empty(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	repo := NewArtifactLabelRepository(db)
	tags, err := repo.ListByArtifactIDs(context.Background(), []string{" ", ""})
	if err != nil {
		t.Fatalf("ListByArtifactIDs returned error: %v", err)
	}
	if len(tags) != 0 {
		t.Fatalf("expected no tags, got %#v", tags)
	}
}

func TestArtifactLabelRepository_ListByJobIDAndValue_AndReleaseVersions(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	repo := NewArtifactLabelRepository(db)
	now := time.Now().UTC()

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, job_id, label_value, label_kind, artifact_id, created_at
		FROM (
			SELECT
				av.id,
				ap.job_id::text AS job_id,
				av.version_text AS label_value,
				'version' AS label_kind,
				av.artifact_id,
				av.created_at
			FROM artifact_versions av
			JOIN artifact_packages ap ON ap.id = av.package_id
			WHERE ap.job_id = $1 AND av.version_text = $2

			UNION ALL

			SELECT
				ac.id,
				ap.job_id::text AS job_id,
				ac.channel_name AS label_value,
				'channel' AS label_kind,
				ac.current_artifact_id AS artifact_id,
				ac.updated_at AS created_at
			FROM artifact_channels ac
			JOIN artifact_packages ap ON ap.id = ac.package_id
			WHERE ap.job_id = $1 AND ac.channel_name = $2
		) labels
		ORDER BY created_at ASC, id ASC
	`)).
		WithArgs("job-1", "prod").
		WillReturnRows(sqlmock.NewRows([]string{"id", "job_id", "label_value", "label_kind", "artifact_id", "created_at"}).
			AddRow("channel-1", "job-1", "prod", "channel", "artifact-1", now))
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT DISTINCT av.version_text
		FROM artifact_versions av
		JOIN artifact_packages ap ON ap.id = av.package_id
		WHERE ap.job_id = $1
		ORDER BY av.version_text ASC
	`)).
		WithArgs("job-1").
		WillReturnRows(sqlmock.NewRows([]string{"version_text"}).AddRow("1.2.3").AddRow("1.2.4"))

	tags, err := repo.ListByJobIDAndValue(context.Background(), "job-1", "prod")
	if err != nil {
		t.Fatalf("ListByJobIDAndValue returned error: %v", err)
	}
	if len(tags) != 1 || tags[0].Kind != domain.VersionTagKindChannel || tags[0].Version != "prod" {
		t.Fatalf("expected current prod channel tag, got %#v", tags)
	}

	versions, err := repo.ListReleaseVersionsByJobID(context.Background(), "job-1")
	if err != nil {
		t.Fatalf("ListReleaseVersionsByJobID returned error: %v", err)
	}
	if len(versions) != 2 || versions[0] != "1.2.3" || versions[1] != "1.2.4" {
		t.Fatalf("expected ordered release versions, got %#v", versions)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestArtifactLabelRepository_ListByArtifactIDs_AndListByJobID(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	repo := NewArtifactLabelRepository(db)
	now := time.Now().UTC()

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, job_id, label_value, label_kind, artifact_id, created_at
		FROM (
			SELECT
				av.id,
				COALESCE(ap.job_id::text, '') AS job_id,
				av.version_text AS label_value,
				'version' AS label_kind,
				av.artifact_id,
				av.created_at
			FROM artifact_versions av
			JOIN artifact_packages ap ON ap.id = av.package_id
			WHERE av.artifact_id IN ($1)
			UNION ALL
			SELECT
				ac.id,
				COALESCE(ap.job_id::text, '') AS job_id,
				ac.channel_name AS label_value,
				'channel' AS label_kind,
				ac.current_artifact_id AS artifact_id,
				ac.updated_at AS created_at
			FROM artifact_channels ac
			JOIN artifact_packages ap ON ap.id = ac.package_id
			WHERE ac.current_artifact_id IN ($2)
		) labels
		ORDER BY created_at ASC, id ASC
	`)).
		WithArgs("artifact-1", "artifact-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "job_id", "label_value", "label_kind", "artifact_id", "created_at"}).
			AddRow("version-1", "job-1", "1.2.3", "version", "artifact-1", now).
			AddRow("channel-1", "job-1", "prod", "channel", "artifact-1", now.Add(time.Minute)))
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, job_id, label_value, label_kind, artifact_id, created_at
		FROM (
			SELECT
				av.id,
				ap.job_id::text AS job_id,
				av.version_text AS label_value,
				'version' AS label_kind,
				av.artifact_id,
				av.created_at
			FROM artifact_versions av
			JOIN artifact_packages ap ON ap.id = av.package_id
			WHERE ap.job_id = $1

			UNION ALL

			SELECT
				ac.id,
				ap.job_id::text AS job_id,
				ac.channel_name AS label_value,
				'channel' AS label_kind,
				ac.current_artifact_id AS artifact_id,
				ac.updated_at AS created_at
			FROM artifact_channels ac
			JOIN artifact_packages ap ON ap.id = ac.package_id
			WHERE ap.job_id = $1
		) labels
		ORDER BY created_at ASC, id ASC
	`)).
		WithArgs("job-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "job_id", "label_value", "label_kind", "artifact_id", "created_at"}).
			AddRow("version-1", "job-1", "1.2.3", "version", "artifact-1", now).
			AddRow("channel-1", "job-1", "prod", "channel", "artifact-1", now.Add(time.Minute)))

	tagsByArtifactIDs, err := repo.ListByArtifactIDs(context.Background(), []string{"artifact-1"})
	if err != nil {
		t.Fatalf("ListByArtifactIDs returned error: %v", err)
	}
	if len(tagsByArtifactIDs) != 2 {
		t.Fatalf("expected 2 artifact tags, got %#v", tagsByArtifactIDs)
	}

	tagsByJobID, err := repo.ListByJobID(context.Background(), "job-1")
	if err != nil {
		t.Fatalf("ListByJobID returned error: %v", err)
	}
	if len(tagsByJobID) != 2 {
		t.Fatalf("expected 2 job tags, got %#v", tagsByJobID)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestArtifactLabelRepository_CreateForArtifacts_VersionUsesLatestArtifactPerPackage(t *testing.T) {
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
		WHERE a.id IN ($1, $2)
		ORDER BY a.package_id ASC, a.created_at DESC, a.id DESC
	`)).
		WithArgs("artifact-older", "artifact-newer").
		WillReturnRows(sqlmock.NewRows([]string{"id", "package_id", "created_at", "job_id"}).
			AddRow("artifact-newer", "package-1", now.Add(time.Minute), "job-1").
			AddRow("artifact-older", "package-1", now, "job-1"))
	mock.ExpectQuery(regexp.QuoteMeta(`
		INSERT INTO artifact_versions (id, package_id, artifact_id, version_text, created_at)
		VALUES ($1, $2, $3, $4, NOW())
		RETURNING id, artifact_id, created_at
	`)).
		WithArgs(sqlmock.AnyArg(), "package-1", "artifact-newer", "1.2.3").
		WillReturnRows(sqlmock.NewRows([]string{"id", "artifact_id", "created_at"}).AddRow("version-1", "artifact-newer", now.Add(2*time.Minute)))
	mock.ExpectCommit()

	created, err := repo.CreateForArtifacts(context.Background(), repository.CreateArtifactLabelsParams{
		JobID:       "job-1",
		Value:       "1.2.3",
		Kind:        domain.VersionTagKindVersion,
		ArtifactIDs: []string{"artifact-older", "artifact-newer"},
	})
	if err != nil {
		t.Fatalf("CreateForArtifacts returned error: %v", err)
	}
	if len(created) != 1 || created[0].ArtifactID == nil || *created[0].ArtifactID != "artifact-newer" {
		t.Fatalf("expected latest artifact in package to be selected, got %#v", created)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestArtifactLabelRepository_CreateForArtifacts_ChannelCreatesNewCurrentArtifact(t *testing.T) {
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
		SELECT id, current_artifact_id, created_at, updated_at
		FROM artifact_channels
		WHERE package_id = $1 AND channel_name = $2
		FOR UPDATE
	`)).
		WithArgs("package-1", "prod").
		WillReturnRows(sqlmock.NewRows([]string{"id", "current_artifact_id", "created_at", "updated_at"}))
	mock.ExpectQuery(regexp.QuoteMeta(`
		INSERT INTO artifact_channels (id, package_id, channel_name, current_artifact_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, NOW(), NOW())
		RETURNING id, current_artifact_id, updated_at
	`)).
		WithArgs(sqlmock.AnyArg(), "package-1", "prod", "artifact-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "current_artifact_id", "updated_at"}).AddRow("channel-1", "artifact-1", now.Add(time.Minute)))
	mock.ExpectExec(regexp.QuoteMeta(`
		INSERT INTO artifact_channel_events (id, package_id, channel_name, previous_artifact_id, new_artifact_id, created_at)
		VALUES ($1, $2, $3, $4, $5, NOW())
	`)).
		WithArgs(sqlmock.AnyArg(), "package-1", "prod", nil, "artifact-1").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	created, err := repo.CreateForArtifacts(context.Background(), repository.CreateArtifactLabelsParams{
		JobID:       "job-1",
		Value:       "prod",
		Kind:        domain.VersionTagKindChannel,
		ArtifactIDs: []string{"artifact-1"},
	})
	if err != nil {
		t.Fatalf("CreateForArtifacts returned error: %v", err)
	}
	if len(created) != 1 || created[0].Kind != domain.VersionTagKindChannel {
		t.Fatalf("expected created current channel tag, got %#v", created)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestArtifactLabelRepository_CreateForArtifacts_ChannelReturnsExistingWhenAlreadyCurrent(t *testing.T) {
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
		SELECT id, current_artifact_id, created_at, updated_at
		FROM artifact_channels
		WHERE package_id = $1 AND channel_name = $2
		FOR UPDATE
	`)).
		WithArgs("package-1", "prod").
		WillReturnRows(sqlmock.NewRows([]string{"id", "current_artifact_id", "created_at", "updated_at"}).AddRow("channel-1", "artifact-1", now, now.Add(time.Minute)))
	mock.ExpectCommit()

	created, err := repo.CreateForArtifacts(context.Background(), repository.CreateArtifactLabelsParams{
		JobID:       "job-1",
		Value:       "prod",
		Kind:        domain.VersionTagKindChannel,
		ArtifactIDs: []string{"artifact-1"},
	})
	if err != nil {
		t.Fatalf("CreateForArtifacts returned error: %v", err)
	}
	if len(created) != 1 || created[0].ArtifactID == nil || *created[0].ArtifactID != "artifact-1" {
		t.Fatalf("expected existing current channel tag, got %#v", created)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestArtifactLabelRepository_CreateForArtifacts_ReturnsTargetResolutionErrors(t *testing.T) {
	tests := []struct {
		name        string
		artifactIDs []string
		rows        *sqlmock.Rows
		expectErr   error
	}{
		{
			name:        "target not found",
			artifactIDs: []string{"artifact-1", "artifact-2"},
			rows: sqlmock.NewRows([]string{"id", "package_id", "created_at", "job_id"}).
				AddRow("artifact-1", "package-1", time.Now().UTC(), "job-1"),
			expectErr: repository.ErrVersionTagTargetNotFound,
		},
		{
			name:        "job mismatch",
			artifactIDs: []string{"artifact-1"},
			rows: sqlmock.NewRows([]string{"id", "package_id", "created_at", "job_id"}).
				AddRow("artifact-1", "package-1", time.Now().UTC(), "job-2"),
			expectErr: repository.ErrVersionTagTargetJobMismatch,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("failed to create sqlmock: %v", err)
			}
			defer func() {
				_ = db.Close()
			}()

			repo := NewArtifactLabelRepository(db)
			mock.ExpectBegin()
			mock.ExpectQuery(regexp.QuoteMeta(`
				SELECT a.id, a.package_id, a.created_at, b.job_id
				FROM build_artifacts a
				JOIN builds b ON b.id = a.build_id
				WHERE a.id IN (`) + ".*" + regexp.QuoteMeta(`)
				ORDER BY a.package_id ASC, a.created_at DESC, a.id DESC
			`)).
				WithArgs(stringSliceToAny(tc.artifactIDs)...).
				WillReturnRows(tc.rows)
			mock.ExpectRollback()

			_, err = repo.CreateForArtifacts(context.Background(), repository.CreateArtifactLabelsParams{
				JobID:       "job-1",
				Value:       "1.2.3",
				Kind:        domain.VersionTagKindVersion,
				ArtifactIDs: tc.artifactIDs,
			})
			if !errors.Is(err, tc.expectErr) {
				t.Fatalf("expected %v, got %v", tc.expectErr, err)
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("unmet sql expectations: %v", err)
			}
		})
	}
}

func stringSliceToAny(values []string) []driver.Value {
	out := make([]driver.Value, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}
