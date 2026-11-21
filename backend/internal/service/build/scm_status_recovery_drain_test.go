package build

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	memoryrepo "github.com/radiation/coyote-ci/backend/internal/repository/memory"
	"github.com/radiation/coyote-ci/backend/internal/runner"
)

func newTerminalTestDrain(t *testing.T, builds *memoryrepo.BuildRepository, publisher *recordingSCMPublisher, batchSize int) *SCMStatusRecoveryDrain {
	t.Helper()
	reporter, err := NewSCMStatusReporter(SCMStatusReporterConfig{BuildRepo: builds, DeliveryRepo: memoryrepo.NewSCMStatusDeliveryRepository(), Publisher: publisher, ProjectRepo: &fakeSCMProjectRepository{project: domain.Project{Slug: "test"}}})
	if err != nil {
		t.Fatal(err)
	}
	drain, err := NewSCMStatusRecoveryDrain(SCMStatusRecoveryDrainConfig{Reporter: reporter, ListTerminalBuilds: builds.ListSCMTerminalBuilds, Interval: time.Second, BatchSize: batchSize})
	if err != nil {
		t.Fatal(err)
	}
	return drain
}

func TestSCMStatusDiscoveryAfterWorkerCompletion(t *testing.T) {
	for _, path := range []string{"synchronous", "kubernetes-result"} {
		for _, status := range []runner.RunStepStatus{runner.RunStepStatusSuccess, runner.RunStepStatusFailed} {
			t.Run(path+"/"+string(status), func(t *testing.T) {
				ctx := context.Background()
				worker, builds, _, request := newAtomicSynchronousFixture(t, true, false)
				publisher := &recordingSCMPublisher{}
				drain := newTerminalTestDrain(t, builds, publisher, 2)
				build, err := builds.GetByID(ctx, request.BuildID)
				if err != nil {
					t.Fatal(err)
				}
				if notifyErr := drain.reporter.NotifyBuildStatus(ctx, build); notifyErr != nil {
					t.Fatal(notifyErr)
				}
				if _, scanErr := drain.RunIteration(ctx); scanErr != nil || len(publisher.reqs) != 1 || publisher.reqs[0].State != domain.SCMCommitStatusStatePending {
					t.Fatalf("before completion: %+v %v", publisher.reqs, scanErr)
				}
				now := time.Now().UTC()
				result := runner.RunStepResult{Status: status, StartedAt: now, FinishedAt: now}
				if path == "synchronous" {
					worker.runner = &fakeRunner{result: result}
					_, _, err = worker.RunStep(ctx, request)
				} else {
					request.RequireAtomicExecutionCompletion = true
					_, err = worker.HandleStepResult(ctx, request, result)
				}
				if err != nil {
					t.Fatal(err)
				}
				for range 2 {
					if _, scanErr := drain.RunIteration(ctx); scanErr != nil {
						t.Fatal(scanErr)
					}
				}
				want := domain.SCMCommitStatusStateSuccess
				if status == runner.RunStepStatusFailed {
					want = domain.SCMCommitStatusStateFailure
				}
				if len(publisher.reqs) != 2 || publisher.reqs[1].State != want {
					t.Fatalf("posts: %+v, want pending then %s", publisher.reqs, want)
				}
			})
		}
	}
}

