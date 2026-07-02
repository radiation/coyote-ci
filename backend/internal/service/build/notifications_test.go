package build

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	platformemail "github.com/radiation/coyote-ci/backend/internal/platform/email"
	platformslack "github.com/radiation/coyote-ci/backend/internal/platform/slack"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	memoryrepo "github.com/radiation/coyote-ci/backend/internal/repository/memory"
	steprunner "github.com/radiation/coyote-ci/backend/internal/runner"
	"github.com/radiation/coyote-ci/backend/internal/source"
)

type recordingEmailSender struct {
	messages []platformemail.Message
	err      error
}

func (s *recordingEmailSender) SendText(_ context.Context, message platformemail.Message) error {
	s.messages = append(s.messages, message)
	return s.err
}

type recordingSlackSender struct {
	webhookURLs []string
	messages    []SlackWebhookMessage
	err         error
}

func (s *recordingSlackSender) Send(_ context.Context, webhookURL string, message SlackWebhookMessage) error {
	s.webhookURLs = append(s.webhookURLs, webhookURL)
	s.messages = append(s.messages, message)
	return s.err
}

type recordingSlackDMClient struct {
	tokens   []string
	userIDs  []string
	messages []platformslack.Message
	err      error
}

func (c *recordingSlackDMClient) PostDirectMessage(_ context.Context, token string, slackUserID string, message platformslack.Message) (platformslack.PostMessageResult, error) {
	c.tokens = append(c.tokens, token)
	c.userIDs = append(c.userIDs, slackUserID)
	c.messages = append(c.messages, message)
	if c.err != nil {
		return platformslack.PostMessageResult{}, c.err
	}
	channelID := "D123"
	timestamp := "1710000000.000100"
	return platformslack.PostMessageResult{ChannelID: &channelID, Timestamp: &timestamp}, nil
}

type scriptedNotificationDeliveryRepo struct {
	acquireFunc                func(context.Context, repository.NotificationDeliveryClaimInput) (repository.NotificationDeliveryClaimResult, error)
	markSentFunc               func(context.Context, repository.NotificationDeliveryMarkSentInput) (repository.NotificationDeliveryUpdateResult, error)
	recordRetryableFailureFunc func(context.Context, repository.NotificationDeliveryRecordFailureInput) (repository.NotificationDeliveryUpdateResult, error)
	recordPermanentFailureFunc func(context.Context, repository.NotificationDeliveryRecordFailureInput) (repository.NotificationDeliveryUpdateResult, error)
	recordExhaustedFailureFunc func(context.Context, repository.NotificationDeliveryRecordFailureInput) (repository.NotificationDeliveryUpdateResult, error)
	createFunc                 func(context.Context, domain.NotificationDelivery) (domain.NotificationDelivery, error)
	getFunc                    func(context.Context, string, domain.NotificationEventType, string) (domain.NotificationDelivery, error)
	updateFunc                 func(context.Context, domain.NotificationDelivery) (domain.NotificationDelivery, error)
}

func (r *scriptedNotificationDeliveryRepo) AcquireForDelivery(ctx context.Context, input repository.NotificationDeliveryClaimInput) (repository.NotificationDeliveryClaimResult, error) {
	if r.acquireFunc != nil {
		return r.acquireFunc(ctx, input)
	}
	if r.createFunc != nil {
		created, err := r.createFunc(ctx, input.Delivery)
		if err != nil {
			if errors.Is(err, repository.ErrNotificationDeliveryDuplicate) {
				if r.getFunc == nil {
					return repository.NotificationDeliveryClaimResult{}, err
				}
				existing, getErr := r.getFunc(ctx, input.Delivery.BuildID, input.Delivery.EventType, input.Delivery.Recipient)
				if getErr != nil {
					return repository.NotificationDeliveryClaimResult{}, getErr
				}
				return repository.NotificationDeliveryClaimResult{
					Delivery: existing,
					Outcome:  repository.NotificationDeliveryClaimOutcomeFromExisting(existing, input.Now),
				}, nil
			}
			return repository.NotificationDeliveryClaimResult{}, err
		}
		claimedAt := input.Now.UTC()
		claimExpiresAt := claimedAt.Add(input.ClaimDuration)
		created.Status = domain.NotificationDeliveryStatusSending
		created.Attempts = 1
		created.MaxAttempts = input.MaxAttempts
		created.LastAttemptAt = &claimedAt
		created.ClaimedAt = &claimedAt
		created.ClaimExpiresAt = &claimExpiresAt
		created.ClaimedBy = strPtr(input.ClaimOwner)
		return repository.NotificationDeliveryClaimResult{Delivery: created, Outcome: repository.NotificationDeliveryClaimOutcomeCreatedClaimed}, nil
	}
	return repository.NotificationDeliveryClaimResult{}, nil
}

func (r *scriptedNotificationDeliveryRepo) MarkSent(ctx context.Context, input repository.NotificationDeliveryMarkSentInput) (repository.NotificationDeliveryUpdateResult, error) {
	if r.markSentFunc != nil {
		return r.markSentFunc(ctx, input)
	}
	if r.updateFunc == nil {
		return repository.NotificationDeliveryUpdateResult{}, nil
	}
	updated, err := r.updateFunc(ctx, domain.NotificationDelivery{ID: input.DeliveryID, Status: domain.NotificationDeliveryStatusSent, SentAt: &input.SentAt, UpdatedAt: input.SentAt})
	if err != nil {
		return repository.NotificationDeliveryUpdateResult{}, err
	}
	return repository.NotificationDeliveryUpdateResult{Delivery: updated, Outcome: repository.NotificationDeliveryUpdateOutcomeUpdated}, nil
}

func (r *scriptedNotificationDeliveryRepo) RecordRetryableFailure(ctx context.Context, input repository.NotificationDeliveryRecordFailureInput) (repository.NotificationDeliveryUpdateResult, error) {
	if r.recordRetryableFailureFunc != nil {
		return r.recordRetryableFailureFunc(ctx, input)
	}
	if r.updateFunc == nil {
		return repository.NotificationDeliveryUpdateResult{}, nil
	}
	updated, err := r.updateFunc(ctx, domain.NotificationDelivery{ID: input.DeliveryID, Status: domain.NotificationDeliveryStatusRetryWaiting, LastError: input.LastError, UpdatedAt: input.FailedAt, NextAttemptAt: input.NextAttemptAt})
	if err != nil {
		return repository.NotificationDeliveryUpdateResult{}, err
	}
	return repository.NotificationDeliveryUpdateResult{Delivery: updated, Outcome: repository.NotificationDeliveryUpdateOutcomeUpdated}, nil
}

func (r *scriptedNotificationDeliveryRepo) RecordPermanentFailure(ctx context.Context, input repository.NotificationDeliveryRecordFailureInput) (repository.NotificationDeliveryUpdateResult, error) {
	if r.recordPermanentFailureFunc != nil {
		return r.recordPermanentFailureFunc(ctx, input)
	}
	if r.updateFunc == nil {
		return repository.NotificationDeliveryUpdateResult{}, nil
	}
	updated, err := r.updateFunc(ctx, domain.NotificationDelivery{ID: input.DeliveryID, Status: domain.NotificationDeliveryStatusFailedPermanent, LastError: input.LastError, UpdatedAt: input.FailedAt})
	if err != nil {
		return repository.NotificationDeliveryUpdateResult{}, err
	}
	return repository.NotificationDeliveryUpdateResult{Delivery: updated, Outcome: repository.NotificationDeliveryUpdateOutcomeUpdated}, nil
}

func (r *scriptedNotificationDeliveryRepo) RecordExhaustedFailure(ctx context.Context, input repository.NotificationDeliveryRecordFailureInput) (repository.NotificationDeliveryUpdateResult, error) {
	if r.recordExhaustedFailureFunc != nil {
		return r.recordExhaustedFailureFunc(ctx, input)
	}
	if r.updateFunc == nil {
		return repository.NotificationDeliveryUpdateResult{}, nil
	}
	updated, err := r.updateFunc(ctx, domain.NotificationDelivery{ID: input.DeliveryID, Status: domain.NotificationDeliveryStatusFailedExhausted, LastError: input.LastError, UpdatedAt: input.FailedAt})
	if err != nil {
		return repository.NotificationDeliveryUpdateResult{}, err
	}
	return repository.NotificationDeliveryUpdateResult{Delivery: updated, Outcome: repository.NotificationDeliveryUpdateOutcomeUpdated}, nil
}

func (r *scriptedNotificationDeliveryRepo) Create(ctx context.Context, delivery domain.NotificationDelivery) (domain.NotificationDelivery, error) {
	if r.createFunc != nil {
		return r.createFunc(ctx, delivery)
	}
	return domain.NotificationDelivery{}, nil
}

func (r *scriptedNotificationDeliveryRepo) GetByBuildEventRecipient(ctx context.Context, buildID string, eventType domain.NotificationEventType, recipient string) (domain.NotificationDelivery, error) {
	if r.getFunc != nil {
		return r.getFunc(ctx, buildID, eventType, recipient)
	}
	return domain.NotificationDelivery{}, nil
}

func (r *scriptedNotificationDeliveryRepo) Update(ctx context.Context, delivery domain.NotificationDelivery) (domain.NotificationDelivery, error) {
	if r.updateFunc != nil {
		return r.updateFunc(ctx, delivery)
	}
	return delivery, nil
}

func mustGetNotificationDelivery(t *testing.T, repo repository.NotificationDeliveryRepository, buildID string, eventType domain.NotificationEventType, recipient string) domain.NotificationDelivery {
	t.Helper()

	delivery, err := repo.GetByBuildEventRecipient(context.Background(), buildID, eventType, recipient)
	if err != nil {
		t.Fatalf("get notification delivery failed: %v", err)
	}

	return delivery
}

func mustCreateNotificationTarget(t *testing.T, repo *memoryrepo.NotificationSubscriptionRepository, recipient string, enabled bool) domain.NotificationTarget {
	t.Helper()

	target, err := repo.CreateTarget(context.Background(), domain.NotificationTarget{
		Type:      domain.NotificationTargetTypeEmail,
		Origin:    domain.NotificationTargetOriginManual,
		Name:      recipient,
		Recipient: recipient,
		Enabled:   enabled,
	})
	if err != nil {
		t.Fatalf("create notification target failed: %v", err)
	}

	return target
}

