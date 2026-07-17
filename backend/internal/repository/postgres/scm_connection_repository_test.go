package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func TestSCMConnectionRepository_CreateGitHubAppInstallationConnection(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewSCMConnectionRepository(db)
	now := time.Now().UTC()
	detail := testPostgresGitHubConnectionDetail(now)

	registrationRows := sqlmock.NewRows([]string{"id", "app_id", "display_name", "api_base_url", "web_base_url", "private_key_secret_ref", "webhook_secret_ref", "created_at", "updated_at"}).
		AddRow("registration-1", "12345", nil, "https://api.github.com", "https://github.com", "secret/github/private-key", "secret/github/webhook", now, now)

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, app_id, display_name, api_base_url, web_base_url, private_key_secret_ref, webhook_secret_ref, created_at, updated_at FROM github_app_registrations").
		WithArgs("12345", "https://api.github.com", "https://github.com").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("INSERT INTO github_app_registrations").WillReturnRows(registrationRows)
	mock.ExpectExec("INSERT INTO scm_connections").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO github_app_installations").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	created, createErr := repo.CreateGitHubAppInstallationConnection(context.Background(), detail)
	if createErr != nil {
		t.Fatalf("create failed: %v", createErr)
	}
	if created.GitHubAppRegistration == nil || created.GitHubAppRegistration.ID != "registration-1" {
		t.Fatalf("expected registration to round-trip, got %+v", created.GitHubAppRegistration)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestSCMConnectionRepository_CreateGitHubAppInstallationConnection_ReusedRegistrationConflict(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewSCMConnectionRepository(db)
	now := time.Now().UTC()
	detail := testPostgresGitHubConnectionDetail(now)

	existing := sqlmock.NewRows([]string{"id", "app_id", "display_name", "api_base_url", "web_base_url", "private_key_secret_ref", "webhook_secret_ref", "created_at", "updated_at"}).
		AddRow("registration-1", "12345", nil, "https://api.github.com", "https://github.com", "other/private-key", "secret/github/webhook", now, now)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, app_id, display_name, api_base_url, web_base_url, private_key_secret_ref, webhook_secret_ref, created_at, updated_at FROM github_app_registrations").WillReturnRows(existing)
	mock.ExpectRollback()

	_, createErr := repo.CreateGitHubAppInstallationConnection(context.Background(), detail)
	if createErr != repository.ErrSCMGitHubAppRegistrationConflict {
		t.Fatalf("expected github app registration conflict, got %v", createErr)
	}
}

func TestSCMConnectionRepository_GetListAndSetEnabled(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewSCMConnectionRepository(db)
	now := time.Now().UTC()
	rows := scmConnectionDetailTestRows(now)

	mock.ExpectQuery("SELECT .* FROM scm_connections c").WillReturnRows(rows)
	list, listErr := repo.List(context.Background())
	if listErr != nil {
		t.Fatalf("list failed: %v", listErr)
	}
	if len(list) != 1 {
		t.Fatalf("expected one connection, got %d", len(list))
	}

	mock.ExpectQuery(`SELECT .* FROM scm_connections c.*WHERE c.id = \$1`).WithArgs("connection-1").WillReturnRows(scmConnectionDetailTestRows(now))
	fetched, getErr := repo.GetByID(context.Background(), "connection-1")
	if getErr != nil {
		t.Fatalf("get failed: %v", getErr)
	}
	if fetched.Connection.ID != "connection-1" {
		t.Fatalf("expected fetched connection id, got %q", fetched.Connection.ID)
	}

	mock.ExpectQuery("UPDATE scm_connections").WithArgs("connection-1", false, now.Add(time.Minute)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("connection-1"))
	mock.ExpectQuery(`SELECT .* FROM scm_connections c.*WHERE c.id = \$1`).WithArgs("connection-1").WillReturnRows(scmConnectionDetailTestRows(now))
	updated, updateErr := repo.SetEnabled(context.Background(), "connection-1", false, now.Add(time.Minute))
	if updateErr != nil {
		t.Fatalf("set enabled failed: %v", updateErr)
	}
	if updated.Connection.ID != "connection-1" {
		t.Fatalf("expected updated connection id, got %q", updated.Connection.ID)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func testPostgresGitHubConnectionDetail(now time.Time) domain.SCMConnectionDetail {
	return domain.SCMConnectionDetail{
		Connection:            domain.SCMConnection{ID: "connection-1", Provider: domain.SCMProviderGitHub, DisplayName: "octo connection", DeploymentKind: domain.SCMDeploymentKindCloud, APIBaseURL: "https://api.github.com", WebBaseURL: "https://github.com", Enabled: true, HealthStatus: domain.SCMConnectionHealthStatusUnknown, CreatedAt: now, UpdatedAt: now},
		GitHubAppRegistration: &domain.GitHubAppRegistration{ID: "registration-1", AppID: "12345", APIBaseURL: "https://api.github.com", WebBaseURL: "https://github.com", PrivateKeySecretRef: "secret/github/private-key", WebhookSecretRef: "secret/github/webhook", CreatedAt: now, UpdatedAt: now},
		GitHubAppInstallation: &domain.GitHubAppInstallation{ConnectionID: "connection-1", AppRegistrationID: "registration-1", InstallationID: "999", AccountLogin: "octo", AccountType: "organization", AccountID: "42", CreatedAt: now, UpdatedAt: now},
	}
}

func scmConnectionDetailTestRows(now time.Time) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "provider", "display_name", "deployment_kind", "api_base_url", "web_base_url", "enabled", "health_status", "health_summary", "last_health_checked_at", "created_at", "updated_at", "ga_id", "ga_app_id", "ga_display_name", "ga_api_base_url", "ga_web_base_url", "ga_private_key_secret_ref", "ga_webhook_secret_ref", "ga_created_at", "ga_updated_at", "gi_connection_id", "gi_app_registration_id", "gi_installation_id", "gi_account_login", "gi_account_type", "gi_account_id", "gi_created_at", "gi_updated_at"}).
		AddRow("connection-1", "github", "octo connection", "cloud", "https://api.github.com", "https://github.com", true, "unknown", nil, nil, now, now, "registration-1", "12345", nil, "https://api.github.com", "https://github.com", "secret/github/private-key", "secret/github/webhook", now, now, "connection-1", "registration-1", "999", "octo", "organization", "42", now, now)
}
