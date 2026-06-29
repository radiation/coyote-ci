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

func TestNotificationInstanceSettingsRepository_GetAndUpsert(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewNotificationInstanceSettingsRepository(db)
	now := time.Date(2026, 6, 28, 18, 5, 0, 0, time.UTC)
	columns := []string{"default_commit_author_failure_email_enabled", "created_at", "updated_at"}

	mock.ExpectQuery("SELECT default_commit_author_failure_email_enabled, created_at, updated_at").
		WillReturnRows(sqlmock.NewRows(columns).AddRow(true, now, now))

	fetched, getErr := repo.Get(context.Background())
	if getErr != nil {
		t.Fatalf("get settings failed: %v", getErr)
	}
	if !fetched.DefaultCommitAuthorFailureEmailEnabled {
		t.Fatalf("expected enabled settings, got %+v", fetched)
	}

	mock.ExpectQuery("SELECT default_commit_author_failure_email_enabled, created_at, updated_at").
		WillReturnError(sql.ErrNoRows)

	_, missingErr := repo.Get(context.Background())
	if !errors.Is(missingErr, repository.ErrNotificationInstanceSettingsNotFound) {
		t.Fatalf("expected not found, got %v", missingErr)
	}

	mock.ExpectQuery("SELECT default_commit_author_failure_email_enabled, created_at, updated_at").
		WillReturnError(errors.New("select failed"))

	_, rawGetErr := repo.Get(context.Background())
	if rawGetErr == nil || rawGetErr.Error() != "select failed" {
		t.Fatalf("expected raw get error, got %v", rawGetErr)
	}

	updatedAt := now.Add(time.Minute)
	mock.ExpectQuery("INSERT INTO notification_instance_settings").
		WithArgs(false, now, updatedAt).
		WillReturnRows(sqlmock.NewRows(columns).AddRow(false, now, updatedAt))

	updated, upsertErr := repo.Upsert(context.Background(), domain.NotificationInstanceSettings{
		DefaultCommitAuthorFailureEmailEnabled: false,
		CreatedAt:                              now,
		UpdatedAt:                              updatedAt,
	})
	if upsertErr != nil {
		t.Fatalf("upsert settings failed: %v", upsertErr)
	}
	if updated.DefaultCommitAuthorFailureEmailEnabled {
		t.Fatalf("expected disabled settings, got %+v", updated)
	}

	mock.ExpectQuery("INSERT INTO notification_instance_settings").
		WillReturnError(errors.New("upsert failed"))

	_, rawUpsertErr := repo.Upsert(context.Background(), domain.NotificationInstanceSettings{})
	if rawUpsertErr == nil || rawUpsertErr.Error() != "upsert failed" {
		t.Fatalf("expected raw upsert error, got %v", rawUpsertErr)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestScanNotificationInstanceSettings(t *testing.T) {
	now := time.Date(2026, 6, 28, 18, 10, 0, 0, time.UTC)
	settings, err := scanNotificationInstanceSettings(notificationTestScanner{
		values: []any{true, now, now.Add(time.Minute)},
	})
	if err != nil {
		t.Fatalf("scan settings failed: %v", err)
	}
	if !settings.DefaultCommitAuthorFailureEmailEnabled {
		t.Fatalf("expected enabled settings, got %+v", settings)
	}

	_, err = scanNotificationInstanceSettings(notificationTestScanner{err: errors.New("scan failed")})
	if err == nil || err.Error() != "scan failed" {
		t.Fatalf("expected raw scan error, got %v", err)
	}
}
