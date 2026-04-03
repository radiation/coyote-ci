package repository

import (
	"context"
	"errors"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

var ErrExecutionJobNotFound = errors.New("execution job not found")

type ExecutionJobRepository interface {
	CreateJobsForBuild(ctx context.Context, jobs []domain.ExecutionJob) ([]domain.ExecutionJob, error)
	GetJobsByBuildID(ctx context.Context, buildID string) ([]domain.ExecutionJob, error)
	GetJobByID(ctx context.Context, id string) (domain.ExecutionJob, error)
	GetJobByStepID(ctx context.Context, stepID string) (domain.ExecutionJob, error)
	ClaimNextRunnableJob(ctx context.Context, claim StepClaim) (domain.ExecutionJob, bool, error)
	ClaimJobByStepID(ctx context.Context, stepID string, claim StepClaim) (domain.ExecutionJob, bool, error)
	RenewJobLease(ctx context.Context, jobID string, claimToken string, leaseExpiresAt time.Time) (domain.ExecutionJob, StepCompletionOutcome, error)
	CompleteJobSuccess(ctx context.Context, jobID string, claimToken string, finishedAt time.Time, exitCode int, outputRefs []domain.ArtifactRef) (domain.ExecutionJob, StepCompletionOutcome, error)
	CompleteJobFailure(ctx context.Context, jobID string, claimToken string, finishedAt time.Time, errorMessage string, exitCode *int, outputRefs []domain.ArtifactRef) (domain.ExecutionJob, StepCompletionOutcome, error)
}
