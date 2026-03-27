package repository

import (
	"context"
	"errors"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

var ErrBuildNotFound = errors.New("build not found")
var ErrInvalidBuildStepTransition = errors.New("invalid build step transition")

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

type StepClaim struct {
	WorkerID       string
	ClaimToken     string
	ClaimedAt      time.Time
	LeaseExpiresAt time.Time
}

type StepCompletionOutcome string

const (
	StepCompletionCompleted         StepCompletionOutcome = "completed"
	StepCompletionDuplicateTerminal StepCompletionOutcome = "duplicate_terminal"
	StepCompletionStaleClaim        StepCompletionOutcome = "stale_claim"
	StepCompletionInvalidTransition StepCompletionOutcome = "invalid_transition"
)

type BuildRepository interface {
	Create(ctx context.Context, build domain.Build) (domain.Build, error)
	CreateQueuedBuild(ctx context.Context, build domain.Build, steps []domain.BuildStep) (domain.Build, error)
	List(ctx context.Context) ([]domain.Build, error)
	GetByID(ctx context.Context, id string) (domain.Build, error)
	UpdateStatus(ctx context.Context, id string, status domain.BuildStatus, errorMessage *string) (domain.Build, error)
	QueueBuild(ctx context.Context, id string, steps []domain.BuildStep) (domain.Build, error)
	GetStepsByBuildID(ctx context.Context, buildID string) ([]domain.BuildStep, error)
	ClaimPendingStep(ctx context.Context, buildID string, stepIndex int, claim StepClaim) (domain.BuildStep, bool, error)
	ReclaimExpiredStep(ctx context.Context, buildID string, stepIndex int, reclaimBefore time.Time, claim StepClaim) (domain.BuildStep, bool, error)
	RenewStepLease(ctx context.Context, buildID string, stepIndex int, claimToken string, leaseExpiresAt time.Time) (domain.BuildStep, StepCompletionOutcome, error)
	CompleteClaimedStepAndAdvanceBuild(ctx context.Context, buildID string, stepIndex int, claimToken string, update StepUpdate) (domain.BuildStep, StepCompletionOutcome, error)
	ClaimStepIfPending(ctx context.Context, buildID string, stepIndex int, workerID *string, startedAt time.Time) (domain.BuildStep, bool, error)
	CompleteStepIfRunning(ctx context.Context, buildID string, stepIndex int, update StepUpdate) (domain.BuildStep, bool, error)
	CompleteStepAndAdvanceBuild(ctx context.Context, buildID string, stepIndex int, update StepUpdate) (domain.BuildStep, StepCompletionOutcome, error)
	UpdateStepByIndex(ctx context.Context, buildID string, stepIndex int, update StepUpdate) (domain.BuildStep, error)
	UpdateCurrentStepIndex(ctx context.Context, id string, currentStepIndex int) (domain.Build, error)
}
