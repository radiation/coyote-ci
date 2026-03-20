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
var ErrInvalidBuildStatusTransition = errors.New("invalid build status transition")

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

func (s *BuildService) QueueBuild(ctx context.Context, id string) (domain.Build, error) {
	return s.transitionBuildStatus(ctx, id, domain.BuildStatusQueued)
}

func (s *BuildService) StartBuild(ctx context.Context, id string) (domain.Build, error) {
	return s.transitionBuildStatus(ctx, id, domain.BuildStatusRunning)
}

func (s *BuildService) CompleteBuild(ctx context.Context, id string) (domain.Build, error) {
	return s.transitionBuildStatus(ctx, id, domain.BuildStatusSuccess)
}

func (s *BuildService) FailBuild(ctx context.Context, id string) (domain.Build, error) {
	return s.transitionBuildStatus(ctx, id, domain.BuildStatusFailed)
}

func (s *BuildService) transitionBuildStatus(ctx context.Context, id string, toStatus domain.BuildStatus) (domain.Build, error) {
	build, err := s.buildRepo.GetByID(ctx, id)
	if err != nil {
		return domain.Build{}, err
	}

	if !isValidBuildTransition(build.Status, toStatus) {
		return domain.Build{}, ErrInvalidBuildStatusTransition
	}

	return s.buildRepo.UpdateStatus(ctx, id, toStatus)
}

func isValidBuildTransition(fromStatus, toStatus domain.BuildStatus) bool {
	switch fromStatus {
	case domain.BuildStatusPending:
		return toStatus == domain.BuildStatusQueued
	case domain.BuildStatusQueued:
		return toStatus == domain.BuildStatusRunning
	case domain.BuildStatusRunning:
		return toStatus == domain.BuildStatusSuccess || toStatus == domain.BuildStatusFailed
	default:
		return false
	}
}
