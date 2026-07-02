package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func TestNotificationDeliveryRepository_ListRecoverable_ValidationAndQuery(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewNotificationDeliveryRepository(db)
	now := time.Now().UTC()
	row := []string{"id", "build_id", "event_type", "transport", "destination_kind", "destination_key", "notification_target_id", "recipient_user_id", "slack_workspace_integration_id", "recipient", "status", "attempts", "max_attempts", "last_attempt_at", "next_attempt_at", "claimed_at", "claim_expires_at", "claimed_by", "failure_category", "failure_reason", "last_error", "created_at", "updated_at", "sent_at"}

	if _, err := repo.ListRecoverable(context.Background(), repository.NotificationDeliveryRecoverableScanInput{}); err == nil {
		t.Fatal("expected missing scan time error")
	}
	if _, err := repo.ListRecoverable(context.Background(), repository.NotificationDeliveryRecoverableScanInput{Now: now, Limit: 0}); err == nil {
		t.Fatal("expected missing limit error")
	}

	mock.ExpectQuery("WITH recoverable AS").WithArgs(now, 2).WillReturnRows(sqlmock.NewRows(row).
		AddRow("delivery-1", "build-1", "build_failed", "email", "shared_target", "email-target:1", "target-1", nil, nil, "<dev@example.com>", "retry_waiting", 1, 3, now, now, nil, nil, nil, "retryable", "email_send_failed", "smtp unavailable", now, now, nil).
		AddRow("delivery-2", "build-1", "build_failed", "email", "shared_target", "email-target:2", "target-2", nil, nil, "<qa@example.com>", "sending", 2, 3, now, nil, now, now, "worker-a", nil, nil, nil, now, now, nil))

	result, listErr := repo.ListRecoverable(context.Background(), repository.NotificationDeliveryRecoverableScanInput{Now: now, Limit: 2})
	if listErr != nil {
		t.Fatalf("list recoverable failed: %v", listErr)
	}
	if len(result) != 2 || result[0].ID != "delivery-1" || result[1].ID != "delivery-2" {
		t.Fatalf("unexpected recoverable result: %+v", result)
	}

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := repo.ListRecoverable(canceledCtx, repository.NotificationDeliveryRecoverableScanInput{Now: now, Limit: 1}); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled query error, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}
