package repository

import (
	"context"
	"errors"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

var ErrArtifactNotFound = errors.New("artifact not found")

// ArtifactRepository persists and queries build artifact metadata.
type ArtifactRepository interface {
	Create(ctx context.Context, artifact domain.BuildArtifact) (domain.BuildArtifact, error)
	ListByBuildID(ctx context.Context, buildID string) ([]domain.BuildArtifact, error)
	GetByID(ctx context.Context, buildID string, artifactID string) (domain.BuildArtifact, error)
	ListByStepID(ctx context.Context, stepID string) ([]domain.BuildArtifact, error)
}
