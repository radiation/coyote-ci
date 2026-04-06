package repository

import (
	"context"
	"errors"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

var ErrJobNotFound = errors.New("job not found")

type JobRepository interface {
	Create(ctx context.Context, job domain.Job) (domain.Job, error)
	List(ctx context.Context) ([]domain.Job, error)
	ListPaged(ctx context.Context, params ListParams) ([]domain.Job, error)
	ListPushEnabledByRepository(ctx context.Context, repositoryURL string) ([]domain.Job, error)
	GetByID(ctx context.Context, id string) (domain.Job, error)
	Update(ctx context.Context, job domain.Job) (domain.Job, error)
}
