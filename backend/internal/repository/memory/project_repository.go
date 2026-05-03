package memory

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type ProjectRepository struct {
	mu       sync.RWMutex
	projects map[string]domain.Project
	jobRepo  repository.JobRepository
}

func NewProjectRepository(jobRepo repository.JobRepository) *ProjectRepository {
	return &ProjectRepository{
		projects: map[string]domain.Project{},
		jobRepo:  jobRepo,
	}
}

func (r *ProjectRepository) Create(_ context.Context, project domain.Project) (domain.Project, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if project.ID == "" {
		project.ID = uuid.NewString()
	}
	if r.slugExistsLocked(project.Slug, "") {
		return domain.Project{}, repository.ErrProjectSlugConflict
	}
	r.projects[project.ID] = project
	return project, nil
}

func (r *ProjectRepository) GetByID(_ context.Context, id string) (domain.Project, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	project, ok := r.projects[id]
	if !ok {
		return domain.Project{}, repository.ErrProjectNotFound
	}
	return project, nil
}

func (r *ProjectRepository) GetBySlug(_ context.Context, slug string) (domain.Project, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, project := range r.projects {
		if project.Slug == slug {
			return project, nil
		}
	}
	return domain.Project{}, repository.ErrProjectNotFound
}

func (r *ProjectRepository) List(_ context.Context) ([]domain.Project, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]domain.Project, 0, len(r.projects))
	for _, project := range r.projects {
		out = append(out, project)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})

	return out, nil
}

func (r *ProjectRepository) Update(_ context.Context, project domain.Project) (domain.Project, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.projects[project.ID]; !ok {
		return domain.Project{}, repository.ErrProjectNotFound
	}
	if r.slugExistsLocked(project.Slug, project.ID) {
		return domain.Project{}, repository.ErrProjectSlugConflict
	}
	r.projects[project.ID] = project
	return project, nil
}

func (r *ProjectRepository) Delete(ctx context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.projects[id]; !ok {
		return repository.ErrProjectNotFound
	}
	if r.jobRepo != nil {
		jobs, err := r.jobRepo.ListByProjectID(ctx, id)
		if err != nil {
			return err
		}
		if len(jobs) > 0 {
			return repository.ErrProjectHasJobs
		}
	}
	delete(r.projects, id)
	return nil
}

func (r *ProjectRepository) slugExistsLocked(slug string, excludeID string) bool {
	for id, project := range r.projects {
		if id == excludeID {
			continue
		}
		if strings.EqualFold(project.Slug, slug) {
			return true
		}
	}
	return false
}
