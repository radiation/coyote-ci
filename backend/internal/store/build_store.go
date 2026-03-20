package store

import (
	"context"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

type BuildStore interface {
	Create(ctx context.Context, build domain.Build) (domain.Build, error)
	List(ctx context.Context) ([]domain.Build, error)
	GetByID(ctx context.Context, id string) (domain.Build, error)
	UpdateStatus(ctx context.Context, id string, status domain.BuildStatus) (domain.Build, error)
}
