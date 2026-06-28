package service

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

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
var ErrNotificationSubscriptionTargetIDRequired = errors.New("notification subscription target_id is required")
var ErrNotificationSubscriptionIDInvalid = errors.New("notification subscription id must be a valid UUID")
var ErrNotificationSubscriptionTargetIDInvalid = errors.New("notification subscription target_id must be a valid UUID")
var ErrNotificationSubscriptionProjectIDInvalid = errors.New("notification subscription project_id must be a valid UUID")
var ErrNotificationSubscriptionJobIDInvalid = errors.New("notification subscription job_id must be a valid UUID")
var ErrNotificationSubscriptionScopeRequired = errors.New("exactly one of project_id or job_id is required")
var ErrNotificationSubscriptionEventTypeInvalid = errors.New("notification subscription event_type must be one of build_succeeded, build_failed")
var ErrNotificationPersonalEmailRequired = errors.New("authenticated user email is required")
var ErrNotificationPersonalUserIDRequired = errors.New("authenticated user id is required")
var ErrNotificationPreferenceEnabledRequired = errors.New("notification preference enabled is required")
var ErrNotificationDefaultEnabledRequired = errors.New("default commit-author failure email enabled is required")
var ErrNotificationPreferencePersonalTargetRequired = errors.New("an enabled owned personal email target is required to enable commit-author failure notifications")

const (
	NotificationPreferenceUnavailableReasonPersonalTargetRequired = "personal_target_required"
	NotificationPreferenceUnavailableReasonPersonalTargetDisabled = "personal_target_disabled"
)

type NotificationService struct {
	repo            repository.NotificationSubscriptionRepository
	preferencesRepo repository.UserNotificationPreferenceRepository
	settingsRepo    repository.NotificationInstanceSettingsRepository
	now             func() time.Time
}

type notificationOwnedEmailTargetInitializer interface {
	EnsureOwnedEmailTargetInitialized(ctx context.Context, input repository.EnsureOwnedNotificationEmailTargetInput) (domain.NotificationTarget, error)
}

type notificationPreferenceRepositoryAware interface {
	SetNotificationPreferenceRepository(preferences repository.UserNotificationPreferenceRepository)
}

type notificationInstanceSettingsRepositoryAware interface {
	SetNotificationInstanceSettingsRepository(settings repository.NotificationInstanceSettingsRepository)
}

func NewNotificationService(repo repository.NotificationSubscriptionRepository) *NotificationService {
	return &NotificationService{repo: repo, now: time.Now}
}

type CommitAuthorFailureNotificationPreferenceState struct {
	Enabled           bool
	Eligible          bool
	DeliveryActive    bool
	Target            *domain.NotificationTarget
	UnavailableReason *string
}

