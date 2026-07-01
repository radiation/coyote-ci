package postgres

import (
	"context"
	"errors"
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
