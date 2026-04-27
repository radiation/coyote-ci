package postgres

import (
	"context"
	"regexp"
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
	stepID := "step-1"

	mock.ExpectQuery("INSERT INTO build_artifacts").
		WithArgs("artifact-1", "build-1", &stepID, "coyote-ci-server", "dist/output.zip", "generic", "build-1/dist/output.zip", "filesystem", int64(10), &contentType, &checksum, now).
		WillReturnRows(sqlmock.NewRows([]string{"id", "build_id", "step_id", "artifact_name", "logical_path", "artifact_type", "storage_key", "storage_provider", "size_bytes", "content_type", "checksum_sha256", "created_at"}).
			AddRow("artifact-1", "build-1", &stepID, "coyote-ci-server", "dist/output.zip", "generic", "build-1/dist/output.zip", "filesystem", int64(10), contentType, checksum, now))

	artifact, err := repo.Create(context.Background(), domain.BuildArtifact{
		ID:              "artifact-1",
		BuildID:         "build-1",
		StepID:          &stepID,
		Name:            "coyote-ci-server",
		LogicalPath:     "dist/output.zip",
		ArtifactType:    domain.ArtifactTypeGeneric,
		StorageKey:      "build-1/dist/output.zip",
		StorageProvider: domain.StorageProviderFilesystem,
		SizeBytes:       10,
		ContentType:     &contentType,
		ChecksumSHA256:  &checksum,
		CreatedAt:       now,
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
		WithArgs("artifact-1", "build-1", nil, nil, "dist/output.zip", nil, "build-1/dist/output.zip", "filesystem", int64(10), nil, nil, nil).
		WillReturnRows(sqlmock.NewRows([]string{"id", "build_id", "step_id", "artifact_name", "logical_path", "artifact_type", "storage_key", "storage_provider", "size_bytes", "content_type", "checksum_sha256", "created_at"}).
			AddRow("artifact-1", "build-1", nil, nil, "dist/output.zip", nil, "build-1/dist/output.zip", "filesystem", int64(10), nil, nil, now))

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

	mock.ExpectQuery("SELECT id, build_id, step_id, artifact_name, logical_path, artifact_type, storage_key, storage_provider, size_bytes, content_type, checksum_sha256, created_at").
		WithArgs("build-1", "missing").
		WillReturnRows(sqlmock.NewRows([]string{"id", "build_id", "step_id", "artifact_name", "logical_path", "artifact_type", "storage_key", "storage_provider", "size_bytes", "content_type", "checksum_sha256", "created_at"}))

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

func TestArtifactRepository_ListForBrowse(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	repo := NewArtifactRepository(db)
	now := time.Now().UTC()
	jobID := "2c1d3f58-ecfe-4bbc-8dc0-5863767db4e7"
	buildRows := sqlmock.NewRows([]string{
		"id", "build_id", "step_id", "artifact_name", "logical_path", "artifact_type", "storage_key", "storage_provider", "size_bytes", "content_type", "checksum_sha256", "created_at",
		"id", "build_number", "project_id", "job_id", "status", "created_at", "queued_at", "started_at", "finished_at", "current_step_index", "attempt_number", "rerun_of_build_id", "rerun_from_step_index", "error_message", "pipeline_name", "pipeline_source", "pipeline_path", "repo_url", "ref", "commit_sha", "trigger_kind", "scm_provider", "event_type", "trigger_repository_owner", "trigger_repository_name", "trigger_repository_url", "trigger_raw_ref", "trigger_ref", "trigger_ref_type", "trigger_ref_name", "trigger_deleted", "trigger_commit_sha", "trigger_delivery_id", "trigger_actor", "requested_image_ref", "resolved_image_ref", "image_source_kind", "managed_image_id", "managed_image_version_id",
		"id", "step_index", "name",
	}).AddRow(
		"artifact-1", "build-1", "step-1", "coyote-ci/package-a", "packages/pkg-a.tgz", "npm_package", "build-1/pkg-a.tgz", "filesystem", int64(12), "application/gzip", "abc123", now,
		"build-1", int64(42), "project-1", jobID, "success", now, nil, nil, nil, 0, 1, nil, nil, nil, nil, nil, nil, nil, nil, nil, "manual", nil, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil, nil, nil, "", nil, nil,
		"step-1", 1, "Publish package",
	)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT 
			a.id, a.build_id, a.step_id, a.artifact_name, a.logical_path, a.artifact_type, a.storage_key, a.storage_provider, a.size_bytes, a.content_type, a.checksum_sha256, a.created_at,
			b.id, b.build_number, b.project_id, b.job_id, b.status, b.created_at, b.queued_at, b.started_at, b.finished_at, b.current_step_index, b.attempt_number, b.rerun_of_build_id, b.rerun_from_step_index, b.error_message, b.pipeline_name, b.pipeline_source, b.pipeline_path, b.repo_url, b.ref, b.commit_sha, b.trigger_kind, b.scm_provider, b.event_type, b.trigger_repository_owner, b.trigger_repository_name, b.trigger_repository_url, b.trigger_raw_ref, b.trigger_ref, b.trigger_ref_type, b.trigger_ref_name, b.trigger_deleted, b.trigger_commit_sha, b.trigger_delivery_id, b.trigger_actor, b.requested_image_ref, b.resolved_image_ref, b.image_source_kind, b.managed_image_id, b.managed_image_version_id,
			s.id,
			s.step_index,
			s.name
		FROM build_artifacts a
		JOIN builds b ON b.id = a.build_id
		LEFT JOIN build_steps s ON s.id = a.step_id
		WHERE (
			$1 = ''
			OR COALESCE(a.artifact_name, '') ILIKE $2
			OR a.logical_path ILIKE $2
			OR b.project_id ILIKE $2
			OR COALESCE(b.job_id::text, '') ILIKE $2
			OR EXISTS (
				SELECT 1
				FROM version_tags vt
				WHERE vt.artifact_id = a.id
				  AND vt.version_text ILIKE $2
			)
		)
		ORDER BY a.created_at DESC, a.logical_path ASC, b.created_at DESC
	`)).WithArgs("pkg-a", "%pkg-a%").WillReturnRows(buildRows)

	records, err := repo.ListForBrowse(context.Background(), "pkg-a")
	if err != nil {
		t.Fatalf("ListForBrowse failed: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Build.JobID == nil || *records[0].Build.JobID != jobID {
		t.Fatalf("expected job id %q, got %#v", jobID, records[0].Build.JobID)
	}
	if records[0].Artifact.ArtifactType != domain.ArtifactTypeNPMPackage {
		t.Fatalf("expected npm_package type, got %q", records[0].Artifact.ArtifactType)
	}
	if records[0].Artifact.Name != "coyote-ci/package-a" {
		t.Fatalf("expected artifact name, got %q", records[0].Artifact.Name)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations failed: %v", err)
	}
}
