package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func TestProjectRepository_PersistsVisibility(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}

	repo := NewProjectRepository(db)
	now := time.Now().UTC()
	description := "Public pipelines"
	project := domain.Project{
		ID:          "11111111-1111-1111-1111-111111111111",
		Name:        "Platform",
		Slug:        "platform",
		Description: &description,
		IsPublic:    true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	projectColumns := []string{"id", "name", "slug", "description", "is_public", "created_at", "updated_at"}

	mock.ExpectQuery("INSERT INTO projects").
		WithArgs(project.ID, project.Name, project.Slug, project.Description, true, project.CreatedAt, project.UpdatedAt).
		WillReturnRows(sqlmock.NewRows(projectColumns).AddRow(project.ID, project.Name, project.Slug, *project.Description, true, project.CreatedAt, project.UpdatedAt))
	created, createErr := repo.Create(context.Background(), project)
	if createErr != nil {
		t.Fatalf("create project failed: %v", createErr)
	}
	if !created.IsPublic {
		t.Fatal("expected created project to retain public visibility")
	}

	mock.ExpectQuery("SELECT id, name, slug, description, is_public, created_at, updated_at FROM projects").
		WithArgs(project.ID).
		WillReturnRows(sqlmock.NewRows(projectColumns).AddRow(project.ID, project.Name, project.Slug, *project.Description, true, project.CreatedAt, project.UpdatedAt))
	byID, getByIDErr := repo.GetByID(context.Background(), project.ID)
	if getByIDErr != nil {
		t.Fatalf("get project by id failed: %v", getByIDErr)
	}
	if !byID.IsPublic {
		t.Fatal("expected get by id to scan public visibility")
	}

	mock.ExpectQuery("SELECT id, name, slug, description, is_public, created_at, updated_at FROM projects").
		WithArgs(project.Slug).
		WillReturnRows(sqlmock.NewRows(projectColumns).AddRow(project.ID, project.Name, project.Slug, *project.Description, true, project.CreatedAt, project.UpdatedAt))
	bySlug, getBySlugErr := repo.GetBySlug(context.Background(), project.Slug)
	if getBySlugErr != nil {
		t.Fatalf("get project by slug failed: %v", getBySlugErr)
	}
	if !bySlug.IsPublic {
		t.Fatal("expected get by slug to scan public visibility")
	}

	mock.ExpectQuery("SELECT id, name, slug, description, is_public, created_at, updated_at FROM projects").
		WillReturnRows(sqlmock.NewRows(projectColumns).AddRow(project.ID, project.Name, project.Slug, *project.Description, true, project.CreatedAt, project.UpdatedAt))
	listed, listErr := repo.List(context.Background())
	if listErr != nil {
		t.Fatalf("list projects failed: %v", listErr)
	}
	if len(listed) != 1 || !listed[0].IsPublic {
		t.Fatalf("expected listed project to retain public visibility, got %+v", listed)
	}

	project.IsPublic = false
	project.UpdatedAt = now.Add(time.Minute)
	mock.ExpectQuery("UPDATE projects").
		WithArgs(project.ID, project.Name, project.Slug, project.Description, false, project.UpdatedAt).
		WillReturnRows(sqlmock.NewRows(projectColumns).AddRow(project.ID, project.Name, project.Slug, *project.Description, false, project.CreatedAt, project.UpdatedAt))
	updated, updateErr := repo.Update(context.Background(), project)
	if updateErr != nil {
		t.Fatalf("update project failed: %v", updateErr)
	}
	if updated.IsPublic {
		t.Fatal("expected updated project to persist private visibility")
	}

	if expectationsErr := mock.ExpectationsWereMet(); expectationsErr != nil {
		t.Fatalf("unmet sql expectations: %v", expectationsErr)
	}
	mock.ExpectClose()
	if closeErr := db.Close(); closeErr != nil {
		t.Fatalf("failed to close sqlmock db: %v", closeErr)
	}
}

func TestProjectRepository_GetByIDs(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}

	repo := NewProjectRepository(db)
	now := time.Now().UTC()
	rows := sqlmock.NewRows([]string{"id", "name", "slug", "description", "is_public", "created_at", "updated_at"}).
		AddRow("11111111-1111-1111-1111-111111111111", "Platform", "platform", nil, false, now, now).
		AddRow("22222222-2222-2222-2222-222222222222", "Backend", "backend", "Backend services", true, now.Add(time.Second), now.Add(time.Second))

	mock.ExpectQuery("SELECT id, name, slug, description, is_public, created_at, updated_at FROM projects").
		WithArgs("11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222").
		WillReturnRows(rows)
	mock.ExpectClose()

	projects, err := repo.GetByIDs(context.Background(), []string{"11111111-1111-1111-1111-111111111111", "", "22222222-2222-2222-2222-222222222222", "11111111-1111-1111-1111-111111111111"})
	if err != nil {
		t.Fatalf("GetByIDs failed: %v", err)
	}
	if len(projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(projects))
	}
	if projects[0].ID != "11111111-1111-1111-1111-111111111111" || projects[1].ID != "22222222-2222-2222-2222-222222222222" {
		t.Fatalf("unexpected project ids: %+v", projects)
	}
	if projects[0].IsPublic || !projects[1].IsPublic {
		t.Fatalf("unexpected project visibility: %+v", projects)
	}
	if closeErr := db.Close(); closeErr != nil {
		t.Fatalf("failed to close sqlmock db: %v", closeErr)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestProjectRepository_GetByIDsSkipsInvalidUUIDs(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}

	repo := NewProjectRepository(db)
	now := time.Now().UTC()
	rows := sqlmock.NewRows([]string{"id", "name", "slug", "description", "is_public", "created_at", "updated_at"}).
		AddRow("11111111-1111-1111-1111-111111111111", "Platform", "platform", nil, false, now, now)

	mock.ExpectQuery("SELECT id, name, slug, description, is_public, created_at, updated_at FROM projects").
		WithArgs("11111111-1111-1111-1111-111111111111").
		WillReturnRows(rows)
	mock.ExpectClose()

	projects, err := repo.GetByIDs(context.Background(), []string{"fixtures", "11111111-1111-1111-1111-111111111111", "fixtures"})
	if err != nil {
		t.Fatalf("GetByIDs failed: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(projects))
	}
	if projects[0].ID != "11111111-1111-1111-1111-111111111111" {
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
