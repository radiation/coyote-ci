package repository

import (
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

func TestNotificationDeliveryAcquireOutcomeFromStatus(t *testing.T) {
	tests := []struct {
		name   string
		status domain.NotificationDeliveryStatus
		want   NotificationDeliveryAcquireOutcome
	}{
		{name: "sent maps to sent", status: domain.NotificationDeliveryStatusSent, want: NotificationDeliveryAcquireOutcomeSent},
		{name: "failed maps to failed", status: domain.NotificationDeliveryStatusFailed, want: NotificationDeliveryAcquireOutcomeFailed},
		{name: "pending maps to pending", status: domain.NotificationDeliveryStatusPending, want: NotificationDeliveryAcquireOutcomePending},
		{name: "unknown defaults to pending", status: domain.NotificationDeliveryStatus("queued"), want: NotificationDeliveryAcquireOutcomePending},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := NotificationDeliveryAcquireOutcomeFromStatus(tc.status); got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}
