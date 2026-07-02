package service

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func (s *NotificationService) ListSubscriptions(ctx context.Context, input ListNotificationSubscriptionsInput) ([]domain.NotificationSubscription, error) {
	projectID, err := normalizeOptionalNotificationUUID(input.ProjectID, ErrNotificationSubscriptionProjectIDInvalid)
	if err != nil {
		return nil, err
	}
	jobID, err := normalizeOptionalNotificationUUID(input.JobID, ErrNotificationSubscriptionJobIDInvalid)
	if err != nil {
		return nil, err
	}
	return s.subscriptionRepo.ListSubscriptions(ctx, repository.NotificationSubscriptionListFilter{
		ProjectID: projectID,
		JobID:     jobID,
	})
}

func (s *NotificationService) CreateSubscription(ctx context.Context, input CreateNotificationSubscriptionInput) (domain.NotificationSubscription, error) {
	targetID, err := normalizeRequiredNotificationUUID(input.TargetID, ErrNotificationSubscriptionTargetIDRequired, ErrNotificationSubscriptionTargetIDInvalid)
	if err != nil {
		return domain.NotificationSubscription{}, err
	}
	if _, getTargetErr := s.subscriptionRepo.GetTargetByID(ctx, targetID); getTargetErr != nil {
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

	return s.subscriptionRepo.CreateSubscription(ctx, domain.NotificationSubscription{
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
	current, err := s.subscriptionRepo.GetSubscriptionByID(ctx, subscriptionID)
	if err != nil {
		return domain.NotificationSubscription{}, err
	}
	if input.Enabled != nil {
		current.Enabled = *input.Enabled
	}
	current.UpdatedAt = s.now().UTC()
	return s.subscriptionRepo.UpdateSubscription(ctx, current)
}

func (s *NotificationService) DeleteSubscription(ctx context.Context, id string) error {
	subscriptionID, err := normalizeRequiredNotificationUUID(id, repository.ErrNotificationSubscriptionNotFound, ErrNotificationSubscriptionIDInvalid)
	if err != nil {
		return err
	}
	return s.subscriptionRepo.DeleteSubscription(ctx, subscriptionID)
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
