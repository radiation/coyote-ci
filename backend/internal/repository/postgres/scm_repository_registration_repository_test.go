package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func TestSCMRepositoryRegistrationRepository_CreateGetListAndUpdate(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewSCMRepositoryRegistrationRepository(db)
	now := time.Now().UTC()
	branch := "main"
	registration := domain.SCMRepositoryRegistration{ID: "repo-1", ConnectionID: "connection-1", ProviderRepositoryID: "1001", Owner: "octo", Name: "widgets", FullName: "octo/widgets", CloneURL: "https://github.com/octo/widgets.git", WebURL: "https://github.com/octo/widgets", DefaultBranch: &branch, MetadataRefreshedAt: now, CreatedAt: now, UpdatedAt: now}
	rows := scmRepositoryRegistrationTestRows(now)

	mock.ExpectQuery("INSERT INTO scm_registered_repositories").WillReturnRows(rows)
	created, createErr := repo.Create(context.Background(), registration)
	if createErr != nil {
		t.Fatalf("create failed: %v", createErr)
	}
	if created.ProviderRepositoryID != "1001" {
		t.Fatalf("expected provider repository id to round-trip, got %q", created.ProviderRepositoryID)
	}

	mock.ExpectQuery(`SELECT id, connection_id, provider_repository_id, owner_name, repository_name, full_name, clone_url, web_url, default_branch, archived, disabled, metadata_refreshed_at, created_at, updated_at FROM scm_registered_repositories WHERE id = \$1`).WithArgs("repo-1").WillReturnRows(scmRepositoryRegistrationTestRows(now))
	if _, err := repo.GetByID(context.Background(), "repo-1"); err != nil {
		t.Fatalf("get failed: %v", err)
	}

	mock.ExpectQuery(`SELECT id, connection_id, provider_repository_id, owner_name, repository_name, full_name, clone_url, web_url, default_branch, archived, disabled, metadata_refreshed_at, created_at, updated_at FROM scm_registered_repositories ORDER BY`).WillReturnRows(scmRepositoryRegistrationTestRows(now))
	list, listErr := repo.List(context.Background())
	if listErr != nil {
		t.Fatalf("list failed: %v", listErr)
	}
	if len(list) != 1 {
		t.Fatalf("expected one repository, got %d", len(list))
	}

	registration.Owner = "acme"
	registration.Name = "platform"
	registration.FullName = "acme/platform"
	registration.CloneURL = "https://github.com/acme/platform.git"
	registration.WebURL = "https://github.com/acme/platform"
	registration.UpdatedAt = now.Add(time.Minute)
	mock.ExpectQuery("UPDATE scm_registered_repositories").WillReturnRows(sqlmock.NewRows([]string{"id", "connection_id", "provider_repository_id", "owner_name", "repository_name", "full_name", "clone_url", "web_url", "default_branch", "archived", "disabled", "metadata_refreshed_at", "created_at", "updated_at"}).AddRow("repo-1", "connection-1", "1001", "acme", "platform", "acme/platform", "https://github.com/acme/platform.git", "https://github.com/acme/platform", "main", false, false, now, now, now.Add(time.Minute)))
	updated, updateErr := repo.Update(context.Background(), registration)
	if updateErr != nil {
		t.Fatalf("update failed: %v", updateErr)
	}
	if updated.FullName != "acme/platform" {
		t.Fatalf("expected updated full name, got %q", updated.FullName)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestSCMRepositoryRegistrationRepository_CreateDuplicateMapsToConflict(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewSCMRepositoryRegistrationRepository(db)
	now := time.Now().UTC()
	registration := domain.SCMRepositoryRegistration{ID: "repo-1", ConnectionID: "connection-1", ProviderRepositoryID: "1001", Owner: "octo", Name: "widgets", FullName: "octo/widgets", CloneURL: "https://github.com/octo/widgets.git", WebURL: "https://github.com/octo/widgets", MetadataRefreshedAt: now, CreatedAt: now, UpdatedAt: now}
	mock.ExpectQuery("INSERT INTO scm_registered_repositories").WillReturnError(&pgconn.PgError{Code: "23505", ConstraintName: "scm_registered_repositories_connection_id_provider_repository_id_key"})
	_, createErr := repo.Create(context.Background(), registration)
	if createErr != repository.ErrSCMRepositoryRegistrationDuplicate {
		t.Fatalf("expected duplicate error, got %v", createErr)
	}
}

func scmRepositoryRegistrationTestRows(now time.Time) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "connection_id", "provider_repository_id", "owner_name", "repository_name", "full_name", "clone_url", "web_url", "default_branch", "archived", "disabled", "metadata_refreshed_at", "created_at", "updated_at"}).
		AddRow("repo-1", "connection-1", "1001", "octo", "widgets", "octo/widgets", "https://github.com/octo/widgets.git", "https://github.com/octo/widgets", "main", false, false, now, now, now)
}
