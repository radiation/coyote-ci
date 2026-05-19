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

type ArtifactLabelRepository struct {
	mu        sync.RWMutex
	artifacts map[string]domain.BuildArtifact
	builds    map[string]domain.Build
	versions  []artifactVersionRecord
	channels  map[string]artifactChannelRecord
	events    []artifactChannelEventRecord
}

type artifactVersionRecord struct {
	ID         string
	PackageID  string
	ArtifactID string
	Value      string
	CreatedAt  time.Time
}

type artifactChannelRecord struct {
	ID         string
	PackageID  string
	ArtifactID string
	Value      string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type artifactChannelEventRecord struct {
	ID                 string
	PackageID          string
	Value              string
	PreviousArtifactID *string
	NewArtifactID      string
	CreatedAt          time.Time
}

func NewArtifactLabelRepository() *ArtifactLabelRepository {
	return &ArtifactLabelRepository{
		artifacts: map[string]domain.BuildArtifact{},
		builds:    map[string]domain.Build{},
		channels:  map[string]artifactChannelRecord{},
	}
}

func (r *ArtifactLabelRepository) SeedBuilds(builds ...domain.Build) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, build := range builds {
		r.builds[build.ID] = build
	}
}

func (r *ArtifactLabelRepository) SeedArtifacts(artifacts ...domain.BuildArtifact) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, artifact := range artifacts {
		artifact.PackageID = r.packageIDLocked(artifact)
		r.artifacts[artifact.ID] = artifact
	}
}

func (r *ArtifactLabelRepository) ListByArtifactID(_ context.Context, artifactID string) ([]domain.VersionTag, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	trimmed := strings.TrimSpace(artifactID)
	return r.listLocked(func(tag domain.VersionTag) bool {
		return tag.ArtifactID != nil && *tag.ArtifactID == trimmed
	}), nil
}

func (r *ArtifactLabelRepository) ListByArtifactIDs(_ context.Context, artifactIDs []string) ([]domain.VersionTag, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	allowed := map[string]struct{}{}
	for _, artifactID := range artifactIDs {
		trimmed := strings.TrimSpace(artifactID)
		if trimmed != "" {
			allowed[trimmed] = struct{}{}
		}
	}
	return r.listLocked(func(tag domain.VersionTag) bool {
		if tag.ArtifactID == nil {
			return false
		}
		_, ok := allowed[*tag.ArtifactID]
		return ok
	}), nil
}

func (r *ArtifactLabelRepository) ListByJobID(_ context.Context, jobID string) ([]domain.VersionTag, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	trimmed := strings.TrimSpace(jobID)
	return r.listLocked(func(tag domain.VersionTag) bool {
		return tag.JobID == trimmed
	}), nil
}

func (r *ArtifactLabelRepository) ListByJobIDAndValue(_ context.Context, jobID string, value string) ([]domain.VersionTag, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	trimmedJobID := strings.TrimSpace(jobID)
	trimmedValue := strings.TrimSpace(value)
	return r.listLocked(func(tag domain.VersionTag) bool {
		return tag.JobID == trimmedJobID && tag.Version == trimmedValue
	}), nil
}

func (r *ArtifactLabelRepository) ListReleaseVersionsByJobID(_ context.Context, jobID string) ([]string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	trimmed := strings.TrimSpace(jobID)
	values := make([]string, 0)
	seen := map[string]struct{}{}
	for _, record := range r.versions {
		artifact, ok := r.artifacts[record.ArtifactID]
		if !ok {
			continue
		}
		build := r.builds[artifact.BuildID]
		if build.JobID == nil || strings.TrimSpace(*build.JobID) != trimmed {
			continue
		}
		if _, ok := seen[record.Value]; ok {
			continue
		}
		seen[record.Value] = struct{}{}
		values = append(values, record.Value)
	}
	sort.Strings(values)
	return values, nil
}

func (r *ArtifactLabelRepository) CreateForArtifacts(_ context.Context, params repository.CreateArtifactLabelsParams) ([]domain.VersionTag, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	jobID := strings.TrimSpace(params.JobID)
	value := strings.TrimSpace(params.Value)
	targets, err := r.resolveTargetsLocked(jobID, params.ArtifactIDs)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	created := make([]domain.VersionTag, 0, len(targets))
	for _, target := range targets {
		switch params.Kind {
		case domain.VersionTagKindVersion:
			var existing *artifactVersionRecord
			for _, record := range r.versions {
				if record.PackageID == target.PackageID && record.Value == value {
					existing = &record
					break
				}
			}
			if existing != nil {
				if existing.ArtifactID != target.ArtifactID {
					return nil, repository.ErrVersionTagConflict
				}
				created = append(created, r.toVersionTagLocked(*existing))
				continue
			}
			record := artifactVersionRecord{
				ID:         uuid.NewString(),
				PackageID:  target.PackageID,
				ArtifactID: target.ArtifactID,
				Value:      value,
				CreatedAt:  now,
			}
			r.versions = append(r.versions, record)
			created = append(created, r.toVersionTagLocked(record))
		case domain.VersionTagKindChannel:
			key := target.PackageID + "::" + value
			record, ok := r.channels[key]
			if ok {
				if record.ArtifactID != target.ArtifactID {
					previousArtifactID := record.ArtifactID
					record.ArtifactID = target.ArtifactID
					record.UpdatedAt = now
					r.channels[key] = record
					r.events = append(r.events, artifactChannelEventRecord{
						ID:                 uuid.NewString(),
						PackageID:          target.PackageID,
						Value:              value,
						PreviousArtifactID: &previousArtifactID,
						NewArtifactID:      target.ArtifactID,
						CreatedAt:          now,
					})
				}
				created = append(created, r.toChannelTagLocked(record))
				continue
			}
			record = artifactChannelRecord{
				ID:         uuid.NewString(),
				PackageID:  target.PackageID,
				ArtifactID: target.ArtifactID,
				Value:      value,
				CreatedAt:  now,
				UpdatedAt:  now,
			}
			r.channels[key] = record
			r.events = append(r.events, artifactChannelEventRecord{
				ID:            uuid.NewString(),
				PackageID:     target.PackageID,
				Value:         value,
				NewArtifactID: target.ArtifactID,
				CreatedAt:     now,
			})
			created = append(created, r.toChannelTagLocked(record))
		}
	}
	return created, nil
}