type NotificationDefaultsState struct {
	DefaultCommitAuthorFailureEmailEnabled bool
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

func (s *NotificationService) ListTargets(ctx context.Context) ([]domain.NotificationTarget, error) {
	return s.repo.ListTargets(ctx)
}

func (s *NotificationService) GetOwnedEmailTarget(ctx context.Context, user domain.User) (domain.NotificationTarget, error) {
	ownerUserID := strings.TrimSpace(user.ID)
	if ownerUserID == "" {
		return domain.NotificationTarget{}, ErrNotificationPersonalUserIDRequired
	}
	return s.repo.GetOwnedEmailTargetByUserID(ctx, ownerUserID)
}

func (s *NotificationService) EnsureOwnedEmailTarget(ctx context.Context, user domain.User) (domain.NotificationTarget, error) {
	ownerUserID := strings.TrimSpace(user.ID)
	if ownerUserID == "" {
		return domain.NotificationTarget{}, ErrNotificationPersonalUserIDRequired
	}

	normalizedIdentityEmail := NormalizeEmail(user.Email)
	if normalizedIdentityEmail == "" {
		return domain.NotificationTarget{}, ErrNotificationPersonalEmailRequired
	}

	recipient, err := normalizeNotificationEmailAddress(normalizedIdentityEmail)
	if err != nil {
		if errors.Is(err, ErrNotificationTargetAddressRequired) || errors.Is(err, ErrNotificationTargetAddressInvalid) {
			return domain.NotificationTarget{}, ErrNotificationPersonalEmailRequired
		}
		return domain.NotificationTarget{}, err
	}

	targetName := normalizedIdentityEmail
	if user.DisplayName != nil {
		if trimmedDisplayName := strings.TrimSpace(*user.DisplayName); trimmedDisplayName != "" {
			targetName = trimmedDisplayName
		}
	}

	now := s.now().UTC()
	input := repository.EnsureOwnedNotificationEmailTargetInput{
		ID:          uuid.NewString(),
		OwnerUserID: ownerUserID,
		Name:        targetName,
		Recipient:   recipient,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if initializer, ok := s.repo.(notificationOwnedEmailTargetInitializer); ok {
		return initializer.EnsureOwnedEmailTargetInitialized(ctx, input)
	}

	hadOwnedTarget := false
	if _, getErr := s.repo.GetOwnedEmailTargetByUserID(ctx, ownerUserID); getErr == nil {
		hadOwnedTarget = true
	} else if !errors.Is(getErr, repository.ErrNotificationTargetNotFound) {
		return domain.NotificationTarget{}, getErr
	}

	target, err := s.repo.EnsureOwnedEmailTarget(ctx, input)
	if err != nil {
		return domain.NotificationTarget{}, err
	}
	if !hadOwnedTarget {
		if initErr := s.initializeCommitAuthorFailurePreference(ctx, ownerUserID); initErr != nil {
			return domain.NotificationTarget{}, initErr
		}
	}
	return target, nil
}

func (s *NotificationService) GetNotificationDefaults(ctx context.Context) (NotificationDefaultsState, error) {
	enabled, err := s.getDefaultCommitAuthorFailureEmailEnabled(ctx)
	if err != nil {
		return NotificationDefaultsState{}, err
	}
	return NotificationDefaultsState{DefaultCommitAuthorFailureEmailEnabled: enabled}, nil
}

func (s *NotificationService) SetNotificationDefaults(ctx context.Context, enabled *bool) (NotificationDefaultsState, error) {
	if enabled == nil {
		return NotificationDefaultsState{}, ErrNotificationDefaultEnabledRequired
	}
	if s.settingsRepo == nil {
		return NotificationDefaultsState{}, errors.New("notification instance settings repository is not configured")
	}
	now := s.now().UTC()
	createdAt := now
	if existing, err := s.settingsRepo.Get(ctx); err == nil {
		createdAt = existing.CreatedAt
	} else if !errors.Is(err, repository.ErrNotificationInstanceSettingsNotFound) {
		return NotificationDefaultsState{}, err
	}
	settings, err := s.settingsRepo.Upsert(ctx, domain.NotificationInstanceSettings{
		DefaultCommitAuthorFailureEmailEnabled: *enabled,
		CreatedAt:                              createdAt,
		UpdatedAt:                              now,
	})
	if err != nil {
		return NotificationDefaultsState{}, err
	}
	return NotificationDefaultsState{DefaultCommitAuthorFailureEmailEnabled: settings.DefaultCommitAuthorFailureEmailEnabled}, nil
}

func (s *NotificationService) GetCommitAuthorFailureNotificationPreference(ctx context.Context, user domain.User) (CommitAuthorFailureNotificationPreferenceState, error) {
	ownerUserID := strings.TrimSpace(user.ID)
	if ownerUserID == "" {
		return CommitAuthorFailureNotificationPreferenceState{}, ErrNotificationPersonalUserIDRequired
	}

	preferenceEnabled, err := s.getCommitAuthorFailurePreferenceEnabled(ctx, ownerUserID)
	if err != nil {
		return CommitAuthorFailureNotificationPreferenceState{}, err
	}

	target, err := s.repo.GetOwnedEmailTargetByUserID(ctx, ownerUserID)
	if err != nil {
		if errors.Is(err, repository.ErrNotificationTargetNotFound) {
			reason := NotificationPreferenceUnavailableReasonPersonalTargetRequired
			return CommitAuthorFailureNotificationPreferenceState{
				Enabled:           preferenceEnabled,
				Eligible:          false,
				DeliveryActive:    false,
				UnavailableReason: &reason,
			}, nil
		}
		return CommitAuthorFailureNotificationPreferenceState{}, err
	}

	state := CommitAuthorFailureNotificationPreferenceState{
		Enabled:        preferenceEnabled,
		Eligible:       true,
		DeliveryActive: preferenceEnabled && target.Enabled,
		Target:         &target,
	}
	if !target.Enabled {
		reason := NotificationPreferenceUnavailableReasonPersonalTargetDisabled
		state.UnavailableReason = &reason
	}
	return state, nil
}

func (s *NotificationService) SetCommitAuthorFailureNotificationPreference(ctx context.Context, user domain.User, enabled *bool) (CommitAuthorFailureNotificationPreferenceState, error) {
	ownerUserID := strings.TrimSpace(user.ID)
	if ownerUserID == "" {
		return CommitAuthorFailureNotificationPreferenceState{}, ErrNotificationPersonalUserIDRequired
	}
	if enabled == nil {
		return CommitAuthorFailureNotificationPreferenceState{}, ErrNotificationPreferenceEnabledRequired
	}
	if *enabled {
		target, err := s.repo.GetOwnedEmailTargetByUserID(ctx, ownerUserID)
		if err != nil {
			if errors.Is(err, repository.ErrNotificationTargetNotFound) {
				return CommitAuthorFailureNotificationPreferenceState{}, ErrNotificationPreferencePersonalTargetRequired
			}
			return CommitAuthorFailureNotificationPreferenceState{}, err
		}
		if !target.Enabled {
			return CommitAuthorFailureNotificationPreferenceState{}, ErrNotificationPreferencePersonalTargetRequired
		}
	}
	if s.preferencesRepo == nil {
		return CommitAuthorFailureNotificationPreferenceState{}, errors.New("notification preference repository is not configured")
	}

	now := s.now().UTC()
	createdAt := now
	if existing, err := s.preferencesRepo.GetByUserID(ctx, ownerUserID); err == nil {
		createdAt = existing.CreatedAt
	} else if !errors.Is(err, repository.ErrUserNotificationPreferenceNotFound) {
		return CommitAuthorFailureNotificationPreferenceState{}, err
	}

	_, err := s.preferencesRepo.Upsert(ctx, domain.UserNotificationPreference{
		UserID:                     ownerUserID,
		CommitAuthorFailureEnabled: *enabled,
		Source:                     domain.UserNotificationPreferenceSourceUser,
		CreatedAt:                  createdAt,
		UpdatedAt:                  now,
	})
	if err != nil {
		return CommitAuthorFailureNotificationPreferenceState{}, err
	}

	return s.GetCommitAuthorFailureNotificationPreference(ctx, user)
}

func (s *NotificationService) CreateTarget(ctx context.Context, input CreateNotificationTargetInput) (domain.NotificationTarget, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return domain.NotificationTarget{}, ErrNotificationTargetNameRequired
	}
	targetType, err := normalizeNotificationTargetType(input.Type)
	if err != nil {
		return domain.NotificationTarget{}, err
	}
	recipient, err := normalizeNotificationTargetRecipient(targetType, input.Address, input.WebhookURL)
	if err != nil {
		return domain.NotificationTarget{}, err
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	now := s.now().UTC()

	return s.repo.CreateTarget(ctx, domain.NotificationTarget{
		ID:        uuid.NewString(),
		Type:      targetType,
		Name:      name,
		Recipient: recipient,
		Enabled:   enabled,
		CreatedAt: now,
		UpdatedAt: now,
	})
}

func (s *NotificationService) CreateEmailTarget(ctx context.Context, input CreateNotificationTargetInput) (domain.NotificationTarget, error) {
	input.Type = string(domain.NotificationTargetTypeEmail)
	return s.CreateTarget(ctx, input)
}

func (s *NotificationService) UpdateTarget(ctx context.Context, id string, input UpdateNotificationTargetInput) (domain.NotificationTarget, error) {
	targetID, err := normalizeRequiredNotificationUUID(id, repository.ErrNotificationTargetNotFound, ErrNotificationTargetIDInvalid)
	if err != nil {
		return domain.NotificationTarget{}, err
	}
	current, err := s.repo.GetTargetByID(ctx, targetID)
	if err != nil {
		return domain.NotificationTarget{}, err
	}
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" {
			return domain.NotificationTarget{}, ErrNotificationTargetNameRequired
		}
		current.Name = name
	}
	if input.Address != nil || input.WebhookURL != nil {
		address := ""
		if input.Address != nil {
			address = *input.Address
		}
		webhookURL := ""
		if input.WebhookURL != nil {
			webhookURL = *input.WebhookURL
		}
		recipient, recipientErr := normalizeNotificationTargetRecipient(current.Type, address, webhookURL)
		if recipientErr != nil {
			return domain.NotificationTarget{}, recipientErr
		}
		current.Recipient = recipient
	}
	if input.Enabled != nil {
		current.Enabled = *input.Enabled
	}
	current.UpdatedAt = s.now().UTC()
	return s.repo.UpdateTarget(ctx, current)
}

