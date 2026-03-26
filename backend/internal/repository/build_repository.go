package repository

import (
	"context"
	"errors"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

var ErrBuildNotFound = errors.New("build not found")

// StepUpdate contains the fields to update on a build step.
type StepUpdate struct {
	Status       domain.BuildStepStatus
	WorkerID     *string
	ExitCode     *int
	Stdout       *string
	Stderr       *string
	ErrorMessage *string
	StartedAt    *time.Time
	FinishedAt   *time.Time
}

type BuildRepository interface {
	Create(ctx context.Context, build domain.Build) (domain.Build, error)
	CreateQueuedBuild(ctx context.Context, build domain.Build, steps []domain.BuildStep) (domain.Build, error)
	List(ctx context.Context) ([]domain.Build, error)
	GetByID(ctx context.Context, id string) (domain.Build, error)
	UpdateStatus(ctx context.Context, id string, status domain.BuildStatus, errorMessage *string) (domain.Build, error)
	QueueBuild(ctx context.Context, id string, steps []domain.BuildStep) (domain.Build, error)
	GetStepsByBuildID(ctx context.Context, buildID string) ([]domain.BuildStep, error)
	ClaimStepIfPending(ctx context.Context, buildID string, stepIndex int, workerID *string, startedAt time.Time) (domain.BuildStep, bool, error)
	CompleteStepIfRunning(ctx context.Context, buildID string, stepIndex int, update StepUpdate) (domain.BuildStep, bool, error)
	UpdateStepByIndex(ctx context.Context, buildID string, stepIndex int, update StepUpdate) (domain.BuildStep, error)
	UpdateCurrentStepIndex(ctx context.Context, id string, currentStepIndex int) (domain.Build, error)
}
