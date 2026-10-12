package postgres

import (
	"context"
	"database/sql"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/radiation/coyote-ci/backend/internal/domain"
)

func TestApplicationReleaseRepository_ReconcileFinalizesAndMovesLatest(t *testing.T) {
	db, mock, newErr := sqlmock.New()
	if newErr != nil {
		t.Fatalf("new sql mock: %v", newErr)
	}
	defer func() { _ = db.Close() }()
	now := time.Now().UTC()
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT " + applicationReleaseColumns + " FROM application_releases WHERE id=$1 FOR UPDATE")).WithArgs("release-1").WillReturnRows(applicationReleaseMockRows(now, "pending", "latest"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT status, error_message FROM builds WHERE id=$1 FOR UPDATE")).WithArgs("build-1").WillReturnRows(sqlmock.NewRows([]string{"status", "error_message"}).AddRow("success", nil))
	mock.ExpectQuery("SELECT id, output_kind.*FROM application_release_outputs WHERE release_id=\\$1 FOR UPDATE").WithArgs("release-1").WillReturnRows(sqlmock.NewRows(applicationReleaseOutputMockColumns))
	mock.ExpectQuery("SELECT EXISTS").WithArgs("release-1").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectExec("UPDATE application_releases SET status='complete'").WithArgs("release-1", sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT id, current_release_id FROM application_channels").WithArgs("project-1", "job-1", "latest").WillReturnError(sql.ErrNoRows)
	mock.ExpectExec("INSERT INTO application_channels").WithArgs(sqlmock.AnyArg(), "project-1", "job-1", "latest", "release-1").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO application_channel_events").WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), nil, "release-1", nil, "automatic release completion").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT " + applicationReleaseColumns + " FROM application_releases WHERE id=$1")).WithArgs("release-1").WillReturnRows(applicationReleaseMockRows(now, "complete", "latest"))
	mock.ExpectQuery("SELECT id, output_kind.*FROM application_release_outputs WHERE release_id=\\$1 ORDER BY output_key").WithArgs("release-1").WillReturnRows(sqlmock.NewRows(applicationReleaseOutputMockColumns))

	release, reconcileErr := NewApplicationReleaseRepository(db).Reconcile(context.Background(), "release-1")
	if reconcileErr != nil {
		t.Fatalf("reconcile release: %v", reconcileErr)
	}
	if release.Status != domain.ApplicationReleaseStatusComplete {
		t.Fatalf("status=%q, want complete", release.Status)
	}
	if expectationsErr := mock.ExpectationsWereMet(); expectationsErr != nil {
		t.Fatalf("unmet SQL expectations: %v", expectationsErr)
	}
}

func TestApplicationReleaseRepository_ReconcileRequiresSuccessfulBuild(t *testing.T) {
	tests := []struct {
		name          string
		buildStatus   string
		buildError    any
		releaseStatus string
		reason        string
	}{
		{name: "running remains pending", buildStatus: "running", releaseStatus: "pending"},
		{name: "failed marks release failed", buildStatus: "failed", buildError: "compile failed", releaseStatus: "failed", reason: "compile failed"},
		{name: "canceled marks release failed", buildStatus: "canceled", releaseStatus: "failed", reason: "build canceled"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			db, mock, newErr := sqlmock.New()
			if newErr != nil {
				t.Fatalf("new sql mock: %v", newErr)
			}
			defer func() { _ = db.Close() }()
			now := time.Now().UTC()
			mock.ExpectBegin()
			mock.ExpectQuery(regexp.QuoteMeta("SELECT " + applicationReleaseColumns + " FROM application_releases WHERE id=$1 FOR UPDATE")).WithArgs("release-1").WillReturnRows(applicationReleaseMockRows(now, "pending", "latest"))
			mock.ExpectQuery(regexp.QuoteMeta("SELECT status, error_message FROM builds WHERE id=$1 FOR UPDATE")).WithArgs("build-1").WillReturnRows(sqlmock.NewRows([]string{"status", "error_message"}).AddRow(testCase.buildStatus, testCase.buildError))
			if testCase.reason != "" {
				mock.ExpectExec("UPDATE application_releases SET status='failed'").WithArgs("release-1", testCase.reason).WillReturnResult(sqlmock.NewResult(0, 1))
			}
			mock.ExpectCommit()
			mock.ExpectQuery(regexp.QuoteMeta("SELECT " + applicationReleaseColumns + " FROM application_releases WHERE id=$1")).WithArgs("release-1").WillReturnRows(applicationReleaseMockRows(now, testCase.releaseStatus, "latest"))
			mock.ExpectQuery("SELECT id, output_kind.*FROM application_release_outputs WHERE release_id=\\$1 ORDER BY output_key").WithArgs("release-1").WillReturnRows(sqlmock.NewRows(applicationReleaseOutputMockColumns))

			release, reconcileErr := NewApplicationReleaseRepository(db).Reconcile(context.Background(), "release-1")
			if reconcileErr != nil {
				t.Fatalf("reconcile release: %v", reconcileErr)
			}
			if string(release.Status) != testCase.releaseStatus {
				t.Fatalf("status=%q, want %q", release.Status, testCase.releaseStatus)
			}
			if expectationsErr := mock.ExpectationsWereMet(); expectationsErr != nil {
				t.Fatalf("unmet SQL expectations: %v", expectationsErr)
			}
		})
	}
}

var applicationReleaseOutputMockColumns = []string{"id", "output_kind", "output_key", "display_name", "definition_json", "status", "artifact_id", "artifact_version_id", "execution_job_id", "immutable_reference", "failure_reason", "published_at", "failed_at", "created_at", "updated_at"}

func applicationReleaseMockRows(now time.Time, status string, autoChannel string) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "build_id", "project_id", "job_id", "application_version", "status", "auto_channel_name", "failure_reason", "created_at", "completed_at", "failed_at", "updated_at"}).AddRow("release-1", "build-1", "project-1", "job-1", "1.2.3", status, autoChannel, nil, now, nil, nil, now)
}
