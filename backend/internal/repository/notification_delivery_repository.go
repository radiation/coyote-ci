package repository

import (
	"context"
	"errors"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

var ErrNotificationDeliveryNotFound = errors.New("notification delivery not found")
var ErrNotificationDeliveryDuplicate = errors.New("notification delivery already exists")

type NotificationDeliveryRepository interface {
	Create(ctx context.Context, delivery domain.NotificationDelivery) (domain.NotificationDelivery, error)
	GetByBuildEventRecipient(ctx context.Context, buildID string, eventType domain.NotificationEventType, recipient string) (domain.NotificationDelivery, error)
	Update(ctx context.Context, delivery domain.NotificationDelivery) (domain.NotificationDelivery, error)
}
