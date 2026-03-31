package memory

import (
	"context"
	"sort"
	"sync"

	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type JobRepository struct {
	mu   sync.RWMutex
	jobs map[string]domain.Job
}

func NewJobRepository() *JobRepository {
	return &JobRepository{jobs: map[string]domain.Job{}}
}

func (r *JobRepository) Create(_ context.Context, job domain.Job) (domain.Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if job.ID == "" {
		job.ID = uuid.NewString()
	}
	r.jobs[job.ID] = job
	return job, nil
}

func (r *JobRepository) List(_ context.Context) ([]domain.Job, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]domain.Job, 0, len(r.jobs))
	for _, job := range r.jobs {
		out = append(out, job)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})

	return out, nil
}

func (r *JobRepository) GetByID(_ context.Context, id string) (domain.Job, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	job, ok := r.jobs[id]
	if !ok {
		return domain.Job{}, repository.ErrJobNotFound
	}
	return job, nil
}

func (r *JobRepository) Update(_ context.Context, job domain.Job) (domain.Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.jobs[job.ID]; !ok {
		return domain.Job{}, repository.ErrJobNotFound
	}
	r.jobs[job.ID] = job
	return job, nil
}
