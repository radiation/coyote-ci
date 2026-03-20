package repository

import (
	"context"
	"errors"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

var ErrBuildNotFound = errors.New("build not found")

type BuildRepository interface {
	Create(ctx context.Context, build domain.Build) (domain.Build, error)
	GetByID(ctx context.Context, id string) (domain.Build, error)
	UpdateStatus(ctx context.Context, id string, status domain.BuildStatus) (domain.Build, error)
}
