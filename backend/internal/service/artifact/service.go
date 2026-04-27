package artifact

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

var ErrArtifactRepositoryNotConfigured = errors.New("artifact repository is not configured")
var ErrInvalidArtifactTypeFilter = errors.New("invalid artifact type filter")

type ListArtifactsInput struct {
	Query string
	Type  string
}

type Service struct {
	repo repository.ArtifactRepository
}

func NewService(repo repository.ArtifactRepository) *Service {
	return &Service{repo: repo}
}

func (s *Service) ListArtifacts(ctx context.Context, input ListArtifactsInput) ([]domain.ArtifactBrowseItem, error) {
	if s.repo == nil {
		return nil, ErrArtifactRepositoryNotConfigured
	}

	wantedType, err := parseArtifactType(input.Type)
	if err != nil {
		return nil, err
	}

	records, err := s.repo.ListForBrowse(ctx, strings.TrimSpace(input.Query))
	if err != nil {
		return nil, err
	}

	grouped := make(map[string]*domain.ArtifactBrowseItem, len(records))
	order := make([]string, 0, len(records))
	for _, record := range records {
		groupKey := artifactGroupKey(record)
		item, ok := grouped[groupKey]
		if !ok {
			item = &domain.ArtifactBrowseItem{
				GroupKey:        groupKey,
				Name:            record.Artifact.Name,
				Path:            record.Artifact.LogicalPath,
				ProjectID:       record.Build.ProjectID,
				JobID:           record.Build.JobID,
				ArtifactType:    detectArtifactType(record.Artifact),
				LatestCreatedAt: record.Artifact.CreatedAt,
				Versions:        make([]domain.ArtifactBrowseVersion, 0, 1),
			}
			grouped[groupKey] = item
			order = append(order, groupKey)
		}

		item.Versions = append(item.Versions, domain.ArtifactBrowseVersion(record))
		if record.Artifact.CreatedAt.After(item.LatestCreatedAt) {
			item.LatestCreatedAt = record.Artifact.CreatedAt
			item.Name = record.Artifact.Name
			item.ArtifactType = detectArtifactType(record.Artifact)
		}
	}

	items := make([]domain.ArtifactBrowseItem, 0, len(order))
	for _, key := range order {
		item := grouped[key]
		if wantedType != "" && item.ArtifactType != wantedType {
			continue
		}
		sort.SliceStable(item.Versions, func(i, j int) bool {
			left := item.Versions[i]
			right := item.Versions[j]
			if !left.Artifact.CreatedAt.Equal(right.Artifact.CreatedAt) {
				return left.Artifact.CreatedAt.After(right.Artifact.CreatedAt)
			}
			return left.Build.BuildNumber > right.Build.BuildNumber
		})
		items = append(items, *item)
	}

	sort.SliceStable(items, func(i, j int) bool {
		if !items[i].LatestCreatedAt.Equal(items[j].LatestCreatedAt) {
			return items[i].LatestCreatedAt.After(items[j].LatestCreatedAt)
		}
		return items[i].Path < items[j].Path
	})

	return items, nil
}

func artifactGroupKey(record domain.ArtifactBrowseRecord) string {
	if record.Build.JobID != nil && strings.TrimSpace(*record.Build.JobID) != "" {
		return strings.TrimSpace(*record.Build.JobID) + "::" + record.Artifact.LogicalPath
	}
	return record.Build.ID + "::" + record.Artifact.LogicalPath
}

func parseArtifactType(value string) (domain.ArtifactType, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", nil
	}
	artifactType, ok := domain.ParseArtifactType(trimmed)
	if !ok {
		return "", ErrInvalidArtifactTypeFilter
	}
	return artifactType, nil
}

func detectArtifactType(artifact domain.BuildArtifact) domain.ArtifactType {
	if artifact.ArtifactType != "" {
		return artifact.ArtifactType
	}
	lowerPath := strings.ToLower(strings.TrimSpace(artifact.LogicalPath))
	lowerContentType := ""
	if artifact.ContentType != nil {
		lowerContentType = strings.ToLower(strings.TrimSpace(*artifact.ContentType))
	}

	switch {
	case strings.Contains(lowerContentType, "docker") || strings.Contains(lowerContentType, "oci"):
		return domain.ArtifactTypeDockerImage
	case strings.HasSuffix(lowerPath, ".oci"):
		return domain.ArtifactTypeDockerImage
	case strings.HasSuffix(lowerPath, ".tar") && (strings.Contains(lowerPath, "docker") || strings.Contains(lowerPath, "image") || strings.Contains(lowerPath, "container")):
		return domain.ArtifactTypeDockerImage
	case strings.HasSuffix(lowerPath, ".tgz"):
		base := filepath.Base(lowerPath)
		if strings.Contains(base, "-") || strings.HasPrefix(base, "@") {
			return domain.ArtifactTypeNPMPackage
		}
		return domain.ArtifactTypeGeneric
	case lowerContentType == "" && filepath.Ext(lowerPath) == "":
		return domain.ArtifactTypeUnknown
	case lowerPath == "" && lowerContentType == "":
		return domain.ArtifactTypeUnknown
	default:
		return domain.ArtifactTypeGeneric
	}
}
