package repository

import (
	"context"
	"errors"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

var ErrArtifactNotFound = errors.New("artifact not found")
var ErrArtifactConflict = errors.New("artifact already exists")

type BrowseArtifactsParams struct {
	Query  string
	Limit  int
	Offset int
}

type ArtifactBrowseRepository interface {
	Browse(ctx context.Context, params BrowseArtifactsParams) ([]domain.ArtifactRecord, error)
}

// ArtifactRepository persists and queries build artifact metadata.
type ArtifactRepository interface {
	ArtifactBrowseRepository
	Create(ctx context.Context, artifact domain.BuildArtifact) (domain.BuildArtifact, error)
	ListByBuildID(ctx context.Context, buildID string) ([]domain.BuildArtifact, error)
	GetByID(ctx context.Context, buildID string, artifactID string) (domain.BuildArtifact, error)
	ListByStepID(ctx context.Context, stepID string) ([]domain.BuildArtifact, error)
}
