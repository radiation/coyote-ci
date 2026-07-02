package memory

import (
	"context"
	"fmt"
	"net/mail"
	"net/url"
	"sort"
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
	preferences   *UserNotificationPreferenceRepository
	settings      *NotificationInstanceSettingsRepository
}

func NewNotificationSubscriptionRepository() *NotificationSubscriptionRepository {
	return &NotificationSubscriptionRepository{
		targets:       make(map[string]domain.NotificationTarget),
		subscriptions: make(map[string]domain.NotificationSubscription),
		projectIndex:  make(map[string]string),
		jobIndex:      make(map[string]string),
	}
}

func (r *NotificationSubscriptionRepository) SetNotificationPreferenceRepository(preferences repository.UserNotificationPreferenceRepository) {
	configured, ok := preferences.(*UserNotificationPreferenceRepository)
	if !ok {
		return
	}
	r.preferences = configured
}

func (r *NotificationSubscriptionRepository) SetNotificationInstanceSettingsRepository(settings repository.NotificationInstanceSettingsRepository) {
	configured, ok := settings.(*NotificationInstanceSettingsRepository)
	if !ok {
		return
	}
	r.settings = configured
}

func (r *NotificationSubscriptionRepository) CreateTarget(_ context.Context, target domain.NotificationTarget) (domain.NotificationTarget, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if strings.TrimSpace(target.ID) == "" {
		target.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	target.Type = domain.NotificationTargetType(strings.TrimSpace(string(target.Type)))
	if target.Type == "" {
		target.Type = domain.NotificationTargetTypeEmail
	}
	if target.Type != domain.NotificationTargetTypeEmail && target.Type != domain.NotificationTargetTypeSlackWebhook {
		return domain.NotificationTarget{}, fmt.Errorf("unsupported notification target type %q", target.Type)
	}
	normalizedRecipient, err := normalizeNotificationTargetRecipient(target.Type, target.Recipient)
	if err != nil {
		return domain.NotificationTarget{}, err
	}
	target.Name = strings.TrimSpace(target.Name)
	target.OwnerUserID = trimOptionalString(target.OwnerUserID)
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

func (r *NotificationSubscriptionRepository) ListTargets(_ context.Context) ([]domain.NotificationTarget, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	targets := make([]domain.NotificationTarget, 0, len(r.targets))
	for _, target := range r.targets {
		targets = append(targets, target)
	}
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].CreatedAt.Equal(targets[j].CreatedAt) {
			return targets[i].ID < targets[j].ID
		}
		return targets[i].CreatedAt.Before(targets[j].CreatedAt)
	})

	return targets, nil
}

func (r *NotificationSubscriptionRepository) GetTargetByID(_ context.Context, id string) (domain.NotificationTarget, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	target, ok := r.targets[strings.TrimSpace(id)]
	if !ok {
		return domain.NotificationTarget{}, repository.ErrNotificationTargetNotFound
	}
	return target, nil
}

func (r *NotificationSubscriptionRepository) GetOwnedEmailTargetByUserID(_ context.Context, userID string) (domain.NotificationTarget, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	trimmedUserID := strings.TrimSpace(userID)
	for _, target := range r.targets {
		if target.Type != domain.NotificationTargetTypeEmail {
			continue
		}
		if target.OwnerUserID == nil {
			continue
		}
		if strings.TrimSpace(*target.OwnerUserID) == trimmedUserID {
			return target, nil
		}
	}

	return domain.NotificationTarget{}, repository.ErrNotificationTargetNotFound
}

