package service

import (
	"context"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/logs"
	"github.com/radiation/coyote-ci/backend/internal/orchestrator"
	"github.com/radiation/coyote-ci/backend/internal/runner"
	"github.com/radiation/coyote-ci/backend/internal/runner/inprocess"
	"github.com/radiation/coyote-ci/backend/internal/store"
	"github.com/radiation/coyote-ci/backend/pkg/contracts"
)

var ErrProjectIDRequired = orchestrator.ErrProjectIDRequired
var ErrInvalidBuildStatusTransition = orchestrator.ErrInvalidBuildStatusTransition
var ErrCustomTemplateStepsRequired = orchestrator.ErrCustomTemplateStepsRequired
var ErrCustomTemplateStepCommandRequired = orchestrator.ErrCustomTemplateStepCommandRequired

type BuildService struct {
	orchestrator *orchestrator.BuildOrchestrator
}

func NewBuildService(buildStore store.BuildStore) *BuildService {
	stepRunner := inprocess.New(nil)
	logSink := logs.NewMemorySink()

	return NewBuildServiceWithExecution(buildStore, stepRunner, logSink)
}

func NewBuildServiceWithExecution(buildStore store.BuildStore, stepRunner runner.Runner, logSink logs.LogSink) *BuildService {
	return &BuildService{
		orchestrator: orchestrator.NewBuildOrchestrator(buildStore, stepRunner, logSink),
	}
}

type CreateBuildInput struct {
	ProjectID string
	Steps     []CreateBuildStepInput
}

type CreateBuildStepInput struct {
	Name           string
	Command        string
	Args           []string
	Env            map[string]string
	WorkingDir     string
	TimeoutSeconds int
}

type QueueBuildCustomStepInput struct {
	Name    string
	Command string
}

func (s *BuildService) CreateBuild(ctx context.Context, input CreateBuildInput) (domain.Build, error) {
	return s.orchestrator.CreateBuild(ctx, orchestrator.CreateBuildInput{
		ProjectID: input.ProjectID,
		Steps:     toOrchestratorStepInputs(input.Steps),
	})
}

func toOrchestratorStepInputs(steps []CreateBuildStepInput) []orchestrator.CreateBuildStepInput {
	out := make([]orchestrator.CreateBuildStepInput, 0, len(steps))
	for _, step := range steps {
		out = append(out, orchestrator.CreateBuildStepInput{
			Name:           step.Name,
			Command:        step.Command,
			Args:           step.Args,
			Env:            step.Env,
			WorkingDir:     step.WorkingDir,
			TimeoutSeconds: step.TimeoutSeconds,
		})
	}

	return out
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

func (s *BuildService) ClaimStepIfPending(ctx context.Context, buildID string, stepIndex int, workerID *string, startedAt time.Time) (contracts.BuildStep, bool, error) {
	return s.orchestrator.ClaimStepIfPending(ctx, buildID, stepIndex, workerID, startedAt)
}

func (s *BuildService) RunStep(ctx context.Context, request contracts.RunStepRequest) (contracts.RunStepResult, error) {
	return s.orchestrator.RunStep(ctx, request)
}

func (s *BuildService) QueueBuild(ctx context.Context, id string) (domain.Build, error) {
	return s.orchestrator.QueueBuild(ctx, id)
}

func (s *BuildService) QueueBuildWithTemplate(ctx context.Context, id string, template string) (domain.Build, error) {
	return s.orchestrator.QueueBuildWithTemplate(ctx, id, template)
}

func (s *BuildService) QueueBuildWithTemplateAndCustomSteps(ctx context.Context, id string, template string, customSteps []QueueBuildCustomStepInput) (domain.Build, error) {
	orchestratorSteps := make([]orchestrator.QueueBuildCustomStepInput, 0, len(customSteps))
	for _, step := range customSteps {
		orchestratorSteps = append(orchestratorSteps, orchestrator.QueueBuildCustomStepInput{
			Name:    step.Name,
			Command: step.Command,
		})
	}

	return s.orchestrator.QueueBuildWithTemplateAndCustomSteps(ctx, id, template, orchestratorSteps)
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
