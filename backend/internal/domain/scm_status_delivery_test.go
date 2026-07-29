package domain

import (
	"testing"
	"time"
)

func TestSCMStatusDeliveryNormalizeAndValidation(t *testing.T) {
	now := time.Date(2026, 7, 16, 21, 10, 0, 0, time.UTC)
	claimExpiresAt := now.Add(time.Minute)
	sentAt := now.Add(2 * time.Minute)

	t.Run("normalize trims and defaults", func(t *testing.T) {
		details := "  https://ci.example.com/builds/1  "
		claimedBy := "  worker-a  "
		reason := "  api error  "
		errText := "  failed  "
		normalized := (SCMStatusDelivery{
			BuildID:         " build-1 ",
			BuildAttempt:    1,
			BuildCreatedAt:  now,
			Provider:        " GITHUB ",
			RepositoryOwner: " octo ",
			RepositoryName:  " repo ",
			CommitSHA:       " deadbeef ",
			Context:         " ctx ",
			DesiredState:    " pending ",
			Status:          " sending ",
			DetailsURL:      &details,
			ClaimedBy:       &claimedBy,
			FailureReason:   &reason,
			LastError:       &errText,
		}).Normalize()
		if normalized.Provider != "github" || normalized.BuildID != "build-1" || normalized.MaxAttempts != 1 {
			t.Fatalf("unexpected normalized delivery: %+v", normalized)
		}
		if normalized.DetailsURL == nil || *normalized.DetailsURL != "https://ci.example.com/builds/1" {
			t.Fatalf("expected trimmed details url, got %+v", normalized.DetailsURL)
		}
	})

	t.Run("validate identity", func(t *testing.T) {
		valid := SCMStatusDelivery{BuildID: "build-1", BuildAttempt: 1, BuildCreatedAt: now, Provider: "github", RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "deadbeef", Context: "ctx", DesiredState: SCMCommitStatusStatePending}
		if err := valid.ValidateIdentity(); err != nil {
			t.Fatalf("expected valid identity, got %v", err)
		}
		cases := []SCMStatusDelivery{{}, {BuildID: "build-1", BuildAttempt: -1, BuildCreatedAt: now, Provider: "github", RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "deadbeef", Context: "ctx", DesiredState: SCMCommitStatusStatePending}, {BuildID: "build-1", BuildAttempt: 1, Provider: "github", RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "deadbeef", Context: "ctx", DesiredState: SCMCommitStatusStatePending}, {BuildID: "build-1", BuildAttempt: 1, BuildCreatedAt: now, Provider: "", RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "deadbeef", Context: "ctx", DesiredState: SCMCommitStatusStatePending}, {BuildID: "build-1", BuildAttempt: 1, BuildCreatedAt: now, Provider: "github", RepositoryOwner: "", RepositoryName: "repo", CommitSHA: "deadbeef", Context: "ctx", DesiredState: SCMCommitStatusStatePending}, {BuildID: "build-1", BuildAttempt: 1, BuildCreatedAt: now, Provider: "github", RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "", Context: "ctx", DesiredState: SCMCommitStatusStatePending}, {BuildID: "build-1", BuildAttempt: 1, BuildCreatedAt: now, Provider: "github", RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "deadbeef", Context: "", DesiredState: SCMCommitStatusStatePending}, {BuildID: "build-1", BuildAttempt: 1, BuildCreatedAt: now, Provider: "github", RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "deadbeef", Context: "ctx", DesiredState: SCMCommitStatusState("bogus")}}
		for idx, delivery := range cases {
			if err := delivery.ValidateIdentity(); err == nil {
				t.Fatalf("expected identity error for case %d", idx)
			}
		}
	})

	t.Run("validate state-specific rules", func(t *testing.T) {
		claimOwner := "worker-a"
		retryCategory := SCMStatusDeliveryFailureCategoryRetryable
		permCategory := SCMStatusDeliveryFailureCategoryPermanent
		lastSentState := SCMCommitStatusStateSuccess
		invalidCategory := SCMStatusDeliveryFailureCategory("bogus")
		invalidState := SCMCommitStatusState("bogus")
		nextAttemptAt := now.Add(time.Minute)
		supersededAt := now.Add(3 * time.Minute)
		cases := []struct {
			name     string
			delivery SCMStatusDelivery
			wantErr  bool
		}{
			{name: "negative attempts invalid", delivery: SCMStatusDelivery{BuildID: "build-1", BuildAttempt: 1, BuildCreatedAt: now, Provider: "github", RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "deadbeef", Context: "ctx", DesiredState: SCMCommitStatusStatePending, Status: SCMStatusDeliveryStatusPending, Attempts: -1}, wantErr: true},
			{name: "attempts above max invalid", delivery: SCMStatusDelivery{BuildID: "build-1", BuildAttempt: 1, BuildCreatedAt: now, Provider: "github", RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "deadbeef", Context: "ctx", DesiredState: SCMCommitStatusStatePending, Status: SCMStatusDeliveryStatusPending, Attempts: 2, MaxAttempts: 1}, wantErr: true},
			{name: "invalid status", delivery: SCMStatusDelivery{BuildID: "build-1", BuildAttempt: 1, BuildCreatedAt: now, Provider: "github", RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "deadbeef", Context: "ctx", DesiredState: SCMCommitStatusStatePending, Status: SCMStatusDeliveryStatus("bogus")}, wantErr: true},
			{name: "invalid failure category", delivery: SCMStatusDelivery{BuildID: "build-1", BuildAttempt: 1, BuildCreatedAt: now, Provider: "github", RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "deadbeef", Context: "ctx", DesiredState: SCMCommitStatusStateFailure, Status: SCMStatusDeliveryStatusFailedPermanent, Attempts: 1, FailureCategory: &invalidCategory}, wantErr: true},
			{name: "invalid last sent state", delivery: SCMStatusDelivery{BuildID: "build-1", BuildAttempt: 1, BuildCreatedAt: now, Provider: "github", RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "deadbeef", Context: "ctx", DesiredState: SCMCommitStatusStateSuccess, Status: SCMStatusDeliveryStatusSent, Attempts: 1, SentAt: &sentAt, LastSentState: &invalidState}, wantErr: true},
			{name: "terminal claim owner invalid", delivery: SCMStatusDelivery{BuildID: "build-1", BuildAttempt: 1, BuildCreatedAt: now, Provider: "github", RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "deadbeef", Context: "ctx", DesiredState: SCMCommitStatusStateSuccess, Status: SCMStatusDeliveryStatusSent, Attempts: 1, SentAt: &sentAt, LastSentState: &lastSentState, ClaimedBy: &claimOwner}, wantErr: true},
			{name: "terminal claimed at invalid", delivery: SCMStatusDelivery{BuildID: "build-1", BuildAttempt: 1, BuildCreatedAt: now, Provider: "github", RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "deadbeef", Context: "ctx", DesiredState: SCMCommitStatusStateSuccess, Status: SCMStatusDeliveryStatusSent, Attempts: 1, SentAt: &sentAt, LastSentState: &lastSentState, ClaimedAt: &now}, wantErr: true},
			{name: "terminal claim expiry invalid", delivery: SCMStatusDelivery{BuildID: "build-1", BuildAttempt: 1, BuildCreatedAt: now, Provider: "github", RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "deadbeef", Context: "ctx", DesiredState: SCMCommitStatusStateSuccess, Status: SCMStatusDeliveryStatusSent, Attempts: 1, SentAt: &sentAt, LastSentState: &lastSentState, ClaimExpiresAt: &claimExpiresAt}, wantErr: true},
			{name: "terminal next attempt invalid", delivery: SCMStatusDelivery{BuildID: "build-1", BuildAttempt: 1, BuildCreatedAt: now, Provider: "github", RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "deadbeef", Context: "ctx", DesiredState: SCMCommitStatusStateSuccess, Status: SCMStatusDeliveryStatusSent, Attempts: 1, SentAt: &sentAt, LastSentState: &lastSentState, NextAttemptAt: &nextAttemptAt}, wantErr: true},
			{name: "pending active claim invalid", delivery: SCMStatusDelivery{BuildID: "build-1", BuildAttempt: 1, BuildCreatedAt: now, Provider: "github", RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "deadbeef", Context: "ctx", DesiredState: SCMCommitStatusStatePending, Status: SCMStatusDeliveryStatusPending, ClaimedBy: &claimOwner}, wantErr: true},
			{name: "pending next attempt invalid", delivery: SCMStatusDelivery{BuildID: "build-1", BuildAttempt: 1, BuildCreatedAt: now, Provider: "github", RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "deadbeef", Context: "ctx", DesiredState: SCMCommitStatusStatePending, Status: SCMStatusDeliveryStatusPending, NextAttemptAt: &nextAttemptAt}, wantErr: true},
			{name: "retry waiting missing next attempt", delivery: SCMStatusDelivery{BuildID: "build-1", BuildAttempt: 1, BuildCreatedAt: now, Provider: "github", RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "deadbeef", Context: "ctx", DesiredState: SCMCommitStatusStateFailure, Status: SCMStatusDeliveryStatusRetryWaiting, Attempts: 1, MaxAttempts: 3, FailureCategory: &retryCategory}, wantErr: true},
			{name: "retry waiting attempts exhausted invalid", delivery: SCMStatusDelivery{BuildID: "build-1", BuildAttempt: 1, BuildCreatedAt: now, Provider: "github", RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "deadbeef", Context: "ctx", DesiredState: SCMCommitStatusStateFailure, Status: SCMStatusDeliveryStatusRetryWaiting, Attempts: 3, MaxAttempts: 3, NextAttemptAt: &nextAttemptAt, FailureCategory: &retryCategory}, wantErr: true},
			{name: "retry waiting wrong category invalid", delivery: SCMStatusDelivery{BuildID: "build-1", BuildAttempt: 1, BuildCreatedAt: now, Provider: "github", RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "deadbeef", Context: "ctx", DesiredState: SCMCommitStatusStateFailure, Status: SCMStatusDeliveryStatusRetryWaiting, Attempts: 1, MaxAttempts: 3, NextAttemptAt: &nextAttemptAt, FailureCategory: &permCategory}, wantErr: true},
			{name: "sending missing claim", delivery: SCMStatusDelivery{BuildID: "build-1", BuildAttempt: 1, BuildCreatedAt: now, Provider: "github", RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "deadbeef", Context: "ctx", DesiredState: SCMCommitStatusStatePending, Status: SCMStatusDeliveryStatusSending, Attempts: 1}, wantErr: true},
			{name: "sending claim expiry before claim invalid", delivery: SCMStatusDelivery{BuildID: "build-1", BuildAttempt: 1, BuildCreatedAt: now, Provider: "github", RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "deadbeef", Context: "ctx", DesiredState: SCMCommitStatusStatePending, Status: SCMStatusDeliveryStatusSending, Attempts: 1, ClaimedAt: &claimExpiresAt, ClaimExpiresAt: &now, ClaimedBy: &claimOwner}, wantErr: true},
			{name: "sent missing last sent state", delivery: SCMStatusDelivery{BuildID: "build-1", BuildAttempt: 1, BuildCreatedAt: now, Provider: "github", RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "deadbeef", Context: "ctx", DesiredState: SCMCommitStatusStateSuccess, Status: SCMStatusDeliveryStatusSent, Attempts: 1, SentAt: &sentAt}, wantErr: true},
			{name: "sent requires attempts", delivery: SCMStatusDelivery{BuildID: "build-1", BuildAttempt: 1, BuildCreatedAt: now, Provider: "github", RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "deadbeef", Context: "ctx", DesiredState: SCMCommitStatusStateSuccess, Status: SCMStatusDeliveryStatusSent, SentAt: &sentAt, LastSentState: &lastSentState}, wantErr: true},
			{name: "permanent wrong category", delivery: SCMStatusDelivery{BuildID: "build-1", BuildAttempt: 1, BuildCreatedAt: now, Provider: "github", RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "deadbeef", Context: "ctx", DesiredState: SCMCommitStatusStateFailure, Status: SCMStatusDeliveryStatusFailedPermanent, Attempts: 1, FailureCategory: &retryCategory}, wantErr: true},
			{name: "exhausted wrong attempts", delivery: SCMStatusDelivery{BuildID: "build-1", BuildAttempt: 1, BuildCreatedAt: now, Provider: "github", RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "deadbeef", Context: "ctx", DesiredState: SCMCommitStatusStateFailure, Status: SCMStatusDeliveryStatusFailedExhausted, Attempts: 1, MaxAttempts: 3, FailureCategory: &retryCategory}, wantErr: true},
			{name: "exhausted wrong category", delivery: SCMStatusDelivery{BuildID: "build-1", BuildAttempt: 1, BuildCreatedAt: now, Provider: "github", RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "deadbeef", Context: "ctx", DesiredState: SCMCommitStatusStateFailure, Status: SCMStatusDeliveryStatusFailedExhausted, Attempts: 3, MaxAttempts: 3, FailureCategory: &permCategory}, wantErr: true},
			{name: "superseded missing timestamp", delivery: SCMStatusDelivery{BuildID: "build-1", BuildAttempt: 1, BuildCreatedAt: now, Provider: "github", RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "deadbeef", Context: "ctx", DesiredState: SCMCommitStatusStateFailure, Status: SCMStatusDeliveryStatusSuperseded, FailureCategory: &permCategory}, wantErr: true},
			{name: "valid superseded", delivery: SCMStatusDelivery{BuildID: "build-1", BuildAttempt: 1, BuildCreatedAt: now, Provider: "github", RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "deadbeef", Context: "ctx", DesiredState: SCMCommitStatusStateFailure, Status: SCMStatusDeliveryStatusSuperseded, Attempts: 1, FailureCategory: &permCategory, SupersededAt: &supersededAt}, wantErr: false},
			{name: "valid sent", delivery: SCMStatusDelivery{BuildID: "build-1", BuildAttempt: 1, BuildCreatedAt: now, Provider: "github", RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "deadbeef", Context: "ctx", DesiredState: SCMCommitStatusStateSuccess, Status: SCMStatusDeliveryStatusSent, Attempts: 1, SentAt: &sentAt, LastSentState: &lastSentState}, wantErr: false},
			{name: "valid sending", delivery: SCMStatusDelivery{BuildID: "build-1", BuildAttempt: 1, BuildCreatedAt: now, Provider: "github", RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "deadbeef", Context: "ctx", DesiredState: SCMCommitStatusStatePending, Status: SCMStatusDeliveryStatusSending, Attempts: 1, ClaimedAt: &now, ClaimExpiresAt: &claimExpiresAt, ClaimedBy: &claimOwner}, wantErr: false},
		}
		for _, test := range cases {
			t.Run(test.name, func(t *testing.T) {
				err := test.delivery.Validate()
				if test.wantErr && err == nil {
					t.Fatal("expected validation error")
				}
				if !test.wantErr && err != nil {
					t.Fatalf("expected valid delivery, got %v", err)
				}
			})
		}
	})
}