func (s *NotificationService) DeleteTarget(ctx context.Context, id string) error {
	targetID, err := normalizeRequiredNotificationUUID(id, repository.ErrNotificationTargetNotFound, ErrNotificationTargetIDInvalid)
	if err != nil {
		return err
	}
	return s.repo.DeleteTarget(ctx, targetID)
}

func (s *NotificationService) ListSubscriptions(ctx context.Context, input ListNotificationSubscriptionsInput) ([]domain.NotificationSubscription, error) {
	projectID, err := normalizeOptionalNotificationUUID(input.ProjectID, ErrNotificationSubscriptionProjectIDInvalid)
	if err != nil {
		return nil, err
	}
	jobID, err := normalizeOptionalNotificationUUID(input.JobID, ErrNotificationSubscriptionJobIDInvalid)
	if err != nil {
		return nil, err
	}
	return s.repo.ListSubscriptions(ctx, repository.NotificationSubscriptionListFilter{
		ProjectID: projectID,
		JobID:     jobID,
	})
}

func (s *NotificationService) CreateSubscription(ctx context.Context, input CreateNotificationSubscriptionInput) (domain.NotificationSubscription, error) {
	targetID, err := normalizeRequiredNotificationUUID(input.TargetID, ErrNotificationSubscriptionTargetIDRequired, ErrNotificationSubscriptionTargetIDInvalid)
	if err != nil {
		return domain.NotificationSubscription{}, err
	}
	if _, getTargetErr := s.repo.GetTargetByID(ctx, targetID); getTargetErr != nil {
		return domain.NotificationSubscription{}, getTargetErr
	}
	projectID, err := normalizeOptionalNotificationUUID(input.ProjectID, ErrNotificationSubscriptionProjectIDInvalid)
	if err != nil {
		return domain.NotificationSubscription{}, err
	}
	jobID, err := normalizeOptionalNotificationUUID(input.JobID, ErrNotificationSubscriptionJobIDInvalid)
	if err != nil {
		return domain.NotificationSubscription{}, err
	}
	if (projectID == nil) == (jobID == nil) {
		return domain.NotificationSubscription{}, ErrNotificationSubscriptionScopeRequired
	}
	eventType, err := normalizeNotificationEventType(input.EventType)
	if err != nil {
		return domain.NotificationSubscription{}, err
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	now := s.now().UTC()

	return s.repo.CreateSubscription(ctx, domain.NotificationSubscription{
		ID:        uuid.NewString(),
		TargetID:  targetID,
		ProjectID: projectID,
		JobID:     jobID,
		EventType: eventType,
		Enabled:   enabled,
		CreatedAt: now,
		UpdatedAt: now,
	})
}

func (s *NotificationService) UpdateSubscription(ctx context.Context, id string, input UpdateNotificationSubscriptionInput) (domain.NotificationSubscription, error) {
	subscriptionID, err := normalizeRequiredNotificationUUID(id, repository.ErrNotificationSubscriptionNotFound, ErrNotificationSubscriptionIDInvalid)
	if err != nil {
		return domain.NotificationSubscription{}, err
	}
	current, err := s.repo.GetSubscriptionByID(ctx, subscriptionID)
	if err != nil {
		return domain.NotificationSubscription{}, err
	}
	if input.Enabled != nil {
		current.Enabled = *input.Enabled
	}
	current.UpdatedAt = s.now().UTC()
	return s.repo.UpdateSubscription(ctx, current)
}

func (s *NotificationService) DeleteSubscription(ctx context.Context, id string) error {
	subscriptionID, err := normalizeRequiredNotificationUUID(id, repository.ErrNotificationSubscriptionNotFound, ErrNotificationSubscriptionIDInvalid)
	if err != nil {
		return err
	}
	return s.repo.DeleteSubscription(ctx, subscriptionID)
}

func normalizeNotificationEmailAddress(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", ErrNotificationTargetAddressRequired
	}
	address, err := mail.ParseAddress(trimmed)
	if err != nil {
		return "", ErrNotificationTargetAddressInvalid
	}
	return (&mail.Address{Address: NormalizeEmail(address.Address)}).String(), nil
}

