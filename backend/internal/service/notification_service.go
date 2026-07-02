package service

import (
	"context"
	"errors"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

var ErrNotificationTargetNameRequired = errors.New("notification target name is required")
var ErrNotificationTargetTypeInvalid = errors.New("notification target type must be one of email, slack_webhook")
var ErrNotificationTargetAddressRequired = errors.New("notification target address is required")
var ErrNotificationTargetAddressInvalid = errors.New("notification target address must be a valid email address")
var ErrNotificationTargetWebhookURLRequired = errors.New("notification target webhook_url is required")
var ErrNotificationTargetWebhookURLInvalid = errors.New("notification target webhook_url must be a valid https URL")
var ErrNotificationTargetIDInvalid = errors.New("notification target id must be a valid UUID")
var ErrNotificationTargetEnabledRequired = errors.New("notification target enabled is required")
var ErrNotificationSubscriptionTargetIDRequired = errors.New("notification subscription target_id is required")
var ErrNotificationSubscriptionIDInvalid = errors.New("notification subscription id must be a valid UUID")
var ErrNotificationSubscriptionTargetIDInvalid = errors.New("notification subscription target_id must be a valid UUID")
var ErrNotificationSubscriptionProjectIDInvalid = errors.New("notification subscription project_id must be a valid UUID")
var ErrNotificationSubscriptionJobIDInvalid = errors.New("notification subscription job_id must be a valid UUID")
var ErrNotificationSubscriptionScopeRequired = errors.New("exactly one of project_id or job_id is required")
var ErrNotificationSubscriptionEventTypeInvalid = errors.New("notification subscription event_type must be one of build_succeeded, build_failed")
var ErrNotificationPersonalEmailRequired = errors.New("authenticated user email is required")
var ErrNotificationPersonalUserIDRequired = errors.New("authenticated user id is required")
var ErrNotificationPreferenceChannelEnabledRequired = errors.New("email_enabled and slack_enabled are required")
var ErrNotificationDefaultsUpdateRequired = errors.New("at least one notification default value is required")
var ErrNotificationDefaultEnabledRequired = ErrNotificationDefaultsUpdateRequired
var ErrNotificationPreferencePersonalTargetRequired = errors.New("an enabled owned personal email target is required to enable commit-author notifications")
var ErrNotificationPreferencePersonalSlackRequired = errors.New("an enabled linked personal Slack identity in the connected workspace is required to enable personal Slack notifications")

const (
	NotificationPreferenceUnavailableReasonPersonalTargetRequired      = "personal_target_required"
	NotificationPreferenceUnavailableReasonPersonalTargetDisabled      = "personal_target_disabled"
	NotificationPreferenceUnavailableReasonSlackIdentityRequired       = "slack_identity_required"
	NotificationPreferenceUnavailableReasonSlackIdentityDisabled       = "slack_identity_disabled"
	NotificationPreferenceUnavailableReasonSlackWorkspaceNotConfigured = "slack_workspace_not_configured"
	NotificationPreferenceUnavailableReasonSlackWorkspaceDisabled      = "slack_workspace_disabled"
	NotificationPreferenceUnavailableReasonSlackWorkspaceMismatch      = "slack_workspace_mismatch"
)

type NotificationService struct {
	repo             repository.NotificationSubscriptionRepository
	targetRepo       notificationTargetRepository
	subscriptionRepo notificationSubscriptionRepository
	preferencesRepo  repository.UserNotificationPreferenceRepository
	settingsRepo     repository.NotificationInstanceSettingsRepository
	identityRepo     repository.UserSlackIdentityRepository
	workspaceRepo    repository.SlackWorkspaceIntegrationRepository
	now              func() time.Time
}

type notificationTargetRepository interface {
	CreateTarget(ctx context.Context, target domain.NotificationTarget) (domain.NotificationTarget, error)
	ListTargets(ctx context.Context) ([]domain.NotificationTarget, error)
	GetTargetByID(ctx context.Context, id string) (domain.NotificationTarget, error)
	GetOwnedEmailTargetByUserID(ctx context.Context, userID string) (domain.NotificationTarget, error)
	SetOwnedEmailTargetEnabled(ctx context.Context, ownerUserID string, enabled bool, updatedAt time.Time) (domain.NotificationTarget, error)
	EnsureOwnedEmailTargetInitialized(ctx context.Context, input repository.EnsureOwnedNotificationEmailTargetInput) (domain.NotificationTarget, error)
	UpdateTarget(ctx context.Context, target domain.NotificationTarget) (domain.NotificationTarget, error)
	DeleteTarget(ctx context.Context, id string) error
}

type notificationSubscriptionRepository interface {
	GetTargetByID(ctx context.Context, id string) (domain.NotificationTarget, error)
	CreateSubscription(ctx context.Context, subscription domain.NotificationSubscription) (domain.NotificationSubscription, error)
	ListSubscriptions(ctx context.Context, filter repository.NotificationSubscriptionListFilter) ([]domain.NotificationSubscription, error)
	GetSubscriptionByID(ctx context.Context, id string) (domain.NotificationSubscription, error)
	UpdateSubscription(ctx context.Context, subscription domain.NotificationSubscription) (domain.NotificationSubscription, error)
	DeleteSubscription(ctx context.Context, id string) error
}

type notificationPreferenceRepositoryAware interface {
	SetNotificationPreferenceRepository(preferences repository.UserNotificationPreferenceRepository)
}

type notificationInstanceSettingsRepositoryAware interface {
	SetNotificationInstanceSettingsRepository(settings repository.NotificationInstanceSettingsRepository)
}

func NewNotificationService(repo repository.NotificationSubscriptionRepository) *NotificationService {
	return &NotificationService{
		repo:             repo,
		targetRepo:       repo,
		subscriptionRepo: repo,
		now:              time.Now,
	}
}

type CommitAuthorNotificationPreferenceState struct {
	Email CommitAuthorEmailNotificationPreferenceState
	Slack CommitAuthorSlackNotificationPreferenceState
}

type CommitAuthorEmailNotificationPreferenceState struct {
	Enabled           bool
	DeliveryActive    bool
	Target            *domain.NotificationTarget
	UnavailableReason *string
}

type CommitAuthorSlackNotificationPreferenceState struct {
	Enabled           bool
	DeliveryActive    bool
	UnavailableReason *string
}

type CommitAuthorFailureNotificationPreferenceState = CommitAuthorNotificationPreferenceState
type CommitAuthorSuccessNotificationPreferenceState = CommitAuthorNotificationPreferenceState

type UpdateCommitAuthorNotificationPreferenceInput struct {
	EmailEnabled *bool
	SlackEnabled *bool
}

type NotificationDefaultsState struct {
	DefaultCommitAuthorFailureEmailEnabled bool
	DefaultCommitAuthorSuccessEmailEnabled bool
}

func (s *NotificationService) WithPreferenceRepository(preferencesRepo repository.UserNotificationPreferenceRepository) *NotificationService {
	s.preferencesRepo = preferencesRepo
	if aware, ok := s.repo.(notificationPreferenceRepositoryAware); ok {
		aware.SetNotificationPreferenceRepository(preferencesRepo)
	}
	return s
}

func (s *NotificationService) WithInstanceSettingsRepository(settingsRepo repository.NotificationInstanceSettingsRepository) *NotificationService {
	s.settingsRepo = settingsRepo
	if aware, ok := s.repo.(notificationInstanceSettingsRepositoryAware); ok {
		aware.SetNotificationInstanceSettingsRepository(settingsRepo)
	}
	return s
}

func (s *NotificationService) WithUserSlackIdentityRepository(identityRepo repository.UserSlackIdentityRepository) *NotificationService {
	s.identityRepo = identityRepo
	return s
}

func (s *NotificationService) WithSlackWorkspaceIntegrationRepository(workspaceRepo repository.SlackWorkspaceIntegrationRepository) *NotificationService {
	s.workspaceRepo = workspaceRepo
	return s
}

type CreateNotificationTargetInput struct {
	Type       string
	Name       string
	Address    string
	WebhookURL string
	Enabled    *bool
}

type UpdateNotificationTargetInput struct {
	Name       *string
	Address    *string
	WebhookURL *string
	Enabled    *bool
}

type ListNotificationSubscriptionsInput struct {
	ProjectID *string
	JobID     *string
}

type CreateNotificationSubscriptionInput struct {
	TargetID  string
	ProjectID *string
	JobID     *string
	EventType string
	Enabled   *bool
}

type UpdateNotificationSubscriptionInput struct {
	Enabled *bool
}
