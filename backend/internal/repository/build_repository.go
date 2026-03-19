package repository

import (
	"context"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

type BuildRepository interface {
	Create(ctx context.Context, build domain.Build) (domain.Build, error)
	GetByID(ctx context.Context, id string) (domain.Build, error)
}
