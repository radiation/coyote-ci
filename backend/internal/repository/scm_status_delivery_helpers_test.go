package repository

import (
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

func TestSCMStatusDeliveryClaimOutcomeFromExisting(t *testing.T) {
	now := time.Date(2026, 7, 16, 21, 0, 0, 0, time.UTC)
	before := now.Add(-time.Minute)
	after := now.Add(time.Minute)
	tests := []struct {
		name     string
		delivery domain.SCMStatusDelivery
		want     SCMStatusDeliveryClaimOutcome
	}{
		{name: "sent", delivery: domain.SCMStatusDelivery{Status: domain.SCMStatusDeliveryStatusSent}, want: SCMStatusDeliveryClaimOutcomeAlreadySent},
		{name: "permanent", delivery: domain.SCMStatusDelivery{Status: domain.SCMStatusDeliveryStatusFailedPermanent}, want: SCMStatusDeliveryClaimOutcomePermanentlyFailed},
		{name: "exhausted", delivery: domain.SCMStatusDelivery{Status: domain.SCMStatusDeliveryStatusFailedExhausted}, want: SCMStatusDeliveryClaimOutcomeAttemptsExhausted},
		{name: "superseded", delivery: domain.SCMStatusDelivery{Status: domain.SCMStatusDeliveryStatusSuperseded}, want: SCMStatusDeliveryClaimOutcomeSuperseded},
		{name: "sending exhausted", delivery: domain.SCMStatusDelivery{Status: domain.SCMStatusDeliveryStatusSending, Attempts: 3, MaxAttempts: 3}, want: SCMStatusDeliveryClaimOutcomeAttemptsExhausted},
		{name: "sending stale", delivery: domain.SCMStatusDelivery{Status: domain.SCMStatusDeliveryStatusSending, Attempts: 1, MaxAttempts: 3, ClaimExpiresAt: &before}, want: SCMStatusDeliveryClaimOutcomeStaleClaimReclaimed},
		{name: "sending active", delivery: domain.SCMStatusDelivery{Status: domain.SCMStatusDeliveryStatusSending, Attempts: 1, MaxAttempts: 3, ClaimExpiresAt: &after}, want: SCMStatusDeliveryClaimOutcomeClaimedByOther},
		{name: "retry not due", delivery: domain.SCMStatusDelivery{Status: domain.SCMStatusDeliveryStatusRetryWaiting, Attempts: 1, MaxAttempts: 3, NextAttemptAt: &after}, want: SCMStatusDeliveryClaimOutcomeRetryNotDue},
		{name: "retry exhausted", delivery: domain.SCMStatusDelivery{Status: domain.SCMStatusDeliveryStatusRetryWaiting, Attempts: 3, MaxAttempts: 3, NextAttemptAt: &before}, want: SCMStatusDeliveryClaimOutcomeAttemptsExhausted},
		{name: "retry due", delivery: domain.SCMStatusDelivery{Status: domain.SCMStatusDeliveryStatusRetryWaiting, Attempts: 1, MaxAttempts: 3, NextAttemptAt: &before}, want: SCMStatusDeliveryClaimOutcomeRetryClaimed},
		{name: "pending exhausted", delivery: domain.SCMStatusDelivery{Status: domain.SCMStatusDeliveryStatusPending, Attempts: 1, MaxAttempts: 1}, want: SCMStatusDeliveryClaimOutcomeAttemptsExhausted},
		{name: "pending due", delivery: domain.SCMStatusDelivery{Status: domain.SCMStatusDeliveryStatusPending, Attempts: 0, MaxAttempts: 1}, want: SCMStatusDeliveryClaimOutcomeRetryClaimed},
		{name: "unknown", delivery: domain.SCMStatusDelivery{}, want: SCMStatusDeliveryClaimOutcomeClaimedByOther},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := SCMStatusDeliveryClaimOutcomeFromExisting(test.delivery, now); got != test.want {
				t.Fatalf("expected %q, got %q", test.want, got)
			}
		})
	}
}