func normalizeNotificationTargetType(value string) (domain.NotificationTargetType, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return domain.NotificationTargetTypeEmail, nil
	}
	switch domain.NotificationTargetType(trimmed) {
	case domain.NotificationTargetTypeEmail, domain.NotificationTargetTypeSlackWebhook:
		return domain.NotificationTargetType(trimmed), nil
	default:
		return "", ErrNotificationTargetTypeInvalid
	}
}

func normalizeNotificationTargetRecipient(targetType domain.NotificationTargetType, address string, webhookURL string) (string, error) {
	switch targetType {
	case domain.NotificationTargetTypeEmail:
		return normalizeNotificationEmailAddress(address)
	case domain.NotificationTargetTypeSlackWebhook:
		return normalizeNotificationWebhookURL(webhookURL)
	default:
		return "", ErrNotificationTargetTypeInvalid
	}
}

func (s *NotificationService) getCommitAuthorFailurePreferenceEnabled(ctx context.Context, userID string) (bool, error) {
	if s.preferencesRepo == nil {
		return false, nil
	}
	preference, err := s.preferencesRepo.GetByUserID(ctx, userID)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotificationPreferenceNotFound) {
			return false, nil
		}
		return false, err
	}
	return preference.CommitAuthorFailureEnabled, nil
}

