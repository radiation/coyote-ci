package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func TestUserNotificationPreferenceRepository_GetByUserIDAndUpsert(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewUserNotificationPreferenceRepository(db)
	now := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)
	columns := []string{"user_id", "commit_author_failure_enabled", "created_at", "updated_at"}

	mock.ExpectQuery("SELECT user_id::text, commit_author_failure_enabled, created_at, updated_at").
		WithArgs("user-1").
		WillReturnRows(sqlmock.NewRows(columns).AddRow("user-1", true, now, now))

	fetched, getErr := repo.GetByUserID(context.Background(), " user-1 ")
	if getErr != nil {
		t.Fatalf("get preference failed: %v", getErr)
	}
	if fetched.UserID != "user-1" || !fetched.CommitAuthorFailureEnabled {
		t.Fatalf("unexpected fetched preference %+v", fetched)
	}

	mock.ExpectQuery("SELECT user_id::text, commit_author_failure_enabled, created_at, updated_at").
		WithArgs("missing").
		WillReturnError(sql.ErrNoRows)

	_, getMissingErr := repo.GetByUserID(context.Background(), "missing")
	if !errors.Is(getMissingErr, repository.ErrUserNotificationPreferenceNotFound) {
		t.Fatalf("expected missing preference error, got %v", getMissingErr)
	}

	mock.ExpectQuery("SELECT user_id::text, commit_author_failure_enabled, created_at, updated_at").
		WithArgs("broken").
		WillReturnError(errors.New("select failed"))

	_, getRawErr := repo.GetByUserID(context.Background(), "broken")
	if getRawErr == nil || getRawErr.Error() != "select failed" {
		t.Fatalf("expected raw get error, got %v", getRawErr)
	}

	mock.ExpectQuery("INSERT INTO user_notification_preferences").
		WithArgs("user-1", false, now, now.Add(time.Minute)).
		WillReturnRows(sqlmock.NewRows(columns).AddRow("user-1", false, now, now.Add(time.Minute)))

	updated, upsertErr := repo.Upsert(context.Background(), domain.UserNotificationPreference{
		UserID:                     " user-1 ",
		CommitAuthorFailureEnabled: false,
		CreatedAt:                  now,
		UpdatedAt:                  now.Add(time.Minute),
	})
	if upsertErr != nil {
		t.Fatalf("upsert preference failed: %v", upsertErr)
	}
	if updated.UserID != "user-1" || updated.CommitAuthorFailureEnabled {
		t.Fatalf("unexpected upserted preference %+v", updated)
	}

	mock.ExpectQuery("INSERT INTO user_notification_preferences").
		WillReturnError(errors.New("upsert failed"))

	_, rawUpsertErr := repo.Upsert(context.Background(), domain.UserNotificationPreference{UserID: "user-2"})
	if rawUpsertErr == nil || rawUpsertErr.Error() != "upsert failed" {
		t.Fatalf("expected raw upsert error, got %v", rawUpsertErr)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestScanUserNotificationPreference(t *testing.T) {
	now := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)
	preference, err := scanUserNotificationPreference(notificationTestScanner{
		values: []any{"user-1", true, now, now.Add(time.Minute)},
	})
	if err != nil {
		t.Fatalf("scan preference failed: %v", err)
	}
	if preference.UserID != "user-1" || !preference.CommitAuthorFailureEnabled {
		t.Fatalf("unexpected scanned preference %+v", preference)
	}

	_, err = scanUserNotificationPreference(notificationTestScanner{err: errors.New("scan failed")})
	if err == nil || err.Error() != "scan failed" {
		t.Fatalf("expected raw scan error, got %v", err)
	}
}
