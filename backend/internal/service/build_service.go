package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

var ErrProjectIDRequired = errors.New("project_id is required")

type BuildService struct {
	buildRepo repository.BuildRepository
}

func NewBuildService(buildRepo repository.BuildRepository) *BuildService {
	return &BuildService{
		buildRepo: buildRepo,
	}
}

type CreateBuildInput struct {
	ProjectID string
}

func (s *BuildService) CreateBuild(ctx context.Context, input CreateBuildInput) (domain.Build, error) {
	if input.ProjectID == "" {
		return domain.Build{}, ErrProjectIDRequired
	}

	build := domain.Build{
		ID:        uuid.NewString(),
		ProjectID: input.ProjectID,
		Status:    domain.BuildStatusPending,
		CreatedAt: time.Now().UTC(),
	}

	return s.buildRepo.Create(ctx, build)
}

func (s *BuildService) GetBuild(ctx context.Context, id string) (domain.Build, error) {
	return s.buildRepo.GetByID(ctx, id)
}
