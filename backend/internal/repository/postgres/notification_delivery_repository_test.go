package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func TestNotificationDeliveryRepository_AcquireForDelivery_CreateAndMarkSent(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewNotificationDeliveryRepository(db)
	now := time.Now().UTC()
	claimExpiresAt := now.Add(2 * time.Minute)
	row := []string{"id", "build_id", "event_type", "transport", "destination_kind", "destination_key", "notification_target_id", "recipient_user_id", "slack_workspace_integration_id", "recipient", "status", "attempts", "max_attempts", "last_attempt_at", "next_attempt_at", "claimed_at", "claim_expires_at", "claimed_by", "failure_category", "failure_reason", "last_error", "created_at", "updated_at", "sent_at"}

	mock.ExpectBegin()
	mock.ExpectQuery("INSERT INTO notification_deliveries").WillReturnRows(sqlmock.NewRows(row).AddRow(
		"delivery-1", "build-1", "build_failed", "email", "shared_target", "email-target:target-1", "target-1", nil, nil, "<dev@example.com>", "sending", 1, 3, now, nil, now, claimExpiresAt, "worker-a", nil, nil, nil, now, now, nil,
	))
	mock.ExpectCommit()

	claimed, claimErr := repo.AcquireForDelivery(context.Background(), repository.NotificationDeliveryClaimInput{
		Delivery: domain.NotificationDelivery{
			BuildID:              "build-1",
			EventType:            domain.NotificationEventTypeBuildFailed,
			Transport:            domain.NotificationTransportEmail,
			DestinationKind:      domain.NotificationDestinationKindSharedTarget,
			DestinationKey:       "email-target:target-1",
			NotificationTargetID: strPtr("target-1"),
			Recipient:            "<dev@example.com>",
		},
		ClaimOwner:    "worker-a",
		Now:           now,
		ClaimDuration: 2 * time.Minute,
		MaxAttempts:   3,
	})
	if claimErr != nil {
		t.Fatalf("acquire for delivery failed: %v", claimErr)
	}
	if claimed.Outcome != repository.NotificationDeliveryClaimOutcomeCreatedClaimed {
		t.Fatalf("expected created_claimed outcome, got %q", claimed.Outcome)
	}

	mock.ExpectQuery("UPDATE notification_deliveries").WillReturnRows(sqlmock.NewRows(row).AddRow(
		"delivery-1", "build-1", "build_failed", "email", "shared_target", "email-target:target-1", "target-1", nil, nil, "<dev@example.com>", "sent", 1, 3, now, nil, nil, nil, nil, nil, nil, nil, now, now, now,
	))

	updated, updateErr := repo.MarkSent(context.Background(), repository.NotificationDeliveryMarkSentInput{
		DeliveryID: "delivery-1",
		ClaimOwner: "worker-a",
		ClaimedAt:  now,
		SentAt:     now,
	})
	if updateErr != nil {
		t.Fatalf("mark sent failed: %v", updateErr)
	}
	if updated.Outcome != repository.NotificationDeliveryUpdateOutcomeUpdated {
		t.Fatalf("expected updated outcome, got %q", updated.Outcome)
	}
	if updated.Delivery.Status != domain.NotificationDeliveryStatusSent {
		t.Fatalf("expected sent status, got %q", updated.Delivery.Status)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestNotificationDeliveryRepository_AcquireForDelivery_ExistingOutcomes(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewNotificationDeliveryRepository(db)
	now := time.Now().UTC()
	row := []string{"id", "build_id", "event_type", "transport", "destination_kind", "destination_key", "notification_target_id", "recipient_user_id", "slack_workspace_integration_id", "recipient", "status", "attempts", "max_attempts", "last_attempt_at", "next_attempt_at", "claimed_at", "claim_expires_at", "claimed_by", "failure_category", "failure_reason", "last_error", "created_at", "updated_at", "sent_at"}

	t.Run("already sent", func(t *testing.T) {
		mock.ExpectBegin()
		mock.ExpectQuery("INSERT INTO notification_deliveries").WillReturnError(sql.ErrNoRows)
		mock.ExpectQuery("SELECT .* FROM notification_deliveries").WillReturnRows(sqlmock.NewRows(row).AddRow(
			"delivery-1", "build-1", "build_failed", "email", "shared_target", "email-target:target-1", nil, nil, nil, "<dev@example.com>", "sent", 1, 3, now, nil, nil, nil, nil, nil, nil, nil, now, now, now,
		))

		result, claimErr := repo.AcquireForDelivery(context.Background(), repository.NotificationDeliveryClaimInput{
			Delivery:      domain.NotificationDelivery{BuildID: "build-1", EventType: domain.NotificationEventTypeBuildFailed, Transport: domain.NotificationTransportEmail, DestinationKind: domain.NotificationDestinationKindSharedTarget, DestinationKey: "email-target:target-1", Recipient: "<dev@example.com>"},
			ClaimOwner:    "worker-a",
			Now:           now,
			ClaimDuration: time.Minute,
			MaxAttempts:   3,
		})
		if claimErr != nil {
			t.Fatalf("acquire failed: %v", claimErr)
		}
		if result.Outcome != repository.NotificationDeliveryClaimOutcomeAlreadySent {
			t.Fatalf("expected already_sent, got %q", result.Outcome)
		}
	})

	t.Run("retry due reclaimed", func(t *testing.T) {
		nextAttemptAt := now.Add(-time.Minute)
		claimExpiresAt := now.Add(2 * time.Minute)
		claimedAt := now

		mock.ExpectBegin()
		mock.ExpectQuery("INSERT INTO notification_deliveries").WillReturnError(sql.ErrNoRows)
		mock.ExpectQuery("SELECT .* FROM notification_deliveries").WillReturnRows(sqlmock.NewRows(row).AddRow(
			"delivery-2", "build-1", "build_failed", "email", "shared_target", "email-target:target-2", nil, nil, nil, "<qa@example.com>", "retry_waiting", 1, 3, now.Add(-time.Minute), nextAttemptAt, nil, nil, nil, "retryable", "email_send_failed", "smtp unavailable", now, now, nil,
		))
		mock.ExpectQuery("UPDATE notification_deliveries").WillReturnRows(sqlmock.NewRows(row).AddRow(
			"delivery-2", "build-1", "build_failed", "email", "shared_target", "email-target:target-2", nil, nil, nil, "<qa@example.com>", "sending", 2, 3, now, nil, claimedAt, claimExpiresAt, "worker-b", "retryable", "email_send_failed", "smtp unavailable", now, now, nil,
		))
		mock.ExpectCommit()

		result, claimErr := repo.AcquireForDelivery(context.Background(), repository.NotificationDeliveryClaimInput{
			Delivery:      domain.NotificationDelivery{BuildID: "build-1", EventType: domain.NotificationEventTypeBuildFailed, Transport: domain.NotificationTransportEmail, DestinationKind: domain.NotificationDestinationKindSharedTarget, DestinationKey: "email-target:target-2", Recipient: "<qa@example.com>"},
			ClaimOwner:    "worker-b",
			Now:           now,
			ClaimDuration: 2 * time.Minute,
			MaxAttempts:   3,
		})
		if claimErr != nil {
			t.Fatalf("acquire failed: %v", claimErr)
		}
		if result.Outcome != repository.NotificationDeliveryClaimOutcomeRetryClaimed {
			t.Fatalf("expected retry_claimed, got %q", result.Outcome)
		}
		if result.Delivery.Attempts != 2 {
			t.Fatalf("expected attempts to increment to 2, got %d", result.Delivery.Attempts)
		}
	})

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestNotificationDeliveryRepository_RecordFailureAndLostClaim(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewNotificationDeliveryRepository(db)
	now := time.Now().UTC()
	nextAttemptAt := now.Add(30 * time.Second)
	row := []string{"id", "build_id", "event_type", "transport", "destination_kind", "destination_key", "notification_target_id", "recipient_user_id", "slack_workspace_integration_id", "recipient", "status", "attempts", "max_attempts", "last_attempt_at", "next_attempt_at", "claimed_at", "claim_expires_at", "claimed_by", "failure_category", "failure_reason", "last_error", "created_at", "updated_at", "sent_at"}

	mock.ExpectQuery("UPDATE notification_deliveries").WillReturnRows(sqlmock.NewRows(row).AddRow(
		"delivery-1", "build-1", "build_failed", "email", "shared_target", "email-target:target-1", nil, nil, nil, "<dev@example.com>", "retry_waiting", 1, 3, now, nextAttemptAt, nil, nil, nil, "retryable", "email_send_failed", "smtp unavailable", now, now, nil,
	))
	updated, updateErr := repo.RecordRetryableFailure(context.Background(), repository.NotificationDeliveryRecordFailureInput{
		DeliveryID:      "delivery-1",
		ClaimOwner:      "worker-a",
		ClaimedAt:       now,
		FailedAt:        now,
		NextAttemptAt:   &nextAttemptAt,
		FailureCategory: domain.NotificationDeliveryFailureCategoryRetryable,
		FailureReason:   "email_send_failed",
		LastError:       strPtrPG("smtp unavailable"),
	})
	if updateErr != nil {
		t.Fatalf("record retryable failure failed: %v", updateErr)
	}
	if updated.Delivery.Status != domain.NotificationDeliveryStatusRetryWaiting {
		t.Fatalf("expected retry_waiting status, got %q", updated.Delivery.Status)
	}

	mock.ExpectQuery("UPDATE notification_deliveries").WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`SELECT .* FROM notification_deliveries WHERE id = \$1`).WithArgs("delivery-2").WillReturnRows(sqlmock.NewRows(row).AddRow(
		"delivery-2", "build-1", "build_failed", "email", "shared_target", "email-target:target-2", nil, nil, nil, "<qa@example.com>", "sending", 2, 3, now, nil, now, now.Add(time.Minute), "worker-b", nil, nil, nil, now, now, nil,
	))
	lost, lostErr := repo.RecordPermanentFailure(context.Background(), repository.NotificationDeliveryRecordFailureInput{
		DeliveryID:      "delivery-2",
		ClaimOwner:      "worker-a",
		ClaimedAt:       now,
		FailedAt:        now,
		FailureCategory: domain.NotificationDeliveryFailureCategoryPermanent,
		FailureReason:   "invalid_email_message",
		LastError:       strPtrPG("invalid email"),
	})
	if lostErr != nil {
		t.Fatalf("record permanent failure failed: %v", lostErr)
	}
	if lost.Outcome != repository.NotificationDeliveryUpdateOutcomeLostClaim {
		t.Fatalf("expected lost_claim outcome, got %q", lost.Outcome)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func strPtrPG(value string) *string {
	return &value
}
