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

type WebhookDeliveryRepository struct {
	mu         sync.RWMutex
	deliveries map[string]domain.WebhookDelivery
	index      map[string]string
}

func NewWebhookDeliveryRepository() *WebhookDeliveryRepository {
	return &WebhookDeliveryRepository{
		deliveries: make(map[string]domain.WebhookDelivery),
		index:      make(map[string]string),
	}
}

func (r *WebhookDeliveryRepository) Create(_ context.Context, delivery domain.WebhookDelivery) (domain.WebhookDelivery, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delivery.Provider = strings.ToLower(strings.TrimSpace(delivery.Provider))
	delivery.DeliveryID = strings.TrimSpace(delivery.DeliveryID)
	key := deliveryLedgerKey(delivery.Provider, delivery.DeliveryID)
	if key == "|" {
		return domain.WebhookDelivery{}, repository.ErrWebhookDeliveryDuplicate
	}
	if _, exists := r.index[key]; exists {
		return domain.WebhookDelivery{}, repository.ErrWebhookDeliveryDuplicate
	}

	now := time.Now().UTC()
	if strings.TrimSpace(delivery.ID) == "" {
		delivery.ID = uuid.NewString()
	}
	if delivery.ReceivedAt.IsZero() {
		delivery.ReceivedAt = now
	}
	if delivery.UpdatedAt.IsZero() {
		delivery.UpdatedAt = delivery.ReceivedAt
	}
	if strings.TrimSpace(string(delivery.Status)) == "" {
		delivery.Status = domain.WebhookDeliveryStatusReceived
	}

	r.deliveries[delivery.ID] = delivery
	r.index[key] = delivery.ID
	return delivery, nil
}

func (r *WebhookDeliveryRepository) GetByProviderDeliveryID(_ context.Context, provider string, deliveryID string) (domain.WebhookDelivery, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	key := deliveryLedgerKey(provider, deliveryID)
	id, ok := r.index[key]
	if !ok {
		return domain.WebhookDelivery{}, repository.ErrWebhookDeliveryNotFound
	}

	delivery, ok := r.deliveries[id]
	if !ok {
		return domain.WebhookDelivery{}, repository.ErrWebhookDeliveryNotFound
	}

	return delivery, nil
}

func (r *WebhookDeliveryRepository) Update(_ context.Context, delivery domain.WebhookDelivery) (domain.WebhookDelivery, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	current, ok := r.deliveries[delivery.ID]
	if !ok {
		return domain.WebhookDelivery{}, repository.ErrWebhookDeliveryNotFound
	}

	if delivery.ReceivedAt.IsZero() {
		delivery.ReceivedAt = current.ReceivedAt
	}
	if delivery.UpdatedAt.IsZero() {
		delivery.UpdatedAt = time.Now().UTC()
	}
	delivery.Provider = strings.ToLower(strings.TrimSpace(delivery.Provider))
	delivery.DeliveryID = strings.TrimSpace(delivery.DeliveryID)

	r.deliveries[delivery.ID] = delivery
	return delivery, nil
}

func deliveryLedgerKey(provider string, deliveryID string) string {
	return strings.ToLower(strings.TrimSpace(provider)) + "|" + strings.TrimSpace(deliveryID)
}
