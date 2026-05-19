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

type ListCatalogInput struct {
	Query     string
	ProjectID string
	JobID     string
	BuildID   string
	Limit     int
	Offset    int
}

type Service struct {
	repo        repository.ArtifactBrowseRepository
	catalogRepo repository.ArtifactCatalogRepository
}

func NewService(repo repository.ArtifactBrowseRepository) *Service {
	service := &Service{repo: repo}
	if catalogRepo, ok := repo.(repository.ArtifactCatalogRepository); ok {
		service.catalogRepo = catalogRepo
	}
	return service
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

func (s *Service) ListCatalog(ctx context.Context, input ListCatalogInput) ([]domain.ArtifactRecord, error) {
	if s.catalogRepo == nil {
		return nil, ErrArtifactRepositoryNotConfigured
	}

	return s.catalogRepo.ListCatalog(ctx, repository.ArtifactCatalogParams{
		Query:     strings.TrimSpace(input.Query),
		ProjectID: strings.TrimSpace(input.ProjectID),
		JobID:     strings.TrimSpace(input.JobID),
		BuildID:   strings.TrimSpace(input.BuildID),
		Limit:     input.Limit,
		Offset:    input.Offset,
	})
}

func (s *Service) GetArtifact(ctx context.Context, artifactID string) (domain.ArtifactRecord, error) {
	if s.catalogRepo == nil {
		return domain.ArtifactRecord{}, ErrArtifactRepositoryNotConfigured
	}

	return s.catalogRepo.GetCatalogByID(ctx, strings.TrimSpace(artifactID))
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
