package memory

import (
	"context"
	"sync"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type NotificationInstanceSettingsRepository struct {
	mu       sync.RWMutex
	settings *domain.NotificationInstanceSettings
}

func NewNotificationInstanceSettingsRepository() *NotificationInstanceSettingsRepository {
	return &NotificationInstanceSettingsRepository{}
}

func (r *NotificationInstanceSettingsRepository) Get(_ context.Context) (domain.NotificationInstanceSettings, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.settings == nil {
		return domain.NotificationInstanceSettings{}, repository.ErrNotificationInstanceSettingsNotFound
	}
	return *r.settings, nil
}

func (r *NotificationInstanceSettingsRepository) Upsert(_ context.Context, settings domain.NotificationInstanceSettings) (domain.NotificationInstanceSettings, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	copy := settings
	r.settings = &copy
	return copy, nil
}
