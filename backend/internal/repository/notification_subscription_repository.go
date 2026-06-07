package repository

import (
	"context"
	"errors"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

var ErrNotificationTargetDuplicate = errors.New("notification target already exists")
var ErrNotificationSubscriptionDuplicate = errors.New("notification subscription already exists")

type NotificationSubscriptionRepository interface {
	ListEnabledMatchesForBuildEvent(ctx context.Context, build domain.Build, eventType domain.NotificationEventType) ([]domain.NotificationSubscriptionMatch, error)
}