func mustCreateNotificationUser(t *testing.T, repo *memoryrepo.UserRepository, email string) domain.User {
	t.Helper()

	user, err := repo.Create(context.Background(), domain.User{
		Email:      strings.ToLower(strings.TrimSpace(email)),
		GlobalRole: domain.GlobalRoleUser,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("create notification user failed: %v", err)
	}

	return user
}

func mustEnsureOwnedNotificationTarget(t *testing.T, repo *memoryrepo.NotificationSubscriptionRepository, userID string, recipient string, enabled bool) domain.NotificationTarget {
	t.Helper()

	target, err := repo.CreateTarget(context.Background(), domain.NotificationTarget{
		OwnerUserID: &userID,
		Type:        domain.NotificationTargetTypeEmail,
		Origin:      domain.NotificationTargetOriginManual,
		Name:        recipient,
		Recipient:   recipient,
		Enabled:     enabled,
	})
	if err != nil {
		t.Fatalf("create owned notification target failed: %v", err)
	}

	return target
}

func mustGetOwnedEmailTargetID(t *testing.T, repo *memoryrepo.NotificationSubscriptionRepository, userID string) string {
	t.Helper()

	target, err := repo.GetOwnedEmailTargetByUserID(context.Background(), userID)
	if err != nil {
		t.Fatalf("get owned email target failed: %v", err)
	}
	return target.ID
}

func mustUpsertNotificationPreference(t *testing.T, repo *memoryrepo.UserNotificationPreferenceRepository, userID string, enabled bool) domain.UserNotificationPreference {
	t.Helper()

	return mustUpsertNotificationPreferenceFlags(t, repo, userID, enabled, false)
}

func mustUpsertNotificationPreferenceFlags(t *testing.T, repo *memoryrepo.UserNotificationPreferenceRepository, userID string, failureEnabled bool, successEnabled bool) domain.UserNotificationPreference {
	t.Helper()

	preference, err := repo.Upsert(context.Background(), domain.UserNotificationPreference{
		UserID:                          userID,
		CommitAuthorFailureEmailEnabled: failureEnabled,
		CommitAuthorFailureEmailSource:  domain.UserNotificationPreferenceSourceUser,
		CommitAuthorSuccessEmailEnabled: successEnabled,
		CreatedAt:                       time.Now().UTC(),
		UpdatedAt:                       time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("upsert notification preference failed: %v", err)
	}

	return preference
}

func mustUpsertNotificationPreferenceChannels(t *testing.T, repo *memoryrepo.UserNotificationPreferenceRepository, userID string, failureEmailEnabled bool, failureSlackEnabled bool, successEmailEnabled bool, successSlackEnabled bool) domain.UserNotificationPreference {
	t.Helper()

	successSource := domain.UserNotificationPreferenceSourceUser
	preference, err := repo.Upsert(context.Background(), domain.UserNotificationPreference{
		UserID:                          userID,
		CommitAuthorFailureEmailEnabled: failureEmailEnabled,
		CommitAuthorFailureSlackEnabled: failureSlackEnabled,
		CommitAuthorFailureEmailSource:  domain.UserNotificationPreferenceSourceUser,
		CommitAuthorSuccessEmailEnabled: successEmailEnabled,
		CommitAuthorSuccessSlackEnabled: successSlackEnabled,
		CommitAuthorSuccessEmailSource:  &successSource,
		CreatedAt:                       time.Now().UTC(),
		UpdatedAt:                       time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("upsert notification preference failed: %v", err)
	}

	return preference
}

type personalCommitAuthorNotificationFixture struct {
	notifier          *BuildNotificationService
	emailSender       *recordingEmailSender
	slackSender       *recordingSlackSender
	slackClient       *recordingSlackDMClient
	deliveryRepo      *memoryrepo.NotificationDeliveryRepository
	subscriptionRepo  *memoryrepo.NotificationSubscriptionRepository
	userRepo          *memoryrepo.UserRepository
	preferenceRepo    *memoryrepo.UserNotificationPreferenceRepository
	identityRepo      *memoryrepo.UserSlackIdentityRepository
	workspaceRepo     *memoryrepo.SlackWorkspaceIntegrationRepository
	authorUser        domain.User
	authorEmail       string
	workspaceID       string
	slackUserID       string
	sharedSlackTarget domain.NotificationTarget
}

type personalCommitAuthorNotificationOptions struct {
	failureEmailEnabled bool
	failureSlackEnabled bool
	successEmailEnabled bool
	successSlackEnabled bool
	createEmailTarget   bool
	emailTargetEnabled  bool
	createWorkspace     bool
	workspaceEnabled    bool
	createIdentity      bool
	identityEnabled     bool
	identityWorkspaceID string
	slackUserID         string
	sharedSlackWebhook  bool
}

func newPersonalCommitAuthorNotificationFixture(t *testing.T, opts personalCommitAuthorNotificationOptions) personalCommitAuthorNotificationFixture {
	t.Helper()

	if opts.slackUserID == "" {
		opts.slackUserID = "U123"
	}
	if opts.identityWorkspaceID == "" {
		opts.identityWorkspaceID = "workspace-integration-1"
	}
	if !opts.createEmailTarget {
		opts.emailTargetEnabled = false
	}

	emailSender := &recordingEmailSender{}
	slackSender := &recordingSlackSender{}
	slackClient := &recordingSlackDMClient{}
	deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
	subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
	userRepo := memoryrepo.NewUserRepository()
	preferenceRepo := memoryrepo.NewUserNotificationPreferenceRepository()
	identityRepo := memoryrepo.NewUserSlackIdentityRepository()
	workspaceRepo := memoryrepo.NewSlackWorkspaceIntegrationRepository()
	workspaceRepo.SetUserSlackIdentityRepository(identityRepo)
	var sharedSlackTarget domain.NotificationTarget

	authorEmail := "author@example.com"
	authorUser := mustCreateNotificationUser(t, userRepo, authorEmail)
	if opts.createEmailTarget {
		mustEnsureOwnedNotificationTarget(t, subscriptionRepo, authorUser.ID, authorEmail, opts.emailTargetEnabled)
	}
	mustUpsertNotificationPreferenceChannels(t, preferenceRepo, authorUser.ID, opts.failureEmailEnabled, opts.failureSlackEnabled, opts.successEmailEnabled, opts.successSlackEnabled)

	now := time.Now().UTC()
	if opts.createWorkspace {
		if _, err := workspaceRepo.ConnectOrReplace(context.Background(), domain.SlackWorkspaceIntegration{
			ID:             "workspace-integration-1",
			WorkspaceID:    "T123",
			BotTokenSecret: "xoxb-secret",
			Enabled:        opts.workspaceEnabled,
			ConnectedAt:    now,
			CreatedAt:      now,
			UpdatedAt:      now,
		}, true); err != nil {
			t.Fatalf("connect slack workspace failed: %v", err)
		}
	}
	if opts.createIdentity {
		if _, err := identityRepo.Upsert(context.Background(), domain.UserSlackIdentity{
			ID:                          "identity-1",
			UserID:                      authorUser.ID,
			SlackWorkspaceIntegrationID: opts.identityWorkspaceID,
			SlackUserID:                 opts.slackUserID,
			SlackDisplayName:            strPtr("Display Name"),
			SlackRealName:               strPtr("Real Name"),
			SlackHandle:                 strPtr("display"),
			SlackEmail:                  strPtr("author@example.com"),
			Enabled:                     opts.identityEnabled,
			LinkedAt:                    now,
			CreatedAt:                   now,
			UpdatedAt:                   now,
		}); err != nil {
			t.Fatalf("upsert slack identity failed: %v", err)
		}
	}

	if opts.sharedSlackWebhook {
		projectID := "project-1"
		sharedSlackTarget = mustCreateSlackNotificationTarget(t, subscriptionRepo, "https://hooks.slack.example/services/T/B/X", true)
		mustCreateNotificationSubscription(t, subscriptionRepo, sharedSlackTarget.ID, &projectID, nil, domain.NotificationEventTypeBuildFailed, true)
	}

	notifier, err := NewBuildNotificationService(BuildNotificationConfig{
		Enabled:          true,
		Sender:           emailSender,
		SlackSender:      slackSender,
		SlackClient:      slackClient,
		DeliveryRepo:     deliveryRepo,
		SubscriptionRepo: subscriptionRepo,
		UserRepo:         userRepo,
		PreferenceRepo:   preferenceRepo,
		IdentityRepo:     identityRepo,
		WorkspaceRepo:    workspaceRepo,
		PublicBaseURL:    "https://ci.example.com/",
	})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	return personalCommitAuthorNotificationFixture{
		notifier:          notifier,
		emailSender:       emailSender,
		slackSender:       slackSender,
		slackClient:       slackClient,
		deliveryRepo:      deliveryRepo,
		subscriptionRepo:  subscriptionRepo,
		userRepo:          userRepo,
		preferenceRepo:    preferenceRepo,
		identityRepo:      identityRepo,
		workspaceRepo:     workspaceRepo,
		authorUser:        authorUser,
		authorEmail:       authorEmail,
		workspaceID:       "workspace-integration-1",
		slackUserID:       opts.slackUserID,
		sharedSlackTarget: sharedSlackTarget,
	}
}

func strPtr(value string) *string {
	return &value
}

func failureCategoryPtr(value domain.NotificationDeliveryFailureCategory) *domain.NotificationDeliveryFailureCategory {
	return &value
}

func assertNotificationDeliveryAbsent(t *testing.T, repo repository.NotificationDeliveryRepository, buildID string, eventType domain.NotificationEventType, recipient string) {
	t.Helper()

	_, err := repo.GetByBuildEventRecipient(context.Background(), buildID, eventType, recipient)
	if !errors.Is(err, repository.ErrNotificationDeliveryNotFound) {
		t.Fatalf("expected notification delivery to be absent for %s, got %v", recipient, err)
	}
}

func TestBuildNotificationService_CommitAuthorSuccessPreferenceDelivery(t *testing.T) {
	type successFixture struct {
		notifier         *BuildNotificationService
		sender           *recordingEmailSender
		deliveryRepo     *memoryrepo.NotificationDeliveryRepository
		subscriptionRepo *memoryrepo.NotificationSubscriptionRepository
		userRepo         *memoryrepo.UserRepository
		preferenceRepo   *memoryrepo.UserNotificationPreferenceRepository
		authorUser       domain.User
	}

	newFixture := func(t *testing.T, authorEmail string, createOwnedTarget bool, targetEnabled bool, failureEnabled bool, successEnabled bool) successFixture {
		t.Helper()

		sender := &recordingEmailSender{}
		deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
		userRepo := memoryrepo.NewUserRepository()
		preferenceRepo := memoryrepo.NewUserNotificationPreferenceRepository()
		subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
		authorUser := mustCreateNotificationUser(t, userRepo, authorEmail)
		if createOwnedTarget {
			mustEnsureOwnedNotificationTarget(t, subscriptionRepo, authorUser.ID, authorEmail, targetEnabled)
		}
		mustUpsertNotificationPreferenceFlags(t, preferenceRepo, authorUser.ID, failureEnabled, successEnabled)

		notifier, err := NewBuildNotificationService(BuildNotificationConfig{
			Enabled:          true,
			Sender:           sender,
			DeliveryRepo:     deliveryRepo,
			SubscriptionRepo: subscriptionRepo,
			UserRepo:         userRepo,
			PreferenceRepo:   preferenceRepo,
		})
		if err != nil {
			t.Fatalf("create notifier failed: %v", err)
		}

		return successFixture{
			notifier:         notifier,
			sender:           sender,
			deliveryRepo:     deliveryRepo,
			subscriptionRepo: subscriptionRepo,
			userRepo:         userRepo,
			preferenceRepo:   preferenceRepo,
			authorUser:       authorUser,
		}
	}

	t.Run("successful build sends to owned enabled personal target when success preference is enabled", func(t *testing.T) {
		authorEmail := "author@example.com"
		fixture := newFixture(t, authorEmail, true, true, false, true)

		if err := fixture.notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-success-1", Status: domain.BuildStatusSuccess, SourceAuthorEmail: &authorEmail}); err != nil {
			t.Fatalf("notify terminal build failed: %v", err)
		}
		if len(fixture.sender.messages) != 1 || fixture.sender.messages[0].To != "<author@example.com>" {
			t.Fatalf("expected one personal success delivery to author target, got %+v", fixture.sender.messages)
		}
		delivery := mustGetNotificationDelivery(t, fixture.deliveryRepo, "build-success-1", domain.NotificationEventTypeBuildSucceeded, "<author@example.com>")
		if delivery.Status != domain.NotificationDeliveryStatusSent {
			t.Fatalf("expected sent success delivery, got %q", delivery.Status)
		}
	})

	t.Run("success-disabled author receives no personal success delivery", func(t *testing.T) {
		authorEmail := "author@example.com"
		fixture := newFixture(t, authorEmail, true, true, false, false)

		if err := fixture.notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-success-2", Status: domain.BuildStatusSuccess, SourceAuthorEmail: &authorEmail}); err != nil {
			t.Fatalf("notify terminal build failed: %v", err)
		}
		if len(fixture.sender.messages) != 0 {
			t.Fatalf("expected no personal success delivery, got %+v", fixture.sender.messages)
		}
	})

	t.Run("failure preference alone does not cause success delivery and remains independently authoritative", func(t *testing.T) {
		authorEmail := "author@example.com"
		fixture := newFixture(t, authorEmail, true, true, true, false)

		if err := fixture.notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-success-3", Status: domain.BuildStatusSuccess, SourceAuthorEmail: &authorEmail}); err != nil {
			t.Fatalf("notify success build failed: %v", err)
		}
		if len(fixture.sender.messages) != 0 {
			t.Fatalf("expected no success delivery when only failure preference is enabled, got %+v", fixture.sender.messages)
		}

		if err := fixture.notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-failure-3", Status: domain.BuildStatusFailed, SourceAuthorEmail: &authorEmail}); err != nil {
			t.Fatalf("notify failure build failed: %v", err)
		}
		if len(fixture.sender.messages) != 1 || fixture.sender.messages[0].To != "<author@example.com>" {
			t.Fatalf("expected failure delivery to remain enabled independently, got %+v", fixture.sender.messages)
		}
	})

	t.Run("success preference alone does not change existing failure delivery behavior", func(t *testing.T) {
		authorEmail := "author@example.com"
		fixture := newFixture(t, authorEmail, true, true, false, true)

		if err := fixture.notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-failure-4", Status: domain.BuildStatusFailed, SourceAuthorEmail: &authorEmail}); err != nil {
			t.Fatalf("notify failure build failed: %v", err)
		}
		if len(fixture.sender.messages) != 0 {
			t.Fatalf("expected failure delivery to remain disabled, got %+v", fixture.sender.messages)
		}

		if err := fixture.notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-success-4", Status: domain.BuildStatusSuccess, SourceAuthorEmail: &authorEmail}); err != nil {
			t.Fatalf("notify success build failed: %v", err)
		}
		if len(fixture.sender.messages) != 1 || fixture.sender.messages[0].To != "<author@example.com>" {
			t.Fatalf("expected success delivery to remain enabled independently, got %+v", fixture.sender.messages)
		}
	})

	t.Run("disabled owned personal target prevents success delivery", func(t *testing.T) {
		authorEmail := "author@example.com"
		fixture := newFixture(t, authorEmail, true, false, false, true)

		if err := fixture.notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-success-5", Status: domain.BuildStatusSuccess, SourceAuthorEmail: &authorEmail}); err != nil {
			t.Fatalf("notify terminal build failed: %v", err)
		}
		if len(fixture.sender.messages) != 0 {
			t.Fatalf("expected disabled target to prevent success delivery, got %+v", fixture.sender.messages)
		}
	})

	t.Run("missing owned personal target prevents success delivery", func(t *testing.T) {
		authorEmail := "author@example.com"
		fixture := newFixture(t, authorEmail, false, false, false, true)

		if err := fixture.notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-success-6", Status: domain.BuildStatusSuccess, SourceAuthorEmail: &authorEmail}); err != nil {
			t.Fatalf("notify terminal build failed: %v", err)
		}
		if len(fixture.sender.messages) != 0 {
			t.Fatalf("expected missing target to prevent success delivery, got %+v", fixture.sender.messages)
		}
	})

	t.Run("target owned by another user is not used", func(t *testing.T) {
		authorEmail := "author@example.com"
		fixture := newFixture(t, authorEmail, false, false, false, true)
		otherOwner := mustCreateNotificationUser(t, fixture.userRepo, "owner@example.com")
		mustEnsureOwnedNotificationTarget(t, fixture.subscriptionRepo, otherOwner.ID, authorEmail, true)

		if err := fixture.notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-success-7", Status: domain.BuildStatusSuccess, SourceAuthorEmail: &authorEmail}); err != nil {
			t.Fatalf("notify terminal build failed: %v", err)
		}
		if len(fixture.sender.messages) != 0 {
			t.Fatalf("expected foreign owned target to be ignored, got %+v", fixture.sender.messages)
		}
	})

	t.Run("unknown commit author is skipped safely", func(t *testing.T) {
		fixture := newFixture(t, "author@example.com", true, true, false, true)
		unknown := "unknown@example.com"

		if err := fixture.notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-success-8", Status: domain.BuildStatusSuccess, SourceAuthorEmail: &unknown}); err != nil {
			t.Fatalf("notify terminal build failed: %v", err)
		}
		if len(fixture.sender.messages) != 0 {
			t.Fatalf("expected unknown author to be skipped, got %+v", fixture.sender.messages)
		}
	})

	t.Run("case-normalized author email matching works for success delivery", func(t *testing.T) {
		fixture := newFixture(t, "author@example.com", true, true, false, true)
		mixedCase := "AUTHOR@EXAMPLE.COM"

		if err := fixture.notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-success-9", Status: domain.BuildStatusSuccess, SourceAuthorEmail: &mixedCase}); err != nil {
			t.Fatalf("notify terminal build failed: %v", err)
		}
		if len(fixture.sender.messages) != 1 || fixture.sender.messages[0].To != "<author@example.com>" {
			t.Fatalf("expected normalized success delivery, got %+v", fixture.sender.messages)
		}
	})

	t.Run("manual success subscription plus personal success preference dedupes same target", func(t *testing.T) {
		authorEmail := "author@example.com"
		fixture := newFixture(t, authorEmail, true, true, false, true)
		targets, err := fixture.subscriptionRepo.ListTargets(context.Background())
		if err != nil {
			t.Fatalf("list targets failed: %v", err)
		}
		projectID := "project-1"
		mustCreateNotificationSubscription(t, fixture.subscriptionRepo, targets[0].ID, &projectID, nil, domain.NotificationEventTypeBuildSucceeded, true)

		if err := fixture.notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-success-10", ProjectID: projectID, Status: domain.BuildStatusSuccess, SourceAuthorEmail: &authorEmail}); err != nil {
			t.Fatalf("notify terminal build failed: %v", err)
		}
		if len(fixture.sender.messages) != 1 || fixture.sender.messages[0].To != "<author@example.com>" {
			t.Fatalf("expected same-target success dedupe, got %+v", fixture.sender.messages)
		}
		delivery := mustGetNotificationDelivery(t, fixture.deliveryRepo, "build-success-10", domain.NotificationEventTypeBuildSucceeded, "<author@example.com>")
		if delivery.Attempts != 1 {
			t.Fatalf("expected one success delivery attempt after same-target dedupe, got %d", delivery.Attempts)
		}
	})

	t.Run("distinct targets still each receive one success delivery", func(t *testing.T) {
		authorEmail := "author@example.com"
		fixture := newFixture(t, authorEmail, true, true, false, true)
		jobID := "job-1"
		manualTarget := mustCreateNotificationTarget(t, fixture.subscriptionRepo, "ops@example.com", true)
		mustCreateNotificationSubscription(t, fixture.subscriptionRepo, manualTarget.ID, nil, &jobID, domain.NotificationEventTypeBuildSucceeded, true)

		if err := fixture.notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-success-11", JobID: &jobID, Status: domain.BuildStatusSuccess, SourceAuthorEmail: &authorEmail}); err != nil {
			t.Fatalf("notify terminal build failed: %v", err)
		}
		if len(fixture.sender.messages) != 2 {
			t.Fatalf("expected distinct success targets to receive delivery, got %+v", fixture.sender.messages)
		}
	})

	t.Run("success and failure events remain distinct for dedupe", func(t *testing.T) {
		authorEmail := "author@example.com"
		fixture := newFixture(t, authorEmail, true, true, true, true)

		if err := fixture.notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-shared-12", Status: domain.BuildStatusSuccess, SourceAuthorEmail: &authorEmail}); err != nil {
			t.Fatalf("notify success build failed: %v", err)
		}
		if err := fixture.notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-shared-12", Status: domain.BuildStatusFailed, SourceAuthorEmail: &authorEmail}); err != nil {
			t.Fatalf("notify failure build failed: %v", err)
		}
		if len(fixture.sender.messages) != 2 {
			t.Fatalf("expected distinct success and failure deliveries, got %+v", fixture.sender.messages)
		}
		mustGetNotificationDelivery(t, fixture.deliveryRepo, "build-shared-12", domain.NotificationEventTypeBuildSucceeded, "<author@example.com>")
		mustGetNotificationDelivery(t, fixture.deliveryRepo, "build-shared-12", domain.NotificationEventTypeBuildFailed, "<author@example.com>")
	})

	t.Run("existing failed success delivery record is skipped without retry", func(t *testing.T) {
		authorEmail := "author@example.com"
		fixture := newFixture(t, authorEmail, true, true, false, true)
		message := "smtp unavailable"
		if _, err := fixture.deliveryRepo.Create(context.Background(), domain.NotificationDelivery{
			BuildID:         "build-success-13",
			EventType:       domain.NotificationEventTypeBuildSucceeded,
			Transport:       domain.NotificationTransportEmail,
			DestinationKind: domain.NotificationDestinationKindPersonalEmail,
			DestinationKey:  "email-personal:" + mustGetOwnedEmailTargetID(t, fixture.subscriptionRepo, fixture.authorUser.ID),
			RecipientUserID: &fixture.authorUser.ID,
			Recipient:       "<author@example.com>",
			Status:          domain.NotificationDeliveryStatusFailedPermanent,
			Attempts:        1,
			MaxAttempts:     3,
			FailureCategory: failureCategoryPtr(domain.NotificationDeliveryFailureCategoryPermanent),
			LastError:       &message,
		}); err != nil {
			t.Fatalf("seed failed success delivery record failed: %v", err)
		}

		if err := fixture.notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-success-13", Status: domain.BuildStatusSuccess, SourceAuthorEmail: &authorEmail}); err != nil {
			t.Fatalf("notify with existing failed success record should skip cleanly, got %v", err)
		}
		if len(fixture.sender.messages) != 0 {
			t.Fatalf("expected no resend when failed success delivery record already exists, got %+v", fixture.sender.messages)
		}
		delivery := mustGetNotificationDelivery(t, fixture.deliveryRepo, "build-success-13", domain.NotificationEventTypeBuildSucceeded, "<author@example.com>")
		if delivery.Status != domain.NotificationDeliveryStatusFailedPermanent {
			t.Fatalf("expected failed success delivery status to remain unchanged, got %q", delivery.Status)
		}
		if delivery.Attempts != 1 {
			t.Fatalf("expected success delivery attempts to remain 1, got %d", delivery.Attempts)
		}
	})
}

func mustCreateSlackNotificationTarget(t *testing.T, repo *memoryrepo.NotificationSubscriptionRepository, webhookURL string, enabled bool) domain.NotificationTarget {
	t.Helper()

	target, err := repo.CreateTarget(context.Background(), domain.NotificationTarget{
		Type:      domain.NotificationTargetTypeSlackWebhook,
		Origin:    domain.NotificationTargetOriginManual,
		Name:      "slack",
		Recipient: webhookURL,
		Enabled:   enabled,
	})
	if err != nil {
		t.Fatalf("create slack notification target failed: %v", err)
	}

	return target
}

func mustCreateNotificationSubscription(t *testing.T, repo *memoryrepo.NotificationSubscriptionRepository, targetID string, projectID *string, jobID *string, eventType domain.NotificationEventType, enabled bool) domain.NotificationSubscription {
	t.Helper()

	subscription, err := repo.CreateSubscription(context.Background(), domain.NotificationSubscription{
		TargetID:  targetID,
		ProjectID: projectID,
		JobID:     jobID,
		EventType: eventType,
		Enabled:   enabled,
	})
	if err != nil {
		t.Fatalf("create notification subscription failed: %v", err)
	}

	return subscription
}

