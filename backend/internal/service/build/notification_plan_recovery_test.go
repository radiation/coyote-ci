package build

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/repository/memory"
)

type failingPlanningSubscriptions struct {
	*memory.NotificationSubscriptionRepository
	failID string
}

type rejectedNotificationPlan struct {
	repository.NotificationPlanningRepository
	candidates int
}

func (p *rejectedNotificationPlan) ListUnplannedBuildIDs(context.Context, string, int) ([]string, error) {
	return nil, errors.New("planning scan unavailable")
}

func (p *rejectedNotificationPlan) CommitNotificationPlan(_ context.Context, _ string, _ domain.BuildStatus, rows []domain.NotificationDelivery) (bool, error) {
	p.candidates = len(rows)
	return false, errors.New("commit failed")
}

func TestNotificationPlanCommitFailureNeverSends(t *testing.T) {
	plans := &rejectedNotificationPlan{}
	sender := &recordingEmailSender{}
	service, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "a@example.com,b@example.com", Sender: sender, PlanningRepo: plans, DeliveryRepo: memory.NewNotificationDeliveryRepository(), SubscriptionRepo: memory.NewNotificationSubscriptionRepository()})
	if err != nil {
		t.Fatal(err)
	}
	if notifyErr := service.NotifyTerminalBuild(context.Background(), domain.Build{ID: "one", Status: domain.BuildStatusSuccess}); notifyErr == nil || plans.candidates != 2 || len(sender.messages) != 0 {
		t.Fatalf("commit error %v, candidates %d, sends %d", notifyErr, plans.candidates, len(sender.messages))
	}
}

