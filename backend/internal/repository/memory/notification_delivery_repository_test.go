package memory

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func TestNotificationDeliveryRepository_CreateGetUpdateAndErrors(t *testing.T) {
	repo := NewNotificationDeliveryRepository()

	created, err := repo.Create(context.Background(), domain.NotificationDelivery{
		BuildID:         " build-1 ",
		EventType:       domain.NotificationEventTypeBuildFailed,
		Transport:       domain.NotificationTransportEmail,
		DestinationKind: domain.NotificationDestinationKindSharedTarget,
		DestinationKey:  "email-target:target-1",
		Recipient:       " <dev@example.com> ",
		MaxAttempts:     1,
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if created.Status != domain.NotificationDeliveryStatusPending {
		t.Fatalf("expected pending status, got %q", created.Status)
	}

	fetched, err := repo.GetByBuildEventRecipient(context.Background(), " build-1 ", domain.NotificationEventTypeBuildFailed, " <dev@example.com> ")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if fetched.ID != created.ID {
		t.Fatalf("expected same delivery id, got %q", fetched.ID)
	}

	now := time.Now().UTC()
	nextAttemptAt := now.Add(time.Minute)
	retryable := domain.NotificationDeliveryFailureCategoryRetryable
	updated, err := repo.Update(context.Background(), domain.NotificationDelivery{
		ID:              created.ID,
		BuildID:         created.BuildID,
		EventType:       created.EventType,
		Transport:       created.Transport,
		DestinationKind: created.DestinationKind,
		DestinationKey:  created.DestinationKey,
		Recipient:       created.Recipient,
		Status:          domain.NotificationDeliveryStatusRetryWaiting,
		Attempts:        1,
		MaxAttempts:     2,
		LastAttemptAt:   &now,
		NextAttemptAt:   &nextAttemptAt,
		FailureCategory: &retryable,
		UpdatedAt:       now,
	})
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}
	if updated.Status != domain.NotificationDeliveryStatusRetryWaiting {
		t.Fatalf("expected retry_waiting status, got %q", updated.Status)
	}

	if _, err := repo.Create(context.Background(), domain.NotificationDelivery{BuildID: created.BuildID, EventType: created.EventType, Transport: created.Transport, DestinationKind: created.DestinationKind, DestinationKey: created.DestinationKey, Recipient: created.Recipient, MaxAttempts: 1}); !errors.Is(err, repository.ErrNotificationDeliveryDuplicate) {
		t.Fatalf("expected duplicate create error, got %v", err)
	}
	if _, err := repo.GetByBuildEventRecipient(context.Background(), "missing", domain.NotificationEventTypeBuildFailed, "<dev@example.com>"); !errors.Is(err, repository.ErrNotificationDeliveryNotFound) {
		t.Fatalf("expected not found get error, got %v", err)
	}
	if _, err := repo.Update(context.Background(), domain.NotificationDelivery{ID: "missing"}); !errors.Is(err, repository.ErrNotificationDeliveryNotFound) {
		t.Fatalf("expected not found update error, got %v", err)
	}
}

func TestNotificationDeliveryRepository_ClaimContractAndLostClaimProtection(t *testing.T) {
	repo := NewNotificationDeliveryRepository()
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	base := domain.NotificationDelivery{
		BuildID:         "build-1",
		EventType:       domain.NotificationEventTypeBuildFailed,
		Transport:       domain.NotificationTransportEmail,
		DestinationKind: domain.NotificationDestinationKindSharedTarget,
		DestinationKey:  "email-target:target-1",
		Recipient:       "<dev@example.com>",
	}

	first, err := repo.AcquireForDelivery(context.Background(), repository.NotificationDeliveryClaimInput{Delivery: base, ClaimOwner: "worker-a", Now: now, ClaimDuration: time.Minute, MaxAttempts: 3})
	if err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	if first.Outcome != repository.NotificationDeliveryClaimOutcomeCreatedClaimed {
		t.Fatalf("expected created_claimed, got %q", first.Outcome)
	}
	if first.Delivery.Attempts != 1 {
		t.Fatalf("expected attempt 1, got %d", first.Delivery.Attempts)
	}

	blocked, err := repo.AcquireForDelivery(context.Background(), repository.NotificationDeliveryClaimInput{Delivery: base, ClaimOwner: "worker-b", Now: now, ClaimDuration: time.Minute, MaxAttempts: 3})
	if err != nil {
		t.Fatalf("blocked acquire failed: %v", err)
	}
	if blocked.Outcome != repository.NotificationDeliveryClaimOutcomeClaimedByOther {
		t.Fatalf("expected claimed_by_other, got %q", blocked.Outcome)
	}

	claimTimestamp := *first.Delivery.ClaimedAt
	retryAt := now.Add(30 * time.Second)
	recorded, err := repo.RecordRetryableFailure(context.Background(), repository.NotificationDeliveryRecordFailureInput{
		DeliveryID:      first.Delivery.ID,
		ClaimOwner:      "worker-a",
		ClaimedAt:       claimTimestamp,
		FailedAt:        now,
		NextAttemptAt:   &retryAt,
		FailureCategory: domain.NotificationDeliveryFailureCategoryRetryable,
		FailureReason:   "email_send_failed",
		LastError:       strPtrMemory("smtp unavailable"),
	})
	if err != nil {
		t.Fatalf("record retryable failure failed: %v", err)
	}
	if recorded.Outcome != repository.NotificationDeliveryUpdateOutcomeUpdated || recorded.Delivery.Status != domain.NotificationDeliveryStatusRetryWaiting {
		t.Fatalf("expected retry_waiting updated result, got outcome=%q status=%q", recorded.Outcome, recorded.Delivery.Status)
	}

	notDue, err := repo.AcquireForDelivery(context.Background(), repository.NotificationDeliveryClaimInput{Delivery: base, ClaimOwner: "worker-b", Now: now, ClaimDuration: time.Minute, MaxAttempts: 3})
	if err != nil {
		t.Fatalf("retry not due acquire failed: %v", err)
	}
	if notDue.Outcome != repository.NotificationDeliveryClaimOutcomeRetryNotDue {
		t.Fatalf("expected retry_not_due, got %q", notDue.Outcome)
	}

	reclaimedAt := retryAt
	reclaimed, err := repo.AcquireForDelivery(context.Background(), repository.NotificationDeliveryClaimInput{Delivery: base, ClaimOwner: "worker-b", Now: reclaimedAt, ClaimDuration: time.Minute, MaxAttempts: 3})
	if err != nil {
		t.Fatalf("reclaim due retry failed: %v", err)
	}
	if reclaimed.Outcome != repository.NotificationDeliveryClaimOutcomeRetryClaimed {
		t.Fatalf("expected retry_claimed, got %q", reclaimed.Outcome)
	}
	if reclaimed.Delivery.Attempts != 2 {
		t.Fatalf("expected attempts 2 after reclaim, got %d", reclaimed.Delivery.Attempts)
	}

	lost, err := repo.MarkSent(context.Background(), repository.NotificationDeliveryMarkSentInput{DeliveryID: reclaimed.Delivery.ID, ClaimOwner: "worker-a", ClaimedAt: claimTimestamp, SentAt: reclaimedAt})
	if err != nil {
		t.Fatalf("old owner mark sent failed: %v", err)
	}
	if lost.Outcome != repository.NotificationDeliveryUpdateOutcomeLostClaim {
		t.Fatalf("expected lost_claim, got %q", lost.Outcome)
	}

	sent, err := repo.MarkSent(context.Background(), repository.NotificationDeliveryMarkSentInput{DeliveryID: reclaimed.Delivery.ID, ClaimOwner: "worker-b", ClaimedAt: *reclaimed.Delivery.ClaimedAt, SentAt: reclaimedAt})
	if err != nil {
		t.Fatalf("winning owner mark sent failed: %v", err)
	}
	if sent.Delivery.Status != domain.NotificationDeliveryStatusSent {
		t.Fatalf("expected sent status, got %q", sent.Delivery.Status)
	}
}

func TestNotificationDeliveryRepository_ConcurrentInitialClaimCreatesOneRow(t *testing.T) {
	repo := NewNotificationDeliveryRepository()
	now := time.Date(2026, 7, 2, 15, 0, 0, 0, time.UTC)
	base := domain.NotificationDelivery{
		BuildID:         "build-1",
		EventType:       domain.NotificationEventTypeBuildFailed,
		Transport:       domain.NotificationTransportEmail,
		DestinationKind: domain.NotificationDestinationKindSharedTarget,
		DestinationKey:  "email-target:target-1",
		Recipient:       "<dev@example.com>",
	}

	const workers = 8
	results := make(chan repository.NotificationDeliveryClaimOutcome, workers)
	var wg sync.WaitGroup
	for idx := 0; idx < workers; idx++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			result, err := repo.AcquireForDelivery(context.Background(), repository.NotificationDeliveryClaimInput{Delivery: base, ClaimOwner: "worker", Now: now, ClaimDuration: time.Minute, MaxAttempts: 3})
			if err != nil {
				t.Errorf("acquire failed: %v", err)
				return
			}
			results <- result.Outcome
		}(idx)
	}
	wg.Wait()
	close(results)

	created := 0
	blocked := 0
	for outcome := range results {
		switch outcome {
		case repository.NotificationDeliveryClaimOutcomeCreatedClaimed:
			created++
		case repository.NotificationDeliveryClaimOutcomeClaimedByOther:
			blocked++
		default:
			t.Fatalf("unexpected outcome %q", outcome)
		}
	}
	if created != 1 || blocked != workers-1 {
		t.Fatalf("expected one created claim and %d blocked claims, got created=%d blocked=%d", workers-1, created, blocked)
	}
}