func TestBuildService_FailBuild_SendsNotificationWhenConfigured(t *testing.T) {
	now := time.Now().UTC()
	jobID := "job-1"
	deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
	buildRepo := &fakeBuildRepository{
		build: domain.Build{
			ID:          "build-1",
			ProjectID:   "project-1",
			JobID:       &jobID,
			BuildNumber: 42,
			Status:      domain.BuildStatusRunning,
			CreatedAt:   now,
		},
	}
	jobRepo := memoryrepo.NewJobRepository()
	projectRepo := memoryrepo.NewProjectRepository(jobRepo)
	if _, err := projectRepo.Create(context.Background(), domain.Project{ID: "project-1", Name: "Payments API", Slug: "payments-api", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create project failed: %v", err)
	}
	if _, err := jobRepo.Create(context.Background(), domain.Job{ID: jobID, ProjectID: "project-1", Name: "backend-ci", RepositoryURL: "https://github.com/example/payments.git", DefaultRef: "main", PipelineYAML: "version: 1", Enabled: true, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create job failed: %v", err)
	}
	sender := &recordingEmailSender{}
	subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{
		Enabled:          true,
		Recipients:       "dev@example.com",
		Sender:           sender,
		JobRepo:          jobRepo,
		ProjectRepo:      projectRepo,
		DeliveryRepo:     deliveryRepo,
		SubscriptionRepo: subscriptionRepo,
	})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	svc := NewBuildServiceFromConfig(buildRepo, nil, nil, BuildServiceConfig{BuildNotifier: notifier})
	build, err := svc.FailBuild(context.Background(), "build-1")
	if err != nil {
		t.Fatalf("fail build returned error: %v", err)
	}
	if build.Status != domain.BuildStatusFailed {
		t.Fatalf("expected failed build, got %q", build.Status)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("expected one notification email, got %d", len(sender.messages))
	}
	message := sender.messages[0]
	if message.To != "<dev@example.com>" {
		t.Fatalf("expected normalized recipient <dev@example.com>, got %q", message.To)
	}
	if !strings.Contains(message.Subject, "backend-ci (job-1)") || !strings.Contains(message.Subject, "build-1") || !strings.Contains(message.Subject, "failed") {
		t.Fatalf("expected subject to include job/build/status context, got %q", message.Subject)
	}
	for _, want := range []string{
		"A Coyote CI build failed.",
		"Build ID: build-1",
		"Status: failed",
		"Project: Payments API (project-1)",
		"Build number: 42",
		"Job: backend-ci (job-1)",
	} {
		if !strings.Contains(message.Body, want) {
			t.Fatalf("expected body to contain %q, got %q", want, message.Body)
		}
	}

	delivery := mustGetNotificationDelivery(t, deliveryRepo, "build-1", domain.NotificationEventTypeBuildFailed, "<dev@example.com>")
	if delivery.Status != domain.NotificationDeliveryStatusSent {
		t.Fatalf("expected sent delivery status, got %q", delivery.Status)
	}
	if delivery.SentAt == nil || delivery.SentAt.IsZero() {
		t.Fatal("expected sent_at to be recorded")
	}
}

func TestBuildService_CompleteBuild_SendsNotificationWhenConfigured(t *testing.T) {
	now := time.Now().UTC()
	jobID := "job-1"
	deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
	buildRepo := &fakeBuildRepository{
		build: domain.Build{
			ID:          "build-1",
			ProjectID:   "project-1",
			JobID:       &jobID,
			BuildNumber: 42,
			Status:      domain.BuildStatusRunning,
			CreatedAt:   now,
		},
	}
	jobRepo := memoryrepo.NewJobRepository()
	projectRepo := memoryrepo.NewProjectRepository(jobRepo)
	if _, err := projectRepo.Create(context.Background(), domain.Project{ID: "project-1", Name: "Payments API", Slug: "payments-api", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create project failed: %v", err)
	}
	if _, err := jobRepo.Create(context.Background(), domain.Job{ID: jobID, ProjectID: "project-1", Name: "backend-ci", RepositoryURL: "https://github.com/example/payments.git", DefaultRef: "main", PipelineYAML: "version: 1", Enabled: true, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create job failed: %v", err)
	}
	sender := &recordingEmailSender{}
	subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{
		Enabled:          true,
		Recipients:       "dev@example.com",
		Sender:           sender,
		JobRepo:          jobRepo,
		ProjectRepo:      projectRepo,
		DeliveryRepo:     deliveryRepo,
		SubscriptionRepo: subscriptionRepo,
	})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	svc := NewBuildServiceFromConfig(buildRepo, nil, nil, BuildServiceConfig{BuildNotifier: notifier})
	build, err := svc.CompleteBuild(context.Background(), "build-1")
	if err != nil {
		t.Fatalf("complete build returned error: %v", err)
	}
	if build.Status != domain.BuildStatusSuccess {
		t.Fatalf("expected successful build, got %q", build.Status)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("expected one notification email, got %d", len(sender.messages))
	}
	message := sender.messages[0]
	if message.To != "<dev@example.com>" {
		t.Fatalf("expected normalized recipient <dev@example.com>, got %q", message.To)
	}
	if !strings.Contains(message.Subject, "backend-ci (job-1)") || !strings.Contains(message.Subject, "build-1") || !strings.Contains(message.Subject, "succeeded") {
		t.Fatalf("expected subject to include job/build/success context, got %q", message.Subject)
	}
	for _, want := range []string{
		"A Coyote CI build succeeded.",
		"Build ID: build-1",
		"Status: success",
		"Project: Payments API (project-1)",
		"Build number: 42",
		"Job: backend-ci (job-1)",
	} {
		if !strings.Contains(message.Body, want) {
			t.Fatalf("expected body to contain %q, got %q", want, message.Body)
		}
	}

	delivery := mustGetNotificationDelivery(t, deliveryRepo, "build-1", domain.NotificationEventTypeBuildSucceeded, "<dev@example.com>")
	if delivery.Status != domain.NotificationDeliveryStatusSent {
		t.Fatalf("expected sent delivery status, got %q", delivery.Status)
	}
	if delivery.SentAt == nil || delivery.SentAt.IsZero() {
		t.Fatal("expected sent_at to be recorded")
	}
}

func TestBuildService_FailBuild_EmailIncludesCoyoteEntityLinksWhenPublicURLSet(t *testing.T) {
	now := time.Now().UTC()
	jobID := "job-1"
	deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
	buildRepo := &fakeBuildRepository{
		build: domain.Build{
			ID:          "build-1",
			ProjectID:   "project-1",
			JobID:       &jobID,
			BuildNumber: 42,
			Status:      domain.BuildStatusRunning,
			CreatedAt:   now,
		},
	}
	jobRepo := memoryrepo.NewJobRepository()
	projectRepo := memoryrepo.NewProjectRepository(jobRepo)
	if _, err := projectRepo.Create(context.Background(), domain.Project{ID: "project-1", Name: "Payments API", Slug: "payments-api", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create project failed: %v", err)
	}
	if _, err := jobRepo.Create(context.Background(), domain.Job{ID: jobID, ProjectID: "project-1", Name: "backend-ci", RepositoryURL: "https://github.com/example/payments.git", DefaultRef: "main", PipelineYAML: "version: 1", Enabled: true, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create job failed: %v", err)
	}
	sender := &recordingEmailSender{}
	subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{
		Enabled:          true,
		Recipients:       "dev@example.com",
		Sender:           sender,
		JobRepo:          jobRepo,
		ProjectRepo:      projectRepo,
		DeliveryRepo:     deliveryRepo,
		SubscriptionRepo: subscriptionRepo,
		PublicBaseURL:    "https://ci.example.com/",
	})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	svc := NewBuildServiceFromConfig(buildRepo, nil, nil, BuildServiceConfig{BuildNotifier: notifier})
	if _, err := svc.FailBuild(context.Background(), "build-1"); err != nil {
		t.Fatalf("fail build returned error: %v", err)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("expected one notification email, got %d", len(sender.messages))
	}
	body := sender.messages[0].Body
	for _, want := range []string{
		"Project: Payments API (project-1)",
		"Project detail: https://ci.example.com/projects/project-1",
		"Job: backend-ci (job-1)",
		"Job detail: https://ci.example.com/jobs/job-1",
		"Build detail: https://ci.example.com/builds/build-1",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected body to contain %q, got %q", want, body)
		}
	}
}

func TestBuildService_FailBuild_DoesNotSendNotificationWhenDisabled(t *testing.T) {
	buildRepo := &fakeBuildRepository{build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: time.Now().UTC()}}
	sender := &recordingEmailSender{}
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{
		Enabled:    false,
		Recipients: "dev@example.com",
		Sender:     sender,
	})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	svc := NewBuildServiceFromConfig(buildRepo, nil, nil, BuildServiceConfig{BuildNotifier: notifier})
	if _, err := svc.FailBuild(context.Background(), "build-1"); err != nil {
		t.Fatalf("fail build returned error: %v", err)
	}
	if len(sender.messages) != 0 {
		t.Fatalf("expected no notification emails when disabled, got %d", len(sender.messages))
	}
}

func TestBuildService_NotificationsDisabled_DoesNotSendForSuccessOrFailure(t *testing.T) {
	tests := []struct {
		name   string
		action func(*BuildService, context.Context, string) (domain.Build, error)
		want   domain.BuildStatus
	}{
		{name: "success", action: (*BuildService).CompleteBuild, want: domain.BuildStatusSuccess},
		{name: "failure", action: (*BuildService).FailBuild, want: domain.BuildStatusFailed},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			buildRepo := &fakeBuildRepository{build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: time.Now().UTC()}}
			sender := &recordingEmailSender{}
			notifier, err := NewBuildNotificationService(BuildNotificationConfig{
				Enabled:    false,
				Recipients: "dev@example.com",
				Sender:     sender,
			})
			if err != nil {
				t.Fatalf("create notifier failed: %v", err)
			}

			svc := NewBuildServiceFromConfig(buildRepo, nil, nil, BuildServiceConfig{BuildNotifier: notifier})
			build, err := tc.action(svc, context.Background(), "build-1")
			if err != nil {
				t.Fatalf("terminal transition returned error: %v", err)
			}
			if build.Status != tc.want {
				t.Fatalf("expected build status %q, got %q", tc.want, build.Status)
			}
			if len(sender.messages) != 0 {
				t.Fatalf("expected no notification emails when disabled, got %d", len(sender.messages))
			}
		})
	}
}

func TestBuildService_FailBuild_DoesNotSendNotificationWithoutRecipients(t *testing.T) {
	buildRepo := &fakeBuildRepository{build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: time.Now().UTC()}}
	sender := &recordingEmailSender{}
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{
		Enabled: true,
		Sender:  sender,
	})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	svc := NewBuildServiceFromConfig(buildRepo, nil, nil, BuildServiceConfig{BuildNotifier: notifier})
	build, err := svc.FailBuild(context.Background(), "build-1")
	if err != nil {
		t.Fatalf("fail build returned error: %v", err)
	}
	if build.Status != domain.BuildStatusFailed {
		t.Fatalf("expected failed build, got %q", build.Status)
	}
	if len(sender.messages) != 0 {
		t.Fatalf("expected no notification emails without recipients, got %d", len(sender.messages))
	}
}

func TestBuildService_FailBuild_SenderFailureDoesNotBreakPersistence(t *testing.T) {
	buildRepo := &fakeBuildRepository{build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: time.Now().UTC()}}
	deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
	subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
	sender := &recordingEmailSender{err: errors.New("smtp unavailable")}
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{
		Enabled:          true,
		Recipients:       "dev@example.com",
		Sender:           sender,
		DeliveryRepo:     deliveryRepo,
		SubscriptionRepo: subscriptionRepo,
	})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	svc := NewBuildServiceFromConfig(buildRepo, nil, nil, BuildServiceConfig{BuildNotifier: notifier})
	build, err := svc.FailBuild(context.Background(), "build-1")
	if err != nil {
		t.Fatalf("fail build returned error: %v", err)
	}
	if build.Status != domain.BuildStatusFailed {
		t.Fatalf("expected failed build to remain persisted, got %q", build.Status)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("expected attempted notification email, got %d", len(sender.messages))
	}
	delivery := mustGetNotificationDelivery(t, deliveryRepo, "build-1", domain.NotificationEventTypeBuildFailed, "<dev@example.com>")
	if delivery.Status != domain.NotificationDeliveryStatusRetryWaiting {
		t.Fatalf("expected retry-waiting delivery status, got %q", delivery.Status)
	}
	if delivery.LastError == nil || !strings.Contains(*delivery.LastError, "smtp unavailable") {
		t.Fatalf("expected last_error to contain smtp failure, got %v", delivery.LastError)
	}
}

func TestBuildService_CompleteBuild_SenderFailureDoesNotBreakPersistence(t *testing.T) {
	buildRepo := &fakeBuildRepository{build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: time.Now().UTC()}}
	sender := &recordingEmailSender{err: errors.New("smtp unavailable")}
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{
		Enabled:          true,
		Recipients:       "dev@example.com",
		Sender:           sender,
		SubscriptionRepo: memoryrepo.NewNotificationSubscriptionRepository(),
	})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	svc := NewBuildServiceFromConfig(buildRepo, nil, nil, BuildServiceConfig{BuildNotifier: notifier})
	build, err := svc.CompleteBuild(context.Background(), "build-1")
	if err != nil {
		t.Fatalf("complete build returned error: %v", err)
	}
	if build.Status != domain.BuildStatusSuccess {
		t.Fatalf("expected successful build to remain persisted, got %q", build.Status)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("expected attempted notification email, got %d", len(sender.messages))
	}
}

func TestBuildService_CancelBuild_DoesNotSendNotificationWhenConfigured(t *testing.T) {
	buildRepo := &fakeBuildRepository{build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: time.Now().UTC()}}
	sender := &recordingEmailSender{}
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{
		Enabled:          true,
		Recipients:       "dev@example.com",
		Sender:           sender,
		SubscriptionRepo: memoryrepo.NewNotificationSubscriptionRepository(),
	})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	svc := NewBuildServiceFromConfig(buildRepo, nil, nil, BuildServiceConfig{BuildNotifier: notifier})
	build, err := svc.CancelBuild(context.Background(), "build-1")
	if err != nil {
		t.Fatalf("cancel build returned error: %v", err)
	}
	if build.Status != domain.BuildStatusCanceled {
		t.Fatalf("expected canceled build, got %q", build.Status)
	}
	if len(sender.messages) != 0 {
		t.Fatalf("expected no notification emails for canceled builds, got %d", len(sender.messages))
	}
}

func TestBuildService_FailBuild_UsesPersistedProvenanceForCommitAuthorNotifications(t *testing.T) {
	buildRepo := memoryrepo.NewBuildRepository()
	deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
	sender := &recordingEmailSender{}
	userRepo := memoryrepo.NewUserRepository()
	preferenceRepo := memoryrepo.NewUserNotificationPreferenceRepository()
	subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
	user := mustCreateNotificationUser(t, userRepo, "author@example.com")
	mustEnsureOwnedNotificationTarget(t, subscriptionRepo, user.ID, "author@example.com", true)
	mustUpsertNotificationPreference(t, preferenceRepo, user.ID, true)
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{
		Enabled:          true,
		Recipients:       "dev@example.com",
		Sender:           sender,
		DeliveryRepo:     deliveryRepo,
		SubscriptionRepo: subscriptionRepo,
		UserRepo:         userRepo,
		PreferenceRepo:   preferenceRepo,
	})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	queuedBuild, err := buildRepo.CreateQueuedBuild(context.Background(), domain.Build{
		ID:        "build-1",
		ProjectID: "project-1",
		Status:    domain.BuildStatusQueued,
		RepoURL:   readOptionalStringPtrForTest("https://github.com/example/repo.git"),
		Ref:       readOptionalStringPtrForTest("main"),
	}, nil)
	if err != nil {
		t.Fatalf("create queued build failed: %v", err)
	}
	if queuedBuild.SourceAuthorEmail != nil {
		t.Fatalf("expected original build value to lack author email, got %v", queuedBuild.SourceAuthorEmail)
	}

	originalReadMetadata := readWorkspaceCommitMetadata
	t.Cleanup(func() {
		readWorkspaceCommitMetadata = originalReadMetadata
	})
	readWorkspaceCommitMetadata = func(context.Context, string) (source.CommitMetadata, error) {
		return source.CommitMetadata{
			AuthorName:  "Author Example",
			AuthorEmail: "author@example.com",
		}, nil
	}

	resolver := &fakeWorkspaceSourceResolver{resolvedCommit: "deadbeef"}
	svc := NewBuildServiceFromConfig(buildRepo, nil, nil, BuildServiceConfig{BuildNotifier: notifier})
	svc.SetSourceResolver(resolver)
	svc.SetExecutionWorkspaceRoot(t.TempDir())

	preparedBuild, err := svc.PrepareBuildExecution(context.Background(), queuedBuild.ID)
	if err != nil {
		t.Fatalf("prepare build execution returned error: %v", err)
	}
	if preparedBuild.Status != domain.BuildStatusRunning {
		t.Fatalf("expected prepared build to be running, got %q", preparedBuild.Status)
	}
	if preparedBuild.SourceAuthorEmail == nil || *preparedBuild.SourceAuthorEmail != "author@example.com" {
		t.Fatalf("expected prepare build execution to persist author email, got %v", preparedBuild.SourceAuthorEmail)
	}

	failedBuild, err := svc.FailBuild(context.Background(), queuedBuild.ID)
	if err != nil {
		t.Fatalf("fail build returned error: %v", err)
	}
	if failedBuild.Status != domain.BuildStatusFailed {
		t.Fatalf("expected failed build, got %q", failedBuild.Status)
	}
	if failedBuild.SourceAuthorEmail == nil || *failedBuild.SourceAuthorEmail != "author@example.com" {
		t.Fatalf("expected failed build returned from repo to include author email, got %v", failedBuild.SourceAuthorEmail)
	}
	if len(sender.messages) != 2 {
		t.Fatalf("expected default plus author notification, got %d", len(sender.messages))
	}

	seen := map[string]bool{}
	for _, message := range sender.messages {
		seen[message.To] = true
	}
	for _, recipient := range []string{"<dev@example.com>", "<author@example.com>"} {
		if !seen[recipient] {
			t.Fatalf("expected notification for %s, got %v", recipient, seen)
		}
		if delivery := mustGetNotificationDelivery(t, deliveryRepo, queuedBuild.ID, domain.NotificationEventTypeBuildFailed, recipient); delivery.Status != domain.NotificationDeliveryStatusSent {
			t.Fatalf("expected sent delivery for %s, got %q", recipient, delivery.Status)
		}
	}
	if queuedBuild.SourceAuthorEmail != nil {
		t.Fatalf("expected stale original build value to remain unchanged, got %v", queuedBuild.SourceAuthorEmail)
	}
	storedBuild, err := buildRepo.GetByID(context.Background(), queuedBuild.ID)
	if err != nil {
		t.Fatalf("reload persisted build failed: %v", err)
	}
	if storedBuild.SourceAuthorEmail == nil || *storedBuild.SourceAuthorEmail != "author@example.com" {
		t.Fatalf("expected persisted build author email, got %v", storedBuild.SourceAuthorEmail)
	}
	if queuedBuild.SourceAuthorEmail != nil {
		t.Fatalf("expected stale original build value to remain unchanged, got %v", queuedBuild.SourceAuthorEmail)
	}
	if failedBuild.SourceAuthorEmail == nil || *failedBuild.SourceAuthorEmail != "author@example.com" {
		t.Fatalf("expected terminal build used for notification to include persisted author email, got %v", failedBuild.SourceAuthorEmail)
	}
}

