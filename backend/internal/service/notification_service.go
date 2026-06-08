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
var ErrNotificationTargetIDInvalid = errors.New("notification target id must be a valid UUID")
var ErrNotificationSubscriptionTargetIDRequired = errors.New("notification subscription target_id is required")
var ErrNotificationSubscriptionIDInvalid = errors.New("notification subscription id must be a valid UUID")
var ErrNotificationSubscriptionTargetIDInvalid = errors.New("notification subscription target_id must be a valid UUID")
var ErrNotificationSubscriptionProjectIDInvalid = errors.New("notification subscription project_id must be a valid UUID")
var ErrNotificationSubscriptionJobIDInvalid = errors.New("notification subscription job_id must be a valid UUID")
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
