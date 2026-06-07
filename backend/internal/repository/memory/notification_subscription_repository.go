package memory

import (
	"context"
	"fmt"
	"net/mail"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type NotificationSubscriptionRepository struct {
	mu            sync.RWMutex
	targets       map[string]domain.NotificationTarget
	subscriptions map[string]domain.NotificationSubscription
	projectIndex  map[string]string
	jobIndex      map[string]string
}

func NewNotificationSubscriptionRepository() *NotificationSubscriptionRepository {
	return &NotificationSubscriptionRepository{
		targets:       make(map[string]domain.NotificationTarget),
		subscriptions: make(map[string]domain.NotificationSubscription),
		projectIndex:  make(map[string]string),
		jobIndex:      make(map[string]string),
	}
}

func (r *NotificationSubscriptionRepository) CreateTarget(_ context.Context, target domain.NotificationTarget) (domain.NotificationTarget, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	normalizedRecipient, err := normalizeNotificationTargetRecipient(target.Recipient)
	if err != nil {
		return domain.NotificationTarget{}, err
	}
	if strings.TrimSpace(target.ID) == "" {
		target.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	target.Type = domain.NotificationTargetType(strings.TrimSpace(string(target.Type)))
	if target.Type == "" {
		target.Type = domain.NotificationTargetTypeEmail
	}
	if target.Type != domain.NotificationTargetTypeEmail {
		return domain.NotificationTarget{}, fmt.Errorf("unsupported notification target type %q", target.Type)
	}
	target.Name = strings.TrimSpace(target.Name)
	target.Recipient = normalizedRecipient
	if target.CreatedAt.IsZero() {
		target.CreatedAt = now
	}
	if target.UpdatedAt.IsZero() {
		target.UpdatedAt = target.CreatedAt
	}
	if _, exists := r.findTargetByTypeAndRecipientLocked(target.Type, target.Recipient); exists {
		return domain.NotificationTarget{}, repository.ErrNotificationTargetDuplicate
	}

	r.targets[target.ID] = target
	return target, nil
}

func (r *NotificationSubscriptionRepository) CreateSubscription(_ context.Context, subscription domain.NotificationSubscription) (domain.NotificationSubscription, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if strings.TrimSpace(subscription.TargetID) == "" {
		return domain.NotificationSubscription{}, fmt.Errorf("notification subscription target_id is required")
	}
	if _, ok := r.targets[strings.TrimSpace(subscription.TargetID)]; !ok {
		return domain.NotificationSubscription{}, fmt.Errorf("notification subscription target %q not found", subscription.TargetID)
	}
	projectID := trimOptionalString(subscription.ProjectID)
	jobID := trimOptionalString(subscription.JobID)
	if (projectID == nil) == (jobID == nil) {
		return domain.NotificationSubscription{}, fmt.Errorf("notification subscription must specify exactly one scope")
	}
	if strings.TrimSpace(string(subscription.EventType)) == "" {
		return domain.NotificationSubscription{}, fmt.Errorf("notification subscription event_type is required")
	}
	if subscription.EventType != domain.NotificationEventTypeBuildSucceeded && subscription.EventType != domain.NotificationEventTypeBuildFailed {
		return domain.NotificationSubscription{}, fmt.Errorf("unsupported notification event type %q", subscription.EventType)
	}
	if strings.TrimSpace(subscription.ID) == "" {
		subscription.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	subscription.TargetID = strings.TrimSpace(subscription.TargetID)
	subscription.ProjectID = projectID
	subscription.JobID = jobID
	if subscription.CreatedAt.IsZero() {
		subscription.CreatedAt = now
	}
	if subscription.UpdatedAt.IsZero() {
		subscription.UpdatedAt = subscription.CreatedAt
	}
	key := notificationSubscriptionKey(subscription.TargetID, subscription.EventType, subscription.ProjectID, subscription.JobID)
	if subscription.ProjectID != nil {
		if _, exists := r.projectIndex[key]; exists {
			return domain.NotificationSubscription{}, repository.ErrNotificationSubscriptionDuplicate
		}
		r.projectIndex[key] = subscription.ID
	} else {
		if _, exists := r.jobIndex[key]; exists {
			return domain.NotificationSubscription{}, repository.ErrNotificationSubscriptionDuplicate
		}
		r.jobIndex[key] = subscription.ID
	}

	r.subscriptions[subscription.ID] = subscription
	return subscription, nil
}

func (r *NotificationSubscriptionRepository) ListEnabledMatchesForBuildEvent(_ context.Context, build domain.Build, eventType domain.NotificationEventType) ([]domain.NotificationSubscriptionMatch, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	matches := make([]domain.NotificationSubscriptionMatch, 0)
	projectID := strings.TrimSpace(build.ProjectID)
	jobID := trimOptionalString(build.JobID)
	for _, subscription := range r.subscriptions {
		if !subscription.Enabled {
			continue
		}
		if subscription.EventType != eventType {
			continue
		}
		if subscription.ProjectID != nil && (projectID == "" || *subscription.ProjectID != projectID) {
			continue
		}
		if subscription.JobID != nil {
			if jobID == nil || *subscription.JobID != *jobID {
				continue
			}
		}
		target, ok := r.targets[subscription.TargetID]
		if !ok || !target.Enabled {
			continue
		}
		matches = append(matches, domain.NotificationSubscriptionMatch{
			Subscription: subscription,
			Target:       target,
		})
	}

	return matches, nil
}

func (r *NotificationSubscriptionRepository) findTargetByTypeAndRecipientLocked(targetType domain.NotificationTargetType, recipient string) (domain.NotificationTarget, bool) {
	for _, target := range r.targets {
		if target.Type == targetType && target.Recipient == recipient {
			return target, true
		}
	}
	return domain.NotificationTarget{}, false
}

func notificationSubscriptionKey(targetID string, eventType domain.NotificationEventType, projectID *string, jobID *string) string {
	scope := ""
	if projectID != nil {
		scope = "project:" + *projectID
	}
	if jobID != nil {
		scope = "job:" + *jobID
	}
	return strings.TrimSpace(targetID) + "|" + strings.TrimSpace(string(eventType)) + "|" + scope
}

func normalizeNotificationTargetRecipient(value string) (string, error) {
	parsed, err := mail.ParseAddress(strings.TrimSpace(value))
	if err != nil {
		return "", fmt.Errorf("invalid notification target recipient %q: %w", strings.TrimSpace(value), err)
	}
	return parsed.String(), nil
}

func trimOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