func (r *NotificationSubscriptionRepository) EnsureSharedTarget(_ context.Context, input repository.EnsureSharedNotificationTargetInput) (domain.NotificationTarget, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	targetType := domain.NotificationTargetType(strings.TrimSpace(string(input.Type)))
	if targetType == "" {
		targetType = domain.NotificationTargetTypeEmail
	}
	if targetType != domain.NotificationTargetTypeEmail && targetType != domain.NotificationTargetTypeSlackWebhook {
		return domain.NotificationTarget{}, fmt.Errorf("unsupported notification target type %q", targetType)
	}
	trimmedRecipient, err := normalizeNotificationTargetRecipient(targetType, input.Recipient)
	if err != nil {
		return domain.NotificationTarget{}, err
	}
	if existing, exists := r.findTargetByTypeAndRecipientLocked(targetType, trimmedRecipient); exists {
		return existing, nil
	}

	now := time.Now().UTC()
	createdAt := input.CreatedAt
	if createdAt.IsZero() {
		createdAt = now
	}
	updatedAt := input.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = createdAt
	}
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = uuid.NewString()
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = trimmedRecipient
	}

	created := domain.NotificationTarget{
		ID:        id,
		Type:      targetType,
		Name:      name,
		Recipient: trimmedRecipient,
		Enabled:   true,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}
	r.targets[created.ID] = created
	return created, nil
}

func (r *NotificationSubscriptionRepository) SetOwnedEmailTargetEnabled(_ context.Context, ownerUserID string, enabled bool, updatedAt time.Time) (domain.NotificationTarget, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	trimmedUserID := strings.TrimSpace(ownerUserID)
	for id, target := range r.targets {
		if target.Type != domain.NotificationTargetTypeEmail || target.OwnerUserID == nil {
			continue
		}
		if strings.TrimSpace(*target.OwnerUserID) != trimmedUserID {
			continue
		}

		target.Enabled = enabled
		if !updatedAt.IsZero() {
			target.UpdatedAt = updatedAt
		}
		r.targets[id] = target
		return target, nil
	}

	return domain.NotificationTarget{}, repository.ErrNotificationTargetNotFound
}

func (r *NotificationSubscriptionRepository) EnsureOwnedEmailTarget(_ context.Context, input repository.EnsureOwnedNotificationEmailTargetInput) (domain.NotificationTarget, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	trimmedOwnerID := strings.TrimSpace(input.OwnerUserID)
	trimmedRecipient := strings.TrimSpace(input.Recipient)

	for _, target := range r.targets {
		if target.Type != domain.NotificationTargetTypeEmail || target.OwnerUserID == nil {
			continue
		}
		if strings.TrimSpace(*target.OwnerUserID) == trimmedOwnerID {
			return target, nil
		}
	}

	for id, target := range r.targets {
		if target.Type != domain.NotificationTargetTypeEmail {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(target.Recipient), trimmedRecipient) {
			continue
		}

		if target.OwnerUserID != nil && strings.TrimSpace(*target.OwnerUserID) != trimmedOwnerID {
			return domain.NotificationTarget{}, repository.ErrNotificationTargetOwnershipConflict
		}

		if target.OwnerUserID == nil {
			for _, subscription := range r.subscriptions {
				if subscription.TargetID == target.ID {
					return domain.NotificationTarget{}, repository.ErrNotificationTargetOwnershipConflict
				}
			}
			target.OwnerUserID = &trimmedOwnerID
			target.UpdatedAt = input.UpdatedAt
			r.targets[id] = target
		}

		return target, nil
	}

	createdAt := input.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	updatedAt := input.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = createdAt
	}
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = uuid.NewString()
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = trimmedRecipient
	}

	created := domain.NotificationTarget{
		ID:          id,
		OwnerUserID: &trimmedOwnerID,
		Type:        domain.NotificationTargetTypeEmail,
		Name:        name,
		Recipient:   trimmedRecipient,
		Enabled:     true,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}
	r.targets[created.ID] = created
	return created, nil
}

