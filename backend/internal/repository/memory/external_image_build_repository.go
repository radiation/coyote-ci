package memory

import (
	"context"
	"sync"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type ExternalImageBuildRepository struct {
	mu     sync.RWMutex
	builds map[string]domain.ExternalImageBuild
	now    func() time.Time
}

func NewExternalImageBuildRepository() *ExternalImageBuildRepository {
	return &ExternalImageBuildRepository{builds: map[string]domain.ExternalImageBuild{}, now: func() time.Time { return time.Now().UTC() }}
}

func (r *ExternalImageBuildRepository) CreateIntent(_ context.Context, build domain.ExternalImageBuild) (domain.ExternalImageBuild, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, found := r.builds[build.ExecutionJobID]; found {
		return cloneExternalImageBuild(existing), nil
	}
	build.SubmissionState = domain.ExternalImageBuildSubmissionIntent
	build.CreatedAt = r.now()
	build.UpdatedAt = build.CreatedAt
	r.builds[build.ExecutionJobID] = cloneExternalImageBuild(build)
	return cloneExternalImageBuild(build), nil
}

func (r *ExternalImageBuildRepository) GetByExecutionJobID(_ context.Context, executionJobID string) (domain.ExternalImageBuild, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	build, found := r.builds[executionJobID]
	if !found {
		return domain.ExternalImageBuild{}, repository.ErrExternalImageBuildNotFound
	}
	return cloneExternalImageBuild(build), nil
}

func (r *ExternalImageBuildRepository) Update(_ context.Context, build domain.ExternalImageBuild) (domain.ExternalImageBuild, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, found := r.builds[build.ExecutionJobID]; !found {
		return domain.ExternalImageBuild{}, repository.ErrExternalImageBuildNotFound
	}
	build.UpdatedAt = r.now()
	r.builds[build.ExecutionJobID] = cloneExternalImageBuild(build)
	return cloneExternalImageBuild(build), nil
}

func cloneExternalImageBuild(build domain.ExternalImageBuild) domain.ExternalImageBuild {
	if build.SubmittedAt != nil {
		value := *build.SubmittedAt
		build.SubmittedAt = &value
	}
	return build
}

var _ repository.ExternalImageBuildRepository = (*ExternalImageBuildRepository)(nil)
