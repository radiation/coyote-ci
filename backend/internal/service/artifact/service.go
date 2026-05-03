package artifact

import (
	"context"
	"errors"
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

var ErrArtifactRepositoryNotConfigured = errors.New("artifact repository is not configured")
var ErrInvalidArtifactTypeFilter = errors.New("invalid artifact type filter")

type ListArtifactsInput struct {
	Query     string
	Type      string
	ProjectID string
	Limit     int
	Offset    int
}

type Service struct {
	repo repository.ArtifactBrowseRepository
}

func NewService(repo repository.ArtifactBrowseRepository) *Service {
	return &Service{repo: repo}
}

func (s *Service) ListArtifacts(ctx context.Context, input ListArtifactsInput) ([]domain.Artifact, error) {
	if s.repo == nil {
		return nil, ErrArtifactRepositoryNotConfigured
	}

	wantedType, err := parseArtifactType(input.Type)
	if err != nil {
		return nil, err
	}

	records, err := s.repo.Browse(ctx, repository.BrowseArtifactsParams{
		Query:     strings.TrimSpace(input.Query),
		Type:      wantedType,
		ProjectID: strings.TrimSpace(input.ProjectID),
		Limit:     input.Limit,
		Offset:    input.Offset,
	})
	if err != nil {
		return nil, err
	}

	return domain.GroupArtifacts(records), nil
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