func (r *NotificationSubscriptionRepository) EnsureOwnedEmailTargetInitialized(_ context.Context, input repository.EnsureOwnedNotificationEmailTargetInput) (domain.NotificationTarget, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	target, newlyEligible, err := r.ensureOwnedEmailTargetLocked(input)
	if err != nil {
		return domain.NotificationTarget{}, err
	}
	if !newlyEligible || r.preferences == nil {
		return target, nil
	}

	defaultFailureEnabled := true
	defaultSuccessEnabled := false
	if r.settings != nil {
		r.settings.mu.RLock()
		if r.settings.settings != nil {
			defaultFailureEnabled = r.settings.settings.DefaultCommitAuthorFailureEmailEnabled
			defaultSuccessEnabled = r.settings.settings.DefaultCommitAuthorSuccessEmailEnabled
		}
		r.settings.mu.RUnlock()
	}

	r.preferences.mu.Lock()
	defer r.preferences.mu.Unlock()

	trimmedUserID := strings.TrimSpace(input.OwnerUserID)
	if existing, exists := r.preferences.preferences[trimmedUserID]; !exists {
		successSource := domain.UserNotificationPreferenceSourceInstanceDefault
		r.preferences.preferences[trimmedUserID] = domain.UserNotificationPreference{
			UserID:                          trimmedUserID,
			CommitAuthorFailureEmailEnabled: defaultFailureEnabled,
			CommitAuthorFailureEmailSource:  domain.UserNotificationPreferenceSourceInstanceDefault,
			CommitAuthorSuccessEmailEnabled: defaultSuccessEnabled,
			CommitAuthorSuccessEmailSource:  &successSource,
			CreatedAt:                       input.CreatedAt,
			UpdatedAt:                       input.UpdatedAt,
		}
	} else if existing.CommitAuthorSuccessEmailSource == nil {
		successSource := domain.UserNotificationPreferenceSourceInstanceDefault
		existing.CommitAuthorSuccessEmailEnabled = defaultSuccessEnabled
		existing.CommitAuthorSuccessEmailSource = &successSource
		existing.UpdatedAt = input.UpdatedAt
		r.preferences.preferences[trimmedUserID] = existing
	}

	return target, nil
}

func (r *NotificationSubscriptionRepository) ensureOwnedEmailTargetLocked(input repository.EnsureOwnedNotificationEmailTargetInput) (domain.NotificationTarget, bool, error) {
	trimmedOwnerID := strings.TrimSpace(input.OwnerUserID)
	trimmedRecipient := strings.TrimSpace(input.Recipient)

	for _, target := range r.targets {
		if target.Type != domain.NotificationTargetTypeEmail || target.OwnerUserID == nil {
			continue
		}
		if strings.TrimSpace(*target.OwnerUserID) == trimmedOwnerID {
			return target, false, nil
		}
	}

	for id, target := range r.targets {
		if target.Type != domain.NotificationTargetTypeEmail {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(target.Recipient), trimmedRecipient) {
			continue
		}

		if target.OwnerUserID != nil && strings.TrimSpace(*target.OwnerUserID) != trimmedOwnerID {
			return domain.NotificationTarget{}, false, repository.ErrNotificationTargetOwnershipConflict
		}

		if target.OwnerUserID == nil {
			for _, subscription := range r.subscriptions {
				if subscription.TargetID == target.ID {
					return domain.NotificationTarget{}, false, repository.ErrNotificationTargetOwnershipConflict
				}
			}
			target.OwnerUserID = &trimmedOwnerID
			target.UpdatedAt = input.UpdatedAt
			r.targets[id] = target
			return target, true, nil
		}

		return target, false, nil
	}

	createdAt := input.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	updatedAt := input.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = createdAt
	}
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = uuid.NewString()
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = trimmedRecipient
	}

	created := domain.NotificationTarget{
		ID:          id,
		OwnerUserID: &trimmedOwnerID,
		Type:        domain.NotificationTargetTypeEmail,
		Name:        name,
		Recipient:   trimmedRecipient,
		Enabled:     true,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}
	r.targets[created.ID] = created
	return created, true, nil
}

