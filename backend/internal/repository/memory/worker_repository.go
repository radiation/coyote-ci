package memory

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

type WorkerRepository struct {
	mu      sync.RWMutex
	workers map[string]domain.Worker
}

func NewWorkerRepository() *WorkerRepository {
	return &WorkerRepository{workers: map[string]domain.Worker{}}
}

func (r *WorkerRepository) UpsertHeartbeat(_ context.Context, heartbeat domain.WorkerHeartbeat) (domain.Worker, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	worker, ok := r.workers[heartbeat.ID]
	now := heartbeat.HeartbeatAt.UTC()
	if !ok {
		worker = domain.Worker{
			ID:        heartbeat.ID,
			CreatedAt: now,
		}
	}
	worker.Name = workerDisplayName(heartbeat.Name, heartbeat.ID)
	worker.LastHeartbeatAt = now
	worker.UpdatedAt = now
	r.workers[worker.ID] = worker

	return worker, nil
}

func (r *WorkerRepository) List(_ context.Context) ([]domain.Worker, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	workers := make([]domain.Worker, 0, len(r.workers))
	for _, worker := range r.workers {
		workers = append(workers, worker)
	}

	sort.Slice(workers, func(i, j int) bool {
		if workers[i].Name == workers[j].Name {
			return workers[i].ID < workers[j].ID
		}
		return workers[i].Name < workers[j].Name
	})

	return workers, nil
}

func workerDisplayName(name string, fallback string) string {
	if trimmed := strings.TrimSpace(name); trimmed != "" {
		return trimmed
	}
	return strings.TrimSpace(fallback)
}
