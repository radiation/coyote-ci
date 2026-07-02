package build

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	platformemail "github.com/radiation/coyote-ci/backend/internal/platform/email"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func TestBuildNotificationService_MarkDeliveryFailedRoutesPersistence(t *testing.T) {
	now := time.Date(2026, time.July, 2, 12, 0, 0, 0, time.UTC)
	claimedAt := now.Add(-time.Minute)
	base := domain.NotificationDelivery{
		ID:          "delivery-1",
		BuildID:     "build-1",
		EventType:   domain.NotificationEventTypeBuildFailed,
		Transport:   domain.NotificationTransportEmail,
		Status:      domain.NotificationDeliveryStatusSending,
		Attempts:    1,
		MaxAttempts: 3,
		ClaimedAt:   &claimedAt,
	}

	t.Run("retryable failure schedules retry", func(t *testing.T) {
		var got repository.NotificationDeliveryRecordFailureInput
		service := &BuildNotificationService{
			claimOwner:  "worker-a",
			retryPolicy: notificationRetryPolicy{maxAttempts: 3, initialDelay: 30 * time.Second, maxDelay: 10 * time.Minute},
			deliveryRepo: &scriptedNotificationDeliveryRepo{
				recordRetryableFailureFunc: func(ctx context.Context, input repository.NotificationDeliveryRecordFailureInput) (repository.NotificationDeliveryUpdateResult, error) {
					_ = ctx
					got = input
					return repository.NotificationDeliveryUpdateResult{Outcome: repository.NotificationDeliveryUpdateOutcomeUpdated}, nil
				},
			},
		}

		outcome, err := service.markDeliveryFailed(context.Background(), base, errors.New("smtp unavailable"), now, notificationRecoveryReasonInline)
		if err != nil {
			t.Fatalf("expected retryable failure to persist, got %v", err)
		}
		if outcome != notificationExecutionOutcomeRetryScheduled {
			t.Fatalf("expected retryable failure to schedule retry, got %q", outcome)
		}
		if got.FailureCategory != domain.NotificationDeliveryFailureCategoryRetryable || got.FailureReason != "email_send_failed" {
			t.Fatalf("unexpected retryable failure input: %+v", got)
		}
		if got.NextAttemptAt == nil || !got.NextAttemptAt.Equal(now.Add(30*time.Second)) {
			t.Fatalf("expected retry scheduled at %s, got %v", now.Add(30*time.Second), got.NextAttemptAt)
		}
	})

	t.Run("retryable failure at max attempts exhausts", func(t *testing.T) {
		called := false
		delivery := base
		delivery.Attempts = 3
		service := &BuildNotificationService{
			claimOwner:  "worker-a",
			retryPolicy: notificationRetryPolicy{maxAttempts: 3, initialDelay: 30 * time.Second, maxDelay: 10 * time.Minute},
			deliveryRepo: &scriptedNotificationDeliveryRepo{
				recordExhaustedFailureFunc: func(ctx context.Context, input repository.NotificationDeliveryRecordFailureInput) (repository.NotificationDeliveryUpdateResult, error) {
					_ = ctx
					called = true
					if input.NextAttemptAt != nil {
						t.Fatalf("expected exhausted retry failure to omit next attempt, got %v", input.NextAttemptAt)
					}
					return repository.NotificationDeliveryUpdateResult{Outcome: repository.NotificationDeliveryUpdateOutcomeUpdated}, nil
				},
			},
		}

		outcome, err := service.markDeliveryFailed(context.Background(), delivery, errors.New("smtp unavailable"), now, notificationRecoveryReasonInline)
		if err != nil {
			t.Fatalf("expected exhausted failure to persist, got %v", err)
		}
		if outcome != notificationExecutionOutcomeAttemptsExhausted || !called {
			t.Fatalf("expected exhausted retry path to be used, outcome=%q called=%t", outcome, called)
		}
	})

	t.Run("permanent failure is recorded permanently", func(t *testing.T) {
		var got repository.NotificationDeliveryRecordFailureInput
		service := &BuildNotificationService{
			claimOwner:  "worker-a",
			retryPolicy: notificationRetryPolicy{maxAttempts: 3, initialDelay: 30 * time.Second, maxDelay: 10 * time.Minute},
			deliveryRepo: &scriptedNotificationDeliveryRepo{
				recordPermanentFailureFunc: func(ctx context.Context, input repository.NotificationDeliveryRecordFailureInput) (repository.NotificationDeliveryUpdateResult, error) {
					_ = ctx
					got = input
					return repository.NotificationDeliveryUpdateResult{Outcome: repository.NotificationDeliveryUpdateOutcomeUpdated}, nil
				},
			},
		}

		outcome, err := service.markDeliveryFailed(context.Background(), base, platformemail.ErrInvalidMessage, now, notificationRecoveryReasonInline)
		if err != nil {
			t.Fatalf("expected permanent failure to persist, got %v", err)
		}
		if outcome != notificationExecutionOutcomePermanentlyFailed {
			t.Fatalf("expected permanent failure outcome, got %q", outcome)
		}
		if got.FailureCategory != domain.NotificationDeliveryFailureCategoryPermanent || got.FailureReason != "invalid_email_message" {
			t.Fatalf("unexpected permanent failure input: %+v", got)
		}
	})

	t.Run("lost claim returns false without error", func(t *testing.T) {
		service := &BuildNotificationService{
			claimOwner:  "worker-a",
			retryPolicy: notificationRetryPolicy{maxAttempts: 3, initialDelay: 30 * time.Second, maxDelay: 10 * time.Minute},
			deliveryRepo: &scriptedNotificationDeliveryRepo{
				recordPermanentFailureFunc: func(ctx context.Context, input repository.NotificationDeliveryRecordFailureInput) (repository.NotificationDeliveryUpdateResult, error) {
					_ = ctx
					_ = input
					return repository.NotificationDeliveryUpdateResult{Outcome: repository.NotificationDeliveryUpdateOutcomeLostClaim}, nil
				},
			},
		}

		outcome, err := service.markDeliveryFailed(context.Background(), base, platformemail.ErrInvalidMessage, now, notificationRecoveryReasonInline)
		if err != nil {
			t.Fatalf("expected lost-claim failure path without error, got %v", err)
		}
		if outcome != notificationExecutionOutcomeLostClaim {
			t.Fatalf("expected lost-claim outcome, got %q", outcome)
		}
	})

	t.Run("non-ledger send handles provider errors without persistence", func(t *testing.T) {
		sender := &recordingEmailSender{err: errors.New("smtp unavailable")}
		service := &BuildNotificationService{
			enabled: true,
			sender:  sender,
		}
		destinations := []notificationDestination{{
			transport:       domain.NotificationTransportEmail,
			destinationKind: domain.NotificationDestinationKindSharedTarget,
			destinationKey:  "email-target:target-1",
			recipient:       "<dev@example.com>",
			emailRecipient:  "dev@example.com",
		}}

		err := service.sendTerminalNotification(context.Background(), "build-1", domain.NotificationEventTypeBuildFailed, destinations, "subject", "body", "slack", "personal slack")
		if err == nil || !strings.Contains(err.Error(), "smtp unavailable") {
			t.Fatalf("expected provider error, got %v", err)
		}
		if len(sender.messages) != 1 {
			t.Fatalf("expected one direct send attempt, got %d", len(sender.messages))
		}
	})
}

