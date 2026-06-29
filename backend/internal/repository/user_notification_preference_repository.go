package repository

import (
	"context"
	"errors"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

var ErrUserNotificationPreferenceNotFound = errors.New("user notification preference not found")

type UserNotificationPreferenceRepository interface {
	GetByUserID(ctx context.Context, userID string) (domain.UserNotificationPreference, error)
	InitializeIfAbsent(ctx context.Context, preference domain.UserNotificationPreference) (domain.UserNotificationPreference, bool, error)
	Upsert(ctx context.Context, preference domain.UserNotificationPreference) (domain.UserNotificationPreference, error)
}
