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
}

func NewNotificationDeliveryRepository() *NotificationDeliveryRepository {
	return &NotificationDeliveryRepository{
		deliveries: make(map[string]domain.NotificationDelivery),
		index:      make(map[string]string),
	}
}

func (r *NotificationDeliveryRepository) Create(_ context.Context, delivery domain.NotificationDelivery) (domain.NotificationDelivery, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := notificationDeliveryKey(delivery.BuildID, delivery.EventType, delivery.Recipient)
	if key == "||" {
		return domain.NotificationDelivery{}, repository.ErrNotificationDeliveryDuplicate
	}
	if _, exists := r.index[key]; exists {
		return domain.NotificationDelivery{}, repository.ErrNotificationDeliveryDuplicate
	}

	now := time.Now().UTC()
	if strings.TrimSpace(delivery.ID) == "" {
		delivery.ID = uuid.NewString()
	}
	delivery.BuildID = strings.TrimSpace(delivery.BuildID)
	delivery.Recipient = strings.TrimSpace(delivery.Recipient)
	if strings.TrimSpace(string(delivery.EventType)) == "" {
		return domain.NotificationDelivery{}, repository.ErrNotificationDeliveryDuplicate
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
	return delivery, nil
}

func (r *NotificationDeliveryRepository) GetByBuildEventRecipient(_ context.Context, buildID string, eventType domain.NotificationEventType, recipient string) (domain.NotificationDelivery, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	key := notificationDeliveryKey(buildID, eventType, recipient)
	id, ok := r.index[key]
	if !ok {
		return domain.NotificationDelivery{}, repository.ErrNotificationDeliveryNotFound
	}

	delivery, ok := r.deliveries[id]
	if !ok {
		return domain.NotificationDelivery{}, repository.ErrNotificationDeliveryNotFound
	}

	return delivery, nil
}

func (r *NotificationDeliveryRepository) Update(_ context.Context, delivery domain.NotificationDelivery) (domain.NotificationDelivery, error) {
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
	delivery.BuildID = strings.TrimSpace(delivery.BuildID)
	delivery.Recipient = strings.TrimSpace(delivery.Recipient)

	r.deliveries[delivery.ID] = delivery
	return delivery, nil
}

func notificationDeliveryKey(buildID string, eventType domain.NotificationEventType, recipient string) string {
	return strings.TrimSpace(buildID) + "|" + strings.TrimSpace(string(eventType)) + "|" + strings.TrimSpace(recipient)
}
