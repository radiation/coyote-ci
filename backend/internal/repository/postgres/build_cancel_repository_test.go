package postgres

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func TestBuildRepository_CancelBuild_UpdatesBuildStepsAndJobsAtomically(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sql mock: %v", err)
	}

	repo := NewBuildRepository(db)
	now := time.Now().UTC()
	canceledAt := now.Add(time.Minute)

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, build_number, project_id, job_id, priority, status, created_at").
		WithArgs("build-1").
		WillReturnRows(buildCancelMockRows(domain.Build{ID: "build-1", BuildNumber: 7, ProjectID: "project-1", Priority: 5, Status: domain.BuildStatusRunning, CreatedAt: now, StartedAt: &now}))
	mock.ExpectExec("UPDATE build_steps").
		WithArgs("build-1", canceledAt, "operator canceled").
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec("UPDATE build_jobs").
		WithArgs("build-1", canceledAt, "operator canceled").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("UPDATE builds").
		WithArgs("build-1", canceledAt, "operator canceled").
		WillReturnRows(buildCancelMockRows(domain.Build{ID: "build-1", BuildNumber: 7, ProjectID: "project-1", Priority: 5, Status: domain.BuildStatusCanceled, CreatedAt: now, StartedAt: &now, FinishedAt: &canceledAt, ErrorMessage: buildCancelStringPtr("operator canceled")}))
	mock.ExpectCommit()

	build, updatedSteps, err := repo.CancelBuild(context.Background(), "build-1", " operator canceled ", canceledAt)
	if err != nil {
		t.Fatalf("CancelBuild returned error: %v", err)
	}
	if build.Status != domain.BuildStatusCanceled {
		t.Fatalf("expected canceled build, got %q", build.Status)
	}
	if updatedSteps != 2 {
		t.Fatalf("expected 2 updated steps, got %d", updatedSteps)
	}
	if build.ErrorMessage == nil || *build.ErrorMessage != "operator canceled" {
		t.Fatalf("expected trimmed cancel reason, got %#v", build.ErrorMessage)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestBuildRepository_CancelBuild_RejectsTerminalBuildsBeforeMutation(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sql mock: %v", err)
	}

	repo := NewBuildRepository(db)
	now := time.Now().UTC()

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, build_number, project_id, job_id, priority, status, created_at").
		WithArgs("build-1").
		WillReturnRows(buildCancelMockRows(domain.Build{ID: "build-1", BuildNumber: 7, ProjectID: "project-1", Priority: 5, Status: domain.BuildStatusCanceled, CreatedAt: now, FinishedAt: &now}))
	mock.ExpectRollback()

	_, _, err = repo.CancelBuild(context.Background(), "build-1", "operator canceled", now.Add(time.Minute))
	if !errors.Is(err, repository.ErrInvalidBuildStatusTransition) {
		t.Fatalf("expected ErrInvalidBuildStatusTransition, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func buildCancelMockRows(build domain.Build) *sqlmock.Rows {
	return sqlmock.NewRows(buildMockColumns).AddRow(buildCancelMockRow(build)...)
}

func buildCancelMockRow(build domain.Build) []driver.Value {
	row := make([]driver.Value, len(buildMockColumns))
	row[0] = build.ID
	row[1] = build.BuildNumber
	row[2] = build.ProjectID
	if build.JobID != nil {
		row[3] = *build.JobID
	}
	row[4] = build.Priority
	row[5] = string(build.Status)
	row[6] = build.CreatedAt
	row[7] = build.QueuedAt
	row[8] = build.StartedAt
	row[9] = build.FinishedAt
	row[10] = build.CurrentStepIndex
	row[11] = build.AttemptNumber
	row[12] = build.RerunOfBuildID
	row[13] = build.RerunFromStepIdx
	row[14] = build.ErrorMessage
	row[22] = string(domain.NormalizeBuildTrigger(build.Trigger).Kind)
	row[37] = string(defaultBuildImageSourceKind(build.ImageSourceKind))
	return row
}

func buildCancelStringPtr(value string) *string {
	return &value
}
