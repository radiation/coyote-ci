package build

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	platformemail "github.com/radiation/coyote-ci/backend/internal/platform/email"
	platformslack "github.com/radiation/coyote-ci/backend/internal/platform/slack"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	memoryrepo "github.com/radiation/coyote-ci/backend/internal/repository/memory"
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
	listRecoverableFunc        func(context.Context, repository.NotificationDeliveryRecoverableScanInput) ([]domain.NotificationDelivery, error)
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

func (r *scriptedNotificationDeliveryRepo) ListRecoverable(ctx context.Context, input repository.NotificationDeliveryRecoverableScanInput) ([]domain.NotificationDelivery, error) {
	if r.listRecoverableFunc != nil {
		return r.listRecoverableFunc(ctx, input)
	}
	return nil, nil
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
