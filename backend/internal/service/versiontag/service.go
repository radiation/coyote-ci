package versiontag

import (
	"context"
	"errors"
	"strings"
	"unicode"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/versioning"
)

const maxVersionTagLength = 255

var ErrJobIDRequired = errors.New("job id is required")
var ErrVersionRequired = errors.New("version is required")
var ErrTargetRequired = errors.New("at least one target is required")
var ErrVersionTooLong = errors.New("version exceeds maximum length")
var ErrVersionContainsControlChars = errors.New("version contains unsupported control characters")
var ErrVersionTagRepositoryNotConfigured = errors.New("version tag repository not configured")

type CreateVersionTagsInput struct {
	Version                string
	ArtifactIDs            []string
	ManagedImageVersionIDs []string
}

type Service struct {
	repo repository.VersionTagRepository
}

func NewService(repo repository.VersionTagRepository) *Service {
	return &Service{repo: repo}
}

func (s *Service) CreateVersionTags(ctx context.Context, jobID string, input CreateVersionTagsInput) ([]domain.VersionTag, error) {
	if s.repo == nil {
		return nil, ErrVersionTagRepositoryNotConfigured
	}
	trimmedJobID := strings.TrimSpace(jobID)
	if trimmedJobID == "" {
		return nil, ErrJobIDRequired
	}
	trimmedVersion, err := validateVersion(input.Version)
	if err != nil {
		return nil, err
	}
	artifactIDs := uniqueTrimmed(input.ArtifactIDs)
	managedImageVersionIDs := uniqueTrimmed(input.ManagedImageVersionIDs)
	if len(artifactIDs) == 0 && len(managedImageVersionIDs) == 0 {
		return nil, ErrTargetRequired
	}
	return s.repo.CreateForTargets(ctx, repository.CreateVersionTagsParams{
		JobID:                  trimmedJobID,
		Version:                trimmedVersion,
		ArtifactIDs:            artifactIDs,
		ManagedImageVersionIDs: managedImageVersionIDs,
	})
}

func (s *Service) ListArtifactTags(ctx context.Context, artifactID string) ([]domain.VersionTag, error) {
	return s.repo.ListByArtifactID(ctx, strings.TrimSpace(artifactID))
}

func (s *Service) ListArtifactTagsByIDs(ctx context.Context, artifactIDs []string) (map[string][]domain.VersionTag, error) {
	tags, err := s.repo.ListByArtifactIDs(ctx, uniqueTrimmed(artifactIDs))
	if err != nil {
		return nil, err
	}
	byArtifactID := make(map[string][]domain.VersionTag, len(artifactIDs))
	for _, tag := range tags {
		if tag.ArtifactID == nil {
			continue
		}
		byArtifactID[*tag.ArtifactID] = append(byArtifactID[*tag.ArtifactID], tag)
	}
	return byArtifactID, nil
}

func (s *Service) ListManagedImageVersionTags(ctx context.Context, managedImageVersionID string) ([]domain.VersionTag, error) {
	return s.repo.ListByManagedImageVersionID(ctx, strings.TrimSpace(managedImageVersionID))
}

func (s *Service) ResolveReleaseVersion(ctx context.Context, build domain.Build, config versioning.Config) (string, error) {
	existing := []string(nil)
	if versioning.NormalizeStrategy(config.Strategy) == versioning.ReleaseStrategySemverPatch {
		if build.JobID == nil || strings.TrimSpace(*build.JobID) == "" {
			return "", ErrJobIDRequired
		}
		tags, err := s.repo.ListByJobID(ctx, strings.TrimSpace(*build.JobID))
		if err != nil {
			return "", err
		}
		existing = make([]string, 0, len(tags))
		for _, tag := range tags {
			existing = append(existing, tag.Version)
		}
	}
	return versioning.ResolveVersion(versioning.ResolveInput{Config: config, Build: build, ExistingVersions: existing})
}

func (s *Service) ListJobVersionTags(ctx context.Context, jobID string, version string) ([]domain.VersionTag, error) {
	trimmedJobID := strings.TrimSpace(jobID)
	if trimmedJobID == "" {
		return nil, ErrJobIDRequired
	}
	trimmedVersion, err := validateVersion(version)
	if err != nil {
		return nil, err
	}
	return s.repo.ListByJobIDAndVersion(ctx, trimmedJobID, trimmedVersion)
}

func validateVersion(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", ErrVersionRequired
	}
	if len(trimmed) > maxVersionTagLength {
		return "", ErrVersionTooLong
	}
	for _, r := range trimmed {
		if unicode.IsControl(r) {
			return "", ErrVersionContainsControlChars
		}
	}
	return trimmed, nil
}

func uniqueTrimmed(values []string) []string {
	seen := make(map[string]struct{}, len(values))
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