func (r *NotificationSubscriptionRepository) UpdateTarget(_ context.Context, target domain.NotificationTarget) (domain.NotificationTarget, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	targetID := strings.TrimSpace(target.ID)
	current, ok := r.targets[targetID]
	if !ok {
		return domain.NotificationTarget{}, repository.ErrNotificationTargetNotFound
	}

	target.Type = domain.NotificationTargetType(strings.TrimSpace(string(target.Type)))
	if target.Type == "" {
		target.Type = domain.NotificationTargetTypeEmail
	}
	if target.Type != domain.NotificationTargetTypeEmail && target.Type != domain.NotificationTargetTypeSlackWebhook {
		return domain.NotificationTarget{}, fmt.Errorf("unsupported notification target type %q", target.Type)
	}
	normalizedRecipient, err := normalizeNotificationTargetRecipient(target.Type, target.Recipient)
	if err != nil {
		return domain.NotificationTarget{}, err
	}
	if existing, exists := r.findTargetByTypeAndRecipientLocked(target.Type, normalizedRecipient); exists && existing.ID != targetID {
		return domain.NotificationTarget{}, repository.ErrNotificationTargetDuplicate
	}

	current.Type = target.Type
	current.OwnerUserID = trimOptionalString(target.OwnerUserID)
	current.Name = strings.TrimSpace(target.Name)
	current.Recipient = normalizedRecipient
	current.Enabled = target.Enabled
	if !target.CreatedAt.IsZero() {
		current.CreatedAt = target.CreatedAt
	}
	if !target.UpdatedAt.IsZero() {
		current.UpdatedAt = target.UpdatedAt
	}

	r.targets[targetID] = current
	return current, nil
}

func (r *NotificationSubscriptionRepository) DeleteTarget(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	targetID := strings.TrimSpace(id)
	if _, ok := r.targets[targetID]; !ok {
		return repository.ErrNotificationTargetNotFound
	}
	delete(r.targets, targetID)
	for subscriptionID, subscription := range r.subscriptions {
		if subscription.TargetID != targetID {
			continue
		}
		delete(r.subscriptions, subscriptionID)
		if subscription.ProjectID != nil {
			delete(r.projectIndex, notificationSubscriptionKey(subscription.TargetID, subscription.EventType, subscription.ProjectID, subscription.JobID))
		} else {
			delete(r.jobIndex, notificationSubscriptionKey(subscription.TargetID, subscription.EventType, subscription.ProjectID, subscription.JobID))
		}
	}
	return nil
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

func (r *NotificationSubscriptionRepository) ListSubscriptions(_ context.Context, filter repository.NotificationSubscriptionListFilter) ([]domain.NotificationSubscription, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	projectID := trimOptionalString(filter.ProjectID)
	jobID := trimOptionalString(filter.JobID)
	subscriptions := make([]domain.NotificationSubscription, 0, len(r.subscriptions))
	for _, subscription := range r.subscriptions {
		if !matchesNotificationSubscriptionFilter(subscription, projectID, jobID) {
			continue
		}
		subscriptions = append(subscriptions, subscription)
	}
	sort.Slice(subscriptions, func(i, j int) bool {
		if subscriptions[i].CreatedAt.Equal(subscriptions[j].CreatedAt) {
			return subscriptions[i].ID < subscriptions[j].ID
		}
		return subscriptions[i].CreatedAt.Before(subscriptions[j].CreatedAt)
	})

	return subscriptions, nil
}

func (r *NotificationSubscriptionRepository) GetSubscriptionByID(_ context.Context, id string) (domain.NotificationSubscription, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	subscription, ok := r.subscriptions[strings.TrimSpace(id)]
	if !ok {
		return domain.NotificationSubscription{}, repository.ErrNotificationSubscriptionNotFound
	}
	return subscription, nil
}

func (r *NotificationSubscriptionRepository) UpdateSubscription(_ context.Context, subscription domain.NotificationSubscription) (domain.NotificationSubscription, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	subscriptionID := strings.TrimSpace(subscription.ID)
	current, ok := r.subscriptions[subscriptionID]
	if !ok {
		return domain.NotificationSubscription{}, repository.ErrNotificationSubscriptionNotFound
	}
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

	oldKey := notificationSubscriptionKey(current.TargetID, current.EventType, current.ProjectID, current.JobID)
	newKey := notificationSubscriptionKey(strings.TrimSpace(subscription.TargetID), subscription.EventType, projectID, jobID)
	if current.ProjectID != nil {
		delete(r.projectIndex, oldKey)
	} else {
		delete(r.jobIndex, oldKey)
	}
	if projectID != nil {
		if existingID, exists := r.projectIndex[newKey]; exists && existingID != subscriptionID {
			r.restoreNotificationSubscriptionIndexLocked(current)
			return domain.NotificationSubscription{}, repository.ErrNotificationSubscriptionDuplicate
		}
		r.projectIndex[newKey] = subscriptionID
	} else {
		if existingID, exists := r.jobIndex[newKey]; exists && existingID != subscriptionID {
			r.restoreNotificationSubscriptionIndexLocked(current)
			return domain.NotificationSubscription{}, repository.ErrNotificationSubscriptionDuplicate
		}
		r.jobIndex[newKey] = subscriptionID
	}

	current.TargetID = strings.TrimSpace(subscription.TargetID)
	current.ProjectID = projectID
	current.JobID = jobID
	current.EventType = subscription.EventType
	current.Enabled = subscription.Enabled
	if !subscription.CreatedAt.IsZero() {
		current.CreatedAt = subscription.CreatedAt
	}
	if !subscription.UpdatedAt.IsZero() {
		current.UpdatedAt = subscription.UpdatedAt
	}

	r.subscriptions[subscriptionID] = current
	return current, nil
}

func (r *NotificationSubscriptionRepository) DeleteSubscription(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	subscriptionID := strings.TrimSpace(id)
	subscription, ok := r.subscriptions[subscriptionID]
	if !ok {
		return repository.ErrNotificationSubscriptionNotFound
	}
	delete(r.subscriptions, subscriptionID)
	if subscription.ProjectID != nil {
		delete(r.projectIndex, notificationSubscriptionKey(subscription.TargetID, subscription.EventType, subscription.ProjectID, subscription.JobID))
	} else {
		delete(r.jobIndex, notificationSubscriptionKey(subscription.TargetID, subscription.EventType, subscription.ProjectID, subscription.JobID))
	}
	return nil
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
		if target.Type != targetType {
			continue
		}
		if targetType == domain.NotificationTargetTypeEmail {
			if strings.EqualFold(target.Recipient, recipient) {
				return target, true
			}
			continue
		}
		if target.Recipient == recipient {
			return target, true
		}
	}
	return domain.NotificationTarget{}, false
}

