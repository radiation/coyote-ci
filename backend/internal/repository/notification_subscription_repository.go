package repository

import (
	"context"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

type NotificationSubscriptionRepository interface {
	ListEnabledMatchesForBuildEvent(ctx context.Context, build domain.Build, eventType domain.NotificationEventType) ([]domain.NotificationSubscriptionMatch, error)
}
