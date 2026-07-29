package build

import (
	"context"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	memoryrepo "github.com/radiation/coyote-ci/backend/internal/repository/memory"
)

func TestSCMStatusRecoveryDrain_RunIteration_ReclaimsAndConverges(t *testing.T) {
	jobID := "job-1"
	build := scmMappedBuild(domain.Build{
		ID:            "build-1",
		ProjectID:     "project-1",
		JobID:         &jobID,
		AttemptNumber: 1,
		CreatedAt:     time.Date(2026, 7, 16, 19, 0, 0, 0, time.UTC),
		Status:        domain.BuildStatusQueued,
		CommitSHA:     strPtr("deadbeef"),
		RepoURL:       strPtr("https://github.com/octo/repo.git"),
	})
	buildRepo := &multiBuildRepository{builds: map[string]domain.Build{"build-1": build}, byJob: map[string][]domain.Build{jobID: {build}}}
	deliveryRepo := memoryrepo.NewSCMStatusDeliveryRepository()
	publisher := &recordingSCMPublisher{err: timeoutPublisherError{}}
	reporter, err := NewSCMStatusReporter(SCMStatusReporterConfig{
		BuildRepo:    buildRepo,
		ProjectRepo:  &fakeSCMProjectRepository{project: domain.Project{ID: "project-1", Slug: "payments"}},
		DeliveryRepo: deliveryRepo,
		Publisher:    publisher,
	})
	if err != nil {
		t.Fatalf("new reporter failed: %v", err)
	}
	now := time.Date(2026, 7, 16, 19, 1, 0, 0, time.UTC)
	reporter.now = func() time.Time { return now }
	if notifyErr := reporter.NotifyBuildStatus(context.Background(), build); notifyErr == nil {
		t.Fatal("expected initial retryable failure")
	}

	delivery, getErr := deliveryRepo.GetByRepositoryIdentity(context.Background(), "scm-connection-1", "provider-repository-1", "deadbeef", "coyote/payments/job-1")
	if getErr != nil {
		t.Fatalf("get delivery failed: %v", getErr)
	}
	if delivery.Status != domain.SCMStatusDeliveryStatusRetryWaiting || delivery.NextAttemptAt == nil {
		t.Fatalf("expected retry_waiting delivery, got %+v", delivery)
	}

	publisher.err = nil
	drain, drainErr := NewSCMStatusRecoveryDrain(SCMStatusRecoveryDrainConfig{Reporter: reporter, Interval: time.Second, BatchSize: 10})
	if drainErr != nil {
		t.Fatalf("new drain failed: %v", drainErr)
	}
	recoveryNow := delivery.NextAttemptAt.Add(time.Second)
	reporter.now = func() time.Time { return recoveryNow }
	drain.now = func() time.Time { return recoveryNow }

	result, runErr := drain.RunIteration(context.Background())
	if runErr != nil {
		t.Fatalf("run iteration failed: %v", runErr)
	}
	if result.Scanned != 1 || result.RetryClaimed != 1 || result.Sent != 1 || result.Errors != 0 {
		t.Fatalf("unexpected recovery iteration result: %+v", result)
	}

	recovered, recoveredErr := deliveryRepo.GetByRepositoryIdentity(context.Background(), "scm-connection-1", "provider-repository-1", "deadbeef", "coyote/payments/job-1")
	if recoveredErr != nil {
		t.Fatalf("get recovered delivery failed: %v", recoveredErr)
	}
	if recovered.Status != domain.SCMStatusDeliveryStatusSent {
		t.Fatalf("expected sent delivery after recovery, got %+v", recovered)
	}
}

