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

	byBuild, getByBuildErr := repo.GetByBuildID(context.Background(), "build-1")
	if getByBuildErr != nil {
		t.Fatalf("get by build id failed: %v", getByBuildErr)
	}
	if byBuild.ID != claimed.Delivery.ID {
		t.Fatalf("expected claimed delivery id by build, got %q", byBuild.ID)
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

func TestSCMStatusDeliveryRepository_RecoverableAndFailures(t *testing.T) {
	repo := NewSCMStatusDeliveryRepository()
	now := time.Date(2026, 7, 16, 17, 0, 0, 0, time.UTC)
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
		Description:     "Coyote build is pending",
	}

	claimed, claimErr := repo.AcquireForDelivery(context.Background(), repository.SCMStatusDeliveryClaimInput{Delivery: base, ClaimOwner: "worker-a", Now: now, ClaimDuration: time.Minute, MaxAttempts: 3})
	if claimErr != nil {
		t.Fatalf("initial claim failed: %v", claimErr)
	}
	retryAt := now.Add(30 * time.Second)
	retryResult, retryErr := repo.RecordRetryableFailure(context.Background(), repository.SCMStatusDeliveryRecordFailureInput{DeliveryID: claimed.Delivery.ID, ClaimOwner: "worker-a", ClaimedAt: *claimed.Delivery.ClaimedAt, FailedAt: now, NextAttemptAt: &retryAt, FailureCategory: domain.SCMStatusDeliveryFailureCategoryRetryable, FailureReason: "retry_later", LastError: strPtrSCMMemory("temporary")})
	if retryErr != nil {
		t.Fatalf("retryable failure failed: %v", retryErr)
	}
	if retryResult.Delivery.Status != domain.SCMStatusDeliveryStatusRetryWaiting {
		t.Fatalf("expected retry waiting status, got %q", retryResult.Delivery.Status)
	}

	second := base
	second.BuildID = "build-2"
	second.CommitSHA = "123456"
	staleClaim, staleErr := repo.AcquireForDelivery(context.Background(), repository.SCMStatusDeliveryClaimInput{Delivery: second, ClaimOwner: "worker-b", Now: now, ClaimDuration: time.Minute, MaxAttempts: 3})
	if staleErr != nil {
		t.Fatalf("stale claim failed: %v", staleErr)
	}

	recoverable, listErr := repo.ListRecoverable(context.Background(), repository.SCMStatusDeliveryRecoverableScanInput{Now: now.Add(2 * time.Minute), Limit: 5})
	if listErr != nil {
		t.Fatalf("list recoverable failed: %v", listErr)
	}
	if len(recoverable) != 2 || recoverable[0].ID != retryResult.Delivery.ID || recoverable[1].ID != staleClaim.Delivery.ID {
		t.Fatalf("unexpected recoverable deliveries: %+v", recoverable)
	}

	permanentBase := base
	permanentBase.BuildID = "build-3"
	permanentBase.CommitSHA = "999999"
	permanentClaim, permanentClaimErr := repo.AcquireForDelivery(context.Background(), repository.SCMStatusDeliveryClaimInput{Delivery: permanentBase, ClaimOwner: "worker-c", Now: now, ClaimDuration: time.Minute, MaxAttempts: 2})
	if permanentClaimErr != nil {
		t.Fatalf("permanent claim failed: %v", permanentClaimErr)
	}
	permanent, permanentErr := repo.RecordPermanentFailure(context.Background(), repository.SCMStatusDeliveryRecordFailureInput{DeliveryID: permanentClaim.Delivery.ID, ClaimOwner: "worker-c", ClaimedAt: *permanentClaim.Delivery.ClaimedAt, FailedAt: now, FailureCategory: domain.SCMStatusDeliveryFailureCategoryPermanent, FailureReason: "bad_request", LastError: strPtrSCMMemory("nope")})
	if permanentErr != nil {
		t.Fatalf("permanent failure failed: %v", permanentErr)
	}
	if permanent.Delivery.Status != domain.SCMStatusDeliveryStatusFailedPermanent {
		t.Fatalf("expected permanent status, got %q", permanent.Delivery.Status)
	}

	exhaustedBase := base
	exhaustedBase.BuildID = "build-4"
	exhaustedBase.CommitSHA = "777777"
	exhaustedClaim, exhaustedClaimErr := repo.AcquireForDelivery(context.Background(), repository.SCMStatusDeliveryClaimInput{Delivery: exhaustedBase, ClaimOwner: "worker-d", Now: now, ClaimDuration: time.Minute, MaxAttempts: 4})
	if exhaustedClaimErr != nil {
		t.Fatalf("exhausted claim failed: %v", exhaustedClaimErr)
	}
	exhaustedDelivery := repo.deliveries[exhaustedClaim.Delivery.ID]
	exhaustedDelivery.Attempts = 2
	repo.deliveries[exhaustedClaim.Delivery.ID] = exhaustedDelivery
	exhausted, exhaustedErr := repo.RecordExhaustedFailure(context.Background(), repository.SCMStatusDeliveryRecordFailureInput{DeliveryID: exhaustedClaim.Delivery.ID, ClaimOwner: "worker-d", ClaimedAt: *exhaustedClaim.Delivery.ClaimedAt, FailedAt: now, FailureCategory: domain.SCMStatusDeliveryFailureCategoryRetryable, FailureReason: "retries_exhausted", LastError: strPtrSCMMemory("still failing")})
	if exhaustedErr != nil {
		t.Fatalf("exhausted failure failed: %v", exhaustedErr)
	}
	if exhausted.Delivery.Status != domain.SCMStatusDeliveryStatusFailedExhausted || exhausted.Delivery.Attempts != exhausted.Delivery.MaxAttempts {
		t.Fatalf("expected exhausted delivery to clamp attempts, got %+v", exhausted.Delivery)
	}

	if _, err := repo.GetByKey(context.Background(), "github", "octo", "repo", "missing", "ctx"); !errors.Is(err, repository.ErrSCMStatusDeliveryNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestSCMStatusDeliveryRepository_InternalHelpers(t *testing.T) {
	now := time.Date(2026, 7, 16, 18, 0, 0, 0, time.UTC)
	claimExpiresAt := now.Add(time.Minute)
	retryAt := now.Add(-time.Minute)
	claimOwner := "worker-a"
	lastSentState := domain.SCMCommitStatusStateSuccess

	if _, _, _, _, err := normalizeSCMClaimInput(repository.SCMStatusDeliveryClaimInput{}); err == nil {
		t.Fatal("expected invalid claim input error")
	}
	valid := domain.SCMStatusDelivery{BuildID: "build-1", BuildAttempt: 1, BuildCreatedAt: now, Provider: "github", RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "abcdef", Context: "ctx", DesiredState: domain.SCMCommitStatusStatePending}
	if _, _, _, _, err := normalizeSCMClaimInput(repository.SCMStatusDeliveryClaimInput{Delivery: valid, ClaimOwner: "worker-a", Now: now, ClaimDuration: time.Minute, MaxAttempts: 2}); err != nil {
		t.Fatalf("expected valid claim input, got %v", err)
	}

	claimed, outcome := claimSCMStatusDelivery(domain.SCMStatusDelivery{Status: domain.SCMStatusDeliveryStatusRetryWaiting, Attempts: 1, MaxAttempts: 3, NextAttemptAt: &retryAt}, now, claimOwner, time.Minute)
	if outcome != repository.SCMStatusDeliveryClaimOutcomeRetryClaimed || claimed.Status != domain.SCMStatusDeliveryStatusSending {
		t.Fatalf("unexpected retry claim result: outcome=%q delivery=%+v", outcome, claimed)
	}
	if _, outcome = claimSCMStatusDelivery(domain.SCMStatusDelivery{Status: domain.SCMStatusDeliveryStatusSending, Attempts: 1, MaxAttempts: 3, ClaimExpiresAt: &claimExpiresAt}, now, claimOwner, time.Minute); outcome != repository.SCMStatusDeliveryClaimOutcomeClaimedByOther {
		t.Fatalf("expected claimed by other, got %q", outcome)
	}

	replaced, replaceOutcome, persist, reassertAfter, replaceErr := reconcileSCMStatusDeliveryForClaim(
		domain.SCMStatusDelivery{ID: "delivery-1", BuildID: "build-1", BuildAttempt: 1, BuildCreatedAt: now, Provider: "github", RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "abcdef", Context: "ctx", DesiredState: domain.SCMCommitStatusStatePending, Status: domain.SCMStatusDeliveryStatusSending, ClaimExpiresAt: &claimExpiresAt, LastSentState: &lastSentState},
		domain.SCMStatusDelivery{BuildID: "build-1", BuildAttempt: 1, BuildCreatedAt: now, Provider: "github", RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "abcdef", Context: "ctx", DesiredState: domain.SCMCommitStatusStateFailure},
		now,
		claimOwner,
		time.Minute,
		3,
	)
	if replaceErr != nil || replaceOutcome != repository.SCMStatusDeliveryClaimOutcomeCreatedClaimed || !persist || reassertAfter == nil || replaced.LastSentState == nil || *replaced.LastSentState != lastSentState {
		t.Fatalf("unexpected reconcile replacement result: delivery=%+v outcome=%q persist=%v reassertAfter=%v err=%v", replaced, replaceOutcome, persist, reassertAfter, replaceErr)
	}

	fresh, freshErr := claimFreshSCMStatusDelivery("", time.Time{}, valid, now, claimOwner, time.Minute, 2, nil)
	if freshErr != nil || fresh.ID == "" || fresh.Attempts != 1 {
		t.Fatalf("unexpected fresh claim result: %+v err=%v", fresh, freshErr)
	}
	if dueAt, ok := scmStatusDeliveryRecoverableDueAt(domain.SCMStatusDelivery{Status: domain.SCMStatusDeliveryStatusRetryWaiting, NextAttemptAt: &retryAt}, now); !ok || !dueAt.Equal(retryAt.UTC()) {
		t.Fatalf("expected retry delivery due at %v, got %v %v", retryAt.UTC(), dueAt, ok)
	}
	if dueAt, ok := scmStatusDeliveryRecoverableDueAt(domain.SCMStatusDelivery{Status: domain.SCMStatusDeliveryStatusSending, ClaimExpiresAt: &retryAt}, now); !ok || !dueAt.Equal(retryAt.UTC()) {
		t.Fatalf("expected stale claim due at %v, got %v %v", retryAt.UTC(), dueAt, ok)
	}
	if !scmStatusDeliveryClaimMatches(domain.SCMStatusDelivery{ClaimedBy: &claimOwner, ClaimedAt: &now}, claimOwner, now) {
		t.Fatal("expected claim to match")
	}
	if scmStatusDeliveryStreamKey(" github ", " octo ", " repo ", " abcdef ", " ctx ") != "github|octo|repo|abcdef|ctx" {
		t.Fatal("unexpected stream key normalization")
	}
	if normalizeSCMRecordFailureTime(nil) != nil || scmFailureCategoryPtr(domain.SCMStatusDeliveryFailureCategoryPermanent) == nil || scmOptionalTrimmedString("  ") != nil || scmNormalizeOptionalString(strPtrSCMMemory(" ")) != nil {
		t.Fatal("unexpected optional helper result")
	}

	olderExisting := valid
	olderExisting.ID = "delivery-older"
	olderExisting.Status = domain.SCMStatusDeliveryStatusSent
	olderExisting.DesiredState = domain.SCMCommitStatusStateFailure
	olderExisting.LastSentState = &lastSentState
	newerIncoming := valid
	newerIncoming.BuildID = "build-2"
	newerIncoming.BuildAttempt = 2
	replacedByOlder, olderOutcome, olderPersist, olderReassertAfter, olderErr := reconcileSCMStatusDeliveryForClaim(olderExisting, valid, now, claimOwner, time.Minute, 2)
	if olderErr != nil || olderOutcome != repository.SCMStatusDeliveryClaimOutcomeSuperseded || olderPersist || olderReassertAfter != nil || replacedByOlder.ID != olderExisting.ID {
		t.Fatalf("expected older owner to win, got delivery=%+v outcome=%q persist=%v reassertAfter=%v err=%v", replacedByOlder, olderOutcome, olderPersist, olderReassertAfter, olderErr)
	}

	obsoleteExisting := valid
	obsoleteExisting.Status = domain.SCMStatusDeliveryStatusSent
	obsoleteExisting.DesiredState = domain.SCMCommitStatusStateSuccess
	obsoleteExisting.LastSentState = &lastSentState
	obsoleteIncoming := valid
	obsoleteIncoming.DesiredState = domain.SCMCommitStatusStatePending
	obsoleteResult, obsoleteOutcome, obsoletePersist, obsoleteReassertAfter, obsoleteErr := reconcileSCMStatusDeliveryForClaim(obsoleteExisting, obsoleteIncoming, now, claimOwner, time.Minute, 2)
	if obsoleteErr != nil || obsoleteOutcome != repository.SCMStatusDeliveryClaimOutcomeSuperseded || obsoletePersist || obsoleteReassertAfter != nil || obsoleteResult.DesiredState != obsoleteExisting.DesiredState {
		t.Fatalf("expected obsolete incoming state to be skipped, got delivery=%+v outcome=%q persist=%v reassertAfter=%v err=%v", obsoleteResult, obsoleteOutcome, obsoletePersist, obsoleteReassertAfter, obsoleteErr)
	}
}

func TestSCMStatusDeliveryRepository_LostClaimAndContextErrors(t *testing.T) {
	repo := NewSCMStatusDeliveryRepository()
	now := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
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
		Description:     "Coyote build is pending",
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := repo.ListRecoverable(ctx, repository.SCMStatusDeliveryRecoverableScanInput{Now: now, Limit: 1}); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled list recoverable error, got %v", err)
	}

	claimed, claimErr := repo.AcquireForDelivery(context.Background(), repository.SCMStatusDeliveryClaimInput{Delivery: base, ClaimOwner: "worker-a", Now: now, ClaimDuration: time.Minute, MaxAttempts: 2})
	if claimErr != nil {
		t.Fatalf("claim failed: %v", claimErr)
	}
	if _, err := repo.MarkSent(context.Background(), repository.SCMStatusDeliveryMarkSentInput{DeliveryID: "missing", ClaimOwner: "worker-a", ClaimedAt: now, SentAt: now, State: domain.SCMCommitStatusStatePending}); !errors.Is(err, repository.ErrSCMStatusDeliveryNotFound) {
		t.Fatalf("expected mark sent not found, got %v", err)
	}
	lostSent, lostSentErr := repo.MarkSent(context.Background(), repository.SCMStatusDeliveryMarkSentInput{DeliveryID: claimed.Delivery.ID, ClaimOwner: "worker-b", ClaimedAt: *claimed.Delivery.ClaimedAt, SentAt: now, State: domain.SCMCommitStatusStatePending})
	if lostSentErr != nil || lostSent.Outcome != repository.SCMStatusDeliveryUpdateOutcomeLostClaim {
		t.Fatalf("expected lost claim on mark sent, got result=%+v err=%v", lostSent, lostSentErr)
	}

	lostPermanent, lostPermanentErr := repo.RecordPermanentFailure(context.Background(), repository.SCMStatusDeliveryRecordFailureInput{DeliveryID: claimed.Delivery.ID, ClaimOwner: "worker-b", ClaimedAt: *claimed.Delivery.ClaimedAt, FailedAt: now, FailureCategory: domain.SCMStatusDeliveryFailureCategoryPermanent, FailureReason: "bad_request"})
	if lostPermanentErr != nil || lostPermanent.Outcome != repository.SCMStatusDeliveryUpdateOutcomeLostClaim {
		t.Fatalf("expected lost claim on permanent failure, got result=%+v err=%v", lostPermanent, lostPermanentErr)
	}

	lostSuperseded, lostSupersededErr := repo.MarkSuperseded(context.Background(), repository.SCMStatusDeliveryMarkSupersededInput{DeliveryID: claimed.Delivery.ID, ClaimOwner: strPtrSCMMemory("worker-a"), SupersededAt: now, Reason: "newer_build_attempt_exists"})
	if lostSupersededErr != nil || lostSuperseded.Outcome != repository.SCMStatusDeliveryUpdateOutcomeLostClaim {
		t.Fatalf("expected lost claim on partial supersede claim metadata, got result=%+v err=%v", lostSuperseded, lostSupersededErr)
	}

	key := scmStatusDeliveryStreamKey(base.Provider, base.RepositoryOwner, base.RepositoryName, base.CommitSHA, base.Context)
	repo.index[key] = "missing-id"
	if _, err := repo.GetByKey(context.Background(), base.Provider, base.RepositoryOwner, base.RepositoryName, base.CommitSHA, base.Context); !errors.Is(err, repository.ErrSCMStatusDeliveryNotFound) {
		t.Fatalf("expected missing delivery behind index to return not found, got %v", err)
	}
}

func strPtrSCMMemory(value string) *string {
	return &value
}
