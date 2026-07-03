package repository

import (
	"context"
	"errors"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

var ErrArtifactNotFound = errors.New("artifact not found")
var ErrArtifactConflict = errors.New("artifact already exists")

type BrowseArtifactsParams struct {
	Query     string
	Type      domain.ArtifactType
	ProjectID string
	JobID     string
	Limit     int
	Offset    int
}

type ArtifactCatalogParams struct {
	Query     string
	ProjectID string
	JobID     string
	BuildID   string
	Limit     int
	Offset    int
}

type ArtifactBrowseRepository interface {
	// Browse paginates logical artifact identities rather than raw artifact
	// instance rows so each returned artifact includes its complete grouped
	// history for the selected page.
	Browse(ctx context.Context, params BrowseArtifactsParams) ([]domain.ArtifactRecord, error)
}

// ArtifactCatalogRepository lists persisted artifact instances with build and
// optional step context for repository catalog and detail views.
type ArtifactCatalogRepository interface {
	ListCatalog(ctx context.Context, params ArtifactCatalogParams) ([]domain.ArtifactRecord, error)
	GetCatalogByID(ctx context.Context, artifactID string) (domain.ArtifactRecord, error)
}

type ArtifactBuildListRepository interface {
	ListByBuildID(ctx context.Context, buildID string) ([]domain.BuildArtifact, error)
}

// ArtifactRepository persists and queries build artifact metadata.
type ArtifactRepository interface {
	ArtifactBrowseRepository
	ArtifactCatalogRepository
	ArtifactBuildListRepository
	Create(ctx context.Context, artifact domain.BuildArtifact) (domain.BuildArtifact, error)
	GetByID(ctx context.Context, buildID string, artifactID string) (domain.BuildArtifact, error)
	ListByStepID(ctx context.Context, stepID string) ([]domain.BuildArtifact, error)
}
