package repository

import (
	"context"
	"errors"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

var ErrExternalImageBuildNotFound = errors.New("external image build not found")

// ExternalImageBuildRepository makes provider submission intent durable before
// an external side effect is attempted.
type ExternalImageBuildRepository interface {
	CreateIntent(ctx context.Context, build domain.ExternalImageBuild) (domain.ExternalImageBuild, error)
	GetByExecutionJobID(ctx context.Context, executionJobID string) (domain.ExternalImageBuild, error)
	Update(ctx context.Context, build domain.ExternalImageBuild) (domain.ExternalImageBuild, error)
}