func TestSCMStatusDeliveryHelpers(t *testing.T) {
	now := time.Date(2026, 7, 16, 21, 20, 0, 0, time.UTC)
	before := now.Add(-time.Second)
	after := now.Add(time.Second)

	if !SCMCommitStatusStateSuccess.IsTerminal() || SCMCommitStatusStatePending.IsTerminal() {
		t.Fatal("unexpected terminal state evaluation")
	}
	if !SCMCommitStatusStatePending.IsValid() || SCMCommitStatusState("bad").IsValid() {
		t.Fatal("unexpected commit status validity result")
	}
	if !SCMStatusDeliveryStatusSent.IsValid() || SCMStatusDeliveryStatus("bad").IsValid() {
		t.Fatal("unexpected delivery status validity result")
	}
	if !SCMStatusDeliveryFailureCategoryRetryable.IsValid() || SCMStatusDeliveryFailureCategory("bad").IsValid() {
		t.Fatal("unexpected failure category validity result")
	}
	if !(SCMStatusDelivery{Status: SCMStatusDeliveryStatusSuperseded}).IsTerminal() || (SCMStatusDelivery{Status: SCMStatusDeliveryStatusSending}).IsTerminal() {
		t.Fatal("unexpected delivery terminal evaluation")
	}
	if !(SCMStatusDelivery{Status: SCMStatusDeliveryStatusPending, MaxAttempts: 2}).CanAttempt(now) {
		t.Fatal("expected pending delivery to be attemptable")
	}
	if !(SCMStatusDelivery{Status: SCMStatusDeliveryStatusRetryWaiting, Attempts: 1, MaxAttempts: 2, NextAttemptAt: &before}).CanAttempt(now) {
		t.Fatal("expected due retry delivery to be attemptable")
	}
	if (SCMStatusDelivery{Status: SCMStatusDeliveryStatusRetryWaiting, Attempts: 1, MaxAttempts: 2, NextAttemptAt: &after}).CanAttempt(now) {
		t.Fatal("did not expect future retry delivery to be attemptable")
	}
	if !(SCMStatusDelivery{Status: SCMStatusDeliveryStatusSending, Attempts: 1, MaxAttempts: 2, ClaimExpiresAt: &before}).CanAttempt(now) {
		t.Fatal("expected stale sending delivery to be attemptable")
	}
	if (SCMStatusDelivery{Status: SCMStatusDeliveryStatusSending, Attempts: 2, MaxAttempts: 2, ClaimExpiresAt: &before}).CanAttempt(now) {
		t.Fatal("did not expect exhausted delivery to be attemptable")
	}
	if got := trimSCMOptionalString(nil); got != nil {
		t.Fatalf("expected nil optional string, got %v", got)
	}
	blank := "   "
	if got := trimSCMOptionalString(&blank); got != nil {
		t.Fatalf("expected trimmed blank string to be nil, got %v", got)
	}
	if got := normalizeSCMOptionalTime(nil); got != nil {
		t.Fatalf("expected nil optional time, got %v", got)
	}
}

