package repository

import (
	"context"
	"errors"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

var ErrNotificationTargetDuplicate = errors.New("notification target already exists")
var ErrNotificationSubscriptionDuplicate = errors.New("notification subscription already exists")
var ErrNotificationTargetNotFound = errors.New("notification target not found")
var ErrNotificationSubscriptionNotFound = errors.New("notification subscription not found")
var ErrNotificationTargetOwnershipConflict = errors.New("notification target ownership conflict")

type EnsureOwnedNotificationEmailTargetInput struct {
	ID          string
	OwnerUserID string
	Name        string
	Recipient   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type NotificationSubscriptionListFilter struct {
	ProjectID *string
	JobID     *string
}

type NotificationSubscriptionRepository interface {
	CreateTarget(ctx context.Context, target domain.NotificationTarget) (domain.NotificationTarget, error)
	ListTargets(ctx context.Context) ([]domain.NotificationTarget, error)
	GetTargetByID(ctx context.Context, id string) (domain.NotificationTarget, error)
	GetOwnedEmailTargetByUserID(ctx context.Context, userID string) (domain.NotificationTarget, error)
	EnsureOwnedEmailTarget(ctx context.Context, input EnsureOwnedNotificationEmailTargetInput) (domain.NotificationTarget, error)
	EnsureOwnedEmailTargetInitialized(ctx context.Context, input EnsureOwnedNotificationEmailTargetInput) (domain.NotificationTarget, error)
	UpdateTarget(ctx context.Context, target domain.NotificationTarget) (domain.NotificationTarget, error)
	DeleteTarget(ctx context.Context, id string) error
	CreateSubscription(ctx context.Context, subscription domain.NotificationSubscription) (domain.NotificationSubscription, error)
	ListSubscriptions(ctx context.Context, filter NotificationSubscriptionListFilter) ([]domain.NotificationSubscription, error)
	GetSubscriptionByID(ctx context.Context, id string) (domain.NotificationSubscription, error)
	UpdateSubscription(ctx context.Context, subscription domain.NotificationSubscription) (domain.NotificationSubscription, error)
	DeleteSubscription(ctx context.Context, id string) error
	ListEnabledMatchesForBuildEvent(ctx context.Context, build domain.Build, eventType domain.NotificationEventType) ([]domain.NotificationSubscriptionMatch, error)
}