func strPtrMemory(value string) *string {
	return &value
}

func TestNormalizeNotificationClaimInputAndHelpers(t *testing.T) {
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	valid := repository.NotificationDeliveryClaimInput{
		Delivery: domain.NotificationDelivery{
			BuildID:         " build-1 ",
			EventType:       domain.NotificationEventTypeBuildFailed,
			Transport:       domain.NotificationTransportEmail,
			DestinationKind: domain.NotificationDestinationKindSharedTarget,
			DestinationKey:  " email-target:target-1 ",
		},
		ClaimOwner:    " worker-a ",
		Now:           now,
		ClaimDuration: time.Minute,
		MaxAttempts:   3,
	}

	delivery, normalizedNow, claimOwner, claimDuration, err := normalizeNotificationClaimInput(valid)
	if err != nil {
		t.Fatalf("expected valid input, got %v", err)
	}
	if delivery.BuildID != "build-1" || delivery.DestinationKey != "email-target:target-1" {
		t.Fatalf("expected normalized delivery identity, got %+v", delivery)
	}
	if !normalizedNow.Equal(now) || claimOwner != "worker-a" || claimDuration != time.Minute {
		t.Fatalf("unexpected normalized claim input: now=%s owner=%q duration=%s", normalizedNow, claimOwner, claimDuration)
	}

	tests := []struct {
		name  string
		input repository.NotificationDeliveryClaimInput
		want  string
	}{
		{name: "invalid identity", input: repository.NotificationDeliveryClaimInput{Delivery: domain.NotificationDelivery{}, ClaimOwner: "worker-a", Now: now, ClaimDuration: time.Minute, MaxAttempts: 3}, want: "build id is required"},
		{name: "zero time", input: repository.NotificationDeliveryClaimInput{Delivery: valid.Delivery, ClaimOwner: "worker-a", ClaimDuration: time.Minute, MaxAttempts: 3}, want: "claim time is required"},
		{name: "blank owner", input: repository.NotificationDeliveryClaimInput{Delivery: valid.Delivery, ClaimOwner: "   ", Now: now, ClaimDuration: time.Minute, MaxAttempts: 3}, want: "claim owner is required"},
		{name: "non-positive duration", input: repository.NotificationDeliveryClaimInput{Delivery: valid.Delivery, ClaimOwner: "worker-a", Now: now, ClaimDuration: 0, MaxAttempts: 3}, want: "claim duration must be positive"},
		{name: "non-positive max attempts", input: repository.NotificationDeliveryClaimInput{Delivery: valid.Delivery, ClaimOwner: "worker-a", Now: now, ClaimDuration: time.Minute, MaxAttempts: 0}, want: "max attempts must be positive"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, _, claimErr := normalizeNotificationClaimInput(tc.input)
			if claimErr == nil || !strings.Contains(claimErr.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %v", tc.want, claimErr)
			}
		})
	}

	if got := normalizeNotificationRecordFailureTime(nil); got != nil {
		t.Fatalf("expected nil normalized failure time, got %v", got)
	}
	zero := time.Time{}
	if got := normalizeNotificationRecordFailureTime(&zero); got != nil {
		t.Fatalf("expected zero normalized failure time to be nil, got %v", got)
	}
	local := time.Date(2026, 7, 2, 12, 0, 0, 0, time.FixedZone("offset", 3600))
	if got := normalizeNotificationRecordFailureTime(&local); got == nil || got.Location() != time.UTC {
		t.Fatalf("expected UTC failure time, got %v", got)
	}
	if got := trimMemoryNotificationOptionalString(nil); got != nil {
		t.Fatalf("expected nil trimmed string, got %v", got)
	}
	blank := "   "
	if got := trimMemoryNotificationOptionalString(&blank); got != nil {
		t.Fatalf("expected blank string to normalize to nil, got %v", got)
	}
	trimmed := " error detail "
	if got := trimMemoryNotificationOptionalString(&trimmed); got == nil || *got != "error detail" {
		t.Fatalf("expected trimmed string, got %v", got)
	}
}

