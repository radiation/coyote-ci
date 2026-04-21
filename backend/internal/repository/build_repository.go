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

// CompleteStepRequest captures a final worker step result submission.
type CompleteStepRequest struct {
	BuildID      string
	StepIndex    int
	ClaimToken   string
	RequireClaim bool
	Update       StepUpdate
}

// CompleteStepResult reports the persisted step and classification of the request.
type CompleteStepResult struct {
	Step    domain.BuildStep
	Outcome StepCompletionOutcome
}

// ListParams controls pagination for list queries. Zero-value fields mean "use
// defaults" (backend picks a sensible limit). Negative values are clamped.
type ListParams struct {
	Limit  int
	Offset int
}

const (
	DefaultPageLimit = 50
	MaxPageLimit     = 200
)

// ClampPageParams returns sanitized limit and offset values. Zero or negative
// limit defaults to DefaultPageLimit; values above MaxPageLimit are capped.
// Negative offset is clamped to 0.
func ClampPageParams(p ListParams) (int, int) {
	limit := p.Limit
	if limit <= 0 {
		limit = DefaultPageLimit
	}
	if limit > MaxPageLimit {
		limit = MaxPageLimit
	}

	offset := p.Offset
	if offset < 0 {
		offset = 0
	}

	return limit, offset
}

type BuildRepository interface {
	Create(ctx context.Context, build domain.Build) (domain.Build, error)
	CreateQueuedBuild(ctx context.Context, build domain.Build, steps []domain.BuildStep) (domain.Build, error)
	List(ctx context.Context) ([]domain.Build, error)
	ListPaged(ctx context.Context, params ListParams) ([]domain.Build, error)
	ListByJobID(ctx context.Context, jobID string) ([]domain.Build, error)
	GetByID(ctx context.Context, id string) (domain.Build, error)
	UpdateStatus(ctx context.Context, id string, status domain.BuildStatus, errorMessage *string) (domain.Build, error)
	UpdateSourceCommitSHA(ctx context.Context, id string, commitSHA string) (domain.Build, error)
	UpdateImageExecution(ctx context.Context, id string, requestedRef *string, resolvedRef *string, sourceKind domain.ImageSourceKind, managedImageID *string, managedImageVersionID *string) (domain.Build, error)
	QueueBuild(ctx context.Context, id string, steps []domain.BuildStep) (domain.Build, error)
	GetStepsByBuildID(ctx context.Context, buildID string) ([]domain.BuildStep, error)
	ClaimPendingStep(ctx context.Context, buildID string, stepIndex int, claim StepClaim) (domain.BuildStep, bool, error)
	ReclaimExpiredStep(ctx context.Context, buildID string, stepIndex int, reclaimBefore time.Time, claim StepClaim) (domain.BuildStep, bool, error)
	RenewStepLease(ctx context.Context, buildID string, stepIndex int, claimToken string, leaseExpiresAt time.Time) (domain.BuildStep, StepCompletionOutcome, error)
	CompleteStep(ctx context.Context, request CompleteStepRequest) (CompleteStepResult, error)
	ClaimStepIfPending(ctx context.Context, buildID string, stepIndex int, workerID *string, startedAt time.Time) (domain.BuildStep, bool, error)
	CompleteStepIfRunning(ctx context.Context, buildID string, stepIndex int, update StepUpdate) (domain.BuildStep, bool, error)
	UpdateStepByIndex(ctx context.Context, buildID string, stepIndex int, update StepUpdate) (domain.BuildStep, error)
	UpdateCurrentStepIndex(ctx context.Context, id string, currentStepIndex int) (domain.Build, error)
}
