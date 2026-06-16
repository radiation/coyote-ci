package postgres

import (
	"context"
	"database/sql/driver"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func artifactRecordColumns() []string {
	columns := strings.Split(strings.ReplaceAll(artifactColumns, " ", ""), ",")
	columns = append(columns, strings.Split(strings.ReplaceAll(buildListColumns, " ", ""), ",")...)
	columns = append(columns, "id", "step_index", "name")
	return columns
}

func artifactColumnPosition(columns []string, want string) int {
	for idx, column := range columns {
		if column == want {
			return idx
		}
	}
	return -1
}

func makeArtifactBrowseRow(
	artifactID string,
	buildID string,
	packageID string,
	stepID string,
	artifactName string,
	logicalPath string,
	artifactType string,
	storageKey string,
	sizeBytes int64,
	contentType string,
	checksum string,
	artifactCreatedAt time.Time,
	buildNumber int64,
	projectID string,
	jobID string,
	buildStatus string,
	buildCreatedAt time.Time,
	stepIndex int,
	stepName string,
) []driver.Value {
	columns := artifactRecordColumns()
	row := make([]driver.Value, len(columns))

	row[artifactColumnPosition(columns, "build_id")] = buildID
	row[artifactColumnPosition(columns, "package_id")] = packageID
	row[artifactColumnPosition(columns, "step_id")] = stepID
	row[artifactColumnPosition(columns, "artifact_name")] = artifactName
	row[artifactColumnPosition(columns, "logical_path")] = logicalPath
	row[artifactColumnPosition(columns, "artifact_type")] = artifactType
	row[artifactColumnPosition(columns, "storage_key")] = storageKey
	row[artifactColumnPosition(columns, "storage_provider")] = "filesystem"
	row[artifactColumnPosition(columns, "size_bytes")] = sizeBytes
	row[artifactColumnPosition(columns, "content_type")] = contentType
	row[artifactColumnPosition(columns, "checksum_sha256")] = checksum
	row[artifactColumnPosition(columns, "created_at")] = artifactCreatedAt

	artifactIDPos := artifactColumnPosition(columns, "id")
	row[artifactIDPos] = artifactID
	buildIDPos := artifactIDPos + len(strings.Split(strings.ReplaceAll(artifactColumns, " ", ""), ","))
	stepIDPos := len(columns) - 3
	row[buildIDPos] = buildID
	row[buildIDPos+1] = buildNumber
	row[buildIDPos+2] = projectID
	row[buildIDPos+3] = jobID
	row[buildIDPos+4] = 5
	row[buildIDPos+5] = buildStatus
	row[buildIDPos+6] = buildCreatedAt
	row[buildColumnPosition(strings.Split(strings.ReplaceAll(buildListColumns, " ", ""), ","), "current_step_index")+len(strings.Split(strings.ReplaceAll(artifactColumns, " ", ""), ","))] = 0
	row[buildColumnPosition(strings.Split(strings.ReplaceAll(buildListColumns, " ", ""), ","), "attempt_number")+len(strings.Split(strings.ReplaceAll(artifactColumns, " ", ""), ","))] = 1
	row[buildColumnPosition(strings.Split(strings.ReplaceAll(buildListColumns, " ", ""), ","), "trigger_kind")+len(strings.Split(strings.ReplaceAll(artifactColumns, " ", ""), ","))] = "manual"
	row[buildColumnPosition(strings.Split(strings.ReplaceAll(buildListColumns, " ", ""), ","), "trigger_deleted")+len(strings.Split(strings.ReplaceAll(artifactColumns, " ", ""), ","))] = false
	row[buildColumnPosition(strings.Split(strings.ReplaceAll(buildListColumns, " ", ""), ","), "image_source_kind")+len(strings.Split(strings.ReplaceAll(artifactColumns, " ", ""), ","))] = "external"

	row[stepIDPos] = stepID
	row[stepIDPos+1] = stepIndex
	row[stepIDPos+2] = stepName

	return row
}

func artifactBrowseSelectPattern() string {
	return regexp.QuoteMeta(`
		SELECT 
			` + qualifyColumns("a", artifactColumns) + `,
			` + qualifyColumns("b", buildListColumns) + `,
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
	`)
}

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
	buildRows := sqlmock.NewRows(artifactRecordColumns()).AddRow(makeArtifactBrowseRow("artifact-1", "build-1", "package-1", "step-1", "coyote-ci/package-a", "packages/pkg-a.tgz", "npm_package", "build-1/pkg-a.tgz", 12, "application/gzip", "abc123", now, 42, "project-1", jobID, "success", now, 1, "Publish package")...)

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

	mock.ExpectQuery(artifactBrowseSelectPattern()).WithArgs("pkg-a", "%pkg-a%", "", "", "", jobID+"::packages/pkg-a.tgz").WillReturnRows(buildRows)

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
	buildRows := sqlmock.NewRows(artifactRecordColumns()).AddRow(makeArtifactBrowseRow("artifact-2", "build-2", "package-1", "step-2", "pkg-a", "packages/pkg-a.tgz", "npm_package", "build-2/pkg-a.tgz", 13, "application/gzip", "abc234", now.Add(time.Minute), 43, "project-1", jobID, "success", now.Add(time.Minute), 1, "Publish package")...).AddRow(makeArtifactBrowseRow("artifact-1", "build-1", "package-1", "step-1", "pkg-a", "packages/pkg-a.tgz", "npm_package", "build-1/pkg-a.tgz", 12, "application/gzip", "abc123", now, 42, "project-1", jobID, "success", now, 1, "Publish package")...)

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

func TestArtifactRepository_BrowseFiltersByJobID(t *testing.T) {
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
	buildRows := sqlmock.NewRows(artifactRecordColumns()).AddRow(makeArtifactBrowseRow("artifact-1", "build-1", "package-1", "step-1", "pkg-a", "packages/pkg-a.tgz", "npm_package", "build-1/pkg-a.tgz", 12, "application/gzip", "abc123", now, 42, "project-1", jobID, "success", now, 1, "Publish package")...)

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
	`)).WithArgs("", "%%", "", "", jobID).WillReturnRows(identityRows)

	mock.ExpectQuery(artifactBrowseSelectPattern()).WithArgs("", "%%", "", "", jobID, jobID+"::packages/pkg-a.tgz").WillReturnRows(buildRows)

	records, err := repo.Browse(context.Background(), repository.BrowseArtifactsParams{JobID: jobID})
	if err != nil {
		t.Fatalf("Browse failed: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Build.JobID == nil || *records[0].Build.JobID != jobID {
		t.Fatalf("expected job id %q, got %#v", jobID, records[0].Build.JobID)
	}
	if records[0].Artifact.ID != "artifact-1" {
		t.Fatalf("expected artifact-1, got %q", records[0].Artifact.ID)
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
	buildRows := sqlmock.NewRows(artifactRecordColumns()).AddRow(makeArtifactBrowseRow("artifact-current", "build-2", "package-1", "step-2", "pkg-a", "packages/pkg-a.tgz", "npm_package", "build-2/pkg-a.tgz", 13, "application/gzip", "abc234", now.Add(time.Minute), 43, "project-1", jobID, "success", now.Add(time.Minute), 1, "Publish package")...)

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

	mock.ExpectQuery(artifactBrowseSelectPattern()).WithArgs("prod", "%prod%", "", "", "", jobID+"::packages/pkg-a.tgz").WillReturnRows(buildRows)

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
	rows := sqlmock.NewRows(artifactRecordColumns()).AddRow(makeArtifactBrowseRow("artifact-1", "build-1", "package-1", "step-1", "coyote-ci/package-a", "packages/pkg-a.tgz", "npm_package", "build-1/pkg-a.tgz", 12, "application/gzip", "abc123", now, 42, "project-1", jobID, "success", now, 1, "Publish package")...)

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
	mock.ExpectQuery("SELECT ").WithArgs("missing").WillReturnRows(sqlmock.NewRows(artifactRecordColumns()))

	_, err = repo.GetCatalogByID(context.Background(), "missing")
	if err != repository.ErrArtifactNotFound {
		t.Fatalf("expected ErrArtifactNotFound, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations failed: %v", err)
	}
}
