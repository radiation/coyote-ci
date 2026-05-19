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
var ErrVersionTagKindInvalid = errors.New("label kind must be version or channel")
var ErrArtifactChannelsRequireArtifactLabelRepository = errors.New("artifact channel labels require artifact label repository")
var ErrManagedImageVersionChannelsUnsupported = errors.New("managed image version labels only support kind=version")

var inferredChannelNames = map[string]struct{}{
	"latest":            {},
	"stable":            {},
	"prod":              {},
	"production":        {},
	"staging":           {},
	"stage":             {},
	"dev":               {},
	"qa":                {},
	"test":              {},
	"canary":            {},
	"release-candidate": {},
}

type CreateVersionTagsInput struct {
	Version                string
	Kind                   string
	ArtifactIDs            []string
	ManagedImageVersionIDs []string
}

type Service struct {
	repo         repository.VersionTagRepository
	artifactRepo repository.ArtifactLabelRepository
}

func NewService(repo repository.VersionTagRepository) *Service {
	return &Service{repo: repo}
}

func (s *Service) WithArtifactLabels(repo repository.ArtifactLabelRepository) *Service {
	s.artifactRepo = repo
	return s
}

func (s *Service) CreateVersionTags(ctx context.Context, jobID string, input CreateVersionTagsInput) ([]domain.VersionTag, error) {
	if s.repo == nil && s.artifactRepo == nil {
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
	kind, err := resolveKind(input.Kind, trimmedVersion)
	if err != nil {
		return nil, err
	}
	if len(managedImageVersionIDs) > 0 && kind != domain.VersionTagKindVersion {
		return nil, ErrManagedImageVersionChannelsUnsupported
	}
	created := make([]domain.VersionTag, 0, len(artifactIDs)+len(managedImageVersionIDs))
	if len(artifactIDs) > 0 {
		if s.artifactRepo != nil {
			artifactTags, createErr := s.artifactRepo.CreateForArtifacts(ctx, repository.CreateArtifactLabelsParams{
				JobID:       trimmedJobID,
				Value:       trimmedVersion,
				Kind:        kind,
				ArtifactIDs: artifactIDs,
			})
			if createErr != nil {
				return nil, createErr
			}
			created = append(created, artifactTags...)
		} else {
			if s.repo == nil {
				return nil, ErrVersionTagRepositoryNotConfigured
			}
			if kind != domain.VersionTagKindVersion {
				return nil, ErrArtifactChannelsRequireArtifactLabelRepository
			}
			artifactTags, createErr := s.repo.CreateForTargets(ctx, repository.CreateVersionTagsParams{
				JobID:       trimmedJobID,
				Version:     trimmedVersion,
				ArtifactIDs: artifactIDs,
			})
			if createErr != nil {
				return nil, createErr
			}
			created = append(created, artifactTags...)
		}
	}
	if len(managedImageVersionIDs) > 0 {
		if s.repo == nil {
			return nil, ErrVersionTagRepositoryNotConfigured
		}
		imageTags, createErr := s.repo.CreateForTargets(ctx, repository.CreateVersionTagsParams{
			JobID:                  trimmedJobID,
			Version:                trimmedVersion,
			ManagedImageVersionIDs: managedImageVersionIDs,
		})
		if createErr != nil {
			return nil, createErr
		}
		created = append(created, imageTags...)
	}
	return created, nil
}

func (s *Service) ListArtifactTags(ctx context.Context, artifactID string) ([]domain.VersionTag, error) {
	if s.artifactRepo != nil {
		return s.artifactRepo.ListByArtifactID(ctx, strings.TrimSpace(artifactID))
	}
	return s.repo.ListByArtifactID(ctx, strings.TrimSpace(artifactID))
}

func (s *Service) ListArtifactTagsByIDs(ctx context.Context, artifactIDs []string) (map[string][]domain.VersionTag, error) {
	var (
		tags []domain.VersionTag
		err  error
	)
	trimmedIDs := uniqueTrimmed(artifactIDs)
	if s.artifactRepo != nil {
		tags, err = s.artifactRepo.ListByArtifactIDs(ctx, trimmedIDs)
	} else {
		tags, err = s.repo.ListByArtifactIDs(ctx, trimmedIDs)
	}
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
		if s.artifactRepo != nil {
			releaseVersions, listErr := s.artifactRepo.ListReleaseVersionsByJobID(ctx, strings.TrimSpace(*build.JobID))
			if listErr != nil {
				return "", listErr
			}
			existing = releaseVersions
		} else {
			tags, listErr := s.repo.ListByJobID(ctx, strings.TrimSpace(*build.JobID))
			if listErr != nil {
				return "", listErr
			}
			existing = make([]string, 0, len(tags))
			for _, tag := range tags {
				existing = append(existing, tag.Version)
			}
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
	if s.artifactRepo != nil && s.repo != nil {
		artifactTags, listErr := s.artifactRepo.ListByJobIDAndValue(ctx, trimmedJobID, trimmedVersion)
		if listErr != nil {
			return nil, listErr
		}
		managedImageTags, listErr := s.repo.ListByJobIDAndVersion(ctx, trimmedJobID, trimmedVersion)
		if listErr != nil {
			return nil, listErr
		}
		return append(artifactTags, managedImageTags...), nil
	}
	if s.artifactRepo != nil {
		return s.artifactRepo.ListByJobIDAndValue(ctx, trimmedJobID, trimmedVersion)
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

func resolveKind(input string, value string) (domain.VersionTagKind, error) {
	trimmedKind := strings.ToLower(strings.TrimSpace(input))
	switch trimmedKind {
	case "":
		return inferKind(value)
	case string(domain.VersionTagKindVersion):
		return domain.VersionTagKindVersion, nil
	case string(domain.VersionTagKindChannel):
		return domain.VersionTagKindChannel, nil
	default:
		return "", ErrVersionTagKindInvalid
	}
}

func inferKind(value string) (domain.VersionTagKind, error) {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	if _, ok := inferredChannelNames[trimmed]; ok {
		return domain.VersionTagKindChannel, nil
	}
	return domain.VersionTagKindVersion, nil
}