func TestBuildService_FailBuild_SendsCommitAuthorNotificationWhenUserOptedIn(t *testing.T) {
	buildRepo := memoryrepo.NewBuildRepository()
	deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
	userRepo := memoryrepo.NewUserRepository()
	preferenceRepo := memoryrepo.NewUserNotificationPreferenceRepository()
	subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
	sender := &recordingEmailSender{}
	authorEmail := "author@example.com"
	user := mustCreateNotificationUser(t, userRepo, authorEmail)
	mustEnsureOwnedNotificationTarget(t, subscriptionRepo, user.ID, authorEmail, true)
	mustUpsertNotificationPreference(t, preferenceRepo, user.ID, true)
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{
		Enabled:          true,
		Recipients:       "dev@example.com",
		Sender:           sender,
		DeliveryRepo:     deliveryRepo,
		SubscriptionRepo: subscriptionRepo,
		UserRepo:         userRepo,
		PreferenceRepo:   preferenceRepo,
	})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	queuedBuild, err := buildRepo.CreateQueuedBuild(context.Background(), domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusQueued}, nil)
	if err != nil {
		t.Fatalf("create queued build failed: %v", err)
	}
	if _, err := buildRepo.UpdateSourceProvenance(context.Background(), queuedBuild.ID, repository.SourceProvenanceUpdate{CommitSHA: "deadbeef", AuthorEmail: "author@example.com"}); err != nil {
		t.Fatalf("persist source provenance failed: %v", err)
	}
	if _, err := buildRepo.UpdateStatus(context.Background(), queuedBuild.ID, domain.BuildStatusRunning, nil); err != nil {
		t.Fatalf("transition to running failed: %v", err)
	}

	svc := NewBuildServiceFromConfig(buildRepo, nil, nil, BuildServiceConfig{BuildNotifier: notifier})
	if _, err := svc.FailBuild(context.Background(), queuedBuild.ID); err != nil {
		t.Fatalf("fail build returned error: %v", err)
	}
	if len(sender.messages) != 2 {
		t.Fatalf("expected configured recipient plus opted-in author recipient, got %d", len(sender.messages))
	}
	seen := map[string]bool{}
	for _, message := range sender.messages {
		seen[message.To] = true
	}
	for _, recipient := range []string{"<dev@example.com>", "<author@example.com>"} {
		if !seen[recipient] {
			t.Fatalf("expected recipient %s, got %+v", recipient, sender.messages)
		}
	}
	if delivery := mustGetNotificationDelivery(t, deliveryRepo, queuedBuild.ID, domain.NotificationEventTypeBuildFailed, "<author@example.com>"); delivery.Status != domain.NotificationDeliveryStatusSent {
		t.Fatalf("expected author delivery, got %q", delivery.Status)
	}
	if delivery := mustGetNotificationDelivery(t, deliveryRepo, queuedBuild.ID, domain.NotificationEventTypeBuildFailed, "<dev@example.com>"); delivery.Status != domain.NotificationDeliveryStatusSent {
		t.Fatalf("expected configured recipient delivery, got %q", delivery.Status)
	}
}

func TestBuildService_CompleteBuild_DoesNotIncludeCommitAuthorOnSuccess(t *testing.T) {
	buildRepo := memoryrepo.NewBuildRepository()
	deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
	subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
	sender := &recordingEmailSender{}
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{
		Enabled:          true,
		Recipients:       "dev@example.com",
		Sender:           sender,
		DeliveryRepo:     deliveryRepo,
		SubscriptionRepo: subscriptionRepo,
	})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	queuedBuild, err := buildRepo.CreateQueuedBuild(context.Background(), domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusQueued}, nil)
	if err != nil {
		t.Fatalf("create queued build failed: %v", err)
	}
	if _, err := buildRepo.UpdateSourceProvenance(context.Background(), queuedBuild.ID, repository.SourceProvenanceUpdate{CommitSHA: "deadbeef", AuthorEmail: "author@example.com"}); err != nil {
		t.Fatalf("persist source provenance failed: %v", err)
	}
	if _, err := buildRepo.UpdateStatus(context.Background(), queuedBuild.ID, domain.BuildStatusRunning, nil); err != nil {
		t.Fatalf("transition to running failed: %v", err)
	}

	svc := NewBuildServiceFromConfig(buildRepo, nil, nil, BuildServiceConfig{BuildNotifier: notifier})
	if _, err := svc.CompleteBuild(context.Background(), queuedBuild.ID); err != nil {
		t.Fatalf("complete build returned error: %v", err)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("expected only configured success recipient, got %d", len(sender.messages))
	}
	if sender.messages[0].To != "<dev@example.com>" {
		t.Fatalf("expected configured success recipient, got %q", sender.messages[0].To)
	}
	if _, err := deliveryRepo.GetByBuildEventRecipient(context.Background(), queuedBuild.ID, domain.NotificationEventTypeBuildSucceeded, "<author@example.com>"); !errors.Is(err, repository.ErrNotificationDeliveryNotFound) {
		t.Fatalf("expected no author success delivery record, got %v", err)
	}
}

func TestBuildService_FailBuild_DedupesCommitAuthorAgainstConfiguredRecipients(t *testing.T) {
	buildRepo := memoryrepo.NewBuildRepository()
	deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
	subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
	sender := &recordingEmailSender{}
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{
		Enabled:          true,
		Recipients:       "dev@example.com",
		Sender:           sender,
		DeliveryRepo:     deliveryRepo,
		SubscriptionRepo: subscriptionRepo,
	})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	queuedBuild, err := buildRepo.CreateQueuedBuild(context.Background(), domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusQueued}, nil)
	if err != nil {
		t.Fatalf("create queued build failed: %v", err)
	}
	if _, err := buildRepo.UpdateSourceProvenance(context.Background(), queuedBuild.ID, repository.SourceProvenanceUpdate{CommitSHA: "deadbeef", AuthorEmail: "dev@example.com"}); err != nil {
		t.Fatalf("persist source provenance failed: %v", err)
	}
	if _, err := buildRepo.UpdateStatus(context.Background(), queuedBuild.ID, domain.BuildStatusRunning, nil); err != nil {
		t.Fatalf("transition to running failed: %v", err)
	}

	svc := NewBuildServiceFromConfig(buildRepo, nil, nil, BuildServiceConfig{BuildNotifier: notifier})
	if _, err := svc.FailBuild(context.Background(), queuedBuild.ID); err != nil {
		t.Fatalf("fail build returned error: %v", err)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("expected deduped configured/author recipient, got %d", len(sender.messages))
	}
	if sender.messages[0].To != "<dev@example.com>" {
		t.Fatalf("expected deduped recipient, got %q", sender.messages[0].To)
	}
	if delivery := mustGetNotificationDelivery(t, deliveryRepo, queuedBuild.ID, domain.NotificationEventTypeBuildFailed, "<dev@example.com>"); delivery.Attempts != 1 {
		t.Fatalf("expected one deduped delivery attempt, got %d", delivery.Attempts)
	}
	if _, err := deliveryRepo.GetByBuildEventRecipient(context.Background(), queuedBuild.ID, domain.NotificationEventTypeBuildFailed, "<author@example.com>"); !errors.Is(err, repository.ErrNotificationDeliveryNotFound) {
		t.Fatalf("expected no second author delivery after dedupe, got %v", err)
	}
}

func TestBuildService_HandleStepResult_FailedStepThenFailBuild_DoesNotDoubleSendEmail(t *testing.T) {
	claimToken := "claim-active"
	buildRepo := &fakeBuildRepository{
		build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusRunning, CurrentStepIndex: 0, CreatedAt: time.Now().UTC()},
		steps: []domain.BuildStep{
			{StepIndex: 0, Name: "step-1", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken},
			{StepIndex: 1, Name: "step-2", Status: domain.BuildStepStatusPending},
		},
	}
	sender := &recordingEmailSender{}
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{
		Enabled:          true,
		Recipients:       "dev@example.com",
		Sender:           sender,
		SubscriptionRepo: memoryrepo.NewNotificationSubscriptionRepository(),
	})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	svc := NewBuildServiceFromConfig(buildRepo, nil, nil, BuildServiceConfig{BuildNotifier: notifier})
	report, err := svc.HandleStepResult(context.Background(), steprunner.RunStepRequest{BuildID: "build-1", StepIndex: 0, StepName: "step-1", ClaimToken: claimToken}, steprunner.RunStepResult{Status: steprunner.RunStepStatusFailed, ExitCode: 7, Stderr: "boom", StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC()})
	if err != nil {
		t.Fatalf("handle step result returned error: %v", err)
	}
	if report.CompletionOutcome != repository.StepCompletionCompleted {
		t.Fatalf("expected completion to persist, got %q", report.CompletionOutcome)
	}
	if buildRepo.build.Status != domain.BuildStatusFailed {
		t.Fatalf("expected build failed after step completion, got %q", buildRepo.build.Status)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("expected one notification email after failed step completion, got %d", len(sender.messages))
	}

	if _, err := svc.FailBuild(context.Background(), "build-1"); !errors.Is(err, ErrInvalidBuildStatusTransition) {
		t.Fatalf("expected invalid transition when failing an already failed build, got %v", err)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("expected no second notification email after invalid FailBuild, got %d", len(sender.messages))
	}
}

func TestBuildNotificationService_SendSampleBuildFailure(t *testing.T) {
	sender := &recordingEmailSender{}
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{
		Enabled:    true,
		Recipients: "dev@example.com, qa@example.com",
		Sender:     sender,
	})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	recipients, err := notifier.SendSampleBuildFailure(context.Background())
	if err != nil {
		t.Fatalf("sample email returned error: %v", err)
	}
	if len(recipients) != 2 || recipients[0] != "<dev@example.com>" || recipients[1] != "<qa@example.com>" {
		t.Fatalf("unexpected recipients: %v", recipients)
	}
	if len(sender.messages) != 2 {
		t.Fatalf("expected one message per recipient, got %d", len(sender.messages))
	}
	if !strings.Contains(sender.messages[0].Subject, "sample build failure") {
		t.Fatalf("expected sample subject, got %q", sender.messages[0].Subject)
	}
	if !strings.Contains(sender.messages[0].Body, "POST /api/dev/notifications/sample-build") {
		t.Fatalf("expected sample endpoint hint in body, got %q", sender.messages[0].Body)
	}
}

func TestBuildNotificationService_SendSampleBuildFailureRequiresEnabledConfig(t *testing.T) {
	disabledNotifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: false, Recipients: "dev@example.com", Sender: &recordingEmailSender{}})
	if err != nil {
		t.Fatalf("create disabled notifier failed: %v", err)
	}
	if _, sendErr := disabledNotifier.SendSampleBuildFailure(context.Background()); !errors.Is(sendErr, ErrEmailNotificationsDisabled) {
		t.Fatalf("expected disabled error, got %v", sendErr)
	}

	noRecipientsNotifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Sender: &recordingEmailSender{}})
	if err != nil {
		t.Fatalf("create no-recipient notifier failed: %v", err)
	}
	if _, sendErr := noRecipientsNotifier.SendSampleBuildFailure(context.Background()); !errors.Is(sendErr, ErrEmailNotificationRecipientsNotConfigured) {
		t.Fatalf("expected no recipient error, got %v", sendErr)
	}
}

func TestNewBuildNotificationService_DisabledIgnoresInvalidRecipients(t *testing.T) {
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{
		Enabled:    false,
		Recipients: "not-an-email",
		Sender:     &recordingEmailSender{},
	})
	if err != nil {
		t.Fatalf("expected disabled notifier to ignore invalid recipients, got %v", err)
	}
	if notifier == nil {
		t.Fatal("expected notifier")
	}
	if len(notifier.defaultRecipients) != 0 {
		t.Fatalf("expected disabled notifier to keep no parsed recipients, got %v", notifier.defaultRecipients)
	}
}

func TestNewBuildNotificationService_EnabledRejectsInvalidRecipients(t *testing.T) {
	_, err := NewBuildNotificationService(BuildNotificationConfig{
		Enabled:    true,
		Recipients: "not-an-email",
		Sender:     &recordingEmailSender{},
	})
	if err == nil {
		t.Fatal("expected invalid recipient error")
	}
}

