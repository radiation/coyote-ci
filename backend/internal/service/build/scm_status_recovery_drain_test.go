package build

import (
	"context"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	memoryrepo "github.com/radiation/coyote-ci/backend/internal/repository/memory"
)

func TestSCMStatusRecoveryDrain_RunIteration_ReclaimsAndConverges(t *testing.T) {
	jobID := "job-1"
	build := domain.Build{
		ID:            "build-1",
		ProjectID:     "project-1",
		JobID:         &jobID,
		AttemptNumber: 1,
		CreatedAt:     time.Date(2026, 7, 16, 19, 0, 0, 0, time.UTC),
		Status:        domain.BuildStatusQueued,
		CommitSHA:     strPtr("deadbeef"),
		RepoURL:       strPtr("https://github.com/octo/repo.git"),
	}
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

	delivery, getErr := deliveryRepo.GetByKey(context.Background(), "github", "octo", "repo", "deadbeef", "coyote/payments/job-1")
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

	recovered, recoveredErr := deliveryRepo.GetByKey(context.Background(), "github", "octo", "repo", "deadbeef", "coyote/payments/job-1")
	if recoveredErr != nil {
		t.Fatalf("get recovered delivery failed: %v", recoveredErr)
	}
	if recovered.Status != domain.SCMStatusDeliveryStatusSent {
		t.Fatalf("expected sent delivery after recovery, got %+v", recovered)
	}
}
