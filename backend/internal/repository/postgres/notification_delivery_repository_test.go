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

func TestNotificationDeliveryRepository_CreateGetAndUpdate(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewNotificationDeliveryRepository(db)
	now := time.Now().UTC()
	row := []string{"id", "build_id", "event_type", "transport", "destination_kind", "destination_key", "notification_target_id", "recipient_user_id", "slack_workspace_integration_id", "recipient", "status", "attempts", "last_error", "created_at", "updated_at", "sent_at"}

	mock.ExpectQuery("INSERT INTO notification_deliveries").WillReturnRows(sqlmock.NewRows(row).AddRow(
		"delivery-1", "build-1", "build_failed", "email", "shared_target", "email-target:target-1", "target-1", nil, nil, "<dev@example.com>", "pending", 0, nil, now, now, nil,
	))

	created, err := repo.Create(context.Background(), domain.NotificationDelivery{
		ID:                   "delivery-1",
		BuildID:              "build-1",
		EventType:            domain.NotificationEventTypeBuildFailed,
		Transport:            domain.NotificationTransportEmail,
		DestinationKind:      domain.NotificationDestinationKindSharedTarget,
		DestinationKey:       "email-target:target-1",
		NotificationTargetID: strPtr("target-1"),
		Recipient:            "<dev@example.com>",
		Status:               domain.NotificationDeliveryStatusPending,
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if created.Status != domain.NotificationDeliveryStatusPending {
		t.Fatalf("expected pending status, got %q", created.Status)
	}

	mock.ExpectQuery("INSERT INTO notification_deliveries").WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("SELECT id, build_id, event_type, transport, destination_kind, destination_key, notification_target_id::text, recipient_user_id::text, slack_workspace_integration_id::text, recipient, status, attempts, last_error, created_at, updated_at, sent_at").WillReturnRows(sqlmock.NewRows(row).AddRow(
		"delivery-1", "build-1", "build_failed", "email", "shared_target", "email-target:target-1", "target-1", nil, nil, "<dev@example.com>", "pending", 0, nil, now, now, nil,
	))
	_, err = repo.Create(context.Background(), domain.NotificationDelivery{
		BuildID:              "build-1",
		EventType:            domain.NotificationEventTypeBuildFailed,
		Transport:            domain.NotificationTransportEmail,
		DestinationKind:      domain.NotificationDestinationKindSharedTarget,
		DestinationKey:       "email-target:target-1",
		NotificationTargetID: strPtr("target-1"),
		Recipient:            "<dev@example.com>",
		Status:               domain.NotificationDeliveryStatusPending,
	})
	if !errors.Is(err, repository.ErrNotificationDeliveryDuplicate) {
		t.Fatalf("expected ErrNotificationDeliveryDuplicate, got %v", err)
	}

	legacyRow := []string{"id", "build_id", "event_type", "transport", "destination_kind", "destination_key", "notification_target_id", "recipient_user_id", "slack_workspace_integration_id", "recipient", "status", "attempts", "last_error", "created_at", "updated_at", "sent_at"}
	mock.ExpectQuery("SELECT id, build_id, event_type, transport, destination_kind, destination_key, notification_target_id::text, recipient_user_id::text, slack_workspace_integration_id::text, recipient, status, attempts, last_error, created_at, updated_at, sent_at").WillReturnRows(sqlmock.NewRows(legacyRow).AddRow(
		"delivery-1", "build-1", "build_failed", "email", "shared_target", "email-target:target-1", "target-1", nil, nil, "<dev@example.com>", "pending", 0, nil, now, now, nil,
	))

	fetched, err := repo.GetByBuildEventRecipient(context.Background(), "build-1", domain.NotificationEventTypeBuildFailed, "<dev@example.com>")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if fetched.ID != "delivery-1" {
		t.Fatalf("unexpected id: %s", fetched.ID)
	}

	mock.ExpectQuery("UPDATE notification_deliveries").WillReturnRows(sqlmock.NewRows(legacyRow).AddRow(
		"delivery-1", "build-1", "build_failed", "email", "shared_target", "email-target:target-1", "target-1", nil, nil, "<dev@example.com>", "failed", 1, "smtp unavailable", now, now, nil,
	))

	lastError := "smtp unavailable"
	updated, err := repo.Update(context.Background(), domain.NotificationDelivery{
		ID:                   "delivery-1",
		BuildID:              "build-1",
		EventType:            domain.NotificationEventTypeBuildFailed,
		Transport:            domain.NotificationTransportEmail,
		DestinationKind:      domain.NotificationDestinationKindSharedTarget,
		DestinationKey:       "email-target:target-1",
		NotificationTargetID: strPtr("target-1"),
		Recipient:            "<dev@example.com>",
		Status:               domain.NotificationDeliveryStatusFailed,
		Attempts:             1,
		LastError:            &lastError,
		UpdatedAt:            now,
	})
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}
	if updated.Status != domain.NotificationDeliveryStatusFailed {
		t.Fatalf("expected failed status, got %q", updated.Status)
	}
	if updated.LastError == nil || *updated.LastError != lastError {
		t.Fatalf("expected last_error %q, got %v", lastError, updated.LastError)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestNotificationDeliveryRepository_ErrorCasesAndSentAtScan(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewNotificationDeliveryRepository(db)
	now := time.Now().UTC()
	legacyRow := []string{"id", "build_id", "event_type", "transport", "destination_kind", "destination_key", "notification_target_id", "recipient_user_id", "slack_workspace_integration_id", "recipient", "status", "attempts", "last_error", "created_at", "updated_at", "sent_at"}

	mock.ExpectQuery("INSERT INTO notification_deliveries").WillReturnError(errors.New("insert failed"))
	_, err = repo.Create(context.Background(), domain.NotificationDelivery{
		BuildID:         "build-1",
		EventType:       domain.NotificationEventTypeBuildFailed,
		Transport:       domain.NotificationTransportEmail,
		DestinationKind: domain.NotificationDestinationKindSharedTarget,
		DestinationKey:  "email-target:target-1",
		Recipient:       "<dev@example.com>",
	})
	if err == nil || err.Error() != "insert failed" {
		t.Fatalf("expected raw create error, got %v", err)
	}

	mock.ExpectQuery("INSERT INTO notification_deliveries").WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("SELECT id, build_id, event_type, transport, destination_kind, destination_key, notification_target_id::text, recipient_user_id::text, slack_workspace_integration_id::text, recipient, status, attempts, last_error, created_at, updated_at, sent_at").WillReturnError(errors.New("select after conflict failed"))
	_, err = repo.Acquire(context.Background(), domain.NotificationDelivery{
		BuildID:         "build-1",
		EventType:       domain.NotificationEventTypeBuildFailed,
		Transport:       domain.NotificationTransportEmail,
		DestinationKind: domain.NotificationDestinationKindSharedTarget,
		DestinationKey:  "email-target:target-1",
		Recipient:       "<dev@example.com>",
	})
	if err == nil || err.Error() != "select after conflict failed" {
		t.Fatalf("expected post-conflict select error, got %v", err)
	}

	mock.ExpectQuery("SELECT id, build_id, event_type, transport, destination_kind, destination_key, notification_target_id::text, recipient_user_id::text, slack_workspace_integration_id::text, recipient, status, attempts, last_error, created_at, updated_at, sent_at").WillReturnError(sql.ErrNoRows)
	_, err = repo.GetByBuildEventRecipient(context.Background(), "build-1", domain.NotificationEventTypeBuildFailed, "<dev@example.com>")
	if !errors.Is(err, repository.ErrNotificationDeliveryNotFound) {
		t.Fatalf("expected not found get error, got %v", err)
	}

	mock.ExpectQuery("SELECT id, build_id, event_type, transport, destination_kind, destination_key, notification_target_id::text, recipient_user_id::text, slack_workspace_integration_id::text, recipient, status, attempts, last_error, created_at, updated_at, sent_at").WillReturnError(errors.New("select failed"))
	_, err = repo.GetByBuildEventRecipient(context.Background(), "build-1", domain.NotificationEventTypeBuildFailed, "<dev@example.com>")
	if err == nil || err.Error() != "select failed" {
		t.Fatalf("expected raw get error, got %v", err)
	}

	mock.ExpectQuery("SELECT id, build_id, event_type, transport, destination_kind, destination_key, notification_target_id::text, recipient_user_id::text, slack_workspace_integration_id::text, recipient, status, attempts, last_error, created_at, updated_at, sent_at").WillReturnRows(sqlmock.NewRows(legacyRow).AddRow(
		"delivery-1", "build-1", "build_succeeded", "email", "shared_target", "email-target:target-1", "target-1", nil, nil, "<dev@example.com>", "sent", 1, nil, now, now, now,
	))
	fetched, err := repo.GetByBuildEventRecipient(context.Background(), "build-1", domain.NotificationEventTypeBuildSucceeded, "<dev@example.com>")
	if err != nil {
		t.Fatalf("get with sent_at failed: %v", err)
	}
	if fetched.SentAt == nil || fetched.SentAt.IsZero() {
		t.Fatalf("expected sent_at to be scanned, got %v", fetched.SentAt)
	}

	mock.ExpectQuery("UPDATE notification_deliveries").WillReturnError(sql.ErrNoRows)
	_, err = repo.Update(context.Background(), domain.NotificationDelivery{ID: "missing"})
	if !errors.Is(err, repository.ErrNotificationDeliveryNotFound) {
		t.Fatalf("expected not found update error, got %v", err)
	}

	mock.ExpectQuery("UPDATE notification_deliveries").WillReturnError(errors.New("update failed"))
	_, err = repo.Update(context.Background(), domain.NotificationDelivery{ID: "delivery-1"})
	if err == nil || err.Error() != "update failed" {
		t.Fatalf("expected raw update error, got %v", err)
	}

	if nullableTimestamp(nil) != nil {
		t.Fatal("expected nil timestamp to stay nil")
	}
	zero := time.Time{}
	if nullableTimestamp(&zero) != nil {
		t.Fatal("expected zero timestamp pointer to become nil")
	}
	if nullableTimestamp(&now) == nil {
		t.Fatal("expected non-zero timestamp pointer to stay non-nil")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}
