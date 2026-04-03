package repository

import (
	"context"
	"errors"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

var ErrExecutionJobOutputNotFound = errors.New("execution job output not found")

type ExecutionJobOutputRepository interface {
	CreateMany(ctx context.Context, outputs []domain.ExecutionJobOutput) ([]domain.ExecutionJobOutput, error)
	ListByBuildID(ctx context.Context, buildID string) ([]domain.ExecutionJobOutput, error)
	ListByJobID(ctx context.Context, jobID string) ([]domain.ExecutionJobOutput, error)
}