func TestRepositoryIdentitySnapshotValidation(t *testing.T) {
	value := func(input string) *string { return &input }
	cases := []struct {
		name      string
		identity  [3]*string
		wantError bool
	}{
		{name: "all absent", identity: [3]*string{nil, nil, nil}},
		{name: "all present", identity: [3]*string{value("repository-1"), value("connection-1"), value("provider-repository-1")}},
		{name: "only registration present", identity: [3]*string{value("repository-1"), nil, nil}, wantError: true},
		{name: "only connection present", identity: [3]*string{nil, value("connection-1"), nil}, wantError: true},
		{name: "only provider repository present", identity: [3]*string{nil, nil, value("provider-repository-1")}, wantError: true},
		{name: "registration whitespace", identity: [3]*string{value("  "), value("connection-1"), value("provider-repository-1")}, wantError: true},
		{name: "connection whitespace", identity: [3]*string{value("repository-1"), value("  "), value("provider-repository-1")}, wantError: true},
		{name: "provider repository whitespace", identity: [3]*string{value("repository-1"), value("connection-1"), value("  ")}, wantError: true},
		{name: "all whitespace", identity: [3]*string{value("  "), value("  "), value("  ")}, wantError: true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			build := Build{RegisteredRepositoryID: test.identity[0], SCMConnectionID: test.identity[1], ProviderRepositoryID: test.identity[2]}
			delivery := SCMStatusDelivery{BuildID: "build-1", BuildAttempt: 1, BuildCreatedAt: time.Now().UTC(), Provider: "github", RepositoryOwner: "octo", RepositoryName: "repo", RegisteredRepositoryID: test.identity[0], SCMConnectionID: test.identity[1], ProviderRepositoryID: test.identity[2], CommitSHA: "deadbeef", Context: "ctx", DesiredState: SCMCommitStatusStatePending, Status: SCMStatusDeliveryStatusPending}
			for _, err := range []error{build.ValidateRepositoryIdentitySnapshot(), delivery.ValidateIdentity(), delivery.Validate()} {
				if (err != nil) != test.wantError {
					t.Fatalf("expected error=%v, got %v", test.wantError, err)
				}
			}
		})
	}
}

func scmFailureCategoryPtr(value SCMStatusDeliveryFailureCategory) *SCMStatusDeliveryFailureCategory {
	return &value
}
