package memory

import (
	"context"
	"strings"
	"sync"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

// ArtifactRepository is an in-memory implementation of repository.ArtifactRepository.
type ArtifactRepository struct {
	mu        sync.RWMutex
	artifacts []domain.BuildArtifact
}

func NewArtifactRepository() *ArtifactRepository {
	return &ArtifactRepository{}
}

func (r *ArtifactRepository) Create(_ context.Context, artifact domain.BuildArtifact) (domain.BuildArtifact, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, existing := range r.artifacts {
		if existing.BuildID != artifact.BuildID || existing.LogicalPath != artifact.LogicalPath {
			continue
		}
		// Step-scoped: conflict only when step_id matches.
		if existing.StepID != nil && artifact.StepID != nil && *existing.StepID == *artifact.StepID {
			return domain.BuildArtifact{}, repository.ErrArtifactConflict
		}
		// Shared-scoped: conflict when both have nil step_id.
		if existing.StepID == nil && artifact.StepID == nil {
			return domain.BuildArtifact{}, repository.ErrArtifactConflict
		}
	}

	r.artifacts = append(r.artifacts, artifact)
	return artifact, nil
}

func (r *ArtifactRepository) ListByBuildID(_ context.Context, buildID string) ([]domain.BuildArtifact, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]domain.BuildArtifact, 0)
	for _, a := range r.artifacts {
		if a.BuildID == buildID {
			out = append(out, a)
		}
	}
	return out, nil
}

func (r *ArtifactRepository) ListForBrowse(_ context.Context, query string) ([]domain.ArtifactBrowseRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	trimmedQuery := strings.TrimSpace(strings.ToLower(query))
	records := make([]domain.ArtifactBrowseRecord, 0, len(r.artifacts))
	for _, artifact := range r.artifacts {
		if trimmedQuery != "" && !strings.Contains(strings.ToLower(artifact.LogicalPath), trimmedQuery) {
			continue
		}
		records = append(records, domain.ArtifactBrowseRecord{
			Artifact: artifact,
			Build: domain.Build{
				ID:        artifact.BuildID,
				CreatedAt: artifact.CreatedAt,
			},
		})
	}
	return records, nil
}

func (r *ArtifactRepository) GetByID(_ context.Context, buildID string, artifactID string) (domain.BuildArtifact, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, a := range r.artifacts {
		if a.BuildID == buildID && a.ID == artifactID {
			return a, nil
		}
	}
	return domain.BuildArtifact{}, repository.ErrArtifactNotFound
}

func (r *ArtifactRepository) ListByStepID(_ context.Context, stepID string) ([]domain.BuildArtifact, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]domain.BuildArtifact, 0)
	for _, a := range r.artifacts {
		if a.StepID != nil && *a.StepID == stepID {
			out = append(out, a)
		}
	}
	return out, nil
}
