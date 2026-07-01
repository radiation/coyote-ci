package postgres

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func TestUserSlackIdentityRepository_UpsertMapsUniqueConstraintConflict(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewUserSlackIdentityRepository(db)
	now := time.Date(2026, 7, 1, 16, 10, 0, 0, time.UTC)
	mock.ExpectQuery("INSERT INTO user_slack_identities").
		WillReturnError(errors.New("duplicate key value violates unique constraint user_slack_identities_workspace_user_key"))

	_, err = repo.Upsert(context.Background(), domain.UserSlackIdentity{
		ID:                          "identity-1",
		UserID:                      "user-1",
		SlackWorkspaceIntegrationID: "workspace-1",
		SlackUserID:                 "U123",
		Enabled:                     true,
		LinkedAt:                    now,
		CreatedAt:                   now,
		UpdatedAt:                   now,
	})
	if !errors.Is(err, repository.ErrUserSlackIdentityConflict) {
		t.Fatalf("expected privacy-safe conflict, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestUserSlackIdentityRepository_CountByWorkspaceIntegrationID(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewUserSlackIdentityRepository(db)
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM user_slack_identities`).
		WithArgs("workspace-1").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))

	count, err := repo.CountByWorkspaceIntegrationID(context.Background(), "workspace-1")
	if err != nil {
		t.Fatalf("count linked identities: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected count 2, got %d", count)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestUserSlackIdentityRepository_GetSetDeleteAndScan(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewUserSlackIdentityRepository(db)
	now := time.Date(2026, 7, 1, 16, 20, 0, 0, time.UTC)
	verifiedAt := now.Add(time.Minute)
	columns := []string{"id", "user_id", "slack_workspace_integration_id", "slack_user_id", "slack_display_name", "slack_real_name", "slack_handle", "slack_email", "profile_image_url", "enabled", "linked_at", "last_verified_at", "created_at", "updated_at"}
	row := sqlmock.NewRows(columns).AddRow(
		"identity-1",
		" user-1 ",
		" workspace-1 ",
		" U123 ",
		" Bryan ",
		" Bryan Choate ",
		" bryan ",
		" bryan@example.com ",
		" https://images.example/avatar.png ",
		true,
		now,
		verifiedAt,
		now,
		now,
	)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT " + userSlackIdentityColumns + "\n\t\tFROM user_slack_identities\n\t\tWHERE user_id = $1\n\t")).
		WithArgs("user-1").
		WillReturnRows(row)

	identity, err := repo.GetByUserID(context.Background(), " user-1 ")
	if err != nil {
		t.Fatalf("get by user id: %v", err)
	}
	if identity.UserID != "user-1" || identity.SlackWorkspaceIntegrationID != "workspace-1" || identity.SlackUserID != "U123" {
		t.Fatalf("expected trimmed identity values, got %+v", identity)
	}
	if identity.SlackDisplayName == nil || *identity.SlackDisplayName != "Bryan" {
		t.Fatalf("expected display name to be trimmed, got %+v", identity.SlackDisplayName)
	}
	if identity.LastVerifiedAt == nil || !identity.LastVerifiedAt.Equal(verifiedAt) {
		t.Fatalf("expected last verified at %s, got %+v", verifiedAt, identity.LastVerifiedAt)
	}

	mock.ExpectQuery("UPDATE user_slack_identities").
		WithArgs("user-1", false, now.Add(2*time.Minute)).
		WillReturnRows(sqlmock.NewRows(columns).AddRow(
			"identity-1", "user-1", "workspace-1", "U123", nil, nil, nil, nil, nil, false, now, nil, now, now.Add(2*time.Minute),
		))

	updated, err := repo.SetEnabled(context.Background(), "user-1", false, now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("set enabled: %v", err)
	}
	if updated.Enabled {
		t.Fatalf("expected disabled identity, got %+v", updated)
	}

	mock.ExpectExec(`DELETE FROM user_slack_identities WHERE user_id = \$1`).
		WithArgs("user-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := repo.DeleteByUserID(context.Background(), "user-1"); err != nil {
		t.Fatalf("delete identity: %v", err)
	}

	mock.ExpectQuery(regexp.QuoteMeta("SELECT " + userSlackIdentityColumns + "\n\t\tFROM user_slack_identities\n\t\tWHERE user_id = $1\n\t")).
		WithArgs("missing-user").
		WillReturnError(sql.ErrNoRows)
	if _, err := repo.GetByUserID(context.Background(), "missing-user"); !errors.Is(err, repository.ErrUserSlackIdentityNotFound) {
		t.Fatalf("expected get not found, got %v", err)
	}

	mock.ExpectQuery("UPDATE user_slack_identities").
		WithArgs("missing-user", true, now.Add(3*time.Minute)).
		WillReturnError(sql.ErrNoRows)
	if _, err := repo.SetEnabled(context.Background(), "missing-user", true, now.Add(3*time.Minute)); !errors.Is(err, repository.ErrUserSlackIdentityNotFound) {
		t.Fatalf("expected set enabled not found, got %v", err)
	}

	mock.ExpectExec(`DELETE FROM user_slack_identities WHERE user_id = \$1`).
		WithArgs("missing-user").
		WillReturnResult(sqlmock.NewResult(0, 0))
	if err := repo.DeleteByUserID(context.Background(), "missing-user"); !errors.Is(err, repository.ErrUserSlackIdentityNotFound) {
		t.Fatalf("expected delete not found, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}
