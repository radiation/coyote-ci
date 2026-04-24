package repository

import (
	"context"
	"errors"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

var ErrVersionTagNotFound = errors.New("version tag not found")
var ErrVersionTagConflict = errors.New("version tag already exists for target")
var ErrVersionTagTargetNotFound = errors.New("version tag target not found")
var ErrVersionTagTargetJobMismatch = errors.New("version tag target does not belong to job")

type CreateVersionTagsParams struct {
	JobID                  string
	Version                string
	ArtifactIDs            []string
	ManagedImageVersionIDs []string
}

type VersionTagRepository interface {
	ListByArtifactID(ctx context.Context, artifactID string) ([]domain.VersionTag, error)
	ListByArtifactIDs(ctx context.Context, artifactIDs []string) ([]domain.VersionTag, error)
	ListByManagedImageVersionID(ctx context.Context, managedImageVersionID string) ([]domain.VersionTag, error)
	ListByJobID(ctx context.Context, jobID string) ([]domain.VersionTag, error)
	CreateForTargets(ctx context.Context, params CreateVersionTagsParams) ([]domain.VersionTag, error)
	ListByJobIDAndVersion(ctx context.Context, jobID string, version string) ([]domain.VersionTag, error)
}
