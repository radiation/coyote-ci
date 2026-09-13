package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

var externalImageBuildMockColumns = []string{"execution_job_id", "provider", "submission_state", "external_build_id", "external_resource_name", "source_bucket", "source_object", "source_generation", "target_image_reference", "submitted_at", "last_provider_status", "terminal_result", "image_digest", "external_log_url", "failure_detail", "created_at", "updated_at"}

func TestExternalImageBuildRepositoryCreateIntentAndGet(t *testing.T) {
	db, mock, newErr := sqlmock.New()
	if newErr != nil {
		t.Fatal(newErr)
	}
	defer func() { _ = db.Close() }()
	now := time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC)
	row := externalImageBuildRow(now)
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO external_image_builds")).WithArgs("execution-1", domain.ImageBuildProviderCloudBuild, domain.ExternalImageBuildSubmissionIntent, nil, nil, nil, nil, nil, "coyote-ci/backend", nil, nil, nil, nil, nil, nil, sqlmock.AnyArg(), sqlmock.AnyArg()).WillReturnRows(sqlmock.NewRows(externalImageBuildMockColumns).AddRow(row...))
	repo := NewExternalImageBuildRepository(db)
	created, createErr := repo.CreateIntent(context.Background(), domain.ExternalImageBuild{ExecutionJobID: "execution-1", Provider: domain.ImageBuildProviderCloudBuild, TargetImageReference: "coyote-ci/backend"})
	if createErr != nil || created.SubmissionState != domain.ExternalImageBuildSubmissionIntent || created.Source.Bucket != "sources" {
		t.Fatalf("created=%+v err=%v", created, createErr)
	}
	if expectationsErr := mock.ExpectationsWereMet(); expectationsErr != nil {
		t.Fatal(expectationsErr)
	}
}

func TestExternalImageBuildRepositoryCreateIntentConflictAndMissingRows(t *testing.T) {
	db, mock, newErr := sqlmock.New()
	if newErr != nil {
		t.Fatal(newErr)
	}
	defer func() { _ = db.Close() }()
	repo := NewExternalImageBuildRepository(db)
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO external_image_builds")).WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT " + externalImageBuildColumns + " FROM external_image_builds WHERE execution_job_id = $1")).WithArgs("execution-1").WillReturnError(sql.ErrNoRows)
	if _, createErr := repo.CreateIntent(context.Background(), domain.ExternalImageBuild{ExecutionJobID: "execution-1"}); !errors.Is(createErr, repository.ErrExternalImageBuildNotFound) {
		t.Fatalf("create error=%v", createErr)
	}
	mock.ExpectQuery(regexp.QuoteMeta("SELECT " + externalImageBuildColumns + " FROM external_image_builds WHERE execution_job_id = $1")).WithArgs("missing").WillReturnError(sql.ErrNoRows)
	if _, getErr := repo.GetByExecutionJobID(context.Background(), "missing"); !errors.Is(getErr, repository.ErrExternalImageBuildNotFound) {
		t.Fatalf("get error=%v", getErr)
	}
}

func TestExternalImageBuildRepositoryUpdate(t *testing.T) {
	db, mock, newErr := sqlmock.New()
	if newErr != nil {
		t.Fatal(newErr)
	}
	defer func() { _ = db.Close() }()
	now := time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC)
	submittedAt := now.Add(time.Minute)
	build := domain.ExternalImageBuild{ExecutionJobID: "execution-1", Provider: domain.ImageBuildProviderCloudBuild, SubmissionState: domain.ExternalImageBuildSubmissionSubmitted, ExternalBuildID: "build-1", Source: domain.ImageBuildSource{Bucket: "sources", Object: "source.tar.gz", Generation: "1"}, TargetImageReference: "coyote-ci/backend", SubmittedAt: &submittedAt}
	mock.ExpectQuery(regexp.QuoteMeta("UPDATE external_image_builds SET")).WithArgs("execution-1", domain.ImageBuildProviderCloudBuild, domain.ExternalImageBuildSubmissionSubmitted, "build-1", nil, "sources", "source.tar.gz", "1", "coyote-ci/backend", &submittedAt, nil, nil, nil, nil, nil).WillReturnRows(sqlmock.NewRows(externalImageBuildMockColumns).AddRow(externalImageBuildRow(now)...))
	updated, updateErr := NewExternalImageBuildRepository(db).Update(context.Background(), build)
	if updateErr != nil || updated.ExecutionJobID != "execution-1" {
		t.Fatalf("updated=%+v err=%v", updated, updateErr)
	}
}

func externalImageBuildRow(now time.Time) []driver.Value {
	return []driver.Value{"execution-1", "cloud_build", "intent", "build-1", "resource-1", "sources", "source.tar.gz", "1", "coyote-ci/backend", nil, "running", nil, nil, "https://logs", nil, now, now}
}
