package memory

import (
	"context"
	"errors"
	"sync"

	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/internal/domain"
)

var ErrBuildNotFound = errors.New("build not found")

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
		return domain.Build{}, ErrBuildNotFound
	}

	return build, nil
}
