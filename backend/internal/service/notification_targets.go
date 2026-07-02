package service

import (
	"context"
	"errors"
	"net/mail"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func (s *NotificationService) ListTargets(ctx context.Context) ([]domain.NotificationTarget, error) {
	return s.targetRepo.ListTargets(ctx)
}

func (s *NotificationService) GetOwnedEmailTarget(ctx context.Context, user domain.User) (domain.NotificationTarget, error) {
	ownerUserID := strings.TrimSpace(user.ID)
	if ownerUserID == "" {
		return domain.NotificationTarget{}, ErrNotificationPersonalUserIDRequired
	}
	return s.targetRepo.GetOwnedEmailTargetByUserID(ctx, ownerUserID)
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
	return s.targetRepo.EnsureOwnedEmailTargetInitialized(ctx, input)
}

func (s *NotificationService) SetOwnedEmailTargetEnabled(ctx context.Context, user domain.User, enabled *bool) (domain.NotificationTarget, error) {
	ownerUserID := strings.TrimSpace(user.ID)
	if ownerUserID == "" {
		return domain.NotificationTarget{}, ErrNotificationPersonalUserIDRequired
	}
	if enabled == nil {
		return domain.NotificationTarget{}, ErrNotificationTargetEnabledRequired
	}

	return s.targetRepo.SetOwnedEmailTargetEnabled(ctx, ownerUserID, *enabled, s.now().UTC())
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

	return s.targetRepo.CreateTarget(ctx, domain.NotificationTarget{
		ID:        uuid.NewString(),
		Type:      targetType,
		Origin:    domain.NotificationTargetOriginManual,
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
	current, err := s.targetRepo.GetTargetByID(ctx, targetID)
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
	return s.targetRepo.UpdateTarget(ctx, current)
}

func (s *NotificationService) DeleteTarget(ctx context.Context, id string) error {
	targetID, err := normalizeRequiredNotificationUUID(id, repository.ErrNotificationTargetNotFound, ErrNotificationTargetIDInvalid)
	if err != nil {
		return err
	}
	return s.targetRepo.DeleteTarget(ctx, targetID)
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
