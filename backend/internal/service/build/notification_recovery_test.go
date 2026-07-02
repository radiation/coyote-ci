package build

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/observability"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	memoryrepo "github.com/radiation/coyote-ci/backend/internal/repository/memory"
)

func TestBuildNotificationService_RehydrateDelivery_CurrentTransports(t *testing.T) {
	buildRepo := &fakeBuildRepository{build: domain.Build{ID: "build-1", Status: domain.BuildStatusFailed, CreatedAt: time.Now().UTC()}}
	subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
	identityRepo := memoryrepo.NewUserSlackIdentityRepository()
	workspaceRepo := memoryrepo.NewSlackWorkspaceIntegrationRepository()
	workspaceRepo.SetUserSlackIdentityRepository(identityRepo)
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{
		Enabled:          true,
		Recipients:       "dev@example.com",
		Sender:           &recordingEmailSender{},
		SlackSender:      &recordingSlackSender{},
		SlackClient:      &recordingSlackDMClient{},
		BuildRepo:        buildRepo,
		DeliveryRepo:     memoryrepo.NewNotificationDeliveryRepository(),
		SubscriptionRepo: subscriptionRepo,
		IdentityRepo:     identityRepo,
		WorkspaceRepo:    workspaceRepo,
		ClaimOwner:       "recovery-test",
	})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	t.Run("shared email uses current target recipient without changing identity", func(t *testing.T) {
		target := mustCreateNotificationTarget(t, subscriptionRepo, "alerts@example.com", true)
		target.Recipient = "renamed-alerts@example.com"
		if _, err := subscriptionRepo.UpdateTarget(context.Background(), target); err != nil {
			t.Fatalf("update target failed: %v", err)
		}
		kind, key, keyErr := domain.NotificationSharedEmailTargetKey(target.ID)
		if keyErr != nil {
			t.Fatalf("shared email key failed: %v", keyErr)
		}
		delivery := domain.NotificationDelivery{BuildID: "build-1", EventType: domain.NotificationEventTypeBuildFailed, Transport: domain.NotificationTransportEmail, DestinationKind: kind, DestinationKey: key, NotificationTargetID: &target.ID, Recipient: "alerts@example.com"}
		_, destination, _, rehydrateErr := notifier.rehydrateDelivery(context.Background(), delivery)
		if rehydrateErr != nil {
			t.Fatalf("rehydrate shared email failed: %v", rehydrateErr)
		}
		if destination.destinationKey != key || destination.emailRecipient != "<renamed-alerts@example.com>" {
			t.Fatalf("unexpected shared email destination: %+v", destination)
		}
	})

	t.Run("personal email uses current target recipient without changing identity", func(t *testing.T) {
		user := mustCreateNotificationUser(t, memoryrepo.NewUserRepository(), "author@example.com")
		target := mustEnsureOwnedNotificationTarget(t, subscriptionRepo, user.ID, "author@example.com", true)
		target.Recipient = "author+updated@example.com"
		if _, err := subscriptionRepo.UpdateTarget(context.Background(), target); err != nil {
			t.Fatalf("update owned target failed: %v", err)
		}
		kind, key, keyErr := domain.NotificationPersonalEmailTargetKey(target.ID)
		if keyErr != nil {
			t.Fatalf("personal email key failed: %v", keyErr)
		}
		delivery := domain.NotificationDelivery{BuildID: "build-1", EventType: domain.NotificationEventTypeBuildFailed, Transport: domain.NotificationTransportEmail, DestinationKind: kind, DestinationKey: key, NotificationTargetID: &target.ID, RecipientUserID: &user.ID, Recipient: "author@example.com"}
		_, destination, _, rehydrateErr := notifier.rehydrateDelivery(context.Background(), delivery)
		if rehydrateErr != nil {
			t.Fatalf("rehydrate personal email failed: %v", rehydrateErr)
		}
		if destination.destinationKey != key || destination.emailRecipient != "<author+updated@example.com>" {
			t.Fatalf("unexpected personal email destination: %+v", destination)
		}
	})

	t.Run("slack webhook uses current target webhook without changing identity", func(t *testing.T) {
		target := mustCreateSlackNotificationTarget(t, subscriptionRepo, "https://hooks.slack.example/services/T/B/OLD", true)
		target.Recipient = "https://hooks.slack.example/services/T/B/NEW"
		if _, err := subscriptionRepo.UpdateTarget(context.Background(), target); err != nil {
			t.Fatalf("update slack target failed: %v", err)
		}
		kind, key, keyErr := domain.NotificationSharedSlackWebhookTargetKey(target.ID)
		if keyErr != nil {
			t.Fatalf("slack webhook key failed: %v", keyErr)
		}
		delivery := domain.NotificationDelivery{BuildID: "build-1", EventType: domain.NotificationEventTypeBuildFailed, Transport: domain.NotificationTransportSlackWebhook, DestinationKind: kind, DestinationKey: key, NotificationTargetID: &target.ID, Recipient: "slack_webhook:" + target.ID}
		_, destination, _, rehydrateErr := notifier.rehydrateDelivery(context.Background(), delivery)
		if rehydrateErr != nil {
			t.Fatalf("rehydrate slack webhook failed: %v", rehydrateErr)
		}
		if destination.destinationKey != key || destination.webhookURL != "https://hooks.slack.example/services/T/B/NEW" {
			t.Fatalf("unexpected slack webhook destination: %+v", destination)
		}
	})

	t.Run("slack dm ignores mutable profile metadata without changing identity", func(t *testing.T) {
		workspace, workspaceErr := workspaceRepo.ConnectOrReplace(context.Background(), domain.SlackWorkspaceIntegration{ID: "workspace-1", WorkspaceID: "T123", BotTokenSecret: "xoxb-token", Enabled: true, ConnectedAt: time.Now().UTC(), CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}, true)
		if workspaceErr != nil {
			t.Fatalf("connect workspace failed: %v", workspaceErr)
		}
		identity, identityErr := identityRepo.Upsert(context.Background(), domain.UserSlackIdentity{UserID: "user-1", SlackWorkspaceIntegrationID: workspace.ID, SlackUserID: "U123", SlackDisplayName: strPtr("Before"), Enabled: true, LinkedAt: time.Now().UTC(), CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()})
		if identityErr != nil {
			t.Fatalf("upsert slack identity failed: %v", identityErr)
		}
		identity.SlackDisplayName = strPtr("After")
		if _, err := identityRepo.Upsert(context.Background(), identity); err != nil {
			t.Fatalf("update slack identity failed: %v", err)
		}
		kind, key, keyErr := domain.NotificationSlackDMDestinationKey(workspace.ID, identity.SlackUserID)
		if keyErr != nil {
			t.Fatalf("slack dm key failed: %v", keyErr)
		}
		delivery := domain.NotificationDelivery{BuildID: "build-1", EventType: domain.NotificationEventTypeBuildFailed, Transport: domain.NotificationTransportSlackDM, DestinationKind: kind, DestinationKey: key, RecipientUserID: &identity.UserID, SlackWorkspaceIntegrationID: &workspace.ID, Recipient: "slack_dm:" + workspace.ID + ":" + identity.SlackUserID}
		_, destination, _, rehydrateErr := notifier.rehydrateDelivery(context.Background(), delivery)
		if rehydrateErr != nil {
			t.Fatalf("rehydrate slack dm failed: %v", rehydrateErr)
		}
		if destination.destinationKey != key || destination.slackUserID != identity.SlackUserID {
			t.Fatalf("unexpected slack dm destination: %+v", destination)
		}
	})
}

