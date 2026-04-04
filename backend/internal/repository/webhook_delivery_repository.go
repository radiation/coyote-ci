package repository

import (
	"context"
	"errors"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

var ErrWebhookDeliveryNotFound = errors.New("webhook delivery not found")
var ErrWebhookDeliveryDuplicate = errors.New("webhook delivery already exists")

type WebhookDeliveryRepository interface {
	Create(ctx context.Context, delivery domain.WebhookDelivery) (domain.WebhookDelivery, error)
	GetByProviderDeliveryID(ctx context.Context, provider string, deliveryID string) (domain.WebhookDelivery, error)
	Update(ctx context.Context, delivery domain.WebhookDelivery) (domain.WebhookDelivery, error)
}
