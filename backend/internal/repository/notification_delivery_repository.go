package repository

import (
	"context"
	"errors"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

var ErrNotificationDeliveryNotFound = errors.New("notification delivery not found")
var ErrNotificationDeliveryDuplicate = errors.New("notification delivery already exists")

type NotificationDeliveryAcquireOutcome string

const (
	NotificationDeliveryAcquireOutcomeCreated NotificationDeliveryAcquireOutcome = "created"
	NotificationDeliveryAcquireOutcomeSent    NotificationDeliveryAcquireOutcome = "sent"
	NotificationDeliveryAcquireOutcomePending NotificationDeliveryAcquireOutcome = "pending"
	NotificationDeliveryAcquireOutcomeFailed  NotificationDeliveryAcquireOutcome = "failed"
)

type NotificationDeliveryAcquireResult struct {
	Delivery domain.NotificationDelivery
	Outcome  NotificationDeliveryAcquireOutcome
}

type NotificationDeliveryRepository interface {
	Acquire(ctx context.Context, delivery domain.NotificationDelivery) (NotificationDeliveryAcquireResult, error)
	GetByBuildEventRecipient(ctx context.Context, buildID string, eventType domain.NotificationEventType, recipient string) (domain.NotificationDelivery, error)
	Update(ctx context.Context, delivery domain.NotificationDelivery) (domain.NotificationDelivery, error)
}

func NotificationDeliveryAcquireOutcomeFromStatus(status domain.NotificationDeliveryStatus) NotificationDeliveryAcquireOutcome {
	switch status {
	case domain.NotificationDeliveryStatusSent:
		return NotificationDeliveryAcquireOutcomeSent
	case domain.NotificationDeliveryStatusFailed:
		return NotificationDeliveryAcquireOutcomeFailed
	default:
		return NotificationDeliveryAcquireOutcomePending
	}
}
