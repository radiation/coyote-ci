package repository

import (
	"context"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

type CreateArtifactLabelsParams struct {
	JobID       string
	Value       string
	Kind        domain.VersionTagKind
	ArtifactIDs []string
}

type ArtifactLabelRepository interface {
	ListByArtifactID(ctx context.Context, artifactID string) ([]domain.VersionTag, error)
	ListByArtifactIDs(ctx context.Context, artifactIDs []string) ([]domain.VersionTag, error)
	ListByJobID(ctx context.Context, jobID string) ([]domain.VersionTag, error)
	ListByJobIDAndValue(ctx context.Context, jobID string, value string) ([]domain.VersionTag, error)
	ListReleaseVersionsByJobID(ctx context.Context, jobID string) ([]string, error)
	CreateForArtifacts(ctx context.Context, params CreateArtifactLabelsParams) ([]domain.VersionTag, error)
}