func TestNotificationPlanConfiguredEmailRecoversAfterCommit(t *testing.T) {
	ctx := context.Background()
	builds := memory.NewBuildRepository()
	ledger := memory.NewNotificationDeliveryRepository()
	plans := memory.NewNotificationPlanningRepository(builds, ledger)
	build := domain.Build{ID: "configured-email", Status: domain.BuildStatusFailed}
	if _, err := builds.Create(ctx, build); err != nil {
		t.Fatal(err)
	}
	sender := &recordingEmailSender{}
	cfg := BuildNotificationConfig{Enabled: true, Recipients: "Ops <Ops@example.com>", Sender: sender, BuildRepo: builds, DeliveryRepo: ledger, PlanningRepo: plans}
	planner, err := NewBuildNotificationService(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, planErr := planner.planTerminalBuild(ctx, build, domain.NotificationEventTypeBuildFailed); planErr != nil {
		t.Fatal(planErr)
	}
	cfg.Recipients = "replacement@example.com"
	restarted, err := NewBuildNotificationService(cfg)
	if err != nil {
		t.Fatal(err)
	}
	drain, err := NewNotificationRecoveryDrain(NotificationRecoveryDrainConfig{Notifier: restarted, Interval: time.Second, BatchSize: 4})
	if err != nil {
		t.Fatal(err)
	}
	result, err := drain.RunIteration(ctx)
	if err != nil || result.Sent != 1 || result.ClaimAcquired != 1 || len(sender.messages) != 1 || sender.messages[0].To != `"Ops" <Ops@example.com>` {
		t.Fatalf("recovery result %+v, messages %+v, err %v", result, sender.messages, err)
	}
	if next, runErr := drain.RunIteration(ctx); runErr != nil || next.Scanned != 0 || len(sender.messages) != 1 {
		t.Fatalf("repeat recovery result %+v, messages %d, err %v", next, len(sender.messages), runErr)
	}
}

func TestNotificationPlanUsesPreferencesUntilCommit(t *testing.T) {
	ctx := context.Background()
	fixture := newPersonalCommitAuthorNotificationFixture(t, personalCommitAuthorNotificationOptions{createEmailTarget: true, emailTargetEnabled: true})
	build := domain.Build{ID: "personal", Status: domain.BuildStatusFailed, SourceAuthorEmail: &fixture.authorEmail}
	builds := memory.NewBuildRepository()
	if _, err := builds.Create(ctx, build); err != nil {
		t.Fatal(err)
	}
	fixture.notifier.buildRepo = builds
	fixture.notifier.plans = memory.NewNotificationPlanningRepository(builds, fixture.deliveryRepo)
	mustUpsertNotificationPreference(t, fixture.preferenceRepo, fixture.authorUser.ID, true)
	if _, err := fixture.notifier.planTerminalBuild(ctx, build, domain.NotificationEventTypeBuildFailed); err != nil {
		t.Fatal(err)
	}
	mustUpsertNotificationPreference(t, fixture.preferenceRepo, fixture.authorUser.ID, false)
	drain, err := NewNotificationRecoveryDrain(NotificationRecoveryDrainConfig{Notifier: fixture.notifier, Interval: time.Second, BatchSize: 4})
	if err != nil {
		t.Fatal(err)
	}
	if result, iterationErr := drain.RunIteration(ctx); iterationErr != nil || result.Sent != 1 || len(fixture.emailSender.messages) != 1 {
		t.Fatalf("committed personal destination lost: %+v %v", result, iterationErr)
	}
}

func (r *failingPlanningSubscriptions) ListEnabledMatchesForBuildEvent(ctx context.Context, build domain.Build, event domain.NotificationEventType) ([]domain.NotificationSubscriptionMatch, error) {
	if build.ID == r.failID {
		return nil, errors.New("subscription lookup unavailable")
	}
	return r.NotificationSubscriptionRepository.ListEnabledMatchesForBuildEvent(ctx, build, event)
}

func TestNotificationPlanRecoveryAfterRestart(t *testing.T) {
	ctx := context.Background()
	builds := memory.NewBuildRepository()
	ledger := memory.NewNotificationDeliveryRepository()
	plans := memory.NewNotificationPlanningRepository(builds, ledger)
	subs := &failingPlanningSubscriptions{memory.NewNotificationSubscriptionRepository(), "a"}
	sender := &recordingEmailSender{}
	for id, status := range map[string]domain.BuildStatus{"a": domain.BuildStatusFailed, "b": domain.BuildStatusSuccess, "c": domain.BuildStatusCanceled} {
		if _, err := builds.Create(ctx, domain.Build{ID: id, Status: status, CreatedAt: time.Unix(1, 0)}); err != nil {
			t.Fatal(err)
		}
	}
	service, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "ops@example.com", Sender: sender, DeliveryRepo: ledger, PlanningRepo: plans, SubscriptionRepo: subs, BuildRepo: builds})
	if err != nil {
		t.Fatal(err)
	}
	drain, err := NewNotificationRecoveryDrain(NotificationRecoveryDrainConfig{Notifier: service, Interval: time.Second, BatchSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, iterationErr := drain.RunIteration(ctx); iterationErr == nil || len(sender.messages) != 0 {
		t.Fatalf("failed resolution: %v, sends %d", iterationErr, len(sender.messages))
	}
	if result, iterationErr := drain.RunIteration(ctx); iterationErr != nil || result.Sent != 1 {
		t.Fatalf("starved healthy candidate: %+v %v", result, iterationErr)
	}
	if _, iterationErr := drain.RunIteration(ctx); iterationErr != nil {
		t.Fatal(iterationErr)
	}
	subs.failID = ""
	if result, iterationErr := drain.RunIteration(ctx); iterationErr != nil || result.Sent != 1 {
		t.Fatalf("repaired candidate: %+v %v", result, iterationErr)
	}
	if ids, listErr := plans.ListUnplannedBuildIDs(ctx, "", 10); listErr != nil || len(ids) != 0 || len(sender.messages) != 2 {
		t.Fatalf("remaining %v, sends %d, err %v", ids, len(sender.messages), listErr)
	}
}

func TestNotificationPlanFreezesMembership(t *testing.T) {
	ctx := context.Background()
	builds := memory.NewBuildRepository()
	ledger := memory.NewNotificationDeliveryRepository()
	plans := memory.NewNotificationPlanningRepository(builds, ledger)
	subs := memory.NewNotificationSubscriptionRepository()
	sender := &recordingEmailSender{}
	newService := func(enabled bool) *BuildNotificationService {
		service, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: enabled, BuildRepo: builds, DeliveryRepo: ledger, PlanningRepo: plans, SubscriptionRepo: subs, Sender: sender})
		if err != nil {
			t.Fatal(err)
		}
		return service
	}
	create := func(id string) domain.Build {
		build, err := builds.Create(ctx, domain.Build{ID: id, ProjectID: "project", Status: domain.BuildStatusFailed})
		if err != nil {
			t.Fatal(err)
		}
		return build
	}
	enabled, disabled := newService(true), newService(false)
	empty := create("empty")
	if err := enabled.NotifyTerminalBuild(ctx, empty); err != nil {
		t.Fatal(err)
	}
	project := "project"
	target := mustCreateNotificationTarget(t, subs, "old@example.com", true)
	mustCreateNotificationSubscription(t, subs, target.ID, &project, nil, domain.NotificationEventTypeBuildFailed, true)
	suppressed := create("disabled")
	if err := disabled.NotifyTerminalBuild(ctx, suppressed); err != nil {
		t.Fatal(err)
	}
	for _, build := range []domain.Build{empty, suppressed} {
		if err := enabled.NotifyTerminalBuild(ctx, build); err != nil {
			t.Fatal(err)
		}
	}
	planned := create("planned")
	if _, err := enabled.planTerminalBuild(ctx, planned, domain.NotificationEventTypeBuildFailed); err != nil {
		t.Fatal(err)
	}
	newTarget := mustCreateNotificationTarget(t, subs, "new@example.com", true)
	mustCreateNotificationSubscription(t, subs, newTarget.ID, &project, nil, domain.NotificationEventTypeBuildFailed, true)
	if err := enabled.NotifyTerminalBuild(ctx, planned); err != nil {
		t.Fatal(err)
	}
	rows, err := ledger.ListRecoverable(ctx, repository.NotificationDeliveryRecoverableScanInput{Now: time.Now(), Limit: 10})
	if err != nil || len(rows) != 1 || rows[0].Status != domain.NotificationDeliveryStatusPending || len(sender.messages) != 0 {
		t.Fatalf("unexpected frozen plan %+v, sends %d, err %v", rows, len(sender.messages), err)
	}
	drain, err := NewNotificationRecoveryDrain(NotificationRecoveryDrainConfig{Notifier: disabled, Interval: time.Second, BatchSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	disabled.plans = &rejectedNotificationPlan{}
	if result, iterationErr := drain.RunIteration(ctx); iterationErr == nil || result.Sent != 1 || len(sender.messages) != 1 || sender.messages[0].To != rows[0].Recipient {
		t.Fatalf("disabled recovery: %+v %+v %v", result, sender.messages, iterationErr)
	}
}
