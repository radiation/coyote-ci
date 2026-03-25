package store

import (
	"context"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

type BuildStore interface {
	Create(ctx context.Context, build domain.Build) (domain.Build, error)
	CreateQueuedBuild(ctx context.Context, build domain.Build, steps []domain.BuildStep) (domain.Build, error)
	List(ctx context.Context) ([]domain.Build, error)
	GetByID(ctx context.Context, id string) (domain.Build, error)
	UpdateStatus(ctx context.Context, id string, status domain.BuildStatus, errorMessage *string) (domain.Build, error)
	QueueBuild(ctx context.Context, id string, steps []domain.BuildStep) (domain.Build, error)
	GetStepsByBuildID(ctx context.Context, buildID string) ([]domain.BuildStep, error)
	ClaimStepIfPending(ctx context.Context, buildID string, stepIndex int, workerID *string, startedAt time.Time) (domain.BuildStep, bool, error)
	UpdateStepByIndex(ctx context.Context, buildID string, stepIndex int, status domain.BuildStepStatus, workerID *string, exitCode *int, stdout *string, stderr *string, errorMessage *string, startedAt *time.Time, finishedAt *time.Time) (domain.BuildStep, error)
	UpdateCurrentStepIndex(ctx context.Context, id string, currentStepIndex int) (domain.Build, error)
}