func TestClaimNotificationDeliveryBranches(t *testing.T) {
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	before := now.Add(-time.Minute)
	after := now.Add(time.Minute)

	tests := []struct {
		name          string
		existing      domain.NotificationDelivery
		wantOutcome   repository.NotificationDeliveryClaimOutcome
		wantStatus    domain.NotificationDeliveryStatus
		wantAttempts  int
		wantClaimedBy string
	}{
		{name: "sent remains terminal", existing: domain.NotificationDelivery{Status: domain.NotificationDeliveryStatusSent, Attempts: 1, MaxAttempts: 3}, wantOutcome: repository.NotificationDeliveryClaimOutcomeAlreadySent, wantStatus: domain.NotificationDeliveryStatusSent, wantAttempts: 1},
		{name: "attempts exhausted returns stored state unchanged", existing: domain.NotificationDelivery{Status: domain.NotificationDeliveryStatusPending, Attempts: 3, MaxAttempts: 3}, wantOutcome: repository.NotificationDeliveryClaimOutcomeAttemptsExhausted, wantStatus: domain.NotificationDeliveryStatusPending, wantAttempts: 3},
		{name: "active send claim blocked", existing: domain.NotificationDelivery{Status: domain.NotificationDeliveryStatusSending, Attempts: 1, MaxAttempts: 3, ClaimExpiresAt: &after}, wantOutcome: repository.NotificationDeliveryClaimOutcomeClaimedByOther, wantStatus: domain.NotificationDeliveryStatusSending, wantAttempts: 1},
		{name: "stale send claim reclaimed", existing: domain.NotificationDelivery{Status: domain.NotificationDeliveryStatusSending, Attempts: 1, MaxAttempts: 3, ClaimExpiresAt: &before}, wantOutcome: repository.NotificationDeliveryClaimOutcomeStaleClaimReclaimed, wantStatus: domain.NotificationDeliveryStatusSending, wantAttempts: 2, wantClaimedBy: "worker-b"},
		{name: "retry waiting not due blocked", existing: domain.NotificationDelivery{Status: domain.NotificationDeliveryStatusRetryWaiting, Attempts: 1, MaxAttempts: 3, NextAttemptAt: &after}, wantOutcome: repository.NotificationDeliveryClaimOutcomeRetryNotDue, wantStatus: domain.NotificationDeliveryStatusRetryWaiting, wantAttempts: 1},
		{name: "retry waiting due claimed", existing: domain.NotificationDelivery{Status: domain.NotificationDeliveryStatusRetryWaiting, Attempts: 1, MaxAttempts: 3, NextAttemptAt: &before}, wantOutcome: repository.NotificationDeliveryClaimOutcomeRetryClaimed, wantStatus: domain.NotificationDeliveryStatusSending, wantAttempts: 2, wantClaimedBy: "worker-b"},
		{name: "pending claimed", existing: domain.NotificationDelivery{Status: domain.NotificationDeliveryStatusPending, Attempts: 0, MaxAttempts: 3}, wantOutcome: repository.NotificationDeliveryClaimOutcomeRetryClaimed, wantStatus: domain.NotificationDeliveryStatusSending, wantAttempts: 1, wantClaimedBy: "worker-b"},
		{name: "unknown defaults to helper outcome", existing: domain.NotificationDelivery{Status: domain.NotificationDeliveryStatus("mystery"), Attempts: 1, MaxAttempts: 3}, wantOutcome: repository.NotificationDeliveryClaimOutcomeClaimedByOther, wantStatus: domain.NotificationDeliveryStatus("mystery"), wantAttempts: 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			claimed, outcome := claimNotificationDelivery(tc.existing, now, "worker-b", time.Minute)
			if outcome != tc.wantOutcome {
				t.Fatalf("expected outcome %q, got %q", tc.wantOutcome, outcome)
			}
			if claimed.Status != tc.wantStatus || claimed.Attempts != tc.wantAttempts {
				t.Fatalf("unexpected claimed delivery: %+v", claimed)
			}
			if tc.wantClaimedBy != "" {
				if claimed.ClaimedBy == nil || *claimed.ClaimedBy != tc.wantClaimedBy {
					t.Fatalf("expected claimed by %q, got %v", tc.wantClaimedBy, claimed.ClaimedBy)
				}
				if claimed.ClaimExpiresAt == nil || !claimed.ClaimExpiresAt.Equal(now.Add(time.Minute)) {
					t.Fatalf("expected claim expiry at %s, got %v", now.Add(time.Minute), claimed.ClaimExpiresAt)
				}
			}
		})
	}

	claimOwner := "worker-a"
	claimedAt := now
	active := domain.NotificationDelivery{Status: domain.NotificationDeliveryStatusSending, ClaimedBy: &claimOwner, ClaimedAt: &claimedAt}
	if !notificationDeliveryClaimMatches(active, "worker-a", claimedAt) {
		t.Fatal("expected sending claim to match exact owner and timestamp")
	}
	if notificationDeliveryClaimMatches(active, "worker-b", claimedAt) {
		t.Fatal("expected mismatched owner to fail claim match")
	}
	if notificationDeliveryClaimMatches(domain.NotificationDelivery{Status: domain.NotificationDeliveryStatusPending}, "worker-a", claimedAt) {
		t.Fatal("expected non-sending status to fail claim match")
	}

	validExhausted := domain.NotificationDelivery{
		BuildID:         "build-1",
		EventType:       domain.NotificationEventTypeBuildFailed,
		Transport:       domain.NotificationTransportEmail,
		DestinationKind: domain.NotificationDestinationKindSharedTarget,
		DestinationKey:  "email-target:target-1",
		Recipient:       "<dev@example.com>",
		Status:          domain.NotificationDeliveryStatusPending,
		Attempts:        3,
		MaxAttempts:     3,
	}
	claimed, outcome := claimNotificationDelivery(validExhausted, now, "worker-b", time.Minute)
	if outcome != repository.NotificationDeliveryClaimOutcomeAttemptsExhausted {
		t.Fatalf("expected exhausted outcome for valid delivery, got %q", outcome)
	}
	if claimed.Status != domain.NotificationDeliveryStatusPending {
		t.Fatalf("expected exhausted non-claim path to leave stored status unchanged, got %q", claimed.Status)
	}
	if validateErr := claimed.Validate(); validateErr != nil {
		t.Fatalf("expected unchanged exhausted delivery to remain valid, got %v", validateErr)
	}
}