func TestBuildNotificationService_NotifyTerminalBuild(t *testing.T) {
	t.Run("nil service is noop", func(t *testing.T) {
		var notifier *BuildNotificationService
		if err := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", Status: domain.BuildStatusFailed}); err != nil {
			t.Fatalf("expected nil service to noop, got %v", err)
		}
	})

	t.Run("no sender returns error", func(t *testing.T) {
		notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com", SubscriptionRepo: memoryrepo.NewNotificationSubscriptionRepository()})
		if err != nil {
			t.Fatalf("create notifier failed: %v", err)
		}
		if err := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", Status: domain.BuildStatusFailed}); err == nil {
			t.Fatal("expected missing sender error")
		}
	})

	t.Run("non failed terminal status is ignored", func(t *testing.T) {
		sender := &recordingEmailSender{}
		notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com", Sender: sender, SubscriptionRepo: memoryrepo.NewNotificationSubscriptionRepository()})
		if err != nil {
			t.Fatalf("create notifier failed: %v", err)
		}
		if err := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", Status: domain.BuildStatusCanceled}); err != nil {
			t.Fatalf("expected canceled status to be ignored, got %v", err)
		}
		if len(sender.messages) != 0 {
			t.Fatalf("expected no email for canceled status, got %d", len(sender.messages))
		}
	})

	t.Run("successful terminal status sends email", func(t *testing.T) {
		sender := &recordingEmailSender{}
		notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com", Sender: sender, SubscriptionRepo: memoryrepo.NewNotificationSubscriptionRepository()})
		if err != nil {
			t.Fatalf("create notifier failed: %v", err)
		}
		if err := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusSuccess}); err != nil {
			t.Fatalf("expected success status to send, got %v", err)
		}
		if len(sender.messages) != 1 {
			t.Fatalf("expected one email for success status, got %d", len(sender.messages))
		}
		if !strings.Contains(sender.messages[0].Subject, "succeeded") {
			t.Fatalf("expected success subject, got %q", sender.messages[0].Subject)
		}
	})

	t.Run("configured recipients send without subscription repo", func(t *testing.T) {
		deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
		sender := &recordingEmailSender{}
		notifier, err := NewBuildNotificationService(BuildNotificationConfig{
			Enabled:      true,
			Recipients:   "Dev <dev@example.com>",
			Sender:       sender,
			DeliveryRepo: deliveryRepo,
		})
		if err != nil {
			t.Fatalf("create notifier failed: %v", err)
		}

		build := domain.Build{ID: "build-no-subscriptions", Status: domain.BuildStatusSuccess}
		if err := notifier.NotifyTerminalBuild(context.Background(), build); err != nil {
			t.Fatalf("expected configured-recipient notifier without subscriptions to send, got %v", err)
		}
		if err := notifier.NotifyTerminalBuild(context.Background(), build); err != nil {
			t.Fatalf("expected duplicate configured-recipient notify to skip cleanly, got %v", err)
		}
		if len(sender.messages) != 1 {
			t.Fatalf("expected one sent email after duplicate notify, got %d", len(sender.messages))
		}
		if sender.messages[0].To != "\"Dev\" <dev@example.com>" {
			t.Fatalf("expected normalized configured recipient, got %q", sender.messages[0].To)
		}

		delivery := mustGetNotificationDelivery(t, deliveryRepo, "build-no-subscriptions", domain.NotificationEventTypeBuildSucceeded, "\"Dev\" <dev@example.com>")
		if delivery.DestinationKey != "email-config:dev@example.com" {
			t.Fatalf("expected stable configured-recipient destination key, got %q", delivery.DestinationKey)
		}
		if delivery.Status != domain.NotificationDeliveryStatusSent {
			t.Fatalf("expected sent delivery status, got %q", delivery.Status)
		}
		if delivery.Attempts != 1 {
			t.Fatalf("expected one delivery attempt, got %d", delivery.Attempts)
		}
	})

	t.Run("records sent delivery state", func(t *testing.T) {
		deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
		sender := &recordingEmailSender{}
		notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com", Sender: sender, DeliveryRepo: deliveryRepo, SubscriptionRepo: memoryrepo.NewNotificationSubscriptionRepository()})
		if err != nil {
			t.Fatalf("create notifier failed: %v", err)
		}
		if err := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", Status: domain.BuildStatusSuccess}); err != nil {
			t.Fatalf("notify terminal build failed: %v", err)
		}

		delivery := mustGetNotificationDelivery(t, deliveryRepo, "build-1", domain.NotificationEventTypeBuildSucceeded, "<dev@example.com>")
		if delivery.Status != domain.NotificationDeliveryStatusSent {
			t.Fatalf("expected sent delivery status, got %q", delivery.Status)
		}
		if delivery.Attempts != 1 {
			t.Fatalf("expected one attempt, got %d", delivery.Attempts)
		}
		if delivery.SentAt == nil || delivery.SentAt.IsZero() {
			t.Fatal("expected sent_at to be set")
		}
	})

	t.Run("records failed delivery state", func(t *testing.T) {
		deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
		sender := &recordingEmailSender{err: errors.New("smtp unavailable")}
		notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com", Sender: sender, DeliveryRepo: deliveryRepo, SubscriptionRepo: memoryrepo.NewNotificationSubscriptionRepository()})
		if err != nil {
			t.Fatalf("create notifier failed: %v", err)
		}
		if err := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", Status: domain.BuildStatusFailed}); err == nil {
			t.Fatal("expected sender failure")
		}

		delivery := mustGetNotificationDelivery(t, deliveryRepo, "build-1", domain.NotificationEventTypeBuildFailed, "<dev@example.com>")
		if delivery.Status != domain.NotificationDeliveryStatusRetryWaiting {
			t.Fatalf("expected retry_waiting delivery status, got %q", delivery.Status)
		}
		if delivery.Attempts != 1 {
			t.Fatalf("expected one attempt, got %d", delivery.Attempts)
		}
		if delivery.LastError == nil || !strings.Contains(*delivery.LastError, "smtp unavailable") {
			t.Fatalf("expected last_error to be recorded, got %v", delivery.LastError)
		}
		if delivery.SentAt != nil {
			t.Fatalf("expected no sent_at for failed delivery, got %v", delivery.SentAt)
		}
	})

	t.Run("duplicate terminal hook does not send twice", func(t *testing.T) {
		deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
		sender := &recordingEmailSender{}
		notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com", Sender: sender, DeliveryRepo: deliveryRepo, SubscriptionRepo: memoryrepo.NewNotificationSubscriptionRepository()})
		if err != nil {
			t.Fatalf("create notifier failed: %v", err)
		}

		build := domain.Build{ID: "build-1", Status: domain.BuildStatusFailed}
		if err := notifier.NotifyTerminalBuild(context.Background(), build); err != nil {
			t.Fatalf("first notify failed: %v", err)
		}
		if err := notifier.NotifyTerminalBuild(context.Background(), build); err != nil {
			t.Fatalf("duplicate notify should skip cleanly, got %v", err)
		}
		if len(sender.messages) != 1 {
			t.Fatalf("expected one sent email after duplicate hook, got %d", len(sender.messages))
		}

		delivery := mustGetNotificationDelivery(t, deliveryRepo, "build-1", domain.NotificationEventTypeBuildFailed, "<dev@example.com>")
		if delivery.Attempts != 1 {
			t.Fatalf("expected attempts to remain 1 after duplicate hook, got %d", delivery.Attempts)
		}
	})

	t.Run("existing failed delivery record is skipped without retry", func(t *testing.T) {
		deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
		subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
		sharedTarget, err := subscriptionRepo.EnsureConfigEmailTarget(context.Background(), repository.EnsureConfigNotificationEmailTargetInput{
			Name:      "dev@example.com",
			Recipient: "dev@example.com",
		})
		if err != nil {
			t.Fatalf("ensure shared target failed: %v", err)
		}
		message := "smtp unavailable"
		if _, createErr := deliveryRepo.Create(context.Background(), domain.NotificationDelivery{
			BuildID:              "build-1",
			EventType:            domain.NotificationEventTypeBuildFailed,
			Transport:            domain.NotificationTransportEmail,
			DestinationKind:      domain.NotificationDestinationKindSharedTarget,
			DestinationKey:       "email-target:" + sharedTarget.ID,
			NotificationTargetID: &sharedTarget.ID,
			Recipient:            "<dev@example.com>",
			Status:               domain.NotificationDeliveryStatusFailedPermanent,
			Attempts:             1,
			MaxAttempts:          3,
			FailureCategory:      failureCategoryPtr(domain.NotificationDeliveryFailureCategoryPermanent),
			LastError:            &message,
		}); createErr != nil {
			t.Fatalf("seed failed delivery record failed: %v", createErr)
		}

		sender := &recordingEmailSender{}
		notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com", Sender: sender, DeliveryRepo: deliveryRepo, SubscriptionRepo: subscriptionRepo})
		if err != nil {
			t.Fatalf("create notifier failed: %v", err)
		}
		if err := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", Status: domain.BuildStatusFailed}); err != nil {
			t.Fatalf("notify with existing failed record should skip cleanly, got %v", err)
		}
		if len(sender.messages) != 0 {
			t.Fatalf("expected no resend when failed delivery record already exists, got %d", len(sender.messages))
		}

		delivery := mustGetNotificationDelivery(t, deliveryRepo, "build-1", domain.NotificationEventTypeBuildFailed, "<dev@example.com>")
		if delivery.Status != domain.NotificationDeliveryStatusFailedPermanent {
			t.Fatalf("expected failed delivery status to remain unchanged, got %q", delivery.Status)
		}
		if delivery.Attempts != 1 {
			t.Fatalf("expected attempts to remain 1 for skipped failed record, got %d", delivery.Attempts)
		}
	})

	t.Run("delivery create error is returned", func(t *testing.T) {
		repo := &scriptedNotificationDeliveryRepo{
			createFunc: func(context.Context, domain.NotificationDelivery) (domain.NotificationDelivery, error) {
				return domain.NotificationDelivery{}, errors.New("create failed")
			},
		}
		notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com", Sender: &recordingEmailSender{}, DeliveryRepo: repo, SubscriptionRepo: memoryrepo.NewNotificationSubscriptionRepository()})
		if err != nil {
			t.Fatalf("create notifier failed: %v", err)
		}
		if err := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", Status: domain.BuildStatusFailed}); err == nil || err.Error() != "create failed" {
			t.Fatalf("expected create failure, got %v", err)
		}
	})

	t.Run("duplicate record lookup error is returned", func(t *testing.T) {
		repo := &scriptedNotificationDeliveryRepo{
			createFunc: func(context.Context, domain.NotificationDelivery) (domain.NotificationDelivery, error) {
				return domain.NotificationDelivery{}, repository.ErrNotificationDeliveryDuplicate
			},
			getFunc: func(context.Context, string, domain.NotificationEventType, string) (domain.NotificationDelivery, error) {
				return domain.NotificationDelivery{}, errors.New("lookup failed")
			},
		}
		notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com", Sender: &recordingEmailSender{}, DeliveryRepo: repo, SubscriptionRepo: memoryrepo.NewNotificationSubscriptionRepository()})
		if err != nil {
			t.Fatalf("create notifier failed: %v", err)
		}
		if err := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", Status: domain.BuildStatusFailed}); err == nil || err.Error() != "lookup failed" {
			t.Fatalf("expected lookup failure, got %v", err)
		}
	})

	t.Run("sent state persistence failure marks delivery failed", func(t *testing.T) {
		updateCalls := 0
		repo := &scriptedNotificationDeliveryRepo{
			createFunc: func(_ context.Context, delivery domain.NotificationDelivery) (domain.NotificationDelivery, error) {
				delivery.ID = "delivery-1"
				delivery.CreatedAt = time.Now().UTC()
				return delivery, nil
			},
			updateFunc: func(_ context.Context, delivery domain.NotificationDelivery) (domain.NotificationDelivery, error) {
				updateCalls++
				if updateCalls == 1 {
					if delivery.Status != domain.NotificationDeliveryStatusSent {
						t.Fatalf("expected first update to persist sent status, got %q", delivery.Status)
					}
					return domain.NotificationDelivery{}, errors.New("write sent failed")
				}
				if delivery.Status != domain.NotificationDeliveryStatusRetryWaiting {
					t.Fatalf("expected fallback update to schedule retry, got %q", delivery.Status)
				}
				if delivery.LastError == nil || !strings.Contains(*delivery.LastError, "persist sent delivery state failed") {
					t.Fatalf("expected fallback last error to describe sent-state persistence failure, got %v", delivery.LastError)
				}
				return delivery, nil
			},
		}
		notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com", Sender: &recordingEmailSender{}, DeliveryRepo: repo, SubscriptionRepo: memoryrepo.NewNotificationSubscriptionRepository()})
		if err != nil {
			t.Fatalf("create notifier failed: %v", err)
		}
		err = notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", Status: domain.BuildStatusSuccess})
		if err == nil || !strings.Contains(err.Error(), "persist sent delivery state failed") {
			t.Fatalf("expected sent-state persistence failure, got %v", err)
		}
		if updateCalls != 2 {
			t.Fatalf("expected two update attempts, got %d", updateCalls)
		}
	})

	t.Run("failed fallback persistence returns joined error", func(t *testing.T) {
		repo := &scriptedNotificationDeliveryRepo{
			createFunc: func(_ context.Context, delivery domain.NotificationDelivery) (domain.NotificationDelivery, error) {
				delivery.ID = "delivery-1"
				delivery.CreatedAt = time.Now().UTC()
				return delivery, nil
			},
			updateFunc: func(_ context.Context, delivery domain.NotificationDelivery) (domain.NotificationDelivery, error) {
				if delivery.Status == domain.NotificationDeliveryStatusSent {
					return domain.NotificationDelivery{}, errors.New("write sent failed")
				}
				return domain.NotificationDelivery{}, errors.New("write failed failed")
			},
		}
		notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com", Sender: &recordingEmailSender{}, DeliveryRepo: repo, SubscriptionRepo: memoryrepo.NewNotificationSubscriptionRepository()})
		if err != nil {
			t.Fatalf("create notifier failed: %v", err)
		}
		err = notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", Status: domain.BuildStatusSuccess})
		if err == nil {
			t.Fatal("expected joined persistence error")
		}
		message := err.Error()
		if !strings.Contains(message, "persist sent delivery state failed") || !strings.Contains(message, "write failed failed") {
			t.Fatalf("expected joined error message, got %q", message)
		}
	})

	t.Run("multiple recipients create separate delivery records", func(t *testing.T) {
		deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
		sender := &recordingEmailSender{}
		notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com, qa@example.com", Sender: sender, DeliveryRepo: deliveryRepo, SubscriptionRepo: memoryrepo.NewNotificationSubscriptionRepository()})
		if err != nil {
			t.Fatalf("create notifier failed: %v", err)
		}
		if err := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", Status: domain.BuildStatusSuccess}); err != nil {
			t.Fatalf("notify terminal build failed: %v", err)
		}
		if len(sender.messages) != 2 {
			t.Fatalf("expected one message per recipient, got %d", len(sender.messages))
		}

		for _, recipient := range []string{"<dev@example.com>", "<qa@example.com>"} {
			delivery := mustGetNotificationDelivery(t, deliveryRepo, "build-1", domain.NotificationEventTypeBuildSucceeded, recipient)
			if delivery.Status != domain.NotificationDeliveryStatusSent {
				t.Fatalf("expected sent delivery for %s, got %q", recipient, delivery.Status)
			}
		}
	})

	t.Run("failed builds can include commit author when enabled", func(t *testing.T) {
		deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
		sender := &recordingEmailSender{}
		userRepo := memoryrepo.NewUserRepository()
		preferenceRepo := memoryrepo.NewUserNotificationPreferenceRepository()
		subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
		authorEmail := "author@example.com"
		user := mustCreateNotificationUser(t, userRepo, authorEmail)
		mustEnsureOwnedNotificationTarget(t, subscriptionRepo, user.ID, authorEmail, true)
		mustUpsertNotificationPreference(t, preferenceRepo, user.ID, true)
		notifier, err := NewBuildNotificationService(BuildNotificationConfig{
			Enabled:          true,
			Recipients:       "dev@example.com",
			Sender:           sender,
			DeliveryRepo:     deliveryRepo,
			SubscriptionRepo: subscriptionRepo,
			UserRepo:         userRepo,
			PreferenceRepo:   preferenceRepo,
		})
		if err != nil {
			t.Fatalf("create notifier failed: %v", err)
		}

		if err := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", Status: domain.BuildStatusFailed, SourceAuthorEmail: &authorEmail}); err != nil {
			t.Fatalf("notify terminal build failed: %v", err)
		}
		if len(sender.messages) != 2 {
			t.Fatalf("expected default plus author recipient, got %d", len(sender.messages))
		}
	})

	t.Run("commit author recipient remains distinct from configured shared target", func(t *testing.T) {
		deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
		sender := &recordingEmailSender{}
		userRepo := memoryrepo.NewUserRepository()
		preferenceRepo := memoryrepo.NewUserNotificationPreferenceRepository()
		subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
		authorEmail := "dev@example.com"
		user := mustCreateNotificationUser(t, userRepo, authorEmail)
		mustEnsureOwnedNotificationTarget(t, subscriptionRepo, user.ID, authorEmail, true)
		mustUpsertNotificationPreference(t, preferenceRepo, user.ID, true)
		notifier, err := NewBuildNotificationService(BuildNotificationConfig{
			Enabled:          true,
			Recipients:       "dev@example.com",
			Sender:           sender,
			DeliveryRepo:     deliveryRepo,
			SubscriptionRepo: subscriptionRepo,
			UserRepo:         userRepo,
			PreferenceRepo:   preferenceRepo,
		})
		if err != nil {
			t.Fatalf("create notifier failed: %v", err)
		}

		if err := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", Status: domain.BuildStatusFailed, SourceAuthorEmail: &authorEmail}); err != nil {
			t.Fatalf("notify terminal build failed: %v", err)
		}
		if len(sender.messages) != 2 {
			t.Fatalf("expected configured shared and personal author targets to remain distinct, got %d", len(sender.messages))
		}
	})
}

func TestNewBuildNotificationService_RejectsClaimDurationBelowProviderSafetyMargin(t *testing.T) {
	_, err := NewBuildNotificationService(BuildNotificationConfig{
		Enabled:       true,
		ClaimDuration: minimumNotificationClaimDuration() - time.Second,
	})
	if err == nil {
		t.Fatal("expected claim duration validation error")
	}
	if !strings.Contains(err.Error(), "notification claim duration") {
		t.Fatalf("expected claim duration error, got %v", err)
	}
}

func TestBuildNotificationService_CommitAuthorPreferenceDelivery(t *testing.T) {
	newNotifier := func(t *testing.T, authorEmail string, targetEnabled bool, preferenceEnabled bool) (*BuildNotificationService, *recordingEmailSender, *memoryrepo.NotificationSubscriptionRepository) {
		t.Helper()

		sender := &recordingEmailSender{}
		deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
		userRepo := memoryrepo.NewUserRepository()
		preferenceRepo := memoryrepo.NewUserNotificationPreferenceRepository()
		subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
		user := mustCreateNotificationUser(t, userRepo, authorEmail)
		mustEnsureOwnedNotificationTarget(t, subscriptionRepo, user.ID, authorEmail, targetEnabled)
		mustUpsertNotificationPreference(t, preferenceRepo, user.ID, preferenceEnabled)

		notifier, err := NewBuildNotificationService(BuildNotificationConfig{
			Enabled:          true,
			Sender:           sender,
			DeliveryRepo:     deliveryRepo,
			SubscriptionRepo: subscriptionRepo,
			UserRepo:         userRepo,
			PreferenceRepo:   preferenceRepo,
		})
		if err != nil {
			t.Fatalf("create notifier failed: %v", err)
		}

		return notifier, sender, subscriptionRepo
	}

	t.Run("non-opted-in author does not receive personal author notification", func(t *testing.T) {
		authorEmail := "author@example.com"
		notifier, sender, _ := newNotifier(t, authorEmail, true, false)
		if err := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", Status: domain.BuildStatusFailed, SourceAuthorEmail: &authorEmail}); err != nil {
			t.Fatalf("notify terminal build failed: %v", err)
		}
		if len(sender.messages) != 0 {
			t.Fatalf("expected no personal author notification, got %+v", sender.messages)
		}
	})

	t.Run("unknown commit author is skipped safely", func(t *testing.T) {
		notifier, sender, _ := newNotifier(t, "author@example.com", true, true)
		unknown := "unknown@example.com"
		if err := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", Status: domain.BuildStatusFailed, SourceAuthorEmail: &unknown}); err != nil {
			t.Fatalf("notify terminal build failed: %v", err)
		}
		if len(sender.messages) != 0 {
			t.Fatalf("expected unknown author to be skipped, got %+v", sender.messages)
		}
	})

	t.Run("disabled personal target prevents delivery", func(t *testing.T) {
		authorEmail := "author@example.com"
		notifier, sender, _ := newNotifier(t, authorEmail, false, true)
		if err := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", Status: domain.BuildStatusFailed, SourceAuthorEmail: &authorEmail}); err != nil {
			t.Fatalf("notify terminal build failed: %v", err)
		}
		if len(sender.messages) != 0 {
			t.Fatalf("expected disabled target to prevent delivery, got %+v", sender.messages)
		}
	})

	t.Run("case-normalized email matching works", func(t *testing.T) {
		notifier, sender, _ := newNotifier(t, "author@example.com", true, true)
		mixedCase := "AUTHOR@EXAMPLE.COM"
		if err := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", Status: domain.BuildStatusFailed, SourceAuthorEmail: &mixedCase}); err != nil {
			t.Fatalf("notify terminal build failed: %v", err)
		}
		if len(sender.messages) != 1 || sender.messages[0].To != "<author@example.com>" {
			t.Fatalf("expected normalized author delivery, got %+v", sender.messages)
		}
	})

	t.Run("manual project subscription plus personal author preference produces one delivery to same target", func(t *testing.T) {
		authorEmail := "author@example.com"
		notifier, sender, subscriptionRepo := newNotifier(t, authorEmail, true, true)
		targets, err := subscriptionRepo.ListTargets(context.Background())
		if err != nil {
			t.Fatalf("list targets failed: %v", err)
		}
		projectID := "project-1"
		mustCreateNotificationSubscription(t, subscriptionRepo, targets[0].ID, &projectID, nil, domain.NotificationEventTypeBuildFailed, true)

		if err := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", ProjectID: projectID, Status: domain.BuildStatusFailed, SourceAuthorEmail: &authorEmail}); err != nil {
			t.Fatalf("notify terminal build failed: %v", err)
		}
		if len(sender.messages) != 1 || sender.messages[0].To != "<author@example.com>" {
			t.Fatalf("expected same-target dedupe, got %+v", sender.messages)
		}
	})

	t.Run("distinct targets still each receive delivery", func(t *testing.T) {
		authorEmail := "author@example.com"
		notifier, sender, subscriptionRepo := newNotifier(t, authorEmail, true, true)
		jobID := "job-1"
		manualTarget := mustCreateNotificationTarget(t, subscriptionRepo, "ops@example.com", true)
		mustCreateNotificationSubscription(t, subscriptionRepo, manualTarget.ID, nil, &jobID, domain.NotificationEventTypeBuildFailed, true)

		if err := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", JobID: &jobID, Status: domain.BuildStatusFailed, SourceAuthorEmail: &authorEmail}); err != nil {
			t.Fatalf("notify terminal build failed: %v", err)
		}
		if len(sender.messages) != 2 {
			t.Fatalf("expected distinct targets to receive delivery, got %+v", sender.messages)
		}
	})
}

func TestBuildNotificationService_SendSampleBuildFailureRequiresSender(t *testing.T) {
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com", SubscriptionRepo: memoryrepo.NewNotificationSubscriptionRepository()})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}
	if _, sendErr := notifier.SendSampleBuildFailure(context.Background()); sendErr == nil {
		t.Fatal("expected missing sender error")
	}
}

func TestBuildNotificationService_NotifyTerminalBuild_UsesProjectSubscriptionRecipients(t *testing.T) {
	sender := &recordingEmailSender{}
	subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
	target := mustCreateNotificationTarget(t, subscriptionRepo, "dev@example.com", true)
	projectID := "project-1"
	mustCreateNotificationSubscription(t, subscriptionRepo, target.ID, &projectID, nil, domain.NotificationEventTypeBuildFailed, true)
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{
		Enabled:          true,
		Recipients:       "fallback@example.com",
		Sender:           sender,
		SubscriptionRepo: subscriptionRepo,
	})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	err = notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", ProjectID: projectID, Status: domain.BuildStatusFailed})
	if err != nil {
		t.Fatalf("notify terminal build failed: %v", err)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("expected one subscription email, got %d", len(sender.messages))
	}
	if sender.messages[0].To != "<dev@example.com>" {
		t.Fatalf("expected project subscription recipient, got %q", sender.messages[0].To)
	}
}

