package memory

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func TestSCMStatusDeliveryRepository_ClaimAndStateUpdates(t *testing.T) {
	repo := NewSCMStatusDeliveryRepository()
	now := time.Date(2026, 7, 16, 14, 0, 0, 0, time.UTC)
	base := domain.SCMStatusDelivery{
		BuildID:         "build-1",
		BuildAttempt:    1,
		BuildCreatedAt:  now,
		Provider:        "github",
		RepositoryOwner: "octo",
		RepositoryName:  "repo",
		CommitSHA:       "abcdef",
		Context:         "coyote/default/job-1",
		DesiredState:    domain.SCMCommitStatusStatePending,
		Description:     "Coyote build is in progress",
	}

	claimed, err := repo.AcquireForDelivery(context.Background(), repository.SCMStatusDeliveryClaimInput{Delivery: base, ClaimOwner: "worker-a", Now: now, ClaimDuration: time.Minute, MaxAttempts: 3})
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}
	if claimed.Outcome != repository.SCMStatusDeliveryClaimOutcomeCreatedClaimed {
		t.Fatalf("expected created_claimed, got %q", claimed.Outcome)
	}

	blocked, err := repo.AcquireForDelivery(context.Background(), repository.SCMStatusDeliveryClaimInput{Delivery: base, ClaimOwner: "worker-b", Now: now, ClaimDuration: time.Minute, MaxAttempts: 3})
	if err != nil {
		t.Fatalf("blocked acquire failed: %v", err)
	}
	if blocked.Outcome != repository.SCMStatusDeliveryClaimOutcomeClaimedByOther {
		t.Fatalf("expected claimed_by_other, got %q", blocked.Outcome)
	}

	sent, err := repo.MarkSent(context.Background(), repository.SCMStatusDeliveryMarkSentInput{DeliveryID: claimed.Delivery.ID, ClaimOwner: "worker-a", ClaimedAt: *claimed.Delivery.ClaimedAt, SentAt: now, State: domain.SCMCommitStatusStatePending})
	if err != nil {
		t.Fatalf("mark sent failed: %v", err)
	}
	if sent.Delivery.Status != domain.SCMStatusDeliveryStatusSent {
		t.Fatalf("expected sent status, got %q", sent.Delivery.Status)
	}

	fetched, err := repo.GetByKey(context.Background(), "github", "octo", "repo", "abcdef", "coyote/default/job-1")
	if err != nil {
		t.Fatalf("get by stream key failed: %v", err)
	}
	if fetched.ID != claimed.Delivery.ID {
		t.Fatalf("expected claimed delivery id, got %q", fetched.ID)
	}
}

