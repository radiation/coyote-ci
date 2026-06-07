package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

func TestNotificationSubscriptionRepository_ListEnabledMatchesForBuildEvent(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewNotificationSubscriptionRepository(db)
	now := time.Now().UTC()
	rows := sqlmock.NewRows([]string{
		"id", "target_id", "project_id", "job_id", "event_type", "enabled", "created_at", "updated_at",
		"id", "type", "name", "recipient", "enabled", "created_at", "updated_at",
	}).AddRow(
		"subscription-1", "target-1", "project-1", nil, "build_failed", true, now, now,
		"target-1", "email", "Dev Mailbox", "<dev@example.com>", true, now, now,
	)

	jobID := "job-1"
	mock.ExpectQuery("SELECT .* FROM notification_subscriptions s").WillReturnRows(rows)
	matches, err := repo.ListEnabledMatchesForBuildEvent(context.Background(), domain.Build{ID: "build-1", ProjectID: "project-1", JobID: &jobID}, domain.NotificationEventTypeBuildFailed)
	if err != nil {
		t.Fatalf("list matches failed: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected one match, got %d", len(matches))
	}
	if matches[0].Target.Recipient != "<dev@example.com>" {
		t.Fatalf("unexpected recipient %q", matches[0].Target.Recipient)
	}
	if matches[0].Subscription.ProjectID == nil || *matches[0].Subscription.ProjectID != "project-1" {
		t.Fatalf("unexpected project scope %+v", matches[0].Subscription)
	}

	mock.ExpectQuery("SELECT .* FROM notification_subscriptions s").WillReturnError(errors.New("query failed"))
	_, err = repo.ListEnabledMatchesForBuildEvent(context.Background(), domain.Build{ProjectID: "project-1"}, domain.NotificationEventTypeBuildFailed)
	if err == nil || err.Error() != "query failed" {
		t.Fatalf("expected raw query error, got %v", err)
	}

	mock.ExpectQuery("SELECT .* FROM notification_subscriptions s").WillReturnRows(sqlmock.NewRows([]string{
		"id", "target_id", "project_id", "job_id", "event_type", "enabled", "created_at", "updated_at",
		"id", "type", "name", "recipient", "enabled", "created_at", "updated_at",
	}))
	matches, err = repo.ListEnabledMatchesForBuildEvent(context.Background(), domain.Build{ProjectID: "project-1"}, domain.NotificationEventTypeBuildSucceeded)
	if err != nil {
		t.Fatalf("empty list failed: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("expected empty matches, got %d", len(matches))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}
