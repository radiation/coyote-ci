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
	columns := []string{"user_id", "commit_author_failure_email_enabled", "commit_author_failure_slack_enabled", "commit_author_failure_email_source", "commit_author_success_email_enabled", "commit_author_success_slack_enabled", "commit_author_success_email_source", "created_at", "updated_at"}

	mock.ExpectQuery("SELECT user_id::text, commit_author_failure_email_enabled, commit_author_failure_slack_enabled, commit_author_failure_email_source, commit_author_success_email_enabled, commit_author_success_slack_enabled, commit_author_success_email_source, created_at, updated_at").
		WithArgs("user-1").
		WillReturnRows(sqlmock.NewRows(columns).AddRow("user-1", true, false, string(domain.UserNotificationPreferenceSourceUser), false, false, nil, now, now))

	fetched, getErr := repo.GetByUserID(context.Background(), " user-1 ")
	if getErr != nil {
		t.Fatalf("get preference failed: %v", getErr)
	}
	if fetched.UserID != "user-1" || !fetched.CommitAuthorFailureEmailEnabled || fetched.CommitAuthorFailureEmailSource != domain.UserNotificationPreferenceSourceUser {
		t.Fatalf("unexpected fetched preference %+v", fetched)
	}

	mock.ExpectQuery("SELECT user_id::text, commit_author_failure_email_enabled, commit_author_failure_slack_enabled, commit_author_failure_email_source, commit_author_success_email_enabled, commit_author_success_slack_enabled, commit_author_success_email_source, created_at, updated_at").
		WithArgs("missing").
		WillReturnError(sql.ErrNoRows)

	_, getMissingErr := repo.GetByUserID(context.Background(), "missing")
	if !errors.Is(getMissingErr, repository.ErrUserNotificationPreferenceNotFound) {
		t.Fatalf("expected missing preference error, got %v", getMissingErr)
	}

	mock.ExpectQuery("SELECT user_id::text, commit_author_failure_email_enabled, commit_author_failure_slack_enabled, commit_author_failure_email_source, commit_author_success_email_enabled, commit_author_success_slack_enabled, commit_author_success_email_source, created_at, updated_at").
		WithArgs("broken").
		WillReturnError(errors.New("select failed"))

	_, getRawErr := repo.GetByUserID(context.Background(), "broken")
	if getRawErr == nil || getRawErr.Error() != "select failed" {
		t.Fatalf("expected raw get error, got %v", getRawErr)
	}

	mock.ExpectQuery("INSERT INTO user_notification_preferences").
		WithArgs("user-1", false, false, domain.UserNotificationPreferenceSourceUser, true, false, sql.NullString{String: string(domain.UserNotificationPreferenceSourceUser), Valid: true}, now, now.Add(time.Minute)).
		WillReturnRows(sqlmock.NewRows(columns).AddRow("user-1", false, false, string(domain.UserNotificationPreferenceSourceUser), true, false, string(domain.UserNotificationPreferenceSourceUser), now, now.Add(time.Minute)))

	successSource := domain.UserNotificationPreferenceSourceUser
	updated, upsertErr := repo.Upsert(context.Background(), domain.UserNotificationPreference{
		UserID:                          " user-1 ",
		CommitAuthorFailureEmailEnabled: false,
		CommitAuthorFailureEmailSource:  domain.UserNotificationPreferenceSourceUser,
		CommitAuthorSuccessEmailEnabled: true,
		CommitAuthorSuccessEmailSource:  &successSource,
		CreatedAt:                       now,
		UpdatedAt:                       now.Add(time.Minute),
	})
	if upsertErr != nil {
		t.Fatalf("upsert preference failed: %v", upsertErr)
	}
	if updated.UserID != "user-1" || updated.CommitAuthorFailureEmailEnabled {
		t.Fatalf("unexpected upserted preference %+v", updated)
	}
	if updated.CommitAuthorFailureEmailSource != domain.UserNotificationPreferenceSourceUser {
		t.Fatalf("expected user source, got %+v", updated)
	}
	if !updated.CommitAuthorSuccessEmailEnabled || updated.CommitAuthorSuccessEmailSource == nil || *updated.CommitAuthorSuccessEmailSource != domain.UserNotificationPreferenceSourceUser {
		t.Fatalf("expected success preference to persist, got %+v", updated)
	}

	mock.ExpectQuery("INSERT INTO user_notification_preferences").
		WithArgs("user-2", true, false, domain.UserNotificationPreferenceSourceInstanceDefault, false, false, sql.NullString{}, now, now.Add(2*time.Minute)).
		WillReturnRows(sqlmock.NewRows(columns).AddRow("user-2", true, false, string(domain.UserNotificationPreferenceSourceInstanceDefault), false, false, nil, now, now.Add(2*time.Minute)))

	initialized, created, initializeErr := repo.InitializeIfAbsent(context.Background(), domain.UserNotificationPreference{
		UserID:                          "user-2",
		CommitAuthorFailureEmailEnabled: true,
		CommitAuthorFailureEmailSource:  domain.UserNotificationPreferenceSourceInstanceDefault,
		CreatedAt:                       now,
		UpdatedAt:                       now.Add(2 * time.Minute),
	})
	if initializeErr != nil {
		t.Fatalf("initialize preference failed: %v", initializeErr)
	}
	if !created || initialized.CommitAuthorFailureEmailSource != domain.UserNotificationPreferenceSourceInstanceDefault {
		t.Fatalf("unexpected initialized preference %+v created=%t", initialized, created)
	}

	mock.ExpectQuery("INSERT INTO user_notification_preferences").
		WithArgs("user-3", false, false, domain.UserNotificationPreferenceSourceInstanceDefault, false, false, sql.NullString{}, now, now.Add(3*time.Minute)).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("SELECT user_id::text, commit_author_failure_email_enabled, commit_author_failure_slack_enabled, commit_author_failure_email_source, commit_author_success_email_enabled, commit_author_success_slack_enabled, commit_author_success_email_source, created_at, updated_at").
		WithArgs("user-3").
		WillReturnRows(sqlmock.NewRows(columns).AddRow("user-3", false, false, string(domain.UserNotificationPreferenceSourceUser), true, false, string(domain.UserNotificationPreferenceSourceUser), now, now.Add(4*time.Minute)))

	existing, existingCreated, existingErr := repo.InitializeIfAbsent(context.Background(), domain.UserNotificationPreference{
		UserID:                         "user-3",
		CommitAuthorFailureEmailSource: domain.UserNotificationPreferenceSourceInstanceDefault,
		CreatedAt:                      now,
		UpdatedAt:                      now.Add(3 * time.Minute),
	})
	if existingErr != nil {
		t.Fatalf("initialize existing preference failed: %v", existingErr)
	}
	if existingCreated || existing.CommitAuthorFailureEmailSource != domain.UserNotificationPreferenceSourceUser {
		t.Fatalf("unexpected existing preference %+v created=%t", existing, existingCreated)
	}

	mock.ExpectQuery("INSERT INTO user_notification_preferences").
		WillReturnError(errors.New("initialize failed"))

	_, _, rawInitializeErr := repo.InitializeIfAbsent(context.Background(), domain.UserNotificationPreference{UserID: "user-4"})
	if rawInitializeErr == nil || rawInitializeErr.Error() != "initialize failed" {
		t.Fatalf("expected raw initialize error, got %v", rawInitializeErr)
	}

	mock.ExpectQuery("INSERT INTO user_notification_preferences").
		WithArgs("user-5", true, false, domain.UserNotificationPreferenceSourceInstanceDefault, false, false, sql.NullString{}, now, now.Add(5*time.Minute)).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("SELECT user_id::text, commit_author_failure_email_enabled, commit_author_failure_slack_enabled, commit_author_failure_email_source, commit_author_success_email_enabled, commit_author_success_slack_enabled, commit_author_success_email_source, created_at, updated_at").
		WithArgs("user-5").
		WillReturnError(errors.New("get existing failed"))

	_, _, getExistingErr := repo.InitializeIfAbsent(context.Background(), domain.UserNotificationPreference{
		UserID:                          "user-5",
		CommitAuthorFailureEmailEnabled: true,
		CommitAuthorFailureEmailSource:  domain.UserNotificationPreferenceSourceInstanceDefault,
		CreatedAt:                       now,
		UpdatedAt:                       now.Add(5 * time.Minute),
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
		values: []any{"user-1", true, false, string(domain.UserNotificationPreferenceSourceUser), false, false, nil, now, now.Add(time.Minute)},
	})
	if err != nil {
		t.Fatalf("scan preference failed: %v", err)
	}
	if preference.UserID != "user-1" || !preference.CommitAuthorFailureEmailEnabled || preference.CommitAuthorFailureEmailSource != domain.UserNotificationPreferenceSourceUser {
		t.Fatalf("unexpected scanned preference %+v", preference)
	}

	_, err = scanUserNotificationPreference(notificationTestScanner{err: errors.New("scan failed")})
	if err == nil || err.Error() != "scan failed" {
		t.Fatalf("expected raw scan error, got %v", err)
	}
}
