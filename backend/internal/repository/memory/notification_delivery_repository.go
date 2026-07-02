package memory

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type NotificationDeliveryRepository struct {
	mu         sync.RWMutex
	deliveries map[string]domain.NotificationDelivery
	index      map[string]string
	recipient  map[string]string
}

func NewNotificationDeliveryRepository() *NotificationDeliveryRepository {
	return &NotificationDeliveryRepository{
		deliveries: make(map[string]domain.NotificationDelivery),
		index:      make(map[string]string),
		recipient:  make(map[string]string),
	}
}

func (r *NotificationDeliveryRepository) Create(ctx context.Context, delivery domain.NotificationDelivery) (domain.NotificationDelivery, error) {
	if err := ctx.Err(); err != nil {
		return domain.NotificationDelivery{}, err
	}
	result, err := r.Acquire(ctx, delivery)
	if err != nil {
		return domain.NotificationDelivery{}, err
	}
	if result.Outcome != repository.NotificationDeliveryAcquireOutcomeCreated {
		return domain.NotificationDelivery{}, repository.ErrNotificationDeliveryDuplicate
	}
	return result.Delivery, nil
}

func (r *NotificationDeliveryRepository) Acquire(ctx context.Context, delivery domain.NotificationDelivery) (repository.NotificationDeliveryAcquireResult, error) {
	if err := ctx.Err(); err != nil {
		return repository.NotificationDeliveryAcquireResult{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	delivery = delivery.Normalize()
	if err := delivery.ValidateIdentity(); err != nil {
		return repository.NotificationDeliveryAcquireResult{}, err
	}

	key := notificationDeliveryLogicalKey(delivery.BuildID, delivery.EventType, delivery.Transport, delivery.DestinationKey)
	if existingID, exists := r.index[key]; exists {
		existing := r.deliveries[existingID]
		return repository.NotificationDeliveryAcquireResult{
			Delivery: existing,
			Outcome:  repository.NotificationDeliveryAcquireOutcomeFromStatus(existing.Status),
		}, nil
	}

	now := time.Now().UTC()
	if strings.TrimSpace(delivery.ID) == "" {
		delivery.ID = uuid.NewString()
	}
	if delivery.CreatedAt.IsZero() {
		delivery.CreatedAt = now
	}
	if delivery.UpdatedAt.IsZero() {
		delivery.UpdatedAt = delivery.CreatedAt
	}
	if strings.TrimSpace(string(delivery.Status)) == "" {
		delivery.Status = domain.NotificationDeliveryStatusPending
	}

	r.deliveries[delivery.ID] = delivery
	r.index[key] = delivery.ID
	recipientKey := notificationDeliveryRecipientKey(delivery.BuildID, delivery.EventType, delivery.Recipient)
	if recipientKey != "||" {
		r.recipient[recipientKey] = delivery.ID
	}
	return repository.NotificationDeliveryAcquireResult{
		Delivery: delivery,
		Outcome:  repository.NotificationDeliveryAcquireOutcomeCreated,
	}, nil
}

func (r *NotificationDeliveryRepository) GetByBuildEventRecipient(ctx context.Context, buildID string, eventType domain.NotificationEventType, recipient string) (domain.NotificationDelivery, error) {
	if err := ctx.Err(); err != nil {
		return domain.NotificationDelivery{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	key := notificationDeliveryRecipientKey(buildID, eventType, recipient)
	id, ok := r.recipient[key]
	if !ok {
		return domain.NotificationDelivery{}, repository.ErrNotificationDeliveryNotFound
	}

	delivery, ok := r.deliveries[id]
	if !ok {
		return domain.NotificationDelivery{}, repository.ErrNotificationDeliveryNotFound
	}

	return delivery, nil
}

func (r *NotificationDeliveryRepository) Update(ctx context.Context, delivery domain.NotificationDelivery) (domain.NotificationDelivery, error) {
	if err := ctx.Err(); err != nil {
		return domain.NotificationDelivery{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	current, ok := r.deliveries[delivery.ID]
	if !ok {
		return domain.NotificationDelivery{}, repository.ErrNotificationDeliveryNotFound
	}

	if delivery.CreatedAt.IsZero() {
		delivery.CreatedAt = current.CreatedAt
	}
	if delivery.UpdatedAt.IsZero() {
		delivery.UpdatedAt = time.Now().UTC()
	}
	delivery = delivery.Normalize()
	if delivery.Transport == "" {
		delivery.Transport = current.Transport
	}
	if delivery.DestinationKind == "" {
		delivery.DestinationKind = current.DestinationKind
	}
	if delivery.DestinationKey == "" {
		delivery.DestinationKey = current.DestinationKey
	}
	if delivery.Recipient == "" {
		delivery.Recipient = current.Recipient
	}
	if delivery.NotificationTargetID == nil {
		delivery.NotificationTargetID = current.NotificationTargetID
	}
	if delivery.RecipientUserID == nil {
		delivery.RecipientUserID = current.RecipientUserID
	}
	if delivery.SlackWorkspaceIntegrationID == nil {
		delivery.SlackWorkspaceIntegrationID = current.SlackWorkspaceIntegrationID
	}

	r.deliveries[delivery.ID] = delivery
	return delivery, nil
}

func notificationDeliveryLogicalKey(buildID string, eventType domain.NotificationEventType, transport domain.NotificationTransport, destinationKey string) string {
	return strings.TrimSpace(buildID) + "|" + strings.TrimSpace(string(eventType)) + "|" + strings.TrimSpace(string(transport)) + "|" + strings.TrimSpace(destinationKey)
}

func notificationDeliveryRecipientKey(buildID string, eventType domain.NotificationEventType, recipient string) string {
	return strings.TrimSpace(buildID) + "|" + strings.TrimSpace(string(eventType)) + "|" + strings.TrimSpace(recipient)
}
