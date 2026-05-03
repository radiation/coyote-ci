package repository

import (
	"context"
	"errors"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

var ErrProjectNotFound = errors.New("project not found")
var ErrProjectSlugConflict = errors.New("project slug already exists")
var ErrProjectHasJobs = errors.New("project has jobs")

type ProjectRepository interface {
	Create(ctx context.Context, project domain.Project) (domain.Project, error)
	GetByID(ctx context.Context, id string) (domain.Project, error)
	GetBySlug(ctx context.Context, slug string) (domain.Project, error)
	List(ctx context.Context) ([]domain.Project, error)
	Update(ctx context.Context, project domain.Project) (domain.Project, error)
	Delete(ctx context.Context, id string) error
}