func TestNotificationRecoveryDrain_RunIteration(t *testing.T) {
	t.Run("due retry is claimed and sent with metrics", func(t *testing.T) {
		fixture := newRecoveryDrainFixture(t, nil)
		seedDueRetryDelivery(t, fixture.deliveryRepo, fixture.target, fixture.build.ID, domain.NotificationEventTypeBuildFailed, fixture.now.Add(-time.Minute), 1, 3)
		drain := fixture.newDrain(t)
		result, err := drain.RunIteration(context.Background())
		if err != nil {
			t.Fatalf("run iteration failed: %v", err)
		}
		if result.Scanned != 1 || result.RetryClaimed != 1 || result.Sent != 1 {
			t.Fatalf("unexpected recovery result: %+v", result)
		}
		if len(fixture.sender.messages) != 1 {
			t.Fatalf("expected one email send, got %d", len(fixture.sender.messages))
		}
		delivery := mustGetNotificationDelivery(t, fixture.deliveryRepo, fixture.build.ID, domain.NotificationEventTypeBuildFailed, fixture.target.Recipient)
		if delivery.Status != domain.NotificationDeliveryStatusSent || delivery.Attempts != 2 {
			t.Fatalf("unexpected recovered delivery state: %+v", delivery)
		}
		if fixture.metrics.OutcomeCount(string(domain.NotificationEventTypeBuildFailed), string(domain.NotificationTransportEmail), string(domain.NotificationDestinationKindSharedTarget), notificationRecoveryReasonDueRetry, observability.NotificationDeliveryOutcomeScanned) != 1 {
			t.Fatal("expected scanned metric to increment once")
		}
	})

	t.Run("stale claim is reclaimed and sent", func(t *testing.T) {
		fixture := newRecoveryDrainFixture(t, nil)
		seedStaleClaimDelivery(t, fixture.deliveryRepo, fixture.target, fixture.build.ID, domain.NotificationEventTypeBuildFailed, fixture.now.Add(-2*time.Minute), fixture.now.Add(-time.Minute))
		drain := fixture.newDrain(t)
		result, err := drain.RunIteration(context.Background())
		if err != nil {
			t.Fatalf("run iteration failed: %v", err)
		}
		if result.Scanned != 1 || result.StaleClaimReclaimed != 1 || result.Sent != 1 {
			t.Fatalf("unexpected stale claim recovery result: %+v", result)
		}
	})

	t.Run("contention safely skips", func(t *testing.T) {
		notifier := &BuildNotificationService{buildRepo: &fakeBuildRepository{build: domain.Build{ID: "build-1", Status: domain.BuildStatusFailed}}, deliveryRepo: &scriptedNotificationDeliveryRepo{listRecoverableFunc: func(context.Context, repository.NotificationDeliveryRecoverableScanInput) ([]domain.NotificationDelivery, error) {
			return []domain.NotificationDelivery{{ID: "delivery-1", BuildID: "build-1", EventType: domain.NotificationEventTypeBuildFailed, Transport: domain.NotificationTransportEmail, DestinationKind: domain.NotificationDestinationKindSharedTarget, DestinationKey: "email-target:1", Recipient: "dev@example.com", Status: domain.NotificationDeliveryStatusRetryWaiting}}, nil
		}, acquireFunc: func(context.Context, repository.NotificationDeliveryClaimInput) (repository.NotificationDeliveryClaimResult, error) {
			return repository.NotificationDeliveryClaimResult{Delivery: domain.NotificationDelivery{ID: "delivery-1", BuildID: "build-1", EventType: domain.NotificationEventTypeBuildFailed, Transport: domain.NotificationTransportEmail, DestinationKind: domain.NotificationDestinationKindSharedTarget, DestinationKey: "email-target:1", Recipient: "dev@example.com", Attempts: 1}, Outcome: repository.NotificationDeliveryClaimOutcomeClaimedByOther}, nil
		}}, deliveryMetrics: observability.NewInMemoryNotificationDeliveryMetrics(), claimOwner: "recovery-a", retryPolicy: defaultNotificationRetryPolicy(), claimDuration: minimumNotificationClaimDuration(), now: func() time.Time { return time.Date(2026, 7, 2, 18, 0, 0, 0, time.UTC) }}
		drain, err := NewNotificationRecoveryDrain(NotificationRecoveryDrainConfig{Notifier: notifier, Interval: time.Millisecond, BatchSize: 1})
		if err != nil {
			t.Fatalf("create drain failed: %v", err)
		}
		result, runErr := drain.RunIteration(context.Background())
		if runErr != nil {
			t.Fatalf("run iteration failed: %v", runErr)
		}
		if result.Skipped != 1 || result.Scanned != 1 {
			t.Fatalf("unexpected contention result: %+v", result)
		}
	})

	t.Run("one failure does not stop later candidates", func(t *testing.T) {
		fixture := newRecoveryDrainFixture(t, nil)
		brokenTarget := mustCreateNotificationTarget(t, fixture.subscriptionRepo, "broken@example.com", true)
		seedDueRetryDelivery(t, fixture.deliveryRepo, brokenTarget, fixture.build.ID, domain.NotificationEventTypeBuildFailed, fixture.now.Add(-time.Minute), 1, 3)
		if err := fixture.subscriptionRepo.DeleteTarget(context.Background(), brokenTarget.ID); err != nil {
			t.Fatalf("delete broken target failed: %v", err)
		}
		seedDueRetryDelivery(t, fixture.deliveryRepo, fixture.target, fixture.build.ID, domain.NotificationEventTypeBuildFailed, fixture.now.Add(-time.Minute), 1, 3)
		drain := fixture.newDrain(t)
		result, err := drain.RunIteration(context.Background())
		if err != nil {
			t.Fatalf("run iteration failed: %v", err)
		}
		if result.PermanentlyFailed != 1 || result.Sent != 1 {
			t.Fatalf("unexpected mixed recovery result: %+v", result)
		}
		if len(fixture.sender.messages) != 1 {
			t.Fatalf("expected later candidate to still send, got %d messages", len(fixture.sender.messages))
		}
	})

	t.Run("retryable provider failure schedules the next attempt", func(t *testing.T) {
		fixture := newRecoveryDrainFixture(t, errors.New("smtp unavailable"))
		seedDueRetryDelivery(t, fixture.deliveryRepo, fixture.target, fixture.build.ID, domain.NotificationEventTypeBuildFailed, fixture.now.Add(-time.Minute), 1, 3)
		drain := fixture.newDrain(t)
		result, err := drain.RunIteration(context.Background())
		if err == nil || !strings.Contains(err.Error(), "smtp unavailable") {
			t.Fatalf("expected provider error, got %v", err)
		}
		if result.RetryScheduled != 1 {
			t.Fatalf("expected retry scheduled result, got %+v", result)
		}
		delivery := mustGetNotificationDelivery(t, fixture.deliveryRepo, fixture.build.ID, domain.NotificationEventTypeBuildFailed, fixture.target.Recipient)
		if delivery.Status != domain.NotificationDeliveryStatusRetryWaiting || delivery.NextAttemptAt == nil {
			t.Fatalf("unexpected retryable delivery state: %+v", delivery)
		}
	})

	t.Run("permanent destination failure becomes terminal", func(t *testing.T) {
		fixture := newRecoveryDrainFixture(t, nil)
		seedDueRetryDelivery(t, fixture.deliveryRepo, fixture.target, fixture.build.ID, domain.NotificationEventTypeBuildFailed, fixture.now.Add(-time.Minute), 1, 3)
		fixture.target.Enabled = false
		if _, err := fixture.subscriptionRepo.UpdateTarget(context.Background(), fixture.target); err != nil {
			t.Fatalf("disable target failed: %v", err)
		}
		drain := fixture.newDrain(t)
		result, err := drain.RunIteration(context.Background())
		if err != nil {
			t.Fatalf("run iteration failed: %v", err)
		}
		if result.PermanentlyFailed != 1 || result.RehydrationFailed != 1 {
			t.Fatalf("unexpected permanent failure result: %+v", result)
		}
		delivery := mustGetNotificationDelivery(t, fixture.deliveryRepo, fixture.build.ID, domain.NotificationEventTypeBuildFailed, fixture.target.Recipient)
		if delivery.Status != domain.NotificationDeliveryStatusFailedPermanent {
			t.Fatalf("expected permanent failure status, got %+v", delivery)
		}
	})

	t.Run("exhausted attempt becomes terminal", func(t *testing.T) {
		fixture := newRecoveryDrainFixture(t, errors.New("smtp unavailable"))
		seedDueRetryDelivery(t, fixture.deliveryRepo, fixture.target, fixture.build.ID, domain.NotificationEventTypeBuildFailed, fixture.now.Add(-time.Minute), 2, 3)
		drain := fixture.newDrain(t)
		result, err := drain.RunIteration(context.Background())
		if err == nil {
			t.Fatal("expected provider failure error")
		}
		if result.AttemptsExhausted != 1 {
			t.Fatalf("unexpected exhausted result: %+v", result)
		}
		delivery := mustGetNotificationDelivery(t, fixture.deliveryRepo, fixture.build.ID, domain.NotificationEventTypeBuildFailed, fixture.target.Recipient)
		if delivery.Status != domain.NotificationDeliveryStatusFailedExhausted || delivery.Attempts != 3 {
			t.Fatalf("unexpected exhausted delivery state: %+v", delivery)
		}
	})

	t.Run("old claim owner cannot overwrite recovered state", func(t *testing.T) {
		fixture := newRecoveryDrainFixture(t, nil)
		claim := seedStaleClaimDelivery(t, fixture.deliveryRepo, fixture.target, fixture.build.ID, domain.NotificationEventTypeBuildFailed, fixture.now.Add(-2*time.Minute), fixture.now.Add(-time.Minute))
		drain := fixture.newDrain(t)
		if _, err := drain.RunIteration(context.Background()); err != nil {
			t.Fatalf("run iteration failed: %v", err)
		}
		result, err := fixture.deliveryRepo.MarkSent(context.Background(), repository.NotificationDeliveryMarkSentInput{DeliveryID: claim.Delivery.ID, ClaimOwner: "old-owner", ClaimedAt: *claim.Delivery.ClaimedAt, SentAt: fixture.now})
		if err != nil {
			t.Fatalf("old owner mark sent failed: %v", err)
		}
		if result.Outcome != repository.NotificationDeliveryUpdateOutcomeLostClaim {
			t.Fatalf("expected lost claim outcome, got %q", result.Outcome)
		}
	})

	t.Run("empty iteration is inexpensive", func(t *testing.T) {
		var acquires atomic.Int32
		notifier := &BuildNotificationService{buildRepo: &fakeBuildRepository{build: domain.Build{ID: "build-1", Status: domain.BuildStatusFailed}}, deliveryRepo: &scriptedNotificationDeliveryRepo{listRecoverableFunc: func(context.Context, repository.NotificationDeliveryRecoverableScanInput) ([]domain.NotificationDelivery, error) {
			return nil, nil
		}, acquireFunc: func(context.Context, repository.NotificationDeliveryClaimInput) (repository.NotificationDeliveryClaimResult, error) {
			acquires.Add(1)
			return repository.NotificationDeliveryClaimResult{}, nil
		}}, deliveryMetrics: observability.NewNoopNotificationDeliveryMetrics(), claimOwner: "recovery-a", retryPolicy: defaultNotificationRetryPolicy(), claimDuration: minimumNotificationClaimDuration(), now: func() time.Time { return time.Date(2026, 7, 2, 18, 0, 0, 0, time.UTC) }}
		drain, err := NewNotificationRecoveryDrain(NotificationRecoveryDrainConfig{Notifier: notifier, Interval: time.Millisecond, BatchSize: 10})
		if err != nil {
			t.Fatalf("create drain failed: %v", err)
		}
		result, runErr := drain.RunIteration(context.Background())
		if runErr != nil {
			t.Fatalf("run iteration failed: %v", runErr)
		}
		if result.Scanned != 0 || acquires.Load() != 0 {
			t.Fatalf("unexpected empty iteration result: %+v acquires=%d", result, acquires.Load())
		}
	})
}

