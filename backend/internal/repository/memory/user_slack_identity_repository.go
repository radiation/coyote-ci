package memory

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type UserSlackIdentityRepository struct {
	mu         sync.RWMutex
	identities map[string]domain.UserSlackIdentity
	byUserID   map[string]string
	bySlackKey map[string]string
}

func NewUserSlackIdentityRepository() *UserSlackIdentityRepository {
	return &UserSlackIdentityRepository{
		identities: make(map[string]domain.UserSlackIdentity),
		byUserID:   make(map[string]string),
		bySlackKey: make(map[string]string),
	}
}

func (r *UserSlackIdentityRepository) GetByUserID(_ context.Context, userID string) (domain.UserSlackIdentity, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	identityID, ok := r.byUserID[strings.TrimSpace(userID)]
	if !ok {
		return domain.UserSlackIdentity{}, repository.ErrUserSlackIdentityNotFound
	}
	identity, ok := r.identities[identityID]
	if !ok {
		return domain.UserSlackIdentity{}, repository.ErrUserSlackIdentityNotFound
	}
	return cloneUserSlackIdentity(identity), nil
}

func (r *UserSlackIdentityRepository) Upsert(_ context.Context, identity domain.UserSlackIdentity) (domain.UserSlackIdentity, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	trimmedUserID := strings.TrimSpace(identity.UserID)
	trimmedWorkspaceID := strings.TrimSpace(identity.SlackWorkspaceIntegrationID)
	trimmedSlackUserID := strings.TrimSpace(identity.SlackUserID)
	newSlackKey := userSlackIdentityKey(trimmedWorkspaceID, trimmedSlackUserID)

	if existingID, ok := r.bySlackKey[newSlackKey]; ok {
		existingIdentity := r.identities[existingID]
		if strings.TrimSpace(existingIdentity.UserID) != trimmedUserID {
			return domain.UserSlackIdentity{}, repository.ErrUserSlackIdentityConflict
		}
	}

	if existingID, ok := r.byUserID[trimmedUserID]; ok {
		stored := r.identities[existingID]
		oldSlackKey := userSlackIdentityKey(strings.TrimSpace(stored.SlackWorkspaceIntegrationID), strings.TrimSpace(stored.SlackUserID))
		if oldSlackKey != newSlackKey {
			delete(r.bySlackKey, oldSlackKey)
			if conflictingID, ok := r.bySlackKey[newSlackKey]; ok && conflictingID != existingID {
				return domain.UserSlackIdentity{}, repository.ErrUserSlackIdentityConflict
			}
			stored.LinkedAt = identity.LinkedAt
		}
		stored.SlackWorkspaceIntegrationID = trimmedWorkspaceID
		stored.SlackUserID = trimmedSlackUserID
		stored.SlackDisplayName = cloneOptionalString(identity.SlackDisplayName)
		stored.SlackRealName = cloneOptionalString(identity.SlackRealName)
		stored.SlackHandle = cloneOptionalString(identity.SlackHandle)
		stored.SlackEmail = cloneOptionalString(identity.SlackEmail)
		stored.ProfileImageURL = cloneOptionalString(identity.ProfileImageURL)
		stored.Enabled = identity.Enabled
		stored.LastVerifiedAt = cloneOptionalTime(identity.LastVerifiedAt)
		stored.UpdatedAt = identity.UpdatedAt
		r.identities[existingID] = stored
		r.bySlackKey[newSlackKey] = existingID
		return cloneUserSlackIdentity(stored), nil
	}

	stored := cloneUserSlackIdentity(identity)
	stored.UserID = trimmedUserID
	stored.SlackWorkspaceIntegrationID = trimmedWorkspaceID
	stored.SlackUserID = trimmedSlackUserID
	r.identities[stored.ID] = stored
	r.byUserID[trimmedUserID] = stored.ID
	r.bySlackKey[newSlackKey] = stored.ID
	return cloneUserSlackIdentity(stored), nil
}

func (r *UserSlackIdentityRepository) SetEnabled(_ context.Context, userID string, enabled bool, updatedAt time.Time) (domain.UserSlackIdentity, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	identityID, ok := r.byUserID[strings.TrimSpace(userID)]
	if !ok {
		return domain.UserSlackIdentity{}, repository.ErrUserSlackIdentityNotFound
	}
	identity := r.identities[identityID]
	identity.Enabled = enabled
	identity.UpdatedAt = updatedAt
	r.identities[identityID] = identity
	return cloneUserSlackIdentity(identity), nil
}

func (r *UserSlackIdentityRepository) DeleteByUserID(_ context.Context, userID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	trimmedUserID := strings.TrimSpace(userID)
	identityID, ok := r.byUserID[trimmedUserID]
	if !ok {
		return repository.ErrUserSlackIdentityNotFound
	}
	identity := r.identities[identityID]
	delete(r.byUserID, trimmedUserID)
	delete(r.bySlackKey, userSlackIdentityKey(strings.TrimSpace(identity.SlackWorkspaceIntegrationID), strings.TrimSpace(identity.SlackUserID)))
	delete(r.identities, identityID)
	return nil
}

func (r *UserSlackIdentityRepository) CountByWorkspaceIntegrationID(ctx context.Context, workspaceIntegrationID string) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	trimmedWorkspaceID := strings.TrimSpace(workspaceIntegrationID)
	count := 0
	for _, identity := range r.identities {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		if strings.TrimSpace(identity.SlackWorkspaceIntegrationID) == trimmedWorkspaceID {
			count++
		}
	}
	return count, nil
}

func cloneUserSlackIdentity(identity domain.UserSlackIdentity) domain.UserSlackIdentity {
	identity.SlackDisplayName = cloneOptionalString(identity.SlackDisplayName)
	identity.SlackRealName = cloneOptionalString(identity.SlackRealName)
	identity.SlackHandle = cloneOptionalString(identity.SlackHandle)
	identity.SlackEmail = cloneOptionalString(identity.SlackEmail)
	identity.ProfileImageURL = cloneOptionalString(identity.ProfileImageURL)
	identity.LastVerifiedAt = cloneOptionalTime(identity.LastVerifiedAt)
	return identity
}

func cloneOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	return &trimmed
}

func cloneOptionalTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	v := *value
	return &v
}

func userSlackIdentityKey(workspaceIntegrationID string, slackUserID string) string {
	return strings.TrimSpace(workspaceIntegrationID) + ":" + strings.TrimSpace(slackUserID)
}