func (s *NotificationService) initializeCommitAuthorFailurePreference(ctx context.Context, ownerUserID string) error {
	if s.preferencesRepo == nil {
		return nil
	}
	enabled, err := s.getDefaultCommitAuthorFailureEmailEnabled(ctx)
	if err != nil {
		return err
	}
	now := s.now().UTC()
	_, _, err = s.preferencesRepo.InitializeIfAbsent(ctx, domain.UserNotificationPreference{
		UserID:                     ownerUserID,
		CommitAuthorFailureEnabled: enabled,
		Source:                     domain.UserNotificationPreferenceSourceInstanceDefault,
		CreatedAt:                  now,
		UpdatedAt:                  now,
	})
	return err
}

func (s *NotificationService) getDefaultCommitAuthorFailureEmailEnabled(ctx context.Context) (bool, error) {
	if s.settingsRepo == nil {
		return true, nil
	}
	settings, err := s.settingsRepo.Get(ctx)
	if err != nil {
		if errors.Is(err, repository.ErrNotificationInstanceSettingsNotFound) {
			return true, nil
		}
		return false, fmt.Errorf("get notification instance settings: %w", err)
	}
	return settings.DefaultCommitAuthorFailureEmailEnabled, nil
}

func normalizeNotificationWebhookURL(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", ErrNotificationTargetWebhookURLRequired
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed == nil || !parsed.IsAbs() || strings.ToLower(parsed.Scheme) != "https" || strings.TrimSpace(parsed.Host) == "" {
		return "", ErrNotificationTargetWebhookURLInvalid
	}
	return parsed.String(), nil
}

func normalizeNotificationEventType(value string) (domain.NotificationEventType, error) {
	trimmed := strings.TrimSpace(value)
	switch domain.NotificationEventType(trimmed) {
	case domain.NotificationEventTypeBuildSucceeded, domain.NotificationEventTypeBuildFailed:
		return domain.NotificationEventType(trimmed), nil
	default:
		return "", ErrNotificationSubscriptionEventTypeInvalid
	}
}

func trimOptionalStringValue(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func normalizeRequiredNotificationUUID(value string, emptyErr error, invalidErr error) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", emptyErr
	}
	parsed, parseErr := uuid.Parse(trimmed)
	if parseErr != nil {
		return "", invalidErr
	}
	return parsed.String(), nil
}

func normalizeOptionalNotificationUUID(value *string, invalidErr error) (*string, error) {
	trimmed := trimOptionalStringValue(value)
	if trimmed == nil {
		return nil, nil
	}
	parsed, parseErr := uuid.Parse(*trimmed)
	if parseErr != nil {
		return nil, invalidErr
	}
	normalized := parsed.String()
	return &normalized, nil
}