func TestNotificationRecoveryDrain_Run(t *testing.T) {
	t.Run("starts and stops with context", func(t *testing.T) {
		calls := make(chan struct{}, 1)
		notifier := &BuildNotificationService{buildRepo: &fakeBuildRepository{build: domain.Build{ID: "build-1", Status: domain.BuildStatusFailed}}, deliveryRepo: &scriptedNotificationDeliveryRepo{listRecoverableFunc: func(context.Context, repository.NotificationDeliveryRecoverableScanInput) ([]domain.NotificationDelivery, error) {
			select {
			case calls <- struct{}{}:
			default:
			}
			return nil, nil
		}}, deliveryMetrics: observability.NewNoopNotificationDeliveryMetrics(), claimOwner: "recovery-a", retryPolicy: defaultNotificationRetryPolicy(), claimDuration: minimumNotificationClaimDuration(), now: func() time.Time { return time.Now().UTC() }}
		drain, err := NewNotificationRecoveryDrain(NotificationRecoveryDrainConfig{Notifier: notifier, Interval: 5 * time.Millisecond, BatchSize: 1})
		if err != nil {
			t.Fatalf("create drain failed: %v", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- drain.Run(ctx) }()
		select {
		case <-calls:
		case <-time.After(100 * time.Millisecond):
			t.Fatal("expected immediate iteration call")
		}
		cancel()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("expected canceled drain, got %v", err)
			}
		case <-time.After(100 * time.Millisecond):
			t.Fatal("expected drain to stop after cancellation")
		}
	})

	t.Run("does not overlap iterations in one process", func(t *testing.T) {
		entered := make(chan struct{}, 1)
		release := make(chan struct{})
		var calls atomic.Int32
		notifier := &BuildNotificationService{buildRepo: &fakeBuildRepository{build: domain.Build{ID: "build-1", Status: domain.BuildStatusFailed}}, deliveryRepo: &scriptedNotificationDeliveryRepo{listRecoverableFunc: func(context.Context, repository.NotificationDeliveryRecoverableScanInput) ([]domain.NotificationDelivery, error) {
			calls.Add(1)
			select {
			case entered <- struct{}{}:
			default:
			}
			<-release
			return nil, nil
		}}, deliveryMetrics: observability.NewNoopNotificationDeliveryMetrics(), claimOwner: "recovery-a", retryPolicy: defaultNotificationRetryPolicy(), claimDuration: minimumNotificationClaimDuration(), now: func() time.Time { return time.Now().UTC() }}
		drain, err := NewNotificationRecoveryDrain(NotificationRecoveryDrainConfig{Notifier: notifier, Interval: 5 * time.Millisecond, BatchSize: 1})
		if err != nil {
			t.Fatalf("create drain failed: %v", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- drain.Run(ctx) }()
		select {
		case <-entered:
		case <-time.After(100 * time.Millisecond):
			t.Fatal("expected iteration to enter")
		}
		cancel()
		close(release)
		<-done
		if calls.Load() != 1 {
			t.Fatalf("expected one in-flight iteration call, got %d", calls.Load())
		}
	})

	t.Run("continues after an iteration error", func(t *testing.T) {
		secondCall := make(chan struct{}, 1)
		var calls atomic.Int32
		notifier := &BuildNotificationService{buildRepo: &fakeBuildRepository{build: domain.Build{ID: "build-1", Status: domain.BuildStatusFailed}}, deliveryRepo: &scriptedNotificationDeliveryRepo{listRecoverableFunc: func(context.Context, repository.NotificationDeliveryRecoverableScanInput) ([]domain.NotificationDelivery, error) {
			if calls.Add(1) == 1 {
				return nil, errors.New("boom")
			}
			select {
			case secondCall <- struct{}{}:
			default:
			}
			return nil, nil
		}}, deliveryMetrics: observability.NewNoopNotificationDeliveryMetrics(), claimOwner: "recovery-a", retryPolicy: defaultNotificationRetryPolicy(), claimDuration: minimumNotificationClaimDuration(), now: func() time.Time { return time.Now().UTC() }}
		drain, err := NewNotificationRecoveryDrain(NotificationRecoveryDrainConfig{Notifier: notifier, Interval: 5 * time.Millisecond, BatchSize: 1})
		if err != nil {
			t.Fatalf("create drain failed: %v", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 1)
		go func() { done <- drain.Run(ctx) }()
		select {
		case <-secondCall:
		case <-time.After(100 * time.Millisecond):
			t.Fatal("expected drain to continue after iteration error")
		}
		cancel()
		<-done
	})
}

type recoveryDrainFixture struct {
	now              time.Time
	build            domain.Build
	sender           *recordingEmailSender
	metrics          *observability.InMemoryNotificationDeliveryMetrics
	deliveryRepo     *memoryrepo.NotificationDeliveryRepository
	subscriptionRepo *memoryrepo.NotificationSubscriptionRepository
	target           domain.NotificationTarget
	notifier         *BuildNotificationService
}

func newRecoveryDrainFixture(t *testing.T, senderErr error) recoveryDrainFixture {
	t.Helper()
	now := time.Date(2026, 7, 2, 18, 0, 0, 0, time.UTC)
	build := domain.Build{ID: "build-1", Status: domain.BuildStatusFailed, CreatedAt: now}
	deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
	subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
	target := mustCreateNotificationTarget(t, subscriptionRepo, "dev@example.com", true)
	sender := &recordingEmailSender{err: senderErr}
	metrics := observability.NewInMemoryNotificationDeliveryMetrics()
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com", Sender: sender, BuildRepo: &fakeBuildRepository{build: build}, DeliveryRepo: deliveryRepo, SubscriptionRepo: subscriptionRepo, ClaimOwner: "recovery-owner", DeliveryMetrics: metrics})
	if err != nil {
		t.Fatalf("create recovery notifier failed: %v", err)
	}
	notifier.now = func() time.Time { return now }
	return recoveryDrainFixture{now: now, build: build, sender: sender, metrics: metrics, deliveryRepo: deliveryRepo, subscriptionRepo: subscriptionRepo, target: target, notifier: notifier}
}

func (f recoveryDrainFixture) newDrain(t *testing.T) *NotificationRecoveryDrain {
	t.Helper()
	drain, err := NewNotificationRecoveryDrain(NotificationRecoveryDrainConfig{Notifier: f.notifier, Interval: time.Millisecond, BatchSize: 10})
	if err != nil {
		t.Fatalf("create recovery drain failed: %v", err)
	}
	drain.now = func() time.Time { return f.now }
	return drain
}

func seedDueRetryDelivery(t *testing.T, repo *memoryrepo.NotificationDeliveryRepository, target domain.NotificationTarget, buildID string, eventType domain.NotificationEventType, nextAttemptAt time.Time, attempts int, maxAttempts int) {
	t.Helper()
	kind, key, err := domain.NotificationSharedEmailTargetKey(target.ID)
	if err != nil {
		t.Fatalf("shared email key failed: %v", err)
	}
	nextAttemptAt = nextAttemptAt.UTC()
	lastAttemptAt := nextAttemptAt.Add(-time.Minute)
	retryable := domain.NotificationDeliveryFailureCategoryRetryable
	if _, err := repo.Create(context.Background(), domain.NotificationDelivery{ID: key, BuildID: buildID, EventType: eventType, Transport: domain.NotificationTransportEmail, DestinationKind: kind, DestinationKey: key, NotificationTargetID: &target.ID, Recipient: target.Recipient, MaxAttempts: maxAttempts}); err != nil {
		t.Fatalf("create due retry delivery failed: %v", err)
	}
	if _, err := repo.Update(context.Background(), domain.NotificationDelivery{ID: key, BuildID: buildID, EventType: eventType, Transport: domain.NotificationTransportEmail, DestinationKind: kind, DestinationKey: key, NotificationTargetID: &target.ID, Recipient: target.Recipient, Status: domain.NotificationDeliveryStatusRetryWaiting, Attempts: attempts, MaxAttempts: maxAttempts, LastAttemptAt: &lastAttemptAt, NextAttemptAt: &nextAttemptAt, FailureCategory: &retryable, UpdatedAt: nextAttemptAt}); err != nil {
		t.Fatalf("update due retry delivery failed: %v", err)
	}
}

func seedStaleClaimDelivery(t *testing.T, repo *memoryrepo.NotificationDeliveryRepository, target domain.NotificationTarget, buildID string, eventType domain.NotificationEventType, claimedAt time.Time, claimExpiresAt time.Time) repository.NotificationDeliveryClaimResult {
	t.Helper()
	kind, key, err := domain.NotificationSharedEmailTargetKey(target.ID)
	if err != nil {
		t.Fatalf("shared email key failed: %v", err)
	}
	result, err := repo.AcquireForDelivery(context.Background(), repository.NotificationDeliveryClaimInput{Delivery: domain.NotificationDelivery{BuildID: buildID, EventType: eventType, Transport: domain.NotificationTransportEmail, DestinationKind: kind, DestinationKey: key, NotificationTargetID: &target.ID, Recipient: target.Recipient}, ClaimOwner: "old-owner", Now: claimedAt, ClaimDuration: claimExpiresAt.Sub(claimedAt), MaxAttempts: 3})
	if err != nil {
		t.Fatalf("seed stale claim acquire failed: %v", err)
	}
	if _, err := repo.Update(context.Background(), domain.NotificationDelivery{ID: result.Delivery.ID, BuildID: buildID, EventType: eventType, Transport: domain.NotificationTransportEmail, DestinationKind: kind, DestinationKey: key, NotificationTargetID: &target.ID, Recipient: target.Recipient, Status: domain.NotificationDeliveryStatusSending, Attempts: 1, MaxAttempts: 3, LastAttemptAt: &claimedAt, ClaimedAt: &claimedAt, ClaimExpiresAt: &claimExpiresAt, ClaimedBy: strPtr("old-owner"), UpdatedAt: claimedAt}); err != nil {
		t.Fatalf("update stale claim delivery failed: %v", err)
	}
	return result
}
