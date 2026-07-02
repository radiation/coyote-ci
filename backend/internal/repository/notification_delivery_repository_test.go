package repository

import (
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

func TestNotificationDeliveryClaimOutcomeFromExisting(t *testing.T) {
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	claimedAt := now.Add(-time.Minute)
	expiresLater := now.Add(time.Minute)
	expiresEarlier := now.Add(-time.Minute)
	retryLater := now.Add(time.Minute)

	tests := []struct {
		name     string
		delivery domain.NotificationDelivery
		want     NotificationDeliveryClaimOutcome
	}{
		{name: "sent maps to already sent", delivery: domain.NotificationDelivery{Status: domain.NotificationDeliveryStatusSent}, want: NotificationDeliveryClaimOutcomeAlreadySent},
		{name: "permanent maps to permanently failed", delivery: domain.NotificationDelivery{Status: domain.NotificationDeliveryStatusFailedPermanent}, want: NotificationDeliveryClaimOutcomePermanentlyFailed},
		{name: "exhausted maps to attempts exhausted", delivery: domain.NotificationDelivery{Status: domain.NotificationDeliveryStatusFailedExhausted}, want: NotificationDeliveryClaimOutcomeAttemptsExhausted},
		{name: "active claim maps to claimed by other", delivery: domain.NotificationDelivery{Status: domain.NotificationDeliveryStatusSending, Attempts: 1, MaxAttempts: 3, ClaimExpiresAt: &expiresLater}, want: NotificationDeliveryClaimOutcomeClaimedByOther},
		{name: "expired claim maps to stale claim reclaimed", delivery: domain.NotificationDelivery{Status: domain.NotificationDeliveryStatusSending, Attempts: 1, MaxAttempts: 3, ClaimedAt: &claimedAt, ClaimExpiresAt: &expiresEarlier}, want: NotificationDeliveryClaimOutcomeStaleClaimReclaimed},
		{name: "expired final-attempt claim maps to attempts exhausted", delivery: domain.NotificationDelivery{Status: domain.NotificationDeliveryStatusSending, Attempts: 3, MaxAttempts: 3, ClaimedAt: &claimedAt, ClaimExpiresAt: &expiresEarlier}, want: NotificationDeliveryClaimOutcomeAttemptsExhausted},
		{name: "retry waiting not due maps to retry not due", delivery: domain.NotificationDelivery{Status: domain.NotificationDeliveryStatusRetryWaiting, NextAttemptAt: &retryLater, Attempts: 1, MaxAttempts: 3}, want: NotificationDeliveryClaimOutcomeRetryNotDue},
		{name: "retry waiting due maps to retry claimed", delivery: domain.NotificationDelivery{Status: domain.NotificationDeliveryStatusRetryWaiting, Attempts: 1, MaxAttempts: 3}, want: NotificationDeliveryClaimOutcomeRetryClaimed},
		{name: "pending due maps to retry claimed", delivery: domain.NotificationDelivery{Status: domain.NotificationDeliveryStatusPending, Attempts: 0, MaxAttempts: 3}, want: NotificationDeliveryClaimOutcomeRetryClaimed},
		{name: "attempt bound maps to exhausted", delivery: domain.NotificationDelivery{Status: domain.NotificationDeliveryStatusPending, Attempts: 3, MaxAttempts: 3}, want: NotificationDeliveryClaimOutcomeAttemptsExhausted},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := NotificationDeliveryClaimOutcomeFromExisting(tc.delivery, now); got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}
