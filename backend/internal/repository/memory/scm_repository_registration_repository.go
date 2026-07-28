package memory

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type SCMRepositoryRegistrationRepository struct {
	mu             sync.RWMutex
	repositories   map[string]domain.SCMRepositoryRegistration
	identityLookup map[string]string
}

func NewSCMRepositoryRegistrationRepository() *SCMRepositoryRegistrationRepository {
	return &SCMRepositoryRegistrationRepository{
		repositories:   map[string]domain.SCMRepositoryRegistration{},
		identityLookup: map[string]string{},
	}
}

func (r *SCMRepositoryRegistrationRepository) Create(_ context.Context, registration domain.SCMRepositoryRegistration) (domain.SCMRepositoryRegistration, error) {
	registration = registration.Normalize()
	if err := registration.Validate(); err != nil {
		return domain.SCMRepositoryRegistration{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	identityKey := scmRegisteredRepositoryIdentityKey(registration.ConnectionID, registration.ProviderRepositoryID)
	if _, exists := r.identityLookup[identityKey]; exists {
		return domain.SCMRepositoryRegistration{}, repository.ErrSCMRepositoryRegistrationDuplicate
	}
	r.repositories[registration.ID] = registration
	r.identityLookup[identityKey] = registration.ID
	return registration, nil
}

func (r *SCMRepositoryRegistrationRepository) List(_ context.Context) ([]domain.SCMRepositoryRegistration, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]domain.SCMRepositoryRegistration, 0, len(r.repositories))
	for _, item := range r.repositories {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	return items, nil
}

func (r *SCMRepositoryRegistrationRepository) GetByIDs(_ context.Context, ids []string) ([]domain.SCMRepositoryRegistration, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	seen := make(map[string]struct{}, len(ids))
	items := make([]domain.SCMRepositoryRegistration, 0, len(ids))
	for _, id := range ids {
		trimmedID := strings.TrimSpace(id)
		if trimmedID == "" {
			continue
		}
		if _, ok := seen[trimmedID]; ok {
			continue
		}
		seen[trimmedID] = struct{}{}
		registration, ok := r.repositories[trimmedID]
		if !ok {
			continue
		}
		items = append(items, registration)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	return items, nil
}

func (r *SCMRepositoryRegistrationRepository) GetByID(_ context.Context, id string) (domain.SCMRepositoryRegistration, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	registration, ok := r.repositories[id]
	if !ok {
		return domain.SCMRepositoryRegistration{}, repository.ErrSCMRepositoryRegistrationNotFound
	}
	return registration, nil
}

func (r *SCMRepositoryRegistrationRepository) GetByConnectionIDAndProviderRepositoryID(_ context.Context, connectionID string, providerRepositoryID string) (domain.SCMRepositoryRegistration, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	registrationID, ok := r.identityLookup[scmRegisteredRepositoryIdentityKey(strings.TrimSpace(connectionID), strings.TrimSpace(providerRepositoryID))]
	if !ok {
		return domain.SCMRepositoryRegistration{}, repository.ErrSCMRepositoryRegistrationNotFound
	}
	return r.repositories[registrationID], nil
}

func (r *SCMRepositoryRegistrationRepository) Update(_ context.Context, registration domain.SCMRepositoryRegistration) (domain.SCMRepositoryRegistration, error) {
	registration = registration.Normalize()
	if err := registration.Validate(); err != nil {
		return domain.SCMRepositoryRegistration{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.repositories[registration.ID]
	if !ok {
		return domain.SCMRepositoryRegistration{}, repository.ErrSCMRepositoryRegistrationNotFound
	}
	oldIdentityKey := scmRegisteredRepositoryIdentityKey(current.ConnectionID, current.ProviderRepositoryID)
	newIdentityKey := scmRegisteredRepositoryIdentityKey(registration.ConnectionID, registration.ProviderRepositoryID)
	if ownerID, exists := r.identityLookup[newIdentityKey]; exists && ownerID != registration.ID {
		return domain.SCMRepositoryRegistration{}, repository.ErrSCMRepositoryRegistrationDuplicate
	}
	delete(r.identityLookup, oldIdentityKey)
	r.identityLookup[newIdentityKey] = registration.ID
	r.repositories[registration.ID] = registration
	return registration, nil
}

func scmRegisteredRepositoryIdentityKey(connectionID string, providerRepositoryID string) string {
	return connectionID + "|" + providerRepositoryID
}