func TestNotificationDeliveryRepository_ListRecoverable(t *testing.T) {
	repo := NewNotificationDeliveryRepository()
	now := time.Date(2026, 7, 2, 16, 0, 0, 0, time.UTC)
	buildID := "build-recoverable"
	retryable := domain.NotificationDeliveryFailureCategoryRetryable
	claimOwner := "worker-a"

	mustCreate := func(t *testing.T, delivery domain.NotificationDelivery) {
		t.Helper()
		if _, err := repo.Create(context.Background(), delivery); err != nil {
			t.Fatalf("create delivery failed: %v", err)
		}
	}
	mustUpdate := func(t *testing.T, delivery domain.NotificationDelivery) {
		t.Helper()
		if _, err := repo.Update(context.Background(), delivery); err != nil {
			t.Fatalf("update delivery failed: %v", err)
		}
	}

	dueRetryAt := now.Add(-2 * time.Minute)
	staleClaimAt := now.Add(-90 * time.Second)
	staleClaimExpiresAt := now.Add(-time.Minute)
	futureRetryAt := now.Add(time.Minute)
	activeClaimAt := now.Add(-30 * time.Second)
	activeClaimExpiresAt := now.Add(time.Minute)

	mustCreate(t, domain.NotificationDelivery{ID: "retry-due-b", BuildID: buildID, EventType: domain.NotificationEventTypeBuildFailed, Transport: domain.NotificationTransportEmail, DestinationKind: domain.NotificationDestinationKindSharedTarget, DestinationKey: "email-target:retry-due-b", Recipient: "retry-due-b@example.com", MaxAttempts: 3})
	mustUpdate(t, domain.NotificationDelivery{ID: "retry-due-b", BuildID: buildID, EventType: domain.NotificationEventTypeBuildFailed, Transport: domain.NotificationTransportEmail, DestinationKind: domain.NotificationDestinationKindSharedTarget, DestinationKey: "email-target:retry-due-b", Recipient: "retry-due-b@example.com", Status: domain.NotificationDeliveryStatusRetryWaiting, Attempts: 1, MaxAttempts: 3, LastAttemptAt: &staleClaimAt, NextAttemptAt: &dueRetryAt, FailureCategory: &retryable, UpdatedAt: dueRetryAt})

	earlierRetryAt := now.Add(-3 * time.Minute)
	mustCreate(t, domain.NotificationDelivery{ID: "retry-due-a", BuildID: buildID, EventType: domain.NotificationEventTypeBuildFailed, Transport: domain.NotificationTransportEmail, DestinationKind: domain.NotificationDestinationKindSharedTarget, DestinationKey: "email-target:retry-due-a", Recipient: "retry-due-a@example.com", MaxAttempts: 3})
	mustUpdate(t, domain.NotificationDelivery{ID: "retry-due-a", BuildID: buildID, EventType: domain.NotificationEventTypeBuildFailed, Transport: domain.NotificationTransportEmail, DestinationKind: domain.NotificationDestinationKindSharedTarget, DestinationKey: "email-target:retry-due-a", Recipient: "retry-due-a@example.com", Status: domain.NotificationDeliveryStatusRetryWaiting, Attempts: 1, MaxAttempts: 3, LastAttemptAt: &staleClaimAt, NextAttemptAt: &earlierRetryAt, FailureCategory: &retryable, UpdatedAt: earlierRetryAt})

	mustCreate(t, domain.NotificationDelivery{ID: "retry-future", BuildID: buildID, EventType: domain.NotificationEventTypeBuildFailed, Transport: domain.NotificationTransportEmail, DestinationKind: domain.NotificationDestinationKindSharedTarget, DestinationKey: "email-target:retry-future", Recipient: "retry-future@example.com", MaxAttempts: 3})
	mustUpdate(t, domain.NotificationDelivery{ID: "retry-future", BuildID: buildID, EventType: domain.NotificationEventTypeBuildFailed, Transport: domain.NotificationTransportEmail, DestinationKind: domain.NotificationDestinationKindSharedTarget, DestinationKey: "email-target:retry-future", Recipient: "retry-future@example.com", Status: domain.NotificationDeliveryStatusRetryWaiting, Attempts: 1, MaxAttempts: 3, LastAttemptAt: &staleClaimAt, NextAttemptAt: &futureRetryAt, FailureCategory: &retryable, UpdatedAt: futureRetryAt})

	mustCreate(t, domain.NotificationDelivery{ID: "claim-stale", BuildID: buildID, EventType: domain.NotificationEventTypeBuildFailed, Transport: domain.NotificationTransportSlackWebhook, DestinationKind: domain.NotificationDestinationKindSharedTarget, DestinationKey: "slack-webhook:claim-stale", Recipient: "slack_webhook:claim-stale", MaxAttempts: 3})
	mustUpdate(t, domain.NotificationDelivery{ID: "claim-stale", BuildID: buildID, EventType: domain.NotificationEventTypeBuildFailed, Transport: domain.NotificationTransportSlackWebhook, DestinationKind: domain.NotificationDestinationKindSharedTarget, DestinationKey: "slack-webhook:claim-stale", Recipient: "slack_webhook:claim-stale", Status: domain.NotificationDeliveryStatusSending, Attempts: 1, MaxAttempts: 3, LastAttemptAt: &staleClaimAt, ClaimedAt: &staleClaimAt, ClaimExpiresAt: &staleClaimExpiresAt, ClaimedBy: &claimOwner, UpdatedAt: staleClaimAt})

	mustCreate(t, domain.NotificationDelivery{ID: "claim-active", BuildID: buildID, EventType: domain.NotificationEventTypeBuildFailed, Transport: domain.NotificationTransportSlackWebhook, DestinationKind: domain.NotificationDestinationKindSharedTarget, DestinationKey: "slack-webhook:claim-active", Recipient: "slack_webhook:claim-active", MaxAttempts: 3})
	mustUpdate(t, domain.NotificationDelivery{ID: "claim-active", BuildID: buildID, EventType: domain.NotificationEventTypeBuildFailed, Transport: domain.NotificationTransportSlackWebhook, DestinationKind: domain.NotificationDestinationKindSharedTarget, DestinationKey: "slack-webhook:claim-active", Recipient: "slack_webhook:claim-active", Status: domain.NotificationDeliveryStatusSending, Attempts: 1, MaxAttempts: 3, LastAttemptAt: &activeClaimAt, ClaimedAt: &activeClaimAt, ClaimExpiresAt: &activeClaimExpiresAt, ClaimedBy: &claimOwner, UpdatedAt: activeClaimAt})

	mustCreate(t, domain.NotificationDelivery{ID: "sent-terminal", BuildID: buildID, EventType: domain.NotificationEventTypeBuildFailed, Transport: domain.NotificationTransportEmail, DestinationKind: domain.NotificationDestinationKindSharedTarget, DestinationKey: "email-target:sent-terminal", Recipient: "sent@example.com", Status: domain.NotificationDeliveryStatusSent, Attempts: 1, MaxAttempts: 1, SentAt: &staleClaimAt, CreatedAt: staleClaimAt, UpdatedAt: staleClaimAt})

	result, err := repo.ListRecoverable(context.Background(), repository.NotificationDeliveryRecoverableScanInput{Now: now, Limit: 10})
	if err != nil {
		t.Fatalf("list recoverable failed: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 recoverable deliveries, got %d", len(result))
	}
	gotIDs := []string{result[0].ID, result[1].ID, result[2].ID}
	wantIDs := []string{"retry-due-a", "retry-due-b", "claim-stale"}
	if strings.Join(gotIDs, ",") != strings.Join(wantIDs, ",") {
		t.Fatalf("expected ordered ids %v, got %v", wantIDs, gotIDs)
	}

	limited, err := repo.ListRecoverable(context.Background(), repository.NotificationDeliveryRecoverableScanInput{Now: now, Limit: 2})
	if err != nil {
		t.Fatalf("list recoverable with limit failed: %v", err)
	}
	if len(limited) != 2 || limited[0].ID != "retry-due-a" || limited[1].ID != "retry-due-b" {
		t.Fatalf("unexpected limited recoverable result: %+v", limited)
	}

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := repo.ListRecoverable(canceledCtx, repository.NotificationDeliveryRecoverableScanInput{Now: now, Limit: 1}); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled context error, got %v", err)
	}
}