func TestBuildNotificationService_NotifyTerminalBuild_UsesJobSubscriptionRecipients(t *testing.T) {
	sender := &recordingEmailSender{}
	subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
	target := mustCreateNotificationTarget(t, subscriptionRepo, "job@example.com", true)
	jobID := "job-1"
	mustCreateNotificationSubscription(t, subscriptionRepo, target.ID, nil, &jobID, domain.NotificationEventTypeBuildSucceeded, true)
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{
		Enabled:          true,
		Sender:           sender,
		SubscriptionRepo: subscriptionRepo,
	})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	err = notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", ProjectID: "project-1", JobID: &jobID, Status: domain.BuildStatusSuccess})
	if err != nil {
		t.Fatalf("notify terminal build failed: %v", err)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("expected one job subscription email, got %d", len(sender.messages))
	}
	if sender.messages[0].To != "<job@example.com>" {
		t.Fatalf("expected job subscription recipient, got %q", sender.messages[0].To)
	}
}

func TestBuildNotificationService_NotifyTerminalBuild_NonMatchingSubscriptionEventDoesNotSend(t *testing.T) {
	sender := &recordingEmailSender{}
	subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
	target := mustCreateNotificationTarget(t, subscriptionRepo, "dev@example.com", true)
	projectID := "project-1"
	mustCreateNotificationSubscription(t, subscriptionRepo, target.ID, &projectID, nil, domain.NotificationEventTypeBuildSucceeded, true)
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{
		Enabled:          true,
		Sender:           sender,
		SubscriptionRepo: subscriptionRepo,
	})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	err = notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", ProjectID: projectID, Status: domain.BuildStatusFailed})
	if err != nil {
		t.Fatalf("notify terminal build failed: %v", err)
	}
	if len(sender.messages) != 0 {
		t.Fatalf("expected no email for non-matching event, got %d", len(sender.messages))
	}
}

func TestBuildNotificationService_NotifyTerminalBuild_DisabledTargetDoesNotSend(t *testing.T) {
	sender := &recordingEmailSender{}
	subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
	target := mustCreateNotificationTarget(t, subscriptionRepo, "dev@example.com", false)
	projectID := "project-1"
	mustCreateNotificationSubscription(t, subscriptionRepo, target.ID, &projectID, nil, domain.NotificationEventTypeBuildFailed, true)
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{
		Enabled:          true,
		Sender:           sender,
		SubscriptionRepo: subscriptionRepo,
	})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	err = notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", ProjectID: projectID, Status: domain.BuildStatusFailed})
	if err != nil {
		t.Fatalf("notify terminal build failed: %v", err)
	}
	if len(sender.messages) != 0 {
		t.Fatalf("expected disabled target to suppress send, got %d", len(sender.messages))
	}
}

func TestBuildNotificationService_NotifyTerminalBuild_DisabledSubscriptionDoesNotSend(t *testing.T) {
	sender := &recordingEmailSender{}
	subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
	target := mustCreateNotificationTarget(t, subscriptionRepo, "dev@example.com", true)
	projectID := "project-1"
	mustCreateNotificationSubscription(t, subscriptionRepo, target.ID, &projectID, nil, domain.NotificationEventTypeBuildFailed, false)
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{
		Enabled:          true,
		Sender:           sender,
		SubscriptionRepo: subscriptionRepo,
	})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	err = notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", ProjectID: projectID, Status: domain.BuildStatusFailed})
	if err != nil {
		t.Fatalf("notify terminal build failed: %v", err)
	}
	if len(sender.messages) != 0 {
		t.Fatalf("expected disabled subscription to suppress send, got %d", len(sender.messages))
	}
}

func TestBuildNotificationService_NotifyTerminalBuild_DedupesProjectAndJobSubscriptionsForSameRecipient(t *testing.T) {
	sender := &recordingEmailSender{}
	deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
	subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
	target := mustCreateNotificationTarget(t, subscriptionRepo, "dev@example.com", true)
	projectID := "project-1"
	jobID := "job-1"
	mustCreateNotificationSubscription(t, subscriptionRepo, target.ID, &projectID, nil, domain.NotificationEventTypeBuildFailed, true)
	mustCreateNotificationSubscription(t, subscriptionRepo, target.ID, nil, &jobID, domain.NotificationEventTypeBuildFailed, true)
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{
		Enabled:          true,
		Sender:           sender,
		DeliveryRepo:     deliveryRepo,
		SubscriptionRepo: subscriptionRepo,
	})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	build := domain.Build{ID: "build-1", ProjectID: projectID, JobID: &jobID, Status: domain.BuildStatusFailed}
	err = notifier.NotifyTerminalBuild(context.Background(), build)
	if err != nil {
		t.Fatalf("notify terminal build failed: %v", err)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("expected one deduped email, got %d", len(sender.messages))
	}
	delivery := mustGetNotificationDelivery(t, deliveryRepo, "build-1", domain.NotificationEventTypeBuildFailed, "<dev@example.com>")
	if delivery.Attempts != 1 {
		t.Fatalf("expected one delivery attempt, got %d", delivery.Attempts)
	}
}

func TestBuildNotificationService_NotifyTerminalBuild_FallsBackToEnvRecipientsWithoutSubscriptions(t *testing.T) {
	sender := &recordingEmailSender{}
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{
		Enabled:          true,
		Recipients:       "fallback@example.com",
		Sender:           sender,
		SubscriptionRepo: memoryrepo.NewNotificationSubscriptionRepository(),
	})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	err = notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusFailed})
	if err != nil {
		t.Fatalf("notify terminal build failed: %v", err)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("expected fallback email, got %d", len(sender.messages))
	}
	if sender.messages[0].To != "<fallback@example.com>" {
		t.Fatalf("expected fallback recipient, got %q", sender.messages[0].To)
	}
}

func TestBuildNotificationService_NotifyTerminalBuild_DurableDedupeStillWorksForSubscriptionRecipients(t *testing.T) {
	sender := &recordingEmailSender{}
	deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
	subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
	target := mustCreateNotificationTarget(t, subscriptionRepo, "dev@example.com", true)
	projectID := "project-1"
	mustCreateNotificationSubscription(t, subscriptionRepo, target.ID, &projectID, nil, domain.NotificationEventTypeBuildFailed, true)
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{
		Enabled:          true,
		Sender:           sender,
		DeliveryRepo:     deliveryRepo,
		SubscriptionRepo: subscriptionRepo,
	})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	build := domain.Build{ID: "build-1", ProjectID: projectID, Status: domain.BuildStatusFailed}
	if err := notifier.NotifyTerminalBuild(context.Background(), build); err != nil {
		t.Fatalf("first notify failed: %v", err)
	}
	if err := notifier.NotifyTerminalBuild(context.Background(), build); err != nil {
		t.Fatalf("second notify failed: %v", err)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("expected one deduped subscription email, got %d", len(sender.messages))
	}
	delivery := mustGetNotificationDelivery(t, deliveryRepo, "build-1", domain.NotificationEventTypeBuildFailed, "<dev@example.com>")
	if delivery.Attempts != 1 {
		t.Fatalf("expected attempts to remain 1, got %d", delivery.Attempts)
	}
}

func TestBuildNotificationService_IsActive(t *testing.T) {
	var nilNotifier *BuildNotificationService
	if nilNotifier.isActive() {
		t.Fatal("expected nil notifier to be inactive")
	}
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com", Sender: &recordingEmailSender{}, SubscriptionRepo: memoryrepo.NewNotificationSubscriptionRepository()})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}
	if !notifier.isActive() {
		t.Fatal("expected notifier to be active")
	}
}

func TestBuildNotificationHelpers(t *testing.T) {
	if got, ok := buildStatusNotificationEventType(domain.BuildStatusSuccess); !ok || got != domain.NotificationEventTypeBuildSucceeded {
		t.Fatalf("expected success event type, got %q ok=%v", got, ok)
	}
	if got, ok := buildStatusNotificationEventType(domain.BuildStatusFailed); !ok || got != domain.NotificationEventTypeBuildFailed {
		t.Fatalf("expected failed event type, got %q ok=%v", got, ok)
	}
	if got, ok := buildStatusNotificationEventType(domain.BuildStatusCanceled); ok || got != "" {
		t.Fatalf("expected no event type for canceled build, got %q ok=%v", got, ok)
	}

	if got := buildStatusNotificationSummary(domain.BuildStatusCanceled); got != string(domain.BuildStatusCanceled) {
		t.Fatalf("expected fallback summary to echo status, got %q", got)
	}

	recipients, err := parseNotificationRecipients("")
	if err != nil {
		t.Fatalf("expected empty recipients to parse cleanly, got %v", err)
	}
	if len(recipients) != 0 {
		t.Fatalf("expected no recipients, got %v", recipients)
	}

	recipients, err = parseNotificationRecipients("dev@example.com, qa@example.com")
	if err != nil {
		t.Fatalf("expected recipients to parse, got %v", err)
	}
	if want := []string{"<dev@example.com>", "<qa@example.com>"}; fmt.Sprint(recipients) != fmt.Sprint(want) {
		t.Fatalf("unexpected parsed recipients: got %v want %v", recipients, want)
	}
}

func TestBuildNotificationService_NotifyTerminalBuild_SendsSlackWebhookSubscription(t *testing.T) {
	slackSender := &recordingSlackSender{}
	deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
	subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
	jobRepo := memoryrepo.NewJobRepository()
	projectRepo := memoryrepo.NewProjectRepository(jobRepo)
	target := mustCreateSlackNotificationTarget(t, subscriptionRepo, "https://hooks.slack.example/services/T/B/X", true)
	projectID := "project-1"
	jobID := "job-1"
	now := time.Now().UTC()
	if _, err := projectRepo.Create(context.Background(), domain.Project{ID: projectID, Name: "Payments API", Slug: "payments-api", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create project failed: %v", err)
	}
	if _, err := jobRepo.Create(context.Background(), domain.Job{ID: jobID, ProjectID: projectID, Name: "backend-ci", RepositoryURL: "https://github.com/example/payments.git", DefaultRef: "main", PipelineYAML: "version: 1", Enabled: true, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create job failed: %v", err)
	}
	mustCreateNotificationSubscription(t, subscriptionRepo, target.ID, &projectID, nil, domain.NotificationEventTypeBuildFailed, true)
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{
		Enabled:          true,
		SlackSender:      slackSender,
		JobRepo:          jobRepo,
		ProjectRepo:      projectRepo,
		DeliveryRepo:     deliveryRepo,
		SubscriptionRepo: subscriptionRepo,
		PublicBaseURL:    "https://ci.example.com/",
	})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}
	ref := "refs/heads/main"
	sha := "deadbeefcafebabedeadbeefcafebabedeadbeef"
	authorName := "Octo Cat"
	authorEmail := "octo@example.com"
	startedAt := time.Now().Add(-3 * time.Minute).UTC()
	finishedAt := time.Now().UTC()
	build := domain.Build{
		ID:                "build-1",
		ProjectID:         projectID,
		JobID:             &jobID,
		Status:            domain.BuildStatusFailed,
		BuildNumber:       42,
		SourceRef:         &ref,
		SourceSHA:         &sha,
		SourceAuthorName:  &authorName,
		SourceAuthorEmail: &authorEmail,
		StartedAt:         &startedAt,
		FinishedAt:        &finishedAt,
	}

	if err := notifier.NotifyTerminalBuild(context.Background(), build); err != nil {
		t.Fatalf("notify terminal build failed: %v", err)
	}
	if len(slackSender.messages) != 1 {
		t.Fatalf("expected one slack message, got %d", len(slackSender.messages))
	}
	if len(slackSender.webhookURLs) != 1 || slackSender.webhookURLs[0] != "https://hooks.slack.example/services/T/B/X" {
		t.Fatalf("unexpected slack webhook urls %v", slackSender.webhookURLs)
	}
	message := slackSender.messages[0].Text
	for _, want := range []string{":x:", "Project: <https://ci.example.com/projects/project-1|Payments API>", "Job: <https://ci.example.com/jobs/job-1|backend-ci>", "Build: <https://ci.example.com/builds/build-1|#42>", "Git: <https://github.com/example/payments/commit/deadbeefcafebabedeadbeefcafebabedeadbeef|main @ deadbee>", "Commit author: Octo Cat (octo@example.com)", "Build detail: https://ci.example.com/builds/build-1"} {
		if !strings.Contains(message, want) {
			t.Fatalf("expected slack text to contain %q, got %q", want, message)
		}
	}
	delivery := mustGetNotificationDelivery(t, deliveryRepo, "build-1", domain.NotificationEventTypeBuildFailed, "slack_webhook:"+target.ID)
	if delivery.Status != domain.NotificationDeliveryStatusSent {
		t.Fatalf("expected sent slack delivery status, got %q", delivery.Status)
	}
}

func TestBuildNotificationService_NotifyTerminalBuild_SlackFailureDoesNotBlockEmail(t *testing.T) {
	emailSender := &recordingEmailSender{}
	slackSender := &recordingSlackSender{err: errors.New("webhook unavailable")}
	subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
	deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
	projectID := "project-1"
	emailTarget := mustCreateNotificationTarget(t, subscriptionRepo, "dev@example.com", true)
	slackTarget := mustCreateSlackNotificationTarget(t, subscriptionRepo, "https://hooks.slack.example/services/T/B/X", true)
	mustCreateNotificationSubscription(t, subscriptionRepo, emailTarget.ID, &projectID, nil, domain.NotificationEventTypeBuildFailed, true)
	mustCreateNotificationSubscription(t, subscriptionRepo, slackTarget.ID, &projectID, nil, domain.NotificationEventTypeBuildFailed, true)
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{
		Enabled:          true,
		Sender:           emailSender,
		SlackSender:      slackSender,
		DeliveryRepo:     deliveryRepo,
		SubscriptionRepo: subscriptionRepo,
	})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	err = notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", ProjectID: projectID, Status: domain.BuildStatusFailed})
	if err == nil || !strings.Contains(err.Error(), "webhook unavailable") {
		t.Fatalf("expected slack failure error, got %v", err)
	}
	if len(emailSender.messages) != 1 {
		t.Fatalf("expected email delivery to proceed, got %d emails", len(emailSender.messages))
	}
	emailDelivery := mustGetNotificationDelivery(t, deliveryRepo, "build-1", domain.NotificationEventTypeBuildFailed, "<dev@example.com>")
	if emailDelivery.Status != domain.NotificationDeliveryStatusSent {
		t.Fatalf("expected email delivery sent, got %q", emailDelivery.Status)
	}
	slackDelivery := mustGetNotificationDelivery(t, deliveryRepo, "build-1", domain.NotificationEventTypeBuildFailed, "slack_webhook:"+slackTarget.ID)
	if slackDelivery.Status != domain.NotificationDeliveryStatusRetryWaiting {
		t.Fatalf("expected slack delivery retry_waiting, got %q", slackDelivery.Status)
	}
}

