package repository

import (
	"context"
	"errors"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

var ErrNotificationInstanceSettingsNotFound = errors.New("notification instance settings not found")

type NotificationInstanceSettingsRepository interface {
	Get(ctx context.Context) (domain.NotificationInstanceSettings, error)
	Upsert(ctx context.Context, settings domain.NotificationInstanceSettings) (domain.NotificationInstanceSettings, error)
}
