package memory

import (
	"context"
	"errors"
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