func TestBuildNotificationService_NotifyTerminalBuild_PersonalSlackDMDistinctFromEmailDelivery(t *testing.T) {
	emailSender := &recordingEmailSender{}
	slackClient := &recordingSlackDMClient{}
	deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
	subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
	userRepo := memoryrepo.NewUserRepository()
	preferenceRepo := memoryrepo.NewUserNotificationPreferenceRepository()
	identityRepo := memoryrepo.NewUserSlackIdentityRepository()
	workspaceRepo := memoryrepo.NewSlackWorkspaceIntegrationRepository()
	workspaceRepo.SetUserSlackIdentityRepository(identityRepo)

	authorEmail := "author@example.com"
	authorUser := mustCreateNotificationUser(t, userRepo, authorEmail)
	mustEnsureOwnedNotificationTarget(t, subscriptionRepo, authorUser.ID, authorEmail, true)
	if _, err := preferenceRepo.Upsert(context.Background(), domain.UserNotificationPreference{
		UserID:                          authorUser.ID,
		CommitAuthorFailureEmailEnabled: true,
		CommitAuthorFailureSlackEnabled: true,
		CommitAuthorFailureEmailSource:  domain.UserNotificationPreferenceSourceUser,
		CreatedAt:                       time.Now().UTC(),
		UpdatedAt:                       time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert notification preference failed: %v", err)
	}
	now := time.Now().UTC()
	if _, err := workspaceRepo.ConnectOrReplace(context.Background(), domain.SlackWorkspaceIntegration{
		ID:             "workspace-integration-1",
		WorkspaceID:    "T123",
		BotTokenSecret: "xoxb-secret",
		Enabled:        true,
		ConnectedAt:    now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, true); err != nil {
		t.Fatalf("connect slack workspace failed: %v", err)
	}
	if _, err := identityRepo.Upsert(context.Background(), domain.UserSlackIdentity{
		ID:                          "identity-1",
		UserID:                      authorUser.ID,
		SlackWorkspaceIntegrationID: "workspace-integration-1",
		SlackUserID:                 "U123",
		Enabled:                     true,
		LinkedAt:                    now,
		CreatedAt:                   now,
		UpdatedAt:                   now,
	}); err != nil {
		t.Fatalf("upsert slack identity failed: %v", err)
	}

	notifier, err := NewBuildNotificationService(BuildNotificationConfig{
		Enabled:          true,
		Sender:           emailSender,
		SlackClient:      slackClient,
		DeliveryRepo:     deliveryRepo,
		SubscriptionRepo: subscriptionRepo,
		UserRepo:         userRepo,
		PreferenceRepo:   preferenceRepo,
		IdentityRepo:     identityRepo,
		WorkspaceRepo:    workspaceRepo,
	})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	if err := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-dm-1", Status: domain.BuildStatusFailed, SourceAuthorEmail: &authorEmail}); err != nil {
		t.Fatalf("notify terminal build failed: %v", err)
	}
	if len(emailSender.messages) != 1 || emailSender.messages[0].To != "<author@example.com>" {
		t.Fatalf("expected one email delivery, got %+v", emailSender.messages)
	}
	if len(slackClient.userIDs) != 1 || slackClient.userIDs[0] != "U123" {
		t.Fatalf("expected one slack dm delivery to U123, got %+v", slackClient.userIDs)
	}
	if len(slackClient.tokens) != 1 || slackClient.tokens[0] != "xoxb-secret" {
		t.Fatalf("expected slack bot token to be used, got %+v", slackClient.tokens)
	}
	if !strings.Contains(slackClient.messages[0].Text, "Build failed") {
		t.Fatalf("expected slack dm message to contain build status, got %q", slackClient.messages[0].Text)
	}
	emailDelivery := mustGetNotificationDelivery(t, deliveryRepo, "build-dm-1", domain.NotificationEventTypeBuildFailed, "<author@example.com>")
	if emailDelivery.Status != domain.NotificationDeliveryStatusSent {
		t.Fatalf("expected sent email delivery, got %q", emailDelivery.Status)
	}
	slackDelivery := mustGetNotificationDelivery(t, deliveryRepo, "build-dm-1", domain.NotificationEventTypeBuildFailed, "slack_dm:workspace-integration-1:U123")
	if slackDelivery.Status != domain.NotificationDeliveryStatusSent {
		t.Fatalf("expected sent slack dm delivery, got %q", slackDelivery.Status)
	}
}

func TestBuildNotificationService_NotifyTerminalBuild_PersonalCommitAuthorPreferenceMatrix(t *testing.T) {
	tests := []struct {
		name         string
		status       domain.BuildStatus
		buildID      string
		options      personalCommitAuthorNotificationOptions
		wantEmails   int
		wantSlackDMs int
		eventType    domain.NotificationEventType
	}{
		{
			name:         "failure neither",
			status:       domain.BuildStatusFailed,
			buildID:      "build-failure-neither",
			options:      personalCommitAuthorNotificationOptions{failureEmailEnabled: false, failureSlackEnabled: false, createEmailTarget: true, emailTargetEnabled: true, createWorkspace: true, workspaceEnabled: true, createIdentity: true, identityEnabled: true},
			wantEmails:   0,
			wantSlackDMs: 0,
			eventType:    domain.NotificationEventTypeBuildFailed,
		},
		{
			name:         "failure email only",
			status:       domain.BuildStatusFailed,
			buildID:      "build-failure-email",
			options:      personalCommitAuthorNotificationOptions{failureEmailEnabled: true, failureSlackEnabled: false, createEmailTarget: true, emailTargetEnabled: true, createWorkspace: true, workspaceEnabled: true, createIdentity: true, identityEnabled: true},
			wantEmails:   1,
			wantSlackDMs: 0,
			eventType:    domain.NotificationEventTypeBuildFailed,
		},
		{
			name:         "failure slack only",
			status:       domain.BuildStatusFailed,
			buildID:      "build-failure-slack",
			options:      personalCommitAuthorNotificationOptions{failureEmailEnabled: false, failureSlackEnabled: true, createEmailTarget: true, emailTargetEnabled: true, createWorkspace: true, workspaceEnabled: true, createIdentity: true, identityEnabled: true},
			wantEmails:   0,
			wantSlackDMs: 1,
			eventType:    domain.NotificationEventTypeBuildFailed,
		},
		{
			name:         "failure both",
			status:       domain.BuildStatusFailed,
			buildID:      "build-failure-both",
			options:      personalCommitAuthorNotificationOptions{failureEmailEnabled: true, failureSlackEnabled: true, createEmailTarget: true, emailTargetEnabled: true, createWorkspace: true, workspaceEnabled: true, createIdentity: true, identityEnabled: true},
			wantEmails:   1,
			wantSlackDMs: 1,
			eventType:    domain.NotificationEventTypeBuildFailed,
		},
		{
			name:         "success neither",
			status:       domain.BuildStatusSuccess,
			buildID:      "build-success-neither",
			options:      personalCommitAuthorNotificationOptions{successEmailEnabled: false, successSlackEnabled: false, createEmailTarget: true, emailTargetEnabled: true, createWorkspace: true, workspaceEnabled: true, createIdentity: true, identityEnabled: true},
			wantEmails:   0,
			wantSlackDMs: 0,
			eventType:    domain.NotificationEventTypeBuildSucceeded,
		},
		{
			name:         "success email only",
			status:       domain.BuildStatusSuccess,
			buildID:      "build-success-email",
			options:      personalCommitAuthorNotificationOptions{successEmailEnabled: true, successSlackEnabled: false, createEmailTarget: true, emailTargetEnabled: true, createWorkspace: true, workspaceEnabled: true, createIdentity: true, identityEnabled: true},
			wantEmails:   1,
			wantSlackDMs: 0,
			eventType:    domain.NotificationEventTypeBuildSucceeded,
		},
		{
			name:         "success slack only",
			status:       domain.BuildStatusSuccess,
			buildID:      "build-success-slack",
			options:      personalCommitAuthorNotificationOptions{successEmailEnabled: false, successSlackEnabled: true, createEmailTarget: true, emailTargetEnabled: true, createWorkspace: true, workspaceEnabled: true, createIdentity: true, identityEnabled: true},
			wantEmails:   0,
			wantSlackDMs: 1,
			eventType:    domain.NotificationEventTypeBuildSucceeded,
		},
		{
			name:         "success both",
			status:       domain.BuildStatusSuccess,
			buildID:      "build-success-both",
			options:      personalCommitAuthorNotificationOptions{successEmailEnabled: true, successSlackEnabled: true, createEmailTarget: true, emailTargetEnabled: true, createWorkspace: true, workspaceEnabled: true, createIdentity: true, identityEnabled: true},
			wantEmails:   1,
			wantSlackDMs: 1,
			eventType:    domain.NotificationEventTypeBuildSucceeded,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newPersonalCommitAuthorNotificationFixture(t, tc.options)
			build := domain.Build{ID: tc.buildID, Status: tc.status, SourceAuthorEmail: &fixture.authorEmail}
			if err := fixture.notifier.NotifyTerminalBuild(context.Background(), build); err != nil {
				t.Fatalf("notify terminal build failed: %v", err)
			}
			if len(fixture.emailSender.messages) != tc.wantEmails {
				t.Fatalf("expected %d emails, got %d", tc.wantEmails, len(fixture.emailSender.messages))
			}
			if len(fixture.slackClient.userIDs) != tc.wantSlackDMs {
				t.Fatalf("expected %d slack dms, got %d", tc.wantSlackDMs, len(fixture.slackClient.userIDs))
			}

			emailRecipient := "<" + fixture.authorEmail + ">"
			slackRecipient := "slack_dm:" + fixture.workspaceID + ":" + fixture.slackUserID
			if tc.wantEmails == 1 {
				if delivery := mustGetNotificationDelivery(t, fixture.deliveryRepo, tc.buildID, tc.eventType, emailRecipient); delivery.Status != domain.NotificationDeliveryStatusSent {
					t.Fatalf("expected email delivery to be sent, got %q", delivery.Status)
				}
			} else {
				assertNotificationDeliveryAbsent(t, fixture.deliveryRepo, tc.buildID, tc.eventType, emailRecipient)
			}
			if tc.wantSlackDMs == 1 {
				if delivery := mustGetNotificationDelivery(t, fixture.deliveryRepo, tc.buildID, tc.eventType, slackRecipient); delivery.Status != domain.NotificationDeliveryStatusSent {
					t.Fatalf("expected slack dm delivery to be sent, got %q", delivery.Status)
				}
			} else {
				assertNotificationDeliveryAbsent(t, fixture.deliveryRepo, tc.buildID, tc.eventType, slackRecipient)
			}
		})
	}
}

func TestBuildNotificationService_NotifyTerminalBuild_PersonalChannelAvailabilityMatrix(t *testing.T) {
	tests := []struct {
		name         string
		options      personalCommitAuthorNotificationOptions
		wantEmails   int
		wantSlackDMs int
	}{
		{name: "email target missing only blocks email", options: personalCommitAuthorNotificationOptions{failureEmailEnabled: true, failureSlackEnabled: true, createWorkspace: true, workspaceEnabled: true, createIdentity: true, identityEnabled: true}, wantEmails: 0, wantSlackDMs: 1},
		{name: "email target disabled only blocks email", options: personalCommitAuthorNotificationOptions{failureEmailEnabled: true, failureSlackEnabled: true, createEmailTarget: true, emailTargetEnabled: false, createWorkspace: true, workspaceEnabled: true, createIdentity: true, identityEnabled: true}, wantEmails: 0, wantSlackDMs: 1},
		{name: "workspace missing only blocks slack", options: personalCommitAuthorNotificationOptions{failureEmailEnabled: true, failureSlackEnabled: true, createEmailTarget: true, emailTargetEnabled: true, createIdentity: true, identityEnabled: true}, wantEmails: 1, wantSlackDMs: 0},
		{name: "workspace disabled only blocks slack", options: personalCommitAuthorNotificationOptions{failureEmailEnabled: true, failureSlackEnabled: true, createEmailTarget: true, emailTargetEnabled: true, createWorkspace: true, workspaceEnabled: false, createIdentity: true, identityEnabled: true}, wantEmails: 1, wantSlackDMs: 0},
		{name: "identity missing only blocks slack", options: personalCommitAuthorNotificationOptions{failureEmailEnabled: true, failureSlackEnabled: true, createEmailTarget: true, emailTargetEnabled: true, createWorkspace: true, workspaceEnabled: true}, wantEmails: 1, wantSlackDMs: 0},
		{name: "identity disabled only blocks slack", options: personalCommitAuthorNotificationOptions{failureEmailEnabled: true, failureSlackEnabled: true, createEmailTarget: true, emailTargetEnabled: true, createWorkspace: true, workspaceEnabled: true, createIdentity: true, identityEnabled: false}, wantEmails: 1, wantSlackDMs: 0},
		{name: "workspace mismatch only blocks slack", options: personalCommitAuthorNotificationOptions{failureEmailEnabled: true, failureSlackEnabled: true, createEmailTarget: true, emailTargetEnabled: true, createWorkspace: true, workspaceEnabled: true, createIdentity: true, identityEnabled: true, identityWorkspaceID: "workspace-integration-2"}, wantEmails: 1, wantSlackDMs: 0},
		{name: "malformed slack user id only blocks slack", options: personalCommitAuthorNotificationOptions{failureEmailEnabled: true, failureSlackEnabled: true, createEmailTarget: true, emailTargetEnabled: true, createWorkspace: true, workspaceEnabled: true, createIdentity: true, identityEnabled: true, slackUserID: "invalid user"}, wantEmails: 1, wantSlackDMs: 0},
		{name: "both unavailable blocks both personal channels", options: personalCommitAuthorNotificationOptions{failureEmailEnabled: true, failureSlackEnabled: true}, wantEmails: 0, wantSlackDMs: 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newPersonalCommitAuthorNotificationFixture(t, tc.options)
			if err := fixture.notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-availability-" + strings.ReplaceAll(tc.name, " ", "-"), Status: domain.BuildStatusFailed, SourceAuthorEmail: &fixture.authorEmail}); err != nil {
				t.Fatalf("notify terminal build failed: %v", err)
			}
			if len(fixture.emailSender.messages) != tc.wantEmails {
				t.Fatalf("expected %d emails, got %d", tc.wantEmails, len(fixture.emailSender.messages))
			}
			if len(fixture.slackClient.userIDs) != tc.wantSlackDMs {
				t.Fatalf("expected %d slack dms, got %d", tc.wantSlackDMs, len(fixture.slackClient.userIDs))
			}
		})
	}
}

func TestBuildNotificationService_NotifyTerminalBuild_PersonalChannelsFailIndependently(t *testing.T) {
	tests := []struct {
		name                  string
		emailErr              error
		slackErr              error
		wantEmailStatus       domain.NotificationDeliveryStatus
		wantSlackStatus       domain.NotificationDeliveryStatus
		wantEmailDeliveries   int
		wantSlackDMDeliveries int
	}{
		{name: "email fails slack succeeds", emailErr: errors.New("smtp unavailable"), wantEmailStatus: domain.NotificationDeliveryStatusRetryWaiting, wantSlackStatus: domain.NotificationDeliveryStatusSent, wantEmailDeliveries: 1, wantSlackDMDeliveries: 1},
		{name: "slack fails email succeeds", slackErr: errors.New("chat.postMessage unavailable"), wantEmailStatus: domain.NotificationDeliveryStatusSent, wantSlackStatus: domain.NotificationDeliveryStatusRetryWaiting, wantEmailDeliveries: 1, wantSlackDMDeliveries: 1},
		{name: "both fail", emailErr: errors.New("smtp unavailable"), slackErr: errors.New("chat.postMessage unavailable"), wantEmailStatus: domain.NotificationDeliveryStatusRetryWaiting, wantSlackStatus: domain.NotificationDeliveryStatusRetryWaiting, wantEmailDeliveries: 1, wantSlackDMDeliveries: 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newPersonalCommitAuthorNotificationFixture(t, personalCommitAuthorNotificationOptions{failureEmailEnabled: true, failureSlackEnabled: true, createEmailTarget: true, emailTargetEnabled: true, createWorkspace: true, workspaceEnabled: true, createIdentity: true, identityEnabled: true})
			fixture.emailSender.err = tc.emailErr
			fixture.slackClient.err = tc.slackErr

			err := fixture.notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-transport-" + strings.ReplaceAll(tc.name, " ", "-"), Status: domain.BuildStatusFailed, SourceAuthorEmail: &fixture.authorEmail})
			if tc.emailErr != nil || tc.slackErr != nil {
				if err == nil {
					t.Fatal("expected notify terminal build to return an error")
				}
			} else if err != nil {
				t.Fatalf("notify terminal build failed: %v", err)
			}

			if len(fixture.emailSender.messages) != tc.wantEmailDeliveries {
				t.Fatalf("expected %d email attempts, got %d", tc.wantEmailDeliveries, len(fixture.emailSender.messages))
			}
			if len(fixture.slackClient.userIDs) != tc.wantSlackDMDeliveries {
				t.Fatalf("expected %d slack dm attempts, got %d", tc.wantSlackDMDeliveries, len(fixture.slackClient.userIDs))
			}

			emailDelivery := mustGetNotificationDelivery(t, fixture.deliveryRepo, "build-transport-"+strings.ReplaceAll(tc.name, " ", "-"), domain.NotificationEventTypeBuildFailed, "<"+fixture.authorEmail+">")
			if emailDelivery.Status != tc.wantEmailStatus {
				t.Fatalf("expected email delivery status %q, got %q", tc.wantEmailStatus, emailDelivery.Status)
			}
			slackDelivery := mustGetNotificationDelivery(t, fixture.deliveryRepo, "build-transport-"+strings.ReplaceAll(tc.name, " ", "-"), domain.NotificationEventTypeBuildFailed, "slack_dm:"+fixture.workspaceID+":"+fixture.slackUserID)
			if slackDelivery.Status != tc.wantSlackStatus {
				t.Fatalf("expected slack dm delivery status %q, got %q", tc.wantSlackStatus, slackDelivery.Status)
			}
		})
	}
}

func TestBuildNotificationService_NotifyTerminalBuild_PersonalSlackDMDedupesSeparatelyFromEmailAndWebhook(t *testing.T) {
	fixture := newPersonalCommitAuthorNotificationFixture(t, personalCommitAuthorNotificationOptions{failureEmailEnabled: true, failureSlackEnabled: true, createEmailTarget: true, emailTargetEnabled: true, createWorkspace: true, workspaceEnabled: true, createIdentity: true, identityEnabled: true, sharedSlackWebhook: true})
	build := domain.Build{ID: "build-dedupe-1", ProjectID: "project-1", Status: domain.BuildStatusFailed, SourceAuthorEmail: &fixture.authorEmail}

	if err := fixture.notifier.NotifyTerminalBuild(context.Background(), build); err != nil {
		t.Fatalf("first notify terminal build failed: %v", err)
	}
	if err := fixture.notifier.NotifyTerminalBuild(context.Background(), build); err != nil {
		t.Fatalf("replayed notify terminal build failed: %v", err)
	}

	if len(fixture.emailSender.messages) != 1 {
		t.Fatalf("expected one deduped email attempt, got %d", len(fixture.emailSender.messages))
	}
	if len(fixture.slackClient.userIDs) != 1 {
		t.Fatalf("expected one deduped personal slack dm attempt, got %d", len(fixture.slackClient.userIDs))
	}
	if len(fixture.slackSender.webhookURLs) != 1 {
		t.Fatalf("expected one shared slack webhook attempt, got %d", len(fixture.slackSender.webhookURLs))
	}

	emailDelivery := mustGetNotificationDelivery(t, fixture.deliveryRepo, build.ID, domain.NotificationEventTypeBuildFailed, "<"+fixture.authorEmail+">")
	if emailDelivery.Attempts != 1 || emailDelivery.Status != domain.NotificationDeliveryStatusSent {
		t.Fatalf("expected one successful email delivery attempt, got status=%q attempts=%d", emailDelivery.Status, emailDelivery.Attempts)
	}
	personalSlackDelivery := mustGetNotificationDelivery(t, fixture.deliveryRepo, build.ID, domain.NotificationEventTypeBuildFailed, "slack_dm:"+fixture.workspaceID+":"+fixture.slackUserID)
	if personalSlackDelivery.Attempts != 1 || personalSlackDelivery.Status != domain.NotificationDeliveryStatusSent {
		t.Fatalf("expected one successful personal slack delivery attempt, got status=%q attempts=%d", personalSlackDelivery.Status, personalSlackDelivery.Attempts)
	}
	sharedSlackDelivery := mustGetNotificationDelivery(t, fixture.deliveryRepo, build.ID, domain.NotificationEventTypeBuildFailed, "slack_webhook:"+fixture.sharedSlackTarget.ID)
	if sharedSlackDelivery.Attempts != 1 || sharedSlackDelivery.Status != domain.NotificationDeliveryStatusSent {
		t.Fatalf("expected one successful shared slack webhook delivery attempt, got status=%q attempts=%d", sharedSlackDelivery.Status, sharedSlackDelivery.Attempts)
	}

	if err := fixture.notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: build.ID, ProjectID: build.ProjectID, Status: domain.BuildStatusSuccess, SourceAuthorEmail: &fixture.authorEmail}); err != nil {
		t.Fatalf("success notify terminal build failed: %v", err)
	}
	if len(fixture.emailSender.messages) != 1 {
		t.Fatalf("expected success event with disabled success preferences not to resend email, got %d", len(fixture.emailSender.messages))
	}
}

