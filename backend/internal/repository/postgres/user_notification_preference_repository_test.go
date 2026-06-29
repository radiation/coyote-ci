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
	columns := []string{"user_id", "commit_author_failure_enabled", "source", "commit_author_success_enabled", "commit_author_success_source", "created_at", "updated_at"}

	mock.ExpectQuery("SELECT user_id::text, commit_author_failure_enabled, source, commit_author_success_enabled, commit_author_success_source, created_at, updated_at").
		WithArgs("user-1").
		WillReturnRows(sqlmock.NewRows(columns).AddRow("user-1", true, string(domain.UserNotificationPreferenceSourceUser), false, nil, now, now))

	fetched, getErr := repo.GetByUserID(context.Background(), " user-1 ")
	if getErr != nil {
		t.Fatalf("get preference failed: %v", getErr)
	}
	if fetched.UserID != "user-1" || !fetched.CommitAuthorFailureEnabled || fetched.Source != domain.UserNotificationPreferenceSourceUser {
		t.Fatalf("unexpected fetched preference %+v", fetched)
	}

	mock.ExpectQuery("SELECT user_id::text, commit_author_failure_enabled, source, commit_author_success_enabled, commit_author_success_source, created_at, updated_at").
		WithArgs("missing").
		WillReturnError(sql.ErrNoRows)

	_, getMissingErr := repo.GetByUserID(context.Background(), "missing")
	if !errors.Is(getMissingErr, repository.ErrUserNotificationPreferenceNotFound) {
		t.Fatalf("expected missing preference error, got %v", getMissingErr)
	}

	mock.ExpectQuery("SELECT user_id::text, commit_author_failure_enabled, source, commit_author_success_enabled, commit_author_success_source, created_at, updated_at").
		WithArgs("broken").
		WillReturnError(errors.New("select failed"))

	_, getRawErr := repo.GetByUserID(context.Background(), "broken")
	if getRawErr == nil || getRawErr.Error() != "select failed" {
		t.Fatalf("expected raw get error, got %v", getRawErr)
	}

	mock.ExpectQuery("INSERT INTO user_notification_preferences").
		WithArgs("user-1", false, domain.UserNotificationPreferenceSourceUser, true, sql.NullString{String: string(domain.UserNotificationPreferenceSourceUser), Valid: true}, now, now.Add(time.Minute)).
		WillReturnRows(sqlmock.NewRows(columns).AddRow("user-1", false, string(domain.UserNotificationPreferenceSourceUser), true, string(domain.UserNotificationPreferenceSourceUser), now, now.Add(time.Minute)))

	successSource := domain.UserNotificationPreferenceSourceUser
	updated, upsertErr := repo.Upsert(context.Background(), domain.UserNotificationPreference{
		UserID:                     " user-1 ",
		CommitAuthorFailureEnabled: false,
		Source:                     domain.UserNotificationPreferenceSourceUser,
		CommitAuthorSuccessEnabled: true,
		CommitAuthorSuccessSource:  &successSource,
		CreatedAt:                  now,
		UpdatedAt:                  now.Add(time.Minute),
	})
	if upsertErr != nil {
		t.Fatalf("upsert preference failed: %v", upsertErr)
	}
	if updated.UserID != "user-1" || updated.CommitAuthorFailureEnabled {
		t.Fatalf("unexpected upserted preference %+v", updated)
	}
	if updated.Source != domain.UserNotificationPreferenceSourceUser {
		t.Fatalf("expected user source, got %+v", updated)
	}
	if !updated.CommitAuthorSuccessEnabled || updated.CommitAuthorSuccessSource == nil || *updated.CommitAuthorSuccessSource != domain.UserNotificationPreferenceSourceUser {
		t.Fatalf("expected success preference to persist, got %+v", updated)
	}

	mock.ExpectQuery("INSERT INTO user_notification_preferences").
		WithArgs("user-2", true, domain.UserNotificationPreferenceSourceInstanceDefault, false, sql.NullString{}, now, now.Add(2*time.Minute)).
		WillReturnRows(sqlmock.NewRows(columns).AddRow("user-2", true, string(domain.UserNotificationPreferenceSourceInstanceDefault), false, nil, now, now.Add(2*time.Minute)))

	initialized, created, initializeErr := repo.InitializeIfAbsent(context.Background(), domain.UserNotificationPreference{
		UserID:                     "user-2",
		CommitAuthorFailureEnabled: true,
		Source:                     domain.UserNotificationPreferenceSourceInstanceDefault,
		CreatedAt:                  now,
		UpdatedAt:                  now.Add(2 * time.Minute),
	})
	if initializeErr != nil {
		t.Fatalf("initialize preference failed: %v", initializeErr)
	}
	if !created || initialized.Source != domain.UserNotificationPreferenceSourceInstanceDefault {
		t.Fatalf("unexpected initialized preference %+v created=%t", initialized, created)
	}

	mock.ExpectQuery("INSERT INTO user_notification_preferences").
		WithArgs("user-3", false, domain.UserNotificationPreferenceSourceInstanceDefault, false, sql.NullString{}, now, now.Add(3*time.Minute)).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("SELECT user_id::text, commit_author_failure_enabled, source, commit_author_success_enabled, commit_author_success_source, created_at, updated_at").
		WithArgs("user-3").
		WillReturnRows(sqlmock.NewRows(columns).AddRow("user-3", false, string(domain.UserNotificationPreferenceSourceUser), true, string(domain.UserNotificationPreferenceSourceUser), now, now.Add(4*time.Minute)))

	existing, existingCreated, existingErr := repo.InitializeIfAbsent(context.Background(), domain.UserNotificationPreference{
		UserID:                     "user-3",
		CommitAuthorFailureEnabled: false,
		Source:                     domain.UserNotificationPreferenceSourceInstanceDefault,
		CreatedAt:                  now,
		UpdatedAt:                  now.Add(3 * time.Minute),
	})
	if existingErr != nil {
		t.Fatalf("initialize existing preference failed: %v", existingErr)
	}
	if existingCreated || existing.Source != domain.UserNotificationPreferenceSourceUser {
		t.Fatalf("unexpected existing preference %+v created=%t", existing, existingCreated)
	}

	mock.ExpectQuery("INSERT INTO user_notification_preferences").
		WillReturnError(errors.New("initialize failed"))

	_, _, rawInitializeErr := repo.InitializeIfAbsent(context.Background(), domain.UserNotificationPreference{UserID: "user-4"})
	if rawInitializeErr == nil || rawInitializeErr.Error() != "initialize failed" {
		t.Fatalf("expected raw initialize error, got %v", rawInitializeErr)
	}

	mock.ExpectQuery("INSERT INTO user_notification_preferences").
		WithArgs("user-5", true, domain.UserNotificationPreferenceSourceInstanceDefault, false, sql.NullString{}, now, now.Add(5*time.Minute)).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("SELECT user_id::text, commit_author_failure_enabled, source, commit_author_success_enabled, commit_author_success_source, created_at, updated_at").
		WithArgs("user-5").
		WillReturnError(errors.New("get existing failed"))

	_, _, getExistingErr := repo.InitializeIfAbsent(context.Background(), domain.UserNotificationPreference{
		UserID:                     "user-5",
		CommitAuthorFailureEnabled: true,
		Source:                     domain.UserNotificationPreferenceSourceInstanceDefault,
		CreatedAt:                  now,
		UpdatedAt:                  now.Add(5 * time.Minute),
	})
	if getExistingErr == nil || getExistingErr.Error() != "get existing failed" {
		t.Fatalf("expected existing get error, got %v", getExistingErr)
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
		values: []any{"user-1", true, string(domain.UserNotificationPreferenceSourceUser), false, nil, now, now.Add(time.Minute)},
	})
	if err != nil {
		t.Fatalf("scan preference failed: %v", err)
	}
	if preference.UserID != "user-1" || !preference.CommitAuthorFailureEnabled || preference.Source != domain.UserNotificationPreferenceSourceUser {
		t.Fatalf("unexpected scanned preference %+v", preference)
	}

	_, err = scanUserNotificationPreference(notificationTestScanner{err: errors.New("scan failed")})
	if err == nil || err.Error() != "scan failed" {
		t.Fatalf("expected raw scan error, got %v", err)
	}
}
