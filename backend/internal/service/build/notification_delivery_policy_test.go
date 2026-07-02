package build

import (
	"strings"
	"testing"
	"time"

	platformemail "github.com/radiation/coyote-ci/backend/internal/platform/email"
)

func TestMinimumNotificationClaimDuration(t *testing.T) {
	want := platformemail.DefaultSMTPTimeout + notificationClaimSafetyMargin
	if got := minimumNotificationClaimDuration(); got != want {
		t.Fatalf("expected minimum claim duration %s, got %s", want, got)
	}
}

func TestValidateNotificationClaimDuration(t *testing.T) {
	minimum := minimumNotificationClaimDuration()
	if err := validateNotificationClaimDuration(minimum); err != nil {
		t.Fatalf("expected exact minimum claim duration to pass, got %v", err)
	}
	if err := validateNotificationClaimDuration(minimum - time.Second); err == nil || !strings.Contains(err.Error(), "must be at least") {
		t.Fatalf("expected too-short claim duration error, got %v", err)
	}
}

func TestDefaultNotificationRetryPolicyAndDelay(t *testing.T) {
	policy := defaultNotificationRetryPolicy()
	if policy.maxAttempts != defaultNotificationDeliveryMaxAttempts || policy.initialDelay != defaultNotificationRetryInitialDelay || policy.maxDelay != defaultNotificationRetryMaxDelay {
		t.Fatalf("unexpected default retry policy: %+v", policy)
	}

	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{attempt: 0, want: defaultNotificationRetryInitialDelay},
		{attempt: 1, want: defaultNotificationRetryInitialDelay},
		{attempt: 2, want: defaultNotificationRetryInitialDelay * 2},
		{attempt: 3, want: defaultNotificationRetryInitialDelay * 4},
		{attempt: 10, want: defaultNotificationRetryMaxDelay},
	}
	for _, tc := range tests {
		if got := policy.delayForAttempt(tc.attempt); got != tc.want {
			t.Fatalf("attempt %d: expected delay %s, got %s", tc.attempt, tc.want, got)
		}
	}
}