func TestBuildNotificationService_NotifyTerminalBuild_SlackFailureSanitizedErrorDoesNotLeakWebhookToken(t *testing.T) {
	emailSender := &recordingEmailSender{}
	secretToken := "SECRET_TOKEN_123"
	slackSender := NewSlackWebhookSender(&recordingSlackHTTPDoer{err: errors.New("Post \"https://hooks.slack.example/services/T/B/" + secretToken + "\": dial tcp: lookup hooks.slack.example: i/o timeout")})
	subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
	deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
	projectID := "project-1"
	emailTarget := mustCreateNotificationTarget(t, subscriptionRepo, "dev@example.com", true)
	slackTarget := mustCreateSlackNotificationTarget(t, subscriptionRepo, "https://hooks.slack.example/services/T/B/"+secretToken, true)
	mustCreateNotificationSubscription(t, subscriptionRepo, emailTarget.ID, &projectID, nil, domain.NotificationEventTypeBuildFailed, true)
	mustCreateNotificationSubscription(t, subscriptionRepo, slackTarget.ID, &projectID, nil, domain.NotificationEventTypeBuildFailed, true)
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{
		Enabled:          true,
		Sender:           emailSender,
		SlackSender:      slackSender,
		DeliveryRepo:     deliveryRepo,
		SubscriptionRepo: subscriptionRepo,
	})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	err = notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", ProjectID: projectID, Status: domain.BuildStatusFailed})
	if err == nil || !strings.Contains(err.Error(), "slack webhook request failed") {
		t.Fatalf("expected sanitized slack failure error, got %v", err)
	}
	if strings.Contains(err.Error(), secretToken) {
		t.Fatalf("expected returned error to hide webhook token, got %q", err.Error())
	}
	if len(emailSender.messages) != 1 {
		t.Fatalf("expected email delivery to proceed, got %d emails", len(emailSender.messages))
	}
	slackDelivery := mustGetNotificationDelivery(t, deliveryRepo, "build-1", domain.NotificationEventTypeBuildFailed, "slack_webhook:"+slackTarget.ID)
	if slackDelivery.LastError == nil || *slackDelivery.LastError == "" {
		t.Fatalf("expected slack delivery last_error to be recorded, got %v", slackDelivery.LastError)
	}
	if strings.Contains(*slackDelivery.LastError, secretToken) {
		t.Fatalf("expected persisted last_error to hide webhook token, got %q", *slackDelivery.LastError)
	}
	if *slackDelivery.LastError != "slack webhook request failed" {
		t.Fatalf("expected sanitized persisted last_error, got %q", *slackDelivery.LastError)
	}
}

func TestBuildNotificationService_NotifyTerminalBuild_SlackMessageOmitsMissingOptionalFields(t *testing.T) {
	slackSender := &recordingSlackSender{}
	subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
	target := mustCreateSlackNotificationTarget(t, subscriptionRepo, "https://hooks.slack.example/services/T/B/X", true)
	projectID := "project-1"
	mustCreateNotificationSubscription(t, subscriptionRepo, target.ID, &projectID, nil, domain.NotificationEventTypeBuildSucceeded, true)
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{
		Enabled:          true,
		SlackSender:      slackSender,
		SubscriptionRepo: subscriptionRepo,
	})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	if err := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", ProjectID: projectID, Status: domain.BuildStatusSuccess}); err != nil {
		t.Fatalf("notify terminal build failed: %v", err)
	}
	message := slackSender.messages[0].Text
	for _, unwanted := range []string{"Git:", "Commit author:", "Build detail:"} {
		if strings.Contains(message, unwanted) {
			t.Fatalf("did not expect slack text to contain %q, got %q", unwanted, message)
		}
	}
}

func TestBuildNotificationHelpers_FrontendEntityURLs(t *testing.T) {
	tests := []struct {
		name      string
		baseURL   string
		projectID string
		jobID     string
		buildID   string
		wantProj  string
		wantJob   string
		wantBuild string
	}{
		{
			name:      "base url without trailing slash",
			baseURL:   "https://ci.example.com",
			projectID: "project-1",
			jobID:     "job-1",
			buildID:   "build-1",
			wantProj:  "https://ci.example.com/projects/project-1",
			wantJob:   "https://ci.example.com/jobs/job-1",
			wantBuild: "https://ci.example.com/builds/build-1",
		},
		{
			name:      "base url with trailing slash",
			baseURL:   "https://ci.example.com/",
			projectID: "project-1",
			jobID:     "job-1",
			buildID:   "build-1",
			wantProj:  "https://ci.example.com/projects/project-1",
			wantJob:   "https://ci.example.com/jobs/job-1",
			wantBuild: "https://ci.example.com/builds/build-1",
		},
		{
			name:      "empty base url falls back",
			baseURL:   "",
			projectID: "project-1",
			jobID:     "job-1",
			buildID:   "build-1",
			wantProj:  "",
			wantJob:   "",
			wantBuild: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := buildProjectDetailURL(tc.baseURL, tc.projectID); got != tc.wantProj {
				t.Fatalf("project url mismatch: got %q want %q", got, tc.wantProj)
			}
			if got := buildJobDetailURL(tc.baseURL, tc.jobID); got != tc.wantJob {
				t.Fatalf("job url mismatch: got %q want %q", got, tc.wantJob)
			}
			if got := buildBuildDetailURL(tc.baseURL, tc.buildID); got != tc.wantBuild {
				t.Fatalf("build url mismatch: got %q want %q", got, tc.wantBuild)
			}
		})
	}
}

func TestBuildNotificationHelpers_GitHubCommitURLs(t *testing.T) {
	fullSHA := "1fccc2972f530f59642f8f88f2f818ca1d2f0f99"
	tests := []struct {
		name   string
		remote string
		sha    string
		want   string
		ok     bool
	}{
		{
			name:   "github https with .git",
			remote: "https://github.com/owner/repo.git",
			sha:    fullSHA,
			want:   "https://github.com/owner/repo/commit/" + fullSHA,
			ok:     true,
		},
		{
			name:   "github https without .git",
			remote: "https://github.com/owner/repo",
			sha:    fullSHA,
			want:   "https://github.com/owner/repo/commit/" + fullSHA,
			ok:     true,
		},
		{
			name:   "github scp ssh",
			remote: "git@github.com:owner/repo.git",
			sha:    fullSHA,
			want:   "https://github.com/owner/repo/commit/" + fullSHA,
			ok:     true,
		},
		{
			name:   "github ssh url",
			remote: "ssh://git@github.com/owner/repo.git",
			sha:    fullSHA,
			want:   "https://github.com/owner/repo/commit/" + fullSHA,
			ok:     true,
		},
		{
			name:   "unknown host fallback",
			remote: "https://gitlab.com/owner/repo.git",
			sha:    fullSHA,
			want:   "",
			ok:     false,
		},
		{
			name:   "missing repository remote",
			remote: "",
			sha:    fullSHA,
			want:   "",
			ok:     false,
		},
		{
			name:   "missing commit sha",
			remote: "https://github.com/owner/repo.git",
			sha:    "",
			want:   "",
			ok:     false,
		},
		{
			name:   "non-full commit sha fallback",
			remote: "https://github.com/owner/repo.git",
			sha:    "1fccc29",
			want:   "",
			ok:     false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := buildRepositoryCommitURL(tc.remote, tc.sha)
			if ok != tc.ok {
				t.Fatalf("expected ok=%v, got %v (url=%q)", tc.ok, ok, got)
			}
			if got != tc.want {
				t.Fatalf("commit url mismatch: got %q want %q", got, tc.want)
			}
		})
	}
}

func TestFormatBuildStatusSlackText_LinkPolish(t *testing.T) {
	fullSHA := "1fccc2972f530f59642f8f88f2f818ca1d2f0f99"
	shortSHA := shortNotificationSHA(fullSHA)

	t.Run("links and escaped labels with configured urls", func(t *testing.T) {
		details := buildNotificationDetails{
			statusSummary: "failed",
			projectName:   "Core & <Proj>",
			projectLabel:  "Core & <Proj> (project-1)",
			projectURL:    "https://ci.example.com/projects/project-1",
			jobName:       "Build > Job",
			jobLabel:      "Build > Job (job-1)",
			jobURL:        "https://ci.example.com/jobs/job-1",
			buildNumber:   42,
			buildLabel:    "#42 (build-1)",
			buildURL:      "https://ci.example.com/builds/build-1",
			refLabel:      "refs/heads/main",
			shaLabel:      shortSHA,
			commitURL:     "https://github.com/owner/repo/commit/" + fullSHA,
			authorName:    "Bryan & <Choate>",
			authorEmail:   "bryan.choate@gmail.com",
		}

		message := formatBuildStatusSlackText(details)
		for _, want := range []string{
			":x: Build failed",
			"Project: <https://ci.example.com/projects/project-1|Core &amp; &lt;Proj&gt;>",
			"Job: <https://ci.example.com/jobs/job-1|Build &gt; Job>",
			"Build: <https://ci.example.com/builds/build-1|#42>",
			"Git: <https://github.com/owner/repo/commit/" + fullSHA + "|main @ " + shortSHA + ">",
			"Commit author: Bryan &amp; &lt;Choate&gt; (bryan.choate@gmail.com)",
			"Build detail: https://ci.example.com/builds/build-1",
		} {
			if !strings.Contains(message, want) {
				t.Fatalf("expected slack text to contain %q, got %q", want, message)
			}
		}
		if strings.Contains(message, "<bryan.choate@gmail.com>") {
			t.Fatalf("expected author to use parentheses, got %q", message)
		}
	})

	t.Run("plain text fallback without urls", func(t *testing.T) {
		details := buildNotificationDetails{
			statusSummary: "succeeded",
			projectLabel:  "project-1",
			jobLabel:      "job-1",
			buildLabel:    "#42 (build-1)",
			refLabel:      "refs/heads/main",
			shaLabel:      shortSHA,
			authorName:    "Bryan Choate",
			authorEmail:   "bryan.choate@gmail.com",
		}

		message := formatBuildStatusSlackText(details)
		for _, want := range []string{
			":white_check_mark: Build succeeded",
			"Project: project-1",
			"Job: job-1",
			"Build: #42 (build-1)",
			"Git: refs/heads/main @ " + shortSHA,
			"Commit author: Bryan Choate (bryan.choate@gmail.com)",
		} {
			if !strings.Contains(message, want) {
				t.Fatalf("expected slack text to contain %q, got %q", want, message)
			}
		}
		if strings.Contains(message, "<http") {
			t.Fatalf("did not expect mrkdwn links without urls, got %q", message)
		}
	})
}

func TestFormatBuildStatusSlackText_NonFullSHAFallbackStillLinksCoyoteEntities(t *testing.T) {
	details := buildNotificationDetails{
		statusSummary: "failed",
		projectName:   "Payments API",
		projectLabel:  "Payments API (project-1)",
		projectURL:    "https://ci.example.com/projects/project-1",
		jobName:       "backend-ci",
		jobLabel:      "backend-ci (job-1)",
		jobURL:        "https://ci.example.com/jobs/job-1",
		buildNumber:   42,
		buildLabel:    "#42 (build-1)",
		buildURL:      "https://ci.example.com/builds/build-1",
		refLabel:      "refs/heads/main",
		shaLabel:      "deadbee",
		// Non-full SHA intentionally means no commit URL should be linked.
		commitURL:   "",
		authorName:  "Octo Cat",
		authorEmail: "octo@example.com",
	}

	message := formatBuildStatusSlackText(details)
	for _, want := range []string{
		"Project: <https://ci.example.com/projects/project-1|Payments API>",
		"Job: <https://ci.example.com/jobs/job-1|backend-ci>",
		"Build: <https://ci.example.com/builds/build-1|#42>",
		"Git: refs/heads/main @ deadbee",
		"Commit author: Octo Cat (octo@example.com)",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("expected slack text to contain %q, got %q", want, message)
		}
	}
	if strings.Contains(message, "github.com") {
		t.Fatalf("did not expect commit link for non-full sha, got %q", message)
	}
}

func TestBuildNotificationHelpers_GitRefLabel(t *testing.T) {
	tests := []struct {
		name string
		ref  string
		want string
	}{
		{
			name: "refs/heads/ prefix",
			ref:  "refs/heads/main",
			want: "main",
		},
		{
			name: "refs/tags/ prefix",
			ref:  "refs/tags/v1.2.3",
			want: "v1.2.3",
		},
		{
			name: "plain branch name",
			ref:  "develop",
			want: "develop",
		},
		{
			name: "empty ref",
			ref:  "",
			want: "",
		},
		{
			name: "whitespace-only ref",
			ref:  "   ",
			want: "",
		},
		{
			name: "refs/heads/ with special chars",
			ref:  "refs/heads/feature/PROJ-123",
			want: "feature/PROJ-123",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := slackGitRefLabel(tc.ref); got != tc.want {
				t.Fatalf("git ref label mismatch: got %q want %q", got, tc.want)
			}
		})
	}
}

func TestBuildNotificationHelpers_RepositoryRemoteParsing(t *testing.T) {
	tests := []struct {
		name      string
		remote    string
		wantHost  string
		wantOwner string
		wantRepo  string
		wantOk    bool
	}{
		{
			name:      "https url with trailing .git",
			remote:    "https://github.com/acme/myrepo.git",
			wantHost:  "github.com",
			wantOwner: "acme",
			wantRepo:  "myrepo",
			wantOk:    true,
		},
		{
			name:      "scp-style git url",
			remote:    "git@github.com:acme/myrepo.git",
			wantHost:  "github.com",
			wantOwner: "acme",
			wantRepo:  "myrepo",
			wantOk:    true,
		},
		{
			name:      "ssh:// url with .git",
			remote:    "ssh://git@github.com/acme/myrepo.git",
			wantHost:  "github.com",
			wantOwner: "acme",
			wantRepo:  "myrepo",
			wantOk:    true,
		},
		{
			name:      "missing owner",
			remote:    "https://github.com//myrepo.git",
			wantHost:  "",
			wantOwner: "",
			wantRepo:  "",
			wantOk:    false,
		},
		{
			name:      "malformed git@ url without colon",
			remote:    "git@github.com/acme/myrepo",
			wantHost:  "",
			wantOwner: "",
			wantRepo:  "",
			wantOk:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			host, owner, repo, ok := parseRepositoryRemote(tc.remote)
			if ok != tc.wantOk {
				t.Fatalf("expected ok=%v, got %v", tc.wantOk, ok)
			}
			if host != tc.wantHost {
				t.Fatalf("host mismatch: got %q want %q", host, tc.wantHost)
			}
			if owner != tc.wantOwner {
				t.Fatalf("owner mismatch: got %q want %q", owner, tc.wantOwner)
			}
			if repo != tc.wantRepo {
				t.Fatalf("repo mismatch: got %q want %q", repo, tc.wantRepo)
			}
		})
	}
}

func TestBuildNotificationHelpers_DedupeRecipients(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  []string
	}{
		{
			name:  "dedupes and trims",
			input: []string{"alice@example.com", " alice@example.com ", "bob@example.com"},
			want:  []string{"alice@example.com", "bob@example.com"},
		},
		{
			name:  "skips empty strings",
			input: []string{"alice@example.com", "", "   ", "bob@example.com"},
			want:  []string{"alice@example.com", "bob@example.com"},
		},
		{
			name:  "empty input",
			input: []string{},
			want:  nil,
		},
		{
			name:  "all duplicates",
			input: []string{"alice@example.com", "alice@example.com", "alice@example.com"},
			want:  []string{"alice@example.com"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := dedupeRecipients(tc.input)
			if len(got) != len(tc.want) {
				t.Fatalf("dedupeRecipients mismatch: got length %d, want %d (got %v, want %v)", len(got), len(tc.want), got, tc.want)
			}
			for i, item := range got {
				if item != tc.want[i] {
					t.Fatalf("dedupeRecipients mismatch at index %d: got %q, want %q", i, item, tc.want[i])
				}
			}
		})
	}
}

func TestBuildNotificationHelpers_SlackStatusIndicator(t *testing.T) {
	tests := []struct {
		status string
		want   string
	}{
		{status: "succeeded", want: ":white_check_mark:"},
		{status: "failed", want: ":x:"},
		{status: "cancelled", want: ":information_source:"},
		{status: "", want: ":information_source:"},
	}

	for _, tc := range tests {
		if got := slackStatusIndicator(tc.status); got != tc.want {
			t.Fatalf("slackStatusIndicator(%q): got %q, want %q", tc.status, got, tc.want)
		}
	}
}

func TestBuildNotificationHelpers_ShortNotificationSHA(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "abc1234def5678", want: "abc1234"},
		{input: "abc", want: "abc"},
		{input: "   deadbeef   ", want: "deadbee"},
		{input: "", want: ""},
	}

	for _, tc := range tests {
		if got := shortNotificationSHA(tc.input); got != tc.want {
			t.Fatalf("shortNotificationSHA(%q): got %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestBuildNotificationHelpers_ParseNotificationRecipient(t *testing.T) {
	valid := "alice@example.com"
	withName := "Alice <alice@example.com>"
	withSpace := "  bob@example.com  "
	invalid := "not-an-email"
	empty := ""

	tests := []struct {
		name      string
		value     *string
		wantOk    bool
		wantCheck func(got string) bool
	}{
		{
			name:      "valid email",
			value:     &valid,
			wantOk:    true,
			wantCheck: func(got string) bool { return strings.Contains(got, "alice@example.com") },
		},
		{
			name:      "email with name",
			value:     &withName,
			wantOk:    true,
			wantCheck: func(got string) bool { return strings.Contains(got, "alice@example.com") },
		},
		{
			name:      "nil value",
			value:     nil,
			wantOk:    false,
			wantCheck: func(got string) bool { return got == "" },
		},
		{
			name:      "empty string",
			value:     &empty,
			wantOk:    false,
			wantCheck: func(got string) bool { return got == "" },
		},
		{
			name:      "whitespace only",
			value:     &withSpace,
			wantOk:    true,
			wantCheck: func(got string) bool { return strings.Contains(got, "bob@example.com") },
		},
		{
			name:      "invalid email",
			value:     &invalid,
			wantOk:    false,
			wantCheck: func(got string) bool { return got == "" },
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseNotificationRecipient(tc.value)
			if ok != tc.wantOk {
				t.Fatalf("parseNotificationRecipient ok mismatch: got %v, want %v", ok, tc.wantOk)
			}
			if !tc.wantCheck(got) {
				t.Fatalf("parseNotificationRecipient result failed check: got %q", got)
			}
		})
	}
}