func TestSCMStatusDeliveryOrderingHelpers(t *testing.T) {
	now := time.Date(2026, 7, 16, 21, 5, 0, 0, time.UTC)
	later := now.Add(time.Minute)
	terminal := domain.SCMCommitStatusStateSuccess

	t.Run("compare owners", func(t *testing.T) {
		tests := []struct {
			name     string
			existing domain.SCMStatusDelivery
			incoming domain.SCMStatusDelivery
			want     int
		}{
			{name: "same build", existing: domain.SCMStatusDelivery{BuildID: "build-1", BuildAttempt: 1, BuildCreatedAt: now}, incoming: domain.SCMStatusDelivery{BuildID: "build-1", BuildAttempt: 2, BuildCreatedAt: later}, want: 0},
			{name: "higher attempt wins", existing: domain.SCMStatusDelivery{BuildID: "build-2", BuildAttempt: 2, BuildCreatedAt: now}, incoming: domain.SCMStatusDelivery{BuildID: "build-1", BuildAttempt: 1, BuildCreatedAt: later}, want: 1},
			{name: "lower attempt loses", existing: domain.SCMStatusDelivery{BuildID: "build-1", BuildAttempt: 1, BuildCreatedAt: now}, incoming: domain.SCMStatusDelivery{BuildID: "build-2", BuildAttempt: 2, BuildCreatedAt: later}, want: -1},
			{name: "later created wins", existing: domain.SCMStatusDelivery{BuildID: "build-2", BuildAttempt: 1, BuildCreatedAt: later}, incoming: domain.SCMStatusDelivery{BuildID: "build-1", BuildAttempt: 1, BuildCreatedAt: now}, want: 1},
			{name: "build id tie break", existing: domain.SCMStatusDelivery{BuildID: "build-z", BuildAttempt: 1, BuildCreatedAt: now}, incoming: domain.SCMStatusDelivery{BuildID: "build-a", BuildAttempt: 1, BuildCreatedAt: now}, want: 1},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				if got := CompareSCMStatusDeliveryOwners(test.existing, test.incoming); got != test.want {
					t.Fatalf("expected %d, got %d", test.want, got)
				}
			})
		}
	})

	t.Run("incoming state obsolete", func(t *testing.T) {
		if !SCMStatusDeliveryIncomingStateObsolete(domain.SCMStatusDelivery{DesiredState: domain.SCMCommitStatusStateSuccess}, domain.SCMStatusDelivery{DesiredState: domain.SCMCommitStatusStatePending}) {
			t.Fatal("expected terminal desired state to obsolete incoming pending state")
		}
		if !SCMStatusDeliveryIncomingStateObsolete(domain.SCMStatusDelivery{DesiredState: domain.SCMCommitStatusStatePending, LastSentState: &terminal}, domain.SCMStatusDelivery{DesiredState: domain.SCMCommitStatusStatePending}) {
			t.Fatal("expected terminal last sent state to obsolete incoming pending state")
		}
		if SCMStatusDeliveryIncomingStateObsolete(domain.SCMStatusDelivery{DesiredState: domain.SCMCommitStatusStatePending}, domain.SCMStatusDelivery{DesiredState: domain.SCMCommitStatusStateFailure}) {
			t.Fatal("did not expect terminal incoming state to be obsolete")
		}
	})

	t.Run("replace current state", func(t *testing.T) {
		if !SCMStatusDeliveryShouldReplaceCurrentState(domain.SCMStatusDelivery{DesiredState: domain.SCMCommitStatusStatePending}, domain.SCMStatusDelivery{DesiredState: domain.SCMCommitStatusStateFailure}) {
			t.Fatal("expected different states to replace")
		}
		if SCMStatusDeliveryShouldReplaceCurrentState(domain.SCMStatusDelivery{DesiredState: domain.SCMCommitStatusStateFailure}, domain.SCMStatusDelivery{DesiredState: domain.SCMCommitStatusStateFailure}) {
			t.Fatal("did not expect equal states to replace")
		}
	})

	t.Run("reassert after replacement", func(t *testing.T) {
		if got := SCMStatusDeliveryReassertAfterReplacement(domain.SCMStatusDelivery{Status: domain.SCMStatusDeliveryStatusRetryWaiting}, now); got != nil {
			t.Fatalf("expected nil reassert time, got %v", got)
		}
		if got := SCMStatusDeliveryReassertAfterReplacement(domain.SCMStatusDelivery{Status: domain.SCMStatusDeliveryStatusSending}, now); got != nil {
			t.Fatalf("expected nil reassert time without claim expiry, got %v", got)
		}
		expired := now.Add(-time.Second)
		if got := SCMStatusDeliveryReassertAfterReplacement(domain.SCMStatusDelivery{Status: domain.SCMStatusDeliveryStatusSending, ClaimExpiresAt: &expired}, now); got != nil {
			t.Fatalf("expected nil reassert time for expired claim, got %v", got)
		}
		active := now.Add(time.Minute)
		got := SCMStatusDeliveryReassertAfterReplacement(domain.SCMStatusDelivery{Status: domain.SCMStatusDeliveryStatusSending, ClaimExpiresAt: &active}, now)
		if got == nil || !got.Equal(active) {
			t.Fatalf("expected active reassert time %v, got %v", active, got)
		}
	})
}
