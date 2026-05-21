package postgres

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"

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
	jobID := "job-1"
	packageID := md5UUID("job-1::dist/output.zip")

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT project_id::text, job_id").
		WithArgs("build-1").
		WillReturnRows(sqlmock.NewRows([]string{"project_id", "job_id"}).AddRow("project-1", jobID))
	mock.ExpectExec("INSERT INTO artifact_packages").
		WithArgs(packageID, "project-1", jobID, nil, "dist/output.zip", now).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("INSERT INTO build_artifacts").
		WithArgs("artifact-1", "build-1", packageID, &stepID, "coyote-ci-server", "dist/output.zip", "generic", "build-1/dist/output.zip", "filesystem", int64(10), &contentType, &checksum, now).
		WillReturnRows(sqlmock.NewRows([]string{"id", "build_id", "package_id", "step_id", "artifact_name", "logical_path", "artifact_type", "storage_key", "storage_provider", "size_bytes", "content_type", "checksum_sha256", "created_at"}).
			AddRow("artifact-1", "build-1", packageID, &stepID, "coyote-ci-server", "dist/output.zip", "generic", "build-1/dist/output.zip", "filesystem", int64(10), contentType, checksum, now))
	mock.ExpectCommit()

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
	if artifact.PackageID != packageID {
		t.Fatalf("expected package id %q, got %q", packageID, artifact.PackageID)
	}
	if _, err := uuid.Parse(artifact.PackageID); err != nil {
		t.Fatalf("expected valid uuid package id, got %q: %v", artifact.PackageID, err)
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
	packageID := md5UUID("build-1::dist/output.zip")

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT project_id::text, job_id").
		WithArgs("build-1").
		WillReturnRows(sqlmock.NewRows([]string{"project_id", "job_id"}).AddRow("project-1", nil))
	mock.ExpectExec("INSERT INTO artifact_packages").
		WithArgs(packageID, "project-1", nil, "build-1", "dist/output.zip", nil).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("INSERT INTO build_artifacts").
		WithArgs("artifact-1", "build-1", packageID, nil, nil, "dist/output.zip", nil, "build-1/dist/output.zip", "filesystem", int64(10), nil, nil, nil).
		WillReturnRows(sqlmock.NewRows([]string{"id", "build_id", "package_id", "step_id", "artifact_name", "logical_path", "artifact_type", "storage_key", "storage_provider", "size_bytes", "content_type", "checksum_sha256", "created_at"}).
			AddRow("artifact-1", "build-1", packageID, nil, nil, "dist/output.zip", nil, "build-1/dist/output.zip", "filesystem", int64(10), nil, nil, now))
	mock.ExpectCommit()

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
	if _, err := uuid.Parse(artifact.PackageID); err != nil {
		t.Fatalf("expected valid uuid package id, got %q: %v", artifact.PackageID, err)
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

	mock.ExpectQuery("SELECT id, build_id, package_id, step_id, artifact_name, logical_path, artifact_type, storage_key, storage_provider, size_bytes, content_type, checksum_sha256, created_at").
		WithArgs("build-1", "missing").
		WillReturnRows(sqlmock.NewRows([]string{"id", "build_id", "package_id", "step_id", "artifact_name", "logical_path", "artifact_type", "storage_key", "storage_provider", "size_bytes", "content_type", "checksum_sha256", "created_at"}))

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

func TestArtifactRepository_ListByBuildIDAndStepID(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	repo := NewArtifactRepository(db)
	now := time.Now().UTC()
	contentType := "application/gzip"
	checksum := "abc123"

	buildRows := sqlmock.NewRows([]string{"id", "build_id", "package_id", "step_id", "artifact_name", "logical_path", "artifact_type", "storage_key", "storage_provider", "size_bytes", "content_type", "checksum_sha256", "created_at"}).
		AddRow("artifact-1", "build-1", "package-1", "step-1", "pkg-a", "packages/pkg-a.tgz", "npm_package", "build-1/pkg-a.tgz", "filesystem", int64(12), contentType, checksum, now)
	stepRows := sqlmock.NewRows([]string{"id", "build_id", "package_id", "step_id", "artifact_name", "logical_path", "artifact_type", "storage_key", "storage_provider", "size_bytes", "content_type", "checksum_sha256", "created_at"}).
		AddRow("artifact-2", "build-1", "package-2", "step-1", "pkg-b", "packages/pkg-b.tgz", "npm_package", "build-1/pkg-b.tgz", "filesystem", int64(18), nil, nil, now.Add(time.Minute))

	mock.ExpectQuery("SELECT id, build_id, package_id, step_id, artifact_name, logical_path, artifact_type, storage_key, storage_provider, size_bytes, content_type, checksum_sha256, created_at").
		WithArgs("build-1").
		WillReturnRows(buildRows)
	mock.ExpectQuery("SELECT id, build_id, package_id, step_id, artifact_name, logical_path, artifact_type, storage_key, storage_provider, size_bytes, content_type, checksum_sha256, created_at").
		WithArgs("step-1").
		WillReturnRows(stepRows)

	buildArtifacts, err := repo.ListByBuildID(context.Background(), "build-1")
	if err != nil {
		t.Fatalf("ListByBuildID returned error: %v", err)
	}
	if len(buildArtifacts) != 1 || buildArtifacts[0].ID != "artifact-1" {
		t.Fatalf("expected one build artifact, got %#v", buildArtifacts)
	}

	stepArtifacts, err := repo.ListByStepID(context.Background(), "step-1")
	if err != nil {
		t.Fatalf("ListByStepID returned error: %v", err)
	}
	if len(stepArtifacts) != 1 || stepArtifacts[0].ID != "artifact-2" {
		t.Fatalf("expected one step artifact, got %#v", stepArtifacts)
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
	identityRows := sqlmock.NewRows([]string{"identity_key"}).AddRow(jobID + "::packages/pkg-a.tgz")
	buildRows := sqlmock.NewRows([]string{
		"id", "build_id", "package_id", "step_id", "artifact_name", "logical_path", "artifact_type", "storage_key", "storage_provider", "size_bytes", "content_type", "checksum_sha256", "created_at",
		"id", "build_number", "project_id", "job_id", "priority", "status", "created_at", "queued_at", "started_at", "finished_at", "current_step_index", "attempt_number", "rerun_of_build_id", "rerun_from_step_index", "error_message", "pipeline_name", "pipeline_source", "pipeline_path", "repo_url", "ref", "commit_sha", "trigger_kind", "scm_provider", "event_type", "trigger_repository_owner", "trigger_repository_name", "trigger_repository_url", "trigger_raw_ref", "trigger_ref", "trigger_ref_type", "trigger_ref_name", "trigger_deleted", "trigger_commit_sha", "trigger_delivery_id", "trigger_actor", "requested_image_ref", "resolved_image_ref", "image_source_kind", "managed_image_id", "managed_image_version_id",
		"id", "step_index", "name",
	}).AddRow(
		"artifact-1", "build-1", "package-1", "step-1", "coyote-ci/package-a", "packages/pkg-a.tgz", "npm_package", "build-1/pkg-a.tgz", "filesystem", int64(12), "application/gzip", "abc123", now,
		"build-1", int64(42), "project-1", jobID, 5, "success", now, nil, nil, nil, 0, 1, nil, nil, nil, nil, nil, nil, nil, nil, nil, "manual", nil, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil, nil, nil, "", nil, nil,
		"step-1", 1, "Publish package",
	)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT page.identity_key
		FROM (
			SELECT COALESCE(b.job_id::text, b.id::text) || '::' || a.logical_path AS identity_key,
			       a.logical_path,
			       MAX(a.created_at) AS latest_created_at
			FROM build_artifacts a
			JOIN builds b ON b.id = a.build_id
			WHERE (
				$1 = ''
				OR COALESCE(a.artifact_name, '') ILIKE $2
				OR a.logical_path ILIKE $2
				OR b.project_id ILIKE $2
				OR COALESCE(b.job_id::text, '') ILIKE $2
				OR EXISTS (
					SELECT 1
					FROM artifact_versions av
					WHERE av.artifact_id = a.id
					  AND av.version_text ILIKE $2
				)
				OR EXISTS (
					SELECT 1
					FROM artifact_channels ac
					WHERE ac.current_artifact_id = a.id
					  AND ac.channel_name ILIKE $2
				)
			)
			AND ($3 = '' OR a.artifact_type = $3)
			AND ($4 = '' OR b.project_id::text = $4)
			AND ($5 = '' OR COALESCE(b.job_id::text, '') = $5)
			GROUP BY identity_key, a.logical_path
		) page
		ORDER BY page.latest_created_at DESC, page.logical_path ASC, page.identity_key ASC
	`)).WithArgs("pkg-a", "%pkg-a%", "", "", "").WillReturnRows(identityRows)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT 
			a.id, a.build_id, a.package_id, a.step_id, a.artifact_name, a.logical_path, a.artifact_type, a.storage_key, a.storage_provider, a.size_bytes, a.content_type, a.checksum_sha256, a.created_at,
			b.id, b.build_number, b.project_id, b.job_id, b.priority, b.status, b.created_at, b.queued_at, b.started_at, b.finished_at, b.current_step_index, b.attempt_number, b.rerun_of_build_id, b.rerun_from_step_index, b.error_message, b.pipeline_name, b.pipeline_source, b.pipeline_path, b.repo_url, b.ref, b.commit_sha, b.trigger_kind, b.scm_provider, b.event_type, b.trigger_repository_owner, b.trigger_repository_name, b.trigger_repository_url, b.trigger_raw_ref, b.trigger_ref, b.trigger_ref_type, b.trigger_ref_name, b.trigger_deleted, b.trigger_commit_sha, b.trigger_delivery_id, b.trigger_actor, b.requested_image_ref, b.resolved_image_ref, b.image_source_kind, b.managed_image_id, b.managed_image_version_id,
			s.id,
			s.step_index,
			s.name
		FROM build_artifacts a
		JOIN builds b ON b.id = a.build_id
		LEFT JOIN build_steps s ON s.id = a.step_id
		WHERE COALESCE(b.job_id::text, b.id::text) || '::' || a.logical_path IN ($6)
		  AND (
			$1 = ''
			OR COALESCE(a.artifact_name, '') ILIKE $2
			OR a.logical_path ILIKE $2
			OR b.project_id ILIKE $2
			OR COALESCE(b.job_id::text, '') ILIKE $2
			OR EXISTS (
				SELECT 1
				FROM artifact_versions av
				WHERE av.artifact_id = a.id
				  AND av.version_text ILIKE $2
			)
			OR EXISTS (
				SELECT 1
				FROM artifact_channels ac
				WHERE ac.current_artifact_id = a.id
				  AND ac.channel_name ILIKE $2
			)
		)
		  AND ($3 = '' OR a.artifact_type = $3)
		  AND ($4 = '' OR b.project_id::text = $4)
		  AND ($5 = '' OR COALESCE(b.job_id::text, '') = $5)
		ORDER BY a.created_at DESC, a.logical_path ASC, b.created_at DESC
	`)).WithArgs("pkg-a", "%pkg-a%", "", "", "", jobID+"::packages/pkg-a.tgz").WillReturnRows(buildRows)

	records, err := repo.Browse(context.Background(), repository.BrowseArtifactsParams{Query: "pkg-a"})
	if err != nil {
		t.Fatalf("Browse failed: %v", err)
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

func TestArtifactRepository_BrowsePaginatesLogicalArtifacts(t *testing.T) {
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
	identityRows := sqlmock.NewRows([]string{"identity_key"}).AddRow(jobID + "::packages/pkg-a.tgz")
	buildRows := sqlmock.NewRows([]string{
		"id", "build_id", "package_id", "step_id", "artifact_name", "logical_path", "artifact_type", "storage_key", "storage_provider", "size_bytes", "content_type", "checksum_sha256", "created_at",
		"id", "build_number", "project_id", "job_id", "priority", "status", "created_at", "queued_at", "started_at", "finished_at", "current_step_index", "attempt_number", "rerun_of_build_id", "rerun_from_step_index", "error_message", "pipeline_name", "pipeline_source", "pipeline_path", "repo_url", "ref", "commit_sha", "trigger_kind", "scm_provider", "event_type", "trigger_repository_owner", "trigger_repository_name", "trigger_repository_url", "trigger_raw_ref", "trigger_ref", "trigger_ref_type", "trigger_ref_name", "trigger_deleted", "trigger_commit_sha", "trigger_delivery_id", "trigger_actor", "requested_image_ref", "resolved_image_ref", "image_source_kind", "managed_image_id", "managed_image_version_id",
		"id", "step_index", "name",
	}).AddRow(
		"artifact-2", "build-2", "package-1", "step-2", "pkg-a", "packages/pkg-a.tgz", "npm_package", "build-2/pkg-a.tgz", "filesystem", int64(13), "application/gzip", "abc234", now.Add(time.Minute),
		"build-2", int64(43), "project-1", jobID, 5, "success", now.Add(time.Minute), nil, nil, nil, 0, 1, nil, nil, nil, nil, nil, nil, nil, nil, nil, "manual", nil, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil, nil, nil, "", nil, nil,
		"step-2", 1, "Publish package",
	).AddRow(
		"artifact-1", "build-1", "package-1", "step-1", "pkg-a", "packages/pkg-a.tgz", "npm_package", "build-1/pkg-a.tgz", "filesystem", int64(12), "application/gzip", "abc123", now,
		"build-1", int64(42), "project-1", jobID, 5, "success", now, nil, nil, nil, 0, 1, nil, nil, nil, nil, nil, nil, nil, nil, nil, "manual", nil, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil, nil, nil, "", nil, nil,
		"step-1", 1, "Publish package",
	)

	mock.ExpectQuery("SELECT page.identity_key").WithArgs("", "%%", "", "", "", 1, 1).WillReturnRows(identityRows)
	mock.ExpectQuery("SELECT ").WithArgs("", "%%", "", "", "", jobID+"::packages/pkg-a.tgz").WillReturnRows(buildRows)

	records, err := repo.Browse(context.Background(), repository.BrowseArtifactsParams{Limit: 1, Offset: 1})
	if err != nil {
		t.Fatalf("Browse failed: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected both rows for selected logical artifact, got %d", len(records))
	}
	for _, record := range records {
		if record.Artifact.LogicalPath != "packages/pkg-a.tgz" {
			t.Fatalf("expected grouped rows to remain together, got %q", record.Artifact.LogicalPath)
		}
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations failed: %v", err)
	}
}

func TestArtifactRepository_Browse_ChannelSearchMatchesCurrentArtifactOnly(t *testing.T) {
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
	identityRows := sqlmock.NewRows([]string{"identity_key"}).AddRow(jobID + "::packages/pkg-a.tgz")
	buildRows := sqlmock.NewRows([]string{
		"id", "build_id", "package_id", "step_id", "artifact_name", "logical_path", "artifact_type", "storage_key", "storage_provider", "size_bytes", "content_type", "checksum_sha256", "created_at",
		"id", "build_number", "project_id", "job_id", "priority", "status", "created_at", "queued_at", "started_at", "finished_at", "current_step_index", "attempt_number", "rerun_of_build_id", "rerun_from_step_index", "error_message", "pipeline_name", "pipeline_source", "pipeline_path", "repo_url", "ref", "commit_sha", "trigger_kind", "scm_provider", "event_type", "trigger_repository_owner", "trigger_repository_name", "trigger_repository_url", "trigger_raw_ref", "trigger_ref", "trigger_ref_type", "trigger_ref_name", "trigger_deleted", "trigger_commit_sha", "trigger_delivery_id", "trigger_actor", "requested_image_ref", "resolved_image_ref", "image_source_kind", "managed_image_id", "managed_image_version_id",
		"id", "step_index", "name",
	}).AddRow(
		"artifact-current", "build-2", "package-1", "step-2", "pkg-a", "packages/pkg-a.tgz", "npm_package", "build-2/pkg-a.tgz", "filesystem", int64(13), "application/gzip", "abc234", now.Add(time.Minute),
		"build-2", int64(43), "project-1", jobID, 5, "success", now.Add(time.Minute), nil, nil, nil, 0, 1, nil, nil, nil, nil, nil, nil, nil, nil, nil, "manual", nil, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil, nil, nil, "", nil, nil,
		"step-2", 1, "Publish package",
	)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT page.identity_key
		FROM (
			SELECT COALESCE(b.job_id::text, b.id::text) || '::' || a.logical_path AS identity_key,
			       a.logical_path,
			       MAX(a.created_at) AS latest_created_at
			FROM build_artifacts a
			JOIN builds b ON b.id = a.build_id
			WHERE (
				$1 = ''
				OR COALESCE(a.artifact_name, '') ILIKE $2
				OR a.logical_path ILIKE $2
				OR b.project_id ILIKE $2
				OR COALESCE(b.job_id::text, '') ILIKE $2
				OR EXISTS (
					SELECT 1
					FROM artifact_versions av
					WHERE av.artifact_id = a.id
					  AND av.version_text ILIKE $2
				)
				OR EXISTS (
					SELECT 1
					FROM artifact_channels ac
					WHERE ac.current_artifact_id = a.id
					  AND ac.channel_name ILIKE $2
				)
			)
			AND ($3 = '' OR a.artifact_type = $3)
			AND ($4 = '' OR b.project_id::text = $4)
			AND ($5 = '' OR COALESCE(b.job_id::text, '') = $5)
			GROUP BY identity_key, a.logical_path
		) page
		ORDER BY page.latest_created_at DESC, page.logical_path ASC, page.identity_key ASC
	`)).WithArgs("prod", "%prod%", "", "", "").WillReturnRows(identityRows)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT 
			a.id, a.build_id, a.package_id, a.step_id, a.artifact_name, a.logical_path, a.artifact_type, a.storage_key, a.storage_provider, a.size_bytes, a.content_type, a.checksum_sha256, a.created_at,
			b.id, b.build_number, b.project_id, b.job_id, b.priority, b.status, b.created_at, b.queued_at, b.started_at, b.finished_at, b.current_step_index, b.attempt_number, b.rerun_of_build_id, b.rerun_from_step_index, b.error_message, b.pipeline_name, b.pipeline_source, b.pipeline_path, b.repo_url, b.ref, b.commit_sha, b.trigger_kind, b.scm_provider, b.event_type, b.trigger_repository_owner, b.trigger_repository_name, b.trigger_repository_url, b.trigger_raw_ref, b.trigger_ref, b.trigger_ref_type, b.trigger_ref_name, b.trigger_deleted, b.trigger_commit_sha, b.trigger_delivery_id, b.trigger_actor, b.requested_image_ref, b.resolved_image_ref, b.image_source_kind, b.managed_image_id, b.managed_image_version_id,
			s.id,
			s.step_index,
			s.name
		FROM build_artifacts a
		JOIN builds b ON b.id = a.build_id
		LEFT JOIN build_steps s ON s.id = a.step_id
		WHERE COALESCE(b.job_id::text, b.id::text) || '::' || a.logical_path IN ($6)
		  AND (
			$1 = ''
			OR COALESCE(a.artifact_name, '') ILIKE $2
			OR a.logical_path ILIKE $2
			OR b.project_id ILIKE $2
			OR COALESCE(b.job_id::text, '') ILIKE $2
			OR EXISTS (
				SELECT 1
				FROM artifact_versions av
				WHERE av.artifact_id = a.id
				  AND av.version_text ILIKE $2
			)
			OR EXISTS (
				SELECT 1
				FROM artifact_channels ac
				WHERE ac.current_artifact_id = a.id
				  AND ac.channel_name ILIKE $2
			)
		)
		  AND ($3 = '' OR a.artifact_type = $3)
		  AND ($4 = '' OR b.project_id::text = $4)
		  AND ($5 = '' OR COALESCE(b.job_id::text, '') = $5)
		ORDER BY a.created_at DESC, a.logical_path ASC, b.created_at DESC
	`)).WithArgs("prod", "%prod%", "", "", "", jobID+"::packages/pkg-a.tgz").WillReturnRows(buildRows)

	records, err := repo.Browse(context.Background(), repository.BrowseArtifactsParams{Query: "prod"})
	if err != nil {
		t.Fatalf("Browse failed: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 current-channel record, got %d", len(records))
	}
	if records[0].Artifact.ID != "artifact-current" {
		t.Fatalf("expected current channel artifact, got %q", records[0].Artifact.ID)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations failed: %v", err)
	}
}

func TestArtifactRepository_ListCatalog(t *testing.T) {
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
	rows := sqlmock.NewRows([]string{
		"id", "build_id", "package_id", "step_id", "artifact_name", "logical_path", "artifact_type", "storage_key", "storage_provider", "size_bytes", "content_type", "checksum_sha256", "created_at",
		"id", "build_number", "project_id", "job_id", "priority", "status", "created_at", "queued_at", "started_at", "finished_at", "current_step_index", "attempt_number", "rerun_of_build_id", "rerun_from_step_index", "error_message", "pipeline_name", "pipeline_source", "pipeline_path", "repo_url", "ref", "commit_sha", "trigger_kind", "scm_provider", "event_type", "trigger_repository_owner", "trigger_repository_name", "trigger_repository_url", "trigger_raw_ref", "trigger_ref", "trigger_ref_type", "trigger_ref_name", "trigger_deleted", "trigger_commit_sha", "trigger_delivery_id", "trigger_actor", "requested_image_ref", "resolved_image_ref", "image_source_kind", "managed_image_id", "managed_image_version_id",
		"id", "step_index", "name",
	}).AddRow(
		"artifact-1", "build-1", "package-1", "step-1", "coyote-ci/package-a", "packages/pkg-a.tgz", "npm_package", "build-1/pkg-a.tgz", "filesystem", int64(12), "application/gzip", "abc123", now,
		"build-1", int64(42), "project-1", jobID, 5, "success", now, nil, nil, nil, 0, 1, nil, nil, nil, nil, nil, nil, nil, nil, nil, "manual", nil, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil, nil, nil, "", nil, nil,
		"step-1", 1, "Publish package",
	)

	mock.ExpectQuery("SELECT ").WithArgs("pkg-a", "%pkg-a%", "project-1", jobID, "build-1", 5, 10).WillReturnRows(rows)

	records, err := repo.ListCatalog(context.Background(), repository.ArtifactCatalogParams{
		Query:     "pkg-a",
		ProjectID: "project-1",
		JobID:     jobID,
		BuildID:   "build-1",
		Limit:     5,
		Offset:    10,
	})
	if err != nil {
		t.Fatalf("ListCatalog failed: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Build.ID != "build-1" {
		t.Fatalf("expected build id build-1, got %q", records[0].Build.ID)
	}
	if records[0].Artifact.StorageKey != "build-1/pkg-a.tgz" {
		t.Fatalf("expected storage key, got %q", records[0].Artifact.StorageKey)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations failed: %v", err)
	}
}

func TestArtifactRepository_GetCatalogByID_NotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	repo := NewArtifactRepository(db)
	mock.ExpectQuery("SELECT ").WithArgs("missing").WillReturnRows(sqlmock.NewRows([]string{
		"id", "build_id", "package_id", "step_id", "artifact_name", "logical_path", "artifact_type", "storage_key", "storage_provider", "size_bytes", "content_type", "checksum_sha256", "created_at",
		"id", "build_number", "project_id", "job_id", "priority", "status", "created_at", "queued_at", "started_at", "finished_at", "current_step_index", "attempt_number", "rerun_of_build_id", "rerun_from_step_index", "error_message", "pipeline_name", "pipeline_source", "pipeline_path", "repo_url", "ref", "commit_sha", "trigger_kind", "scm_provider", "event_type", "trigger_repository_owner", "trigger_repository_name", "trigger_repository_url", "trigger_raw_ref", "trigger_ref", "trigger_ref_type", "trigger_ref_name", "trigger_deleted", "trigger_commit_sha", "trigger_delivery_id", "trigger_actor", "requested_image_ref", "resolved_image_ref", "image_source_kind", "managed_image_id", "managed_image_version_id",
		"id", "step_index", "name",
	}))

	_, err = repo.GetCatalogByID(context.Background(), "missing")
	if err != repository.ErrArtifactNotFound {
		t.Fatalf("expected ErrArtifactNotFound, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations failed: %v", err)
	}
}
