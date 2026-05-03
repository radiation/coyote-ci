package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func TestProjectRepository_GetByIDs(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}

	repo := NewProjectRepository(db)
	now := time.Now().UTC()
	rows := sqlmock.NewRows([]string{"id", "name", "slug", "description", "created_at", "updated_at"}).
		AddRow("project-1", "Platform", "platform", nil, now, now).
		AddRow("project-2", "Backend", "backend", "Backend services", now.Add(time.Second), now.Add(time.Second))

	mock.ExpectQuery("SELECT id, name, slug, description, created_at, updated_at FROM projects").
		WithArgs("project-1", "project-2").
		WillReturnRows(rows)
	mock.ExpectClose()

	projects, err := repo.GetByIDs(context.Background(), []string{"project-1", "", "project-2", "project-1"})
	if err != nil {
		t.Fatalf("GetByIDs failed: %v", err)
	}
	if len(projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(projects))
	}
	if projects[0].ID != "project-1" || projects[1].ID != "project-2" {
		t.Fatalf("unexpected project ids: %+v", projects)
	}
	if closeErr := db.Close(); closeErr != nil {
		t.Fatalf("failed to close sqlmock db: %v", closeErr)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestProjectRepository_DeleteMapsJobForeignKeyViolation(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}

	repo := NewProjectRepository(db)
	mock.ExpectExec("DELETE FROM projects").
		WithArgs("project-1").
		WillReturnError(&pgconn.PgError{Code: "23503", ConstraintName: "fk_jobs_project_id"})
	mock.ExpectClose()

	err = repo.Delete(context.Background(), "project-1")
	if !errors.Is(err, repository.ErrProjectHasJobs) {
		t.Fatalf("expected ErrProjectHasJobs, got %v", err)
	}
	if closeErr := db.Close(); closeErr != nil {
		t.Fatalf("failed to close sqlmock db: %v", closeErr)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestProjectRepository_DeleteReturnsNotFoundWhenNoRowsDeleted(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}

	repo := NewProjectRepository(db)
	mock.ExpectExec("DELETE FROM projects").
		WithArgs("missing-project").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectClose()

	err = repo.Delete(context.Background(), "missing-project")
	if !errors.Is(err, repository.ErrProjectNotFound) {
		t.Fatalf("expected ErrProjectNotFound, got %v", err)
	}
	if closeErr := db.Close(); closeErr != nil {
		t.Fatalf("failed to close sqlmock db: %v", closeErr)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}
