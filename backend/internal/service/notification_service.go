package service

import (
	"context"
	"errors"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

var ErrNotificationTargetNameRequired = errors.New("notification target name is required")
var ErrNotificationTargetAddressRequired = errors.New("notification target address is required")
var ErrNotificationTargetAddressInvalid = errors.New("notification target address must be a valid email address")
var ErrNotificationSubscriptionTargetIDRequired = errors.New("notification subscription target_id is required")
var ErrNotificationSubscriptionScopeRequired = errors.New("exactly one of project_id or job_id is required")
var ErrNotificationSubscriptionEventTypeInvalid = errors.New("notification subscription event_type must be one of build_succeeded, build_failed")

type NotificationService struct {
	repo repository.NotificationSubscriptionRepository
	now  func() time.Time
}

func NewNotificationService(repo repository.NotificationSubscriptionRepository) *NotificationService {
	return &NotificationService{repo: repo, now: time.Now}
}

type CreateNotificationTargetInput struct {
	Name    string
	Address string
	Enabled *bool
}

type UpdateNotificationTargetInput struct {
	Name    *string
	Address *string
	Enabled *bool
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

func (s *NotificationService) CreateEmailTarget(ctx context.Context, input CreateNotificationTargetInput) (domain.NotificationTarget, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return domain.NotificationTarget{}, ErrNotificationTargetNameRequired
	}
	address, err := normalizeNotificationEmailAddress(input.Address)
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
		Type:      domain.NotificationTargetTypeEmail,
		Name:      name,
		Recipient: address,
		Enabled:   enabled,
		CreatedAt: now,
		UpdatedAt: now,
	})
}

func (s *NotificationService) UpdateTarget(ctx context.Context, id string, input UpdateNotificationTargetInput) (domain.NotificationTarget, error) {
	trimmedID := strings.TrimSpace(id)
	if trimmedID == "" {
		return domain.NotificationTarget{}, repository.ErrNotificationTargetNotFound
	}
	current, err := s.repo.GetTargetByID(ctx, trimmedID)
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
	if input.Address != nil {
		address, addressErr := normalizeNotificationEmailAddress(*input.Address)
		if addressErr != nil {
			return domain.NotificationTarget{}, addressErr
		}
		current.Recipient = address
	}
	if input.Enabled != nil {
		current.Enabled = *input.Enabled
	}
	current.UpdatedAt = s.now().UTC()
	return s.repo.UpdateTarget(ctx, current)
}

func (s *NotificationService) ListSubscriptions(ctx context.Context, input ListNotificationSubscriptionsInput) ([]domain.NotificationSubscription, error) {
	return s.repo.ListSubscriptions(ctx, repository.NotificationSubscriptionListFilter{
		ProjectID: trimOptionalStringValue(input.ProjectID),
		JobID:     trimOptionalStringValue(input.JobID),
	})
}

func (s *NotificationService) CreateSubscription(ctx context.Context, input CreateNotificationSubscriptionInput) (domain.NotificationSubscription, error) {
	targetID := strings.TrimSpace(input.TargetID)
	if targetID == "" {
		return domain.NotificationSubscription{}, ErrNotificationSubscriptionTargetIDRequired
	}
	if _, err := s.repo.GetTargetByID(ctx, targetID); err != nil {
		return domain.NotificationSubscription{}, err
	}
	projectID := trimOptionalStringValue(input.ProjectID)
	jobID := trimOptionalStringValue(input.JobID)
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
	trimmedID := strings.TrimSpace(id)
	if trimmedID == "" {
		return domain.NotificationSubscription{}, repository.ErrNotificationSubscriptionNotFound
	}
	current, err := s.repo.GetSubscriptionByID(ctx, trimmedID)
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
	trimmedID := strings.TrimSpace(id)
	if trimmedID == "" {
		return repository.ErrNotificationSubscriptionNotFound
	}
	return s.repo.DeleteSubscription(ctx, trimmedID)
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
	return address.String(), nil
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
