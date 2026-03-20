package memory

import (
	"context"
	"sync"

	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type BuildRepository struct {
	mu     sync.RWMutex
	builds map[string]domain.Build
}

func NewBuildRepository() *BuildRepository {
	return &BuildRepository{
		builds: make(map[string]domain.Build),
	}
}

func (r *BuildRepository) Create(_ context.Context, build domain.Build) (domain.Build, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if build.ID == "" {
		build.ID = uuid.NewString()
	}

	r.builds[build.ID] = build
	return build, nil
}

func (r *BuildRepository) GetByID(_ context.Context, id string) (domain.Build, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	build, ok := r.builds[id]
	if !ok {
		return domain.Build{}, repository.ErrBuildNotFound
	}

	return build, nil
}

func (r *BuildRepository) UpdateStatus(_ context.Context, id string, status domain.BuildStatus) (domain.Build, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	build, ok := r.builds[id]
	if !ok {
		return domain.Build{}, repository.ErrBuildNotFound
	}

	build.Status = status
	r.builds[id] = build

	return build, nil
}