func (r *NotificationSubscriptionRepository) restoreNotificationSubscriptionIndexLocked(subscription domain.NotificationSubscription) {
	key := notificationSubscriptionKey(subscription.TargetID, subscription.EventType, subscription.ProjectID, subscription.JobID)
	if subscription.ProjectID != nil {
		r.projectIndex[key] = subscription.ID
		return
	}
	r.jobIndex[key] = subscription.ID
}

func matchesNotificationSubscriptionFilter(subscription domain.NotificationSubscription, projectID *string, jobID *string) bool {
	if projectID == nil && jobID == nil {
		return true
	}
	if projectID != nil && subscription.ProjectID != nil && *subscription.ProjectID == *projectID {
		return true
	}
	if jobID != nil && subscription.JobID != nil && *subscription.JobID == *jobID {
		return true
	}
	return false
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

func normalizeNotificationTargetRecipient(targetType domain.NotificationTargetType, value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	switch targetType {
	case domain.NotificationTargetTypeEmail:
		parsed, err := mail.ParseAddress(trimmed)
		if err != nil {
			return "", fmt.Errorf("invalid notification target recipient %q: %w", trimmed, err)
		}
		return parsed.String(), nil
	case domain.NotificationTargetTypeSlackWebhook:
		parsed, err := url.Parse(trimmed)
		if err != nil || parsed == nil || !parsed.IsAbs() || strings.ToLower(parsed.Scheme) != "https" || strings.TrimSpace(parsed.Host) == "" {
			return "", fmt.Errorf("notification target webhook_url must be a valid https URL")
		}
		return parsed.String(), nil
	default:
		return "", fmt.Errorf("unsupported notification target type %q", targetType)
	}
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
