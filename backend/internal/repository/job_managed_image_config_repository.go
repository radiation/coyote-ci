package repository

import (
	"context"
	"errors"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

var ErrJobManagedImageConfigNotFound = errors.New("job managed image config not found")

type JobManagedImageConfigRepository interface {
	Create(ctx context.Context, config domain.JobManagedImageConfig) (domain.JobManagedImageConfig, error)
	GetByJobID(ctx context.Context, jobID string) (domain.JobManagedImageConfig, error)
	UpsertByJobID(ctx context.Context, config domain.JobManagedImageConfig) (domain.JobManagedImageConfig, error)
	DeleteByJobID(ctx context.Context, jobID string) error
}
