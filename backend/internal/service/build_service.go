package service

import (
	"context"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/orchestrator"
	"github.com/radiation/coyote-ci/backend/internal/store"
	"github.com/radiation/coyote-ci/backend/pkg/contracts"
)

var ErrProjectIDRequired = orchestrator.ErrProjectIDRequired
var ErrInvalidBuildStatusTransition = orchestrator.ErrInvalidBuildStatusTransition

type BuildService struct {
	orchestrator *orchestrator.BuildOrchestrator
}

func NewBuildService(buildStore store.BuildStore) *BuildService {
	return &BuildService{
		orchestrator: orchestrator.NewBuildOrchestrator(buildStore, nil, nil),
	}
}

type CreateBuildInput struct {
	ProjectID string
}

func (s *BuildService) CreateBuild(ctx context.Context, input CreateBuildInput) (domain.Build, error) {
	return s.orchestrator.CreateBuild(ctx, orchestrator.CreateBuildInput{
		ProjectID: input.ProjectID,
	})
}

func (s *BuildService) GetBuild(ctx context.Context, id string) (domain.Build, error) {
	return s.orchestrator.GetBuild(ctx, id)
}

func (s *BuildService) ListBuilds(ctx context.Context) ([]domain.Build, error) {
	return s.orchestrator.ListBuilds(ctx)
}

func (s *BuildService) GetBuildSteps(ctx context.Context, id string) ([]contracts.BuildStep, error) {
	return s.orchestrator.GetBuildSteps(ctx, id)
}

func (s *BuildService) GetBuildLogs(ctx context.Context, id string) ([]contracts.BuildLogLine, error) {
	return s.orchestrator.GetBuildLogs(ctx, id)
}

func (s *BuildService) QueueBuild(ctx context.Context, id string) (domain.Build, error) {
	return s.orchestrator.QueueBuild(ctx, id)
}

func (s *BuildService) StartBuild(ctx context.Context, id string) (domain.Build, error) {
	return s.orchestrator.StartBuild(ctx, id)
}

func (s *BuildService) CompleteBuild(ctx context.Context, id string) (domain.Build, error) {
	return s.orchestrator.CompleteBuild(ctx, id)
}

func (s *BuildService) FailBuild(ctx context.Context, id string) (domain.Build, error) {
	return s.orchestrator.FailBuild(ctx, id)
}
