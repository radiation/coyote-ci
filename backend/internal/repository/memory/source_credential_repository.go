package memory

import (
	"context"
	"sync"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type SourceCredentialRepository struct {
	mu          sync.RWMutex
	credentials map[string]domain.SourceCredential
}

func NewSourceCredentialRepository() *SourceCredentialRepository {
	return &SourceCredentialRepository{credentials: map[string]domain.SourceCredential{}}
}

func (r *SourceCredentialRepository) Create(_ context.Context, credential domain.SourceCredential) (domain.SourceCredential, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.credentials[credential.ID] = credential
	return credential, nil
}

func (r *SourceCredentialRepository) ListByProjectID(_ context.Context, projectID string) ([]domain.SourceCredential, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]domain.SourceCredential, 0)
	for _, credential := range r.credentials {
		if credential.ProjectID == projectID {
			result = append(result, credential)
		}
	}
	return result, nil
}

func (r *SourceCredentialRepository) GetByID(_ context.Context, id string) (domain.SourceCredential, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	credential, ok := r.credentials[id]
	if !ok {
		return domain.SourceCredential{}, repository.ErrSourceCredentialNotFound
	}
	return credential, nil
}

func (r *SourceCredentialRepository) Update(_ context.Context, credential domain.SourceCredential) (domain.SourceCredential, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.credentials[credential.ID]; !ok {
		return domain.SourceCredential{}, repository.ErrSourceCredentialNotFound
	}
	r.credentials[credential.ID] = credential
	return credential, nil
}

func (r *SourceCredentialRepository) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.credentials[id]; !ok {
		return repository.ErrSourceCredentialNotFound
	}
	delete(r.credentials, id)
	return nil
}
