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
	builds    map[string]domain.Build
}

func NewArtifactRepository() *ArtifactRepository {
	return &ArtifactRepository{}
}

func (r *ArtifactRepository) SeedBuilds(builds ...domain.Build) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.builds == nil {
		r.builds = make(map[string]domain.Build, len(builds))
	}
	for _, build := range builds {
		r.builds[build.ID] = build
	}
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

func (r *ArtifactRepository) Browse(_ context.Context, params repository.BrowseArtifactsParams) ([]domain.ArtifactRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	trimmedQuery := strings.TrimSpace(strings.ToLower(params.Query))
	records := make([]domain.ArtifactRecord, 0, len(r.artifacts))
	for _, artifact := range r.artifacts {
		build := r.builds[artifact.BuildID]
		if build.ID == "" {
			build = domain.Build{ID: artifact.BuildID, CreatedAt: artifact.CreatedAt}
		}
		if trimmedQuery != "" && !matchesBrowseQuery(trimmedQuery, artifact, build) {
			continue
		}
		records = append(records, domain.ArtifactRecord{
			Artifact: artifact,
			Build:    build,
		})
	}

	items := domain.GroupArtifacts(records)
	if params.Type != "" {
		filtered := make([]domain.Artifact, 0, len(items))
		for _, item := range items {
			if item.ArtifactType == params.Type {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}

	start := params.Offset
	if start > len(items) {
		start = len(items)
	}
	end := len(items)
	if params.Limit > 0 && start+params.Limit < end {
		end = start + params.Limit
	}

	out := make([]domain.ArtifactRecord, 0)
	for _, item := range items[start:end] {
		for _, version := range item.Versions {
			out = append(out, domain.ArtifactRecord(version))
		}
	}
	return out, nil
}

func matchesBrowseQuery(query string, artifact domain.BuildArtifact, build domain.Build) bool {
	if strings.Contains(strings.ToLower(strings.TrimSpace(artifact.Name)), query) {
		return true
	}
	if strings.Contains(strings.ToLower(strings.TrimSpace(artifact.LogicalPath)), query) {
		return true
	}
	if strings.Contains(strings.ToLower(strings.TrimSpace(build.ProjectID)), query) {
		return true
	}
	if build.JobID != nil && strings.Contains(strings.ToLower(strings.TrimSpace(*build.JobID)), query) {
		return true
	}
	return false
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
