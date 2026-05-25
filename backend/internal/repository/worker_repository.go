package repository

import (
	"context"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

type WorkerRepository interface {
	UpsertHeartbeat(ctx context.Context, heartbeat domain.WorkerHeartbeat) (domain.Worker, error)
	List(ctx context.Context) ([]domain.Worker, error)
}
