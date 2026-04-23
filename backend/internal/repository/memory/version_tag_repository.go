package memory

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type VersionTagRepository struct {
	mu                       sync.RWMutex
	tags                     []domain.VersionTag
	artifactsByID            map[string]domain.BuildArtifact
	buildsByID               map[string]domain.Build
	managedImageVersionsByID map[string]domain.ManagedImageVersion
}

func NewVersionTagRepository() *VersionTagRepository {
	return &VersionTagRepository{
		artifactsByID:            map[string]domain.BuildArtifact{},
		buildsByID:               map[string]domain.Build{},
		managedImageVersionsByID: map[string]domain.ManagedImageVersion{},
	}
}

func (r *VersionTagRepository) SeedArtifacts(artifacts ...domain.BuildArtifact) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, artifact := range artifacts {
		r.artifactsByID[artifact.ID] = artifact
	}
}

func (r *VersionTagRepository) SeedBuilds(builds ...domain.Build) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, build := range builds {
		r.buildsByID[build.ID] = build
	}
}

func (r *VersionTagRepository) SeedManagedImageVersions(versions ...domain.ManagedImageVersion) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, version := range versions {
		r.managedImageVersionsByID[version.ID] = version
	}
}

func (r *VersionTagRepository) ListByArtifactID(_ context.Context, artifactID string) ([]domain.VersionTag, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.filterLocked(func(tag domain.VersionTag) bool {
		return tag.ArtifactID != nil && *tag.ArtifactID == strings.TrimSpace(artifactID)
	}), nil
}

func (r *VersionTagRepository) ListByArtifactIDs(_ context.Context, artifactIDs []string) ([]domain.VersionTag, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	allowed := make(map[string]struct{}, len(artifactIDs))
	for _, artifactID := range artifactIDs {
		trimmed := strings.TrimSpace(artifactID)
		if trimmed != "" {
			allowed[trimmed] = struct{}{}
		}
	}
	return r.filterLocked(func(tag domain.VersionTag) bool {
		if tag.ArtifactID == nil {
			return false
		}
		_, ok := allowed[*tag.ArtifactID]
		return ok
	}), nil
}

func (r *VersionTagRepository) ListByManagedImageVersionID(_ context.Context, managedImageVersionID string) ([]domain.VersionTag, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	trimmed := strings.TrimSpace(managedImageVersionID)
	return r.filterLocked(func(tag domain.VersionTag) bool {
		return tag.ManagedImageVersionID != nil && *tag.ManagedImageVersionID == trimmed
	}), nil
}

func (r *VersionTagRepository) ListByJobIDAndVersion(_ context.Context, jobID string, version string) ([]domain.VersionTag, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	trimmedJobID := strings.TrimSpace(jobID)
	trimmedVersion := strings.TrimSpace(version)
	return r.filterLocked(func(tag domain.VersionTag) bool {
		return tag.JobID == trimmedJobID && tag.Version == trimmedVersion
	}), nil
}

func (r *VersionTagRepository) CreateForTargets(_ context.Context, params repository.CreateVersionTagsParams) ([]domain.VersionTag, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	jobID := strings.TrimSpace(params.JobID)
	version := strings.TrimSpace(params.Version)
	artifactIDs := uniqueTrimmedStrings(params.ArtifactIDs)
	managedImageVersionIDs := uniqueTrimmedStrings(params.ManagedImageVersionIDs)
	createdAt := time.Now().UTC()

	for _, artifactID := range artifactIDs {
		artifact, ok := r.artifactsByID[artifactID]
		if !ok {
			return nil, repository.ErrVersionTagTargetNotFound
		}
		build, ok := r.buildsByID[artifact.BuildID]
		if !ok {
			return nil, repository.ErrVersionTagTargetNotFound
		}
		if build.JobID == nil || strings.TrimSpace(*build.JobID) != jobID {
			return nil, repository.ErrVersionTagTargetJobMismatch
		}
		if r.hasDuplicateLocked(jobID, version, &artifactID, nil) {
			return nil, repository.ErrVersionTagConflict
		}
	}

	for _, managedImageVersionID := range managedImageVersionIDs {
		if _, ok := r.managedImageVersionsByID[managedImageVersionID]; !ok {
			return nil, repository.ErrVersionTagTargetNotFound
		}
		if r.hasDuplicateLocked(jobID, version, nil, &managedImageVersionID) {
			return nil, repository.ErrVersionTagConflict
		}
	}

	created := make([]domain.VersionTag, 0, len(artifactIDs)+len(managedImageVersionIDs))
	for _, artifactID := range artifactIDs {
		artifactID := artifactID
		tag := domain.VersionTag{
			ID:         uuid.NewString(),
			JobID:      jobID,
			Version:    version,
			TargetType: domain.VersionTagTargetArtifact,
			ArtifactID: &artifactID,
			CreatedAt:  createdAt,
		}
		r.tags = append(r.tags, tag)
		created = append(created, tag)
	}
	for _, managedImageVersionID := range managedImageVersionIDs {
		managedImageVersionID := managedImageVersionID
		tag := domain.VersionTag{
			ID:                    uuid.NewString(),
			JobID:                 jobID,
			Version:               version,
			TargetType:            domain.VersionTagTargetManagedImageVersion,
			ManagedImageVersionID: &managedImageVersionID,
			CreatedAt:             createdAt,
		}
		r.tags = append(r.tags, tag)
		created = append(created, tag)
	}
	return created, nil
}

func (r *VersionTagRepository) hasDuplicateLocked(jobID string, version string, artifactID *string, managedImageVersionID *string) bool {
	for _, tag := range r.tags {
		if tag.JobID != jobID || tag.Version != version {
			continue
		}
		if artifactID != nil && tag.ArtifactID != nil && *artifactID == *tag.ArtifactID {
			return true
		}
		if managedImageVersionID != nil && tag.ManagedImageVersionID != nil && *managedImageVersionID == *tag.ManagedImageVersionID {
			return true
		}
	}
	return false
}

func (r *VersionTagRepository) filterLocked(include func(domain.VersionTag) bool) []domain.VersionTag {
	out := make([]domain.VersionTag, 0)
	for _, tag := range r.tags {
		if include(tag) {
			out = append(out, tag)
		}
	}
	sort.Slice(out, func(i int, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

func uniqueTrimmedStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}