func TestSCMStatusRecoveryDrain_RunIteration_ReassertsAfterInterruptedReplacement(t *testing.T) {
	jobID := "job-1"
	build := scmMappedBuild(domain.Build{
		ID:            "build-2",
		ProjectID:     "project-1",
		JobID:         &jobID,
		AttemptNumber: 2,
		CreatedAt:     time.Date(2026, 7, 16, 20, 0, 0, 0, time.UTC),
		Status:        domain.BuildStatusSuccess,
		CommitSHA:     strPtr("deadbeef"),
		RepoURL:       strPtr("https://github.com/octo/repo.git"),
	})
	buildRepo := &multiBuildRepository{builds: map[string]domain.Build{"build-2": build}, byJob: map[string][]domain.Build{jobID: {build}}}
	deliveryRepo := memoryrepo.NewSCMStatusDeliveryRepository()
	publisher := newBlockingSCMPublisher(nil, nil)
	reporter, err := NewSCMStatusReporter(SCMStatusReporterConfig{
		BuildRepo:    buildRepo,
		ProjectRepo:  &fakeSCMProjectRepository{project: domain.Project{ID: "project-1", Slug: "payments"}},
		DeliveryRepo: deliveryRepo,
		Publisher:    publisher,
	})
	if err != nil {
		t.Fatalf("new reporter failed: %v", err)
	}
	now := time.Date(2026, 7, 16, 20, 1, 0, 0, time.UTC)
	reporter.now = func() time.Time { return now }

	planned, ok, planErr := reporter.planDelivery(context.Background(), build)
	if planErr != nil || !ok {
		t.Fatalf("plan delivery failed: ok=%v err=%v", ok, planErr)
	}
	claimResult, claimErr := deliveryRepo.AcquireForDelivery(context.Background(), repository.SCMStatusDeliveryClaimInput{
		Delivery:      planned,
		ClaimOwner:    reporter.claimOwner,
		Now:           now,
		ClaimDuration: reporter.claimDuration,
		MaxAttempts:   reporter.retryPolicy.maxAttempts,
	})
	if claimErr != nil {
		t.Fatalf("acquire delivery failed: %v", claimErr)
	}
	reassertAt := now.Add(2 * time.Second)
	_, updateErr := deliveryRepo.RecordRetryableFailure(context.Background(), repository.SCMStatusDeliveryRecordFailureInput{
		DeliveryID:      claimResult.Delivery.ID,
		ClaimOwner:      reporter.claimOwner,
		ClaimedAt:       *claimResult.Delivery.ClaimedAt,
		FailedAt:        now,
		NextAttemptAt:   &reassertAt,
		FailureCategory: domain.SCMStatusDeliveryFailureCategoryRetryable,
		FailureReason:   scmStatusFailureReasonAuthoritativeReassert,
	})
	if updateErr != nil {
		t.Fatalf("schedule authoritative reassert failed: %v", updateErr)
	}
	publisher.forceRemote(SCMCommitStatusPublishRequest{
		Provider:        "github",
		RepositoryOwner: "repository-snapshot",
		RepositoryName:  "repository-snapshot",
		CommitSHA:       "deadbeef",
		Context:         "coyote/payments/job-1",
		State:           domain.SCMCommitStatusStatePending,
		Description:     "Coyote build is queued",
	})

	drain, drainErr := NewSCMStatusRecoveryDrain(SCMStatusRecoveryDrainConfig{Reporter: reporter, Interval: time.Second, BatchSize: 10})
	if drainErr != nil {
		t.Fatalf("new drain failed: %v", drainErr)
	}
	recoveryNow := reassertAt.Add(time.Second)
	reporter.now = func() time.Time { return recoveryNow }
	drain.now = func() time.Time { return recoveryNow }

	result, runErr := drain.RunIteration(context.Background())
	if runErr != nil {
		t.Fatalf("run iteration failed: %v", runErr)
	}
	if result.Scanned != 1 || result.RetryClaimed != 1 || result.Sent != 1 || result.Errors != 0 {
		t.Fatalf("unexpected recovery iteration result: %+v", result)
	}

	finalRemote, ok := publisher.remoteState("github", "repository-snapshot", "repository-snapshot", "deadbeef", "coyote/payments/job-1")
	if !ok || finalRemote.State != domain.SCMCommitStatusStateSuccess {
		t.Fatalf("expected authoritative success state after recovery, got ok=%v req=%+v", ok, finalRemote)
	}
	recovered, recoveredErr := deliveryRepo.GetByRepositoryIdentity(context.Background(), "scm-connection-1", "provider-repository-1", "deadbeef", "coyote/payments/job-1")
	if recoveredErr != nil {
		t.Fatalf("get recovered delivery failed: %v", recoveredErr)
	}
	if recovered.Status != domain.SCMStatusDeliveryStatusSent {
		t.Fatalf("expected sent delivery after authoritative reassert recovery, got %+v", recovered)
	}
}
