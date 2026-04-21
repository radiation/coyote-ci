package memory

import (
	"context"
	"sync"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type RepoWritebackConfigRepository struct {
	mu      sync.RWMutex
	configs map[string]domain.RepoWritebackConfig
}

func NewRepoWritebackConfigRepository() *RepoWritebackConfigRepository {
	return &RepoWritebackConfigRepository{configs: map[string]domain.RepoWritebackConfig{}}
}

func (r *RepoWritebackConfigRepository) Create(_ context.Context, cfg domain.RepoWritebackConfig) (domain.RepoWritebackConfig, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.configs[cfg.ID] = cfg
	return cfg, nil
}

func (r *RepoWritebackConfigRepository) ListByProjectID(_ context.Context, projectID string) ([]domain.RepoWritebackConfig, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]domain.RepoWritebackConfig, 0)
	for _, cfg := range r.configs {
		if cfg.ProjectID == projectID {
			result = append(result, cfg)
		}
	}
	return result, nil
}

func (r *RepoWritebackConfigRepository) GetByID(_ context.Context, id string) (domain.RepoWritebackConfig, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cfg, ok := r.configs[id]
	if !ok {
		return domain.RepoWritebackConfig{}, repository.ErrRepoWritebackConfigNotFound
	}
	return cfg, nil
}

func (r *RepoWritebackConfigRepository) GetByProjectAndRepo(_ context.Context, projectID string, repositoryURL string) (domain.RepoWritebackConfig, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, cfg := range r.configs {
		if cfg.ProjectID == projectID && cfg.RepositoryURL == repositoryURL {
			return cfg, nil
		}
	}
	return domain.RepoWritebackConfig{}, repository.ErrRepoWritebackConfigNotFound
}

func (r *RepoWritebackConfigRepository) Update(_ context.Context, cfg domain.RepoWritebackConfig) (domain.RepoWritebackConfig, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.configs[cfg.ID]; !ok {
		return domain.RepoWritebackConfig{}, repository.ErrRepoWritebackConfigNotFound
	}
	r.configs[cfg.ID] = cfg
	return cfg, nil
}

func (r *RepoWritebackConfigRepository) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.configs[id]; !ok {
		return repository.ErrRepoWritebackConfigNotFound
	}
	delete(r.configs, id)
	return nil
}