type artifactLabelTarget struct {
	PackageID  string
	ArtifactID string
}

func (r *ArtifactLabelRepository) resolveTargetsLocked(jobID string, artifactIDs []string) ([]artifactLabelTarget, error) {
	selected := make([]artifactLabelTarget, 0)
	byPackage := map[string]artifactLabelTarget{}
	for _, artifactID := range uniqueTrimmedStrings(artifactIDs) {
		artifact, ok := r.artifacts[artifactID]
		if !ok {
			return nil, repository.ErrVersionTagTargetNotFound
		}
		build, ok := r.builds[artifact.BuildID]
		if !ok || build.JobID == nil {
			return nil, repository.ErrVersionTagTargetNotFound
		}
		if strings.TrimSpace(*build.JobID) != jobID {
			return nil, repository.ErrVersionTagTargetJobMismatch
		}
		packageID := r.packageIDLocked(artifact)
		current, ok := byPackage[packageID]
		if !ok {
			byPackage[packageID] = artifactLabelTarget{PackageID: packageID, ArtifactID: artifact.ID}
			continue
		}
		currentArtifact := r.artifacts[current.ArtifactID]
		if artifact.CreatedAt.After(currentArtifact.CreatedAt) || (artifact.CreatedAt.Equal(currentArtifact.CreatedAt) && artifact.ID > currentArtifact.ID) {
			byPackage[packageID] = artifactLabelTarget{PackageID: packageID, ArtifactID: artifact.ID}
		}
	}
	for _, target := range byPackage {
		selected = append(selected, target)
	}
	sort.Slice(selected, func(i int, j int) bool {
		if selected[i].PackageID == selected[j].PackageID {
			return selected[i].ArtifactID < selected[j].ArtifactID
		}
		return selected[i].PackageID < selected[j].PackageID
	})
	return selected, nil
}

func (r *ArtifactLabelRepository) listLocked(include func(domain.VersionTag) bool) []domain.VersionTag {
	out := make([]domain.VersionTag, 0)
	for _, record := range r.versions {
		tag := r.toVersionTagLocked(record)
		if include(tag) {
			out = append(out, tag)
		}
	}
	for _, record := range r.channels {
		tag := r.toChannelTagLocked(record)
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

func (r *ArtifactLabelRepository) toVersionTagLocked(record artifactVersionRecord) domain.VersionTag {
	artifactID := record.ArtifactID
	return domain.VersionTag{
		ID:         record.ID,
		JobID:      r.jobIDForArtifactLocked(record.ArtifactID),
		Kind:       domain.VersionTagKindVersion,
		Version:    record.Value,
		TargetType: domain.VersionTagTargetArtifact,
		ArtifactID: &artifactID,
		CreatedAt:  record.CreatedAt,
	}
}

func (r *ArtifactLabelRepository) toChannelTagLocked(record artifactChannelRecord) domain.VersionTag {
	artifactID := record.ArtifactID
	return domain.VersionTag{
		ID:         record.ID,
		JobID:      r.jobIDForArtifactLocked(record.ArtifactID),
		Kind:       domain.VersionTagKindChannel,
		Version:    record.Value,
		TargetType: domain.VersionTagTargetArtifact,
		ArtifactID: &artifactID,
		CreatedAt:  record.UpdatedAt,
	}
}

func (r *ArtifactLabelRepository) jobIDForArtifactLocked(artifactID string) string {
	artifact, ok := r.artifacts[artifactID]
	if !ok {
		return ""
	}
	build, ok := r.builds[artifact.BuildID]
	if !ok || build.JobID == nil {
		return ""
	}
	return strings.TrimSpace(*build.JobID)
}

func (r *ArtifactLabelRepository) packageIDLocked(artifact domain.BuildArtifact) string {
	if strings.TrimSpace(artifact.PackageID) != "" {
		return strings.TrimSpace(artifact.PackageID)
	}
	build := r.builds[artifact.BuildID]
	scopeID := artifact.BuildID
	if build.JobID != nil && strings.TrimSpace(*build.JobID) != "" {
		scopeID = strings.TrimSpace(*build.JobID)
	}
	return scopeID + "::" + strings.TrimSpace(artifact.LogicalPath)
}
