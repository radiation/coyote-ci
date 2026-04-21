package memory

import (
	"context"
	"sync"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type JobManagedImageConfigRepository struct {
	mu      sync.RWMutex
	configs map[string]domain.JobManagedImageConfig
}

func NewJobManagedImageConfigRepository() *JobManagedImageConfigRepository {
	return &JobManagedImageConfigRepository{configs: map[string]domain.JobManagedImageConfig{}}
}

func (r *JobManagedImageConfigRepository) Create(_ context.Context, config domain.JobManagedImageConfig) (domain.JobManagedImageConfig, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.configs[config.JobID] = config
	return config, nil
}

func (r *JobManagedImageConfigRepository) GetByJobID(_ context.Context, jobID string) (domain.JobManagedImageConfig, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	config, ok := r.configs[jobID]
	if !ok {
		return domain.JobManagedImageConfig{}, repository.ErrJobManagedImageConfigNotFound
	}
	return config, nil
}

func (r *JobManagedImageConfigRepository) UpsertByJobID(_ context.Context, config domain.JobManagedImageConfig) (domain.JobManagedImageConfig, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.configs[config.JobID] = config
	return config, nil
}

func (r *JobManagedImageConfigRepository) DeleteByJobID(_ context.Context, jobID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.configs[jobID]; !ok {
		return repository.ErrJobManagedImageConfigNotFound
	}
	delete(r.configs, jobID)
	return nil
}
