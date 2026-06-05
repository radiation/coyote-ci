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

func TestNotificationDeliveryRepository_CreateGetAndUpdate(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}

	repo := NewNotificationDeliveryRepository(db)
	now := time.Now().UTC()
	row := []string{"id", "build_id", "event_type", "recipient", "status", "attempts", "last_error", "created_at", "updated_at", "sent_at"}

	mock.ExpectQuery("INSERT INTO notification_deliveries").WillReturnRows(sqlmock.NewRows(row).AddRow(
		"delivery-1", "build-1", "build_failed", "<dev@example.com>", "pending", 0, nil, now, now, nil,
	))

	created, err := repo.Create(context.Background(), domain.NotificationDelivery{
		ID:        "delivery-1",
		BuildID:   "build-1",
		EventType: domain.NotificationEventTypeBuildFailed,
		Recipient: "<dev@example.com>",
		Status:    domain.NotificationDeliveryStatusPending,
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if created.Status != domain.NotificationDeliveryStatusPending {
		t.Fatalf("expected pending status, got %q", created.Status)
	}

	mock.ExpectQuery("INSERT INTO notification_deliveries").WillReturnError(errors.New("duplicate key value violates unique constraint notification_deliveries_build_event_recipient_key"))
	_, err = repo.Create(context.Background(), domain.NotificationDelivery{
		BuildID:   "build-1",
		EventType: domain.NotificationEventTypeBuildFailed,
		Recipient: "<dev@example.com>",
		Status:    domain.NotificationDeliveryStatusPending,
	})
	if !errors.Is(err, repository.ErrNotificationDeliveryDuplicate) {
		t.Fatalf("expected ErrNotificationDeliveryDuplicate, got %v", err)
	}

	mock.ExpectQuery("SELECT id, build_id, event_type, recipient, status, attempts, last_error, created_at, updated_at, sent_at").WillReturnRows(sqlmock.NewRows(row).AddRow(
		"delivery-1", "build-1", "build_failed", "<dev@example.com>", "pending", 0, nil, now, now, nil,
	))

	fetched, err := repo.GetByBuildEventRecipient(context.Background(), "build-1", domain.NotificationEventTypeBuildFailed, "<dev@example.com>")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if fetched.ID != "delivery-1" {
		t.Fatalf("unexpected id: %s", fetched.ID)
	}

	mock.ExpectQuery("UPDATE notification_deliveries").WillReturnRows(sqlmock.NewRows(row).AddRow(
		"delivery-1", "build-1", "build_failed", "<dev@example.com>", "failed", 1, "smtp unavailable", now, now, nil,
	))

	lastError := "smtp unavailable"
	updated, err := repo.Update(context.Background(), domain.NotificationDelivery{
		ID:        "delivery-1",
		BuildID:   "build-1",
		EventType: domain.NotificationEventTypeBuildFailed,
		Recipient: "<dev@example.com>",
		Status:    domain.NotificationDeliveryStatusFailed,
		Attempts:  1,
		LastError: &lastError,
		UpdatedAt: now,
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
