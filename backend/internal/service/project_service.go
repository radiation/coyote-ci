package service

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

var ErrProjectIDRequired = errors.New("project id is required")
var ErrProjectNameRequired = errors.New("project name is required")
var ErrProjectSlugRequired = errors.New("project slug is required")
var ErrProjectSlugInvalid = errors.New("project slug must be lowercase kebab-case")
var ErrDefaultProjectDeleteForbidden = errors.New("default project cannot be deleted")

var projectSlugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

type ProjectService struct {
	projects repository.ProjectRepository
	now      func() time.Time
}

func NewProjectService(projects repository.ProjectRepository) *ProjectService {
	return &ProjectService{projects: projects, now: time.Now}
}

type CreateProjectInput struct {
	Name        string
	Slug        string
	Description *string
	IsPublic    bool
}

type UpdateProjectInput struct {
	Name        *string
	Slug        *string
	Description OptionalStringPatch
	IsPublic    *bool
}

func (s *ProjectService) CreateProject(ctx context.Context, input CreateProjectInput) (domain.Project, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return domain.Project{}, ErrProjectNameRequired
	}
	slug := normalizeProjectSlug(input.Slug)
	if slug == "" {
		return domain.Project{}, ErrProjectSlugRequired
	}
	if !projectSlugPattern.MatchString(slug) {
		return domain.Project{}, ErrProjectSlugInvalid
	}

	now := s.now().UTC()
	return s.projects.Create(ctx, domain.Project{
		ID:          uuid.NewString(),
		Name:        name,
		Slug:        slug,
		Description: normalizeStringPtr(input.Description),
		IsPublic:    input.IsPublic,
		CreatedAt:   now,
		UpdatedAt:   now,
	})
}

func (s *ProjectService) GetProject(ctx context.Context, id string) (domain.Project, error) {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return domain.Project{}, ErrProjectIDRequired
	}
	return s.projects.GetByID(ctx, trimmed)
}

func (s *ProjectService) GetProjectsByIDs(ctx context.Context, ids []string) ([]domain.Project, error) {
	return s.projects.GetByIDs(ctx, ids)
}

func (s *ProjectService) GetProjectBySlug(ctx context.Context, slug string) (domain.Project, error) {
	trimmed := normalizeProjectSlug(slug)
	if trimmed == "" {
		return domain.Project{}, ErrProjectSlugRequired
	}
	return s.projects.GetBySlug(ctx, trimmed)
}

func (s *ProjectService) ListProjects(ctx context.Context) ([]domain.Project, error) {
	return s.projects.List(ctx)
}

func (s *ProjectService) UpdateProject(ctx context.Context, id string, input UpdateProjectInput) (domain.Project, error) {
	trimmedID := strings.TrimSpace(id)
	if trimmedID == "" {
		return domain.Project{}, ErrProjectIDRequired
	}

	current, err := s.projects.GetByID(ctx, trimmedID)
	if err != nil {
		return domain.Project{}, err
	}

	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" {
			return domain.Project{}, ErrProjectNameRequired
		}
		current.Name = name
	}
	if input.Slug != nil {
		slug := normalizeProjectSlug(*input.Slug)
		if slug == "" {
			return domain.Project{}, ErrProjectSlugRequired
		}
		if !projectSlugPattern.MatchString(slug) {
			return domain.Project{}, ErrProjectSlugInvalid
		}
		current.Slug = slug
	}
	if input.Description.Set {
		current.Description = normalizeStringPtr(input.Description.Value)
	}
	if input.IsPublic != nil {
		current.IsPublic = *input.IsPublic
	}

	current.UpdatedAt = s.now().UTC()
	return s.projects.Update(ctx, current)
}

func (s *ProjectService) DeleteProject(ctx context.Context, id string) error {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return ErrProjectIDRequired
	}
	project, err := s.projects.GetByID(ctx, trimmed)
	if err != nil {
		return err
	}
	if project.Slug == domain.DefaultProjectSlug {
		return ErrDefaultProjectDeleteForbidden
	}
	return s.projects.Delete(ctx, trimmed)
}

func normalizeProjectSlug(value string) string {
	return strings.TrimSpace(strings.ToLower(value))
}