func TestSCMStatusDeliveryRepository_RetryAndSupersede(t *testing.T) {
	repo := NewSCMStatusDeliveryRepository()
	now := time.Date(2026, 7, 16, 15, 0, 0, 0, time.UTC)
	base := domain.SCMStatusDelivery{
		BuildID:         "build-1",
		BuildAttempt:    1,
		BuildCreatedAt:  now,
		Provider:        "github",
		RepositoryOwner: "octo",
		RepositoryName:  "repo",
		CommitSHA:       "abcdef",
		Context:         "coyote/default/job-1",
		DesiredState:    domain.SCMCommitStatusStateFailure,
		Description:     "Coyote build failed",
	}

	claimed, err := repo.AcquireForDelivery(context.Background(), repository.SCMStatusDeliveryClaimInput{Delivery: base, ClaimOwner: "worker-a", Now: now, ClaimDuration: time.Minute, MaxAttempts: 3})
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}
	retryAt := now.Add(30 * time.Second)
	updated, err := repo.RecordRetryableFailure(context.Background(), repository.SCMStatusDeliveryRecordFailureInput{DeliveryID: claimed.Delivery.ID, ClaimOwner: "worker-a", ClaimedAt: *claimed.Delivery.ClaimedAt, FailedAt: now, NextAttemptAt: &retryAt, FailureCategory: domain.SCMStatusDeliveryFailureCategoryRetryable, FailureReason: "github_api_unavailable", LastError: strPtrSCMMemory("api unavailable")})
	if err != nil {
		t.Fatalf("record retryable failure failed: %v", err)
	}
	if updated.Delivery.Status != domain.SCMStatusDeliveryStatusRetryWaiting {
		t.Fatalf("expected retry_waiting, got %q", updated.Delivery.Status)
	}

	reclaimed, err := repo.AcquireForDelivery(context.Background(), repository.SCMStatusDeliveryClaimInput{Delivery: base, ClaimOwner: "worker-b", Now: retryAt, ClaimDuration: time.Minute, MaxAttempts: 3})
	if err != nil {
		t.Fatalf("reclaim failed: %v", err)
	}
	if reclaimed.Outcome != repository.SCMStatusDeliveryClaimOutcomeRetryClaimed {
		t.Fatalf("expected retry_claimed, got %q", reclaimed.Outcome)
	}

	superseded, err := repo.MarkSuperseded(context.Background(), repository.SCMStatusDeliveryMarkSupersededInput{DeliveryID: reclaimed.Delivery.ID, ClaimOwner: strPtrSCMMemory("worker-b"), ClaimedAt: reclaimed.Delivery.ClaimedAt, SupersededAt: retryAt, Reason: "newer_build_attempt_exists"})
	if err != nil {
		t.Fatalf("mark superseded failed: %v", err)
	}
	if superseded.Delivery.Status != domain.SCMStatusDeliveryStatusSuperseded {
		t.Fatalf("expected superseded status, got %q", superseded.Delivery.Status)
	}

	skipped, err := repo.AcquireForDelivery(context.Background(), repository.SCMStatusDeliveryClaimInput{Delivery: base, ClaimOwner: "worker-c", Now: retryAt.Add(time.Minute), ClaimDuration: time.Minute, MaxAttempts: 3})
	if err != nil {
		t.Fatalf("post-supersede acquire failed: %v", err)
	}
	if skipped.Outcome != repository.SCMStatusDeliveryClaimOutcomeSuperseded {
		t.Fatalf("expected superseded outcome, got %q", skipped.Outcome)
	}
}

func TestSCMStatusDeliveryRepository_ConcurrentInitialClaimCreatesOneRow(t *testing.T) {
	repo := NewSCMStatusDeliveryRepository()
	now := time.Date(2026, 7, 16, 16, 0, 0, 0, time.UTC)
	base := domain.SCMStatusDelivery{
		BuildID:         "build-1",
		BuildAttempt:    1,
		BuildCreatedAt:  now,
		Provider:        "github",
		RepositoryOwner: "octo",
		RepositoryName:  "repo",
		CommitSHA:       "abcdef",
		Context:         "coyote/default/job-1",
		DesiredState:    domain.SCMCommitStatusStatePending,
		Description:     "Coyote build is in progress",
	}

	const workers = 6
	results := make(chan repository.SCMStatusDeliveryClaimOutcome, workers)
	var wg sync.WaitGroup
	for idx := 0; idx < workers; idx++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := repo.AcquireForDelivery(context.Background(), repository.SCMStatusDeliveryClaimInput{Delivery: base, ClaimOwner: "worker", Now: now, ClaimDuration: time.Minute, MaxAttempts: 2})
			if err != nil {
				t.Errorf("acquire failed: %v", err)
				return
			}
			results <- result.Outcome
		}()
	}
	wg.Wait()
	close(results)

	created := 0
	blocked := 0
	for outcome := range results {
		switch outcome {
		case repository.SCMStatusDeliveryClaimOutcomeCreatedClaimed:
			created++
		case repository.SCMStatusDeliveryClaimOutcomeClaimedByOther:
			blocked++
		default:
			t.Fatalf("unexpected outcome %q", outcome)
		}
	}
	if created != 1 || blocked != workers-1 {
		t.Fatalf("expected one created claim and %d blocked claims, got created=%d blocked=%d", workers-1, created, blocked)
	}
}

func strPtrSCMMemory(value string) *string {
	return &value
}