func TestNotificationDeliveryRepository_RecordFailureBranches(t *testing.T) {
	repo := NewNotificationDeliveryRepository()
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	base := domain.NotificationDelivery{
		BuildID:         "build-1",
		EventType:       domain.NotificationEventTypeBuildFailed,
		Transport:       domain.NotificationTransportEmail,
		DestinationKind: domain.NotificationDestinationKindSharedTarget,
		DestinationKey:  "email-target:target-1",
		Recipient:       "<dev@example.com>",
	}

	claimed, err := repo.AcquireForDelivery(context.Background(), repository.NotificationDeliveryClaimInput{Delivery: base, ClaimOwner: "worker-a", Now: now, ClaimDuration: time.Minute, MaxAttempts: 3})
	if err != nil {
		t.Fatalf("acquire delivery failed: %v", err)
	}
	claimTimestamp := *claimed.Delivery.ClaimedAt

	permanent, err := repo.RecordPermanentFailure(context.Background(), repository.NotificationDeliveryRecordFailureInput{
		DeliveryID:      claimed.Delivery.ID,
		ClaimOwner:      "worker-a",
		ClaimedAt:       claimTimestamp,
		FailedAt:        now,
		FailureCategory: domain.NotificationDeliveryFailureCategoryPermanent,
		FailureReason:   " invalid_request ",
		LastError:       strPtrMemory(" smtp rejected "),
	})
	if err != nil {
		t.Fatalf("record permanent failure failed: %v", err)
	}
	if permanent.Outcome != repository.NotificationDeliveryUpdateOutcomeUpdated || permanent.Delivery.Status != domain.NotificationDeliveryStatusFailedPermanent {
		t.Fatalf("unexpected permanent failure result: %+v", permanent)
	}
	if permanent.Delivery.ClaimedAt != nil || permanent.Delivery.ClaimExpiresAt != nil || permanent.Delivery.ClaimedBy != nil {
		t.Fatalf("expected permanent failure to clear claim metadata, got %+v", permanent.Delivery)
	}
	if permanent.Delivery.FailureReason == nil || *permanent.Delivery.FailureReason != "invalid_request" {
		t.Fatalf("expected trimmed failure reason, got %v", permanent.Delivery.FailureReason)
	}
	if permanent.Delivery.LastError == nil || *permanent.Delivery.LastError != "smtp rejected" {
		t.Fatalf("expected trimmed last error, got %v", permanent.Delivery.LastError)
	}

	claimedExhausted, err := repo.AcquireForDelivery(context.Background(), repository.NotificationDeliveryClaimInput{Delivery: withDifferentMemoryDelivery(base, "build-2"), ClaimOwner: "worker-a", Now: now, ClaimDuration: time.Minute, MaxAttempts: 3})
	if err != nil {
		t.Fatalf("acquire exhausted delivery failed: %v", err)
	}
	exhausted, err := repo.RecordExhaustedFailure(context.Background(), repository.NotificationDeliveryRecordFailureInput{
		DeliveryID:      claimedExhausted.Delivery.ID,
		ClaimOwner:      "worker-a",
		ClaimedAt:       *claimedExhausted.Delivery.ClaimedAt,
		FailedAt:        now,
		FailureCategory: domain.NotificationDeliveryFailureCategoryRetryable,
		FailureReason:   "temporary",
		LastError:       strPtrMemory(" smtp unavailable "),
	})
	if err != nil {
		t.Fatalf("record exhausted failure failed: %v", err)
	}
	if exhausted.Delivery.Status != domain.NotificationDeliveryStatusFailedExhausted || exhausted.Delivery.Attempts != exhausted.Delivery.MaxAttempts {
		t.Fatalf("expected exhausted state to force attempts to max, got %+v", exhausted.Delivery)
	}
	if exhausted.Delivery.NextAttemptAt != nil {
		t.Fatalf("expected exhausted failure to clear next attempt, got %v", exhausted.Delivery.NextAttemptAt)
	}

	lost, err := repo.RecordPermanentFailure(context.Background(), repository.NotificationDeliveryRecordFailureInput{
		DeliveryID:      claimedExhausted.Delivery.ID,
		ClaimOwner:      "worker-b",
		ClaimedAt:       *claimedExhausted.Delivery.ClaimedAt,
		FailedAt:        now,
		FailureCategory: domain.NotificationDeliveryFailureCategoryPermanent,
	})
	if err != nil {
		t.Fatalf("record permanent failure with lost claim failed: %v", err)
	}
	if lost.Outcome != repository.NotificationDeliveryUpdateOutcomeLostClaim {
		t.Fatalf("expected lost claim outcome, got %+v", lost)
	}

	if _, err := repo.RecordPermanentFailure(context.Background(), repository.NotificationDeliveryRecordFailureInput{DeliveryID: "missing", ClaimOwner: "worker-a", ClaimedAt: now, FailedAt: now, FailureCategory: domain.NotificationDeliveryFailureCategoryPermanent}); !errors.Is(err, repository.ErrNotificationDeliveryNotFound) {
		t.Fatalf("expected not found error, got %v", err)
	}
}

func withDifferentMemoryDelivery(base domain.NotificationDelivery, buildID string) domain.NotificationDelivery {
	copy := base
	copy.BuildID = buildID
	copy.DestinationKey = "email-target:" + buildID
	copy.Recipient = "<" + buildID + "@example.com>"
	return copy
}
