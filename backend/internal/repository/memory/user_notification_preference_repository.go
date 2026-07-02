package memory

import (
	"context"
	"strings"
	"sync"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type UserNotificationPreferenceRepository struct {
	mu          sync.RWMutex
	preferences map[string]domain.UserNotificationPreference
}

func NewUserNotificationPreferenceRepository() *UserNotificationPreferenceRepository {
	return &UserNotificationPreferenceRepository{preferences: map[string]domain.UserNotificationPreference{}}
}

func (r *UserNotificationPreferenceRepository) GetByUserID(_ context.Context, userID string) (domain.UserNotificationPreference, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	preference, ok := r.preferences[strings.TrimSpace(userID)]
	if !ok {
		return domain.UserNotificationPreference{}, repository.ErrUserNotificationPreferenceNotFound
	}
	return preference, nil
}

func (r *UserNotificationPreferenceRepository) InitializeIfAbsent(_ context.Context, preference domain.UserNotificationPreference) (domain.UserNotificationPreference, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	preference.UserID = strings.TrimSpace(preference.UserID)
	preference.CommitAuthorSuccessEmailSource = clonePreferenceSource(preference.CommitAuthorSuccessEmailSource)
	if existing, ok := r.preferences[preference.UserID]; ok {
		return existing, false, nil
	}
	r.preferences[preference.UserID] = preference
	return preference, true, nil
}

func (r *UserNotificationPreferenceRepository) Upsert(_ context.Context, preference domain.UserNotificationPreference) (domain.UserNotificationPreference, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	preference.UserID = strings.TrimSpace(preference.UserID)
	preference.CommitAuthorSuccessEmailSource = clonePreferenceSource(preference.CommitAuthorSuccessEmailSource)
	r.preferences[preference.UserID] = preference
	return preference, nil
}

func clonePreferenceSource(source *domain.UserNotificationPreferenceSource) *domain.UserNotificationPreferenceSource {
	if source == nil {
		return nil
	}
	copy := *source
	return &copy
}