func TestBuildNotificationService_MarkDeliverySentAndCancellationBranches(t *testing.T) {
	now := time.Date(2026, time.July, 2, 12, 0, 0, 0, time.UTC)
	claimExpiresAt := now.Add(time.Minute)
	claimedAt := now.Add(-time.Minute)
	delivery := domain.NotificationDelivery{
		ID:              "delivery-1",
		BuildID:         "build-1",
		EventType:       domain.NotificationEventTypeBuildFailed,
		Transport:       domain.NotificationTransportEmail,
		DestinationKind: domain.NotificationDestinationKindSharedTarget,
		DestinationKey:  "email-target:target-1",
		Recipient:       "<dev@example.com>",
		Status:          domain.NotificationDeliveryStatusSending,
		Attempts:        1,
		MaxAttempts:     3,
		ClaimedAt:       &claimedAt,
		ClaimExpiresAt:  &claimExpiresAt,
		ClaimedBy:       strPtr("worker-a"),
	}

	t.Run("missing claimed timestamp fails early", func(t *testing.T) {
		service := &BuildNotificationService{claimOwner: "worker-a", deliveryRepo: &scriptedNotificationDeliveryRepo{}}
		missing := delivery
		missing.ClaimedAt = nil
		outcome, err := service.markDeliverySent(context.Background(), missing, now, notificationRecoveryReasonInline)
		if outcome != notificationExecutionOutcomeNone || err == nil || !strings.Contains(err.Error(), "claim timestamp is required") {
			t.Fatalf("expected missing claim timestamp error, got outcome=%q err=%v", outcome, err)
		}
	})

	t.Run("lost claim while marking sent returns false", func(t *testing.T) {
		service := &BuildNotificationService{
			claimOwner: "worker-a",
			deliveryRepo: &scriptedNotificationDeliveryRepo{
				markSentFunc: func(ctx context.Context, input repository.NotificationDeliveryMarkSentInput) (repository.NotificationDeliveryUpdateResult, error) {
					_ = ctx
					_ = input
					return repository.NotificationDeliveryUpdateResult{Outcome: repository.NotificationDeliveryUpdateOutcomeLostClaim}, nil
				},
			},
		}
		outcome, err := service.markDeliverySent(context.Background(), delivery, now, notificationRecoveryReasonInline)
		if err != nil {
			t.Fatalf("expected lost-claim mark sent path without error, got %v", err)
		}
		if outcome != notificationExecutionOutcomeLostClaim {
			t.Fatalf("expected lost-claim outcome, got %q", outcome)
		}
	})

	t.Run("send terminal notification leaves claim active on cancellation", func(t *testing.T) {
		markSentCalled := false
		failureCalled := false
		service := &BuildNotificationService{
			claimOwner: "worker-a",
			sender:     &recordingEmailSender{err: context.Canceled},
			deliveryRepo: &scriptedNotificationDeliveryRepo{
				acquireFunc: func(ctx context.Context, input repository.NotificationDeliveryClaimInput) (repository.NotificationDeliveryClaimResult, error) {
					_ = ctx
					_ = input
					return repository.NotificationDeliveryClaimResult{Delivery: delivery, Outcome: repository.NotificationDeliveryClaimOutcomeCreatedClaimed}, nil
				},
				markSentFunc: func(ctx context.Context, input repository.NotificationDeliveryMarkSentInput) (repository.NotificationDeliveryUpdateResult, error) {
					_ = ctx
					_ = input
					markSentCalled = true
					return repository.NotificationDeliveryUpdateResult{}, nil
				},
				recordRetryableFailureFunc: func(ctx context.Context, input repository.NotificationDeliveryRecordFailureInput) (repository.NotificationDeliveryUpdateResult, error) {
					_ = ctx
					_ = input
					failureCalled = true
					return repository.NotificationDeliveryUpdateResult{}, nil
				},
				recordPermanentFailureFunc: func(ctx context.Context, input repository.NotificationDeliveryRecordFailureInput) (repository.NotificationDeliveryUpdateResult, error) {
					_ = ctx
					_ = input
					failureCalled = true
					return repository.NotificationDeliveryUpdateResult{}, nil
				},
				recordExhaustedFailureFunc: func(ctx context.Context, input repository.NotificationDeliveryRecordFailureInput) (repository.NotificationDeliveryUpdateResult, error) {
					_ = ctx
					_ = input
					failureCalled = true
					return repository.NotificationDeliveryUpdateResult{}, nil
				},
			},
			now:           func() time.Time { return now },
			retryPolicy:   notificationRetryPolicy{maxAttempts: 3, initialDelay: 30 * time.Second, maxDelay: 10 * time.Minute},
			claimDuration: time.Minute,
		}
		destinations := []notificationDestination{{
			transport:       domain.NotificationTransportEmail,
			destinationKind: domain.NotificationDestinationKindSharedTarget,
			destinationKey:  "email-target:target-1",
			recipient:       "<dev@example.com>",
			emailRecipient:  "dev@example.com",
		}}

		err := service.sendTerminalNotification(context.Background(), "build-1", domain.NotificationEventTypeBuildFailed, destinations, "subject", "body", "slack", "personal slack")
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected canceled send error, got %v", err)
		}
		if markSentCalled || failureCalled {
			t.Fatalf("expected cancellation path to avoid persistence updates, markSent=%t failure=%t", markSentCalled, failureCalled)
		}
	})
}