func TestSCMStatusDiscoveryPagesAndWraps(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	builds := memoryrepo.NewBuildRepository()
	publisher := &recordingSCMPublisher{}
	drain := newTerminalTestDrain(t, builds, publisher, 1)
	drain.now = func() time.Time { return now }
	create := func(id string, finished time.Time, sha *string, mapped bool) domain.Build {
		build := domain.Build{ID: id, JobID: strPtr("job"), CreatedAt: finished, FinishedAt: &finished, Status: domain.BuildStatusCanceled, CommitSHA: sha, RepoURL: strPtr("https://github.com/test/project")}
		if mapped {
			build = scmMappedBuild(build)
		}
		stored, err := builds.Create(ctx, build)
		if err != nil {
			t.Fatal(err)
		}
		return stored
	}
	converged := create("c", now, strPtr("c"), true)
	if err := drain.reporter.NotifyBuildStatus(ctx, converged); err != nil {
		t.Fatal(err)
	}
	create("b", now, nil, true)
	canceled := create("a", now, strPtr("a"), true)
	canceled.Status = domain.BuildStatusRunning
	if err := drain.reporter.NotifyBuildStatus(ctx, canceled); err != nil {
		t.Fatal(err)
	}
	create("old", now.Add(-7*24*time.Hour-time.Second), strPtr("old"), true)
	create("legacy", now, strPtr("legacy"), false)
	for index, count := range []int{2, 2, 3, 3, 4} {
		if _, err := drain.RunIteration(ctx); err != nil {
			t.Fatal(err)
		}
		if len(publisher.reqs) != count {
			t.Fatalf("page %d: %+v", index, publisher.reqs)
		}
		if index == 0 {
			create("d", now, strPtr("d"), true)
		}
	}
	if publisher.reqs[2].CommitSHA != "a" || publisher.reqs[2].State != domain.SCMCommitStatusStateError || publisher.reqs[3].CommitSHA != "d" {
		t.Fatalf("unexpected terminal posts: %+v", publisher.reqs)
	}
}

type unavailableRetryScan struct {
	scmStatusDeliveryRepository
	err error
}

func (repo unavailableRetryScan) ListRecoverable(context.Context, repository.SCMStatusDeliveryRecoverableScanInput) ([]domain.SCMStatusDelivery, error) {
	return nil, repo.err
}

func TestSCMStatusDiscoveryContinuesWhenRetryScanFails(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	builds := memoryrepo.NewBuildRepository()
	publisher := &recordingSCMPublisher{}
	drain := newTerminalTestDrain(t, builds, publisher, 4)
	failure := errors.New("retry scan failed")
	drain.reporter.deliveryRepo = unavailableRetryScan{drain.reporter.deliveryRepo, failure}
	for _, attempt := range []int{1, 2} {
		id := "older"
		if attempt == 2 {
			id = "newer"
		}
		_, err := builds.Create(ctx, scmMappedBuild(domain.Build{ID: id, CreatedAt: now, FinishedAt: &now, Status: domain.BuildStatusSuccess, JobID: strPtr("job"), CommitSHA: strPtr("sha"), AttemptNumber: attempt}))
		if err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if _, err := drain.RunIteration(ctx); !errors.Is(err, failure) {
			t.Fatalf("retry error: %v", err)
		}
	}
	row, err := drain.reporter.deliveryRepo.GetByRepositoryIdentity(ctx, "scm-connection-1", "provider-repository-1", "sha", "coyote/test/job")
	if err != nil || row.BuildID != "newer" || len(publisher.reqs) != 1 {
		t.Fatalf("delivery: %+v posts: %+v error: %v", row, publisher.reqs, err)
	}
}

func TestSCMStatusDiscoveryLeavesTerminalFailuresAlone(t *testing.T) {
	for _, failure := range []error{errors.New("invalid configuration"), timeoutPublisherError{}} {
		t.Run(failure.Error(), func(t *testing.T) {
			ctx := context.Background()
			now := time.Now().UTC()
			builds := memoryrepo.NewBuildRepository()
			build, err := builds.Create(ctx, scmMappedBuild(domain.Build{ID: "build", CreatedAt: now, FinishedAt: &now, Status: domain.BuildStatusFailed, JobID: strPtr("job"), CommitSHA: strPtr("sha")}))
			if err != nil {
				t.Fatal(err)
			}
			publisher := &recordingSCMPublisher{err: failure}
			drain := newTerminalTestDrain(t, builds, publisher, 2)
			drain.reporter.retryPolicy.maxAttempts = 1
			if notifyErr := drain.reporter.NotifyBuildStatus(ctx, build); notifyErr == nil {
				t.Fatal("expected failure")
			}
			if _, scanErr := drain.RunIteration(ctx); scanErr != nil || len(publisher.reqs) != 1 {
				t.Fatalf("terminal failure retried: %+v %v", publisher.reqs, scanErr)
			}
		})
	}
}

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
	scanFailure := errors.New("terminal scan failed")
	drain.listTerminalBuilds = func(context.Context, time.Time, time.Time, string, int) ([]domain.Build, error) {
		return nil, scanFailure
	}

	result, runErr := drain.RunIteration(context.Background())
	if !errors.Is(runErr, scanFailure) {
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
