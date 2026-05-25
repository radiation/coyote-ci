package memory

import (
	"context"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

func TestWorkerRepository_UpsertHeartbeatAndList(t *testing.T) {
	repo := NewWorkerRepository()
	first := time.Date(2026, time.May, 24, 12, 0, 0, 0, time.UTC)
	second := first.Add(15 * time.Second)

	worker, err := repo.UpsertHeartbeat(context.Background(), domain.WorkerHeartbeat{
		ID:          "worker-a",
		Name:        "worker-a",
		HeartbeatAt: first,
	})
	if err != nil {
		t.Fatalf("UpsertHeartbeat returned error: %v", err)
	}
	if worker.CreatedAt != first {
		t.Fatalf("expected created_at %s, got %s", first, worker.CreatedAt)
	}

	worker, err = repo.UpsertHeartbeat(context.Background(), domain.WorkerHeartbeat{
		ID:          "worker-a",
		Name:        "worker-alpha",
		HeartbeatAt: second,
	})
	if err != nil {
		t.Fatalf("UpsertHeartbeat returned error: %v", err)
	}
	if worker.Name != "worker-alpha" {
		t.Fatalf("expected updated name worker-alpha, got %q", worker.Name)
	}
	if worker.LastHeartbeatAt != second {
		t.Fatalf("expected last heartbeat %s, got %s", second, worker.LastHeartbeatAt)
	}

	workers, err := repo.List(context.Background())
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(workers) != 1 {
		t.Fatalf("expected 1 worker, got %d", len(workers))
	}
	if workers[0].UpdatedAt != second {
		t.Fatalf("expected updated_at %s, got %s", second, workers[0].UpdatedAt)
	}
}
