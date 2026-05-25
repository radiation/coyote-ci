package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

func TestWorkerRepository_UpsertHeartbeatAndList(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sql mock: %v", err)
	}
	repo := NewWorkerRepository(db)
	first := time.Date(2026, time.May, 24, 12, 0, 0, 0, time.UTC)
	second := first.Add(20 * time.Second)

	mock.ExpectQuery("INSERT INTO workers").
		WithArgs("worker-a", "worker-a", first).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "last_heartbeat_at", "created_at", "updated_at"}).
			AddRow("worker-a", "worker-a", first, first, first))

	worker, err := repo.UpsertHeartbeat(context.Background(), domain.WorkerHeartbeat{
		ID:          "worker-a",
		Name:        "worker-a",
		HeartbeatAt: first,
	})
	if err != nil {
		t.Fatalf("UpsertHeartbeat returned error: %v", err)
	}
	if worker.ID != "worker-a" {
		t.Fatalf("expected worker id worker-a, got %q", worker.ID)
	}

	mock.ExpectQuery("INSERT INTO workers").
		WithArgs("worker-a", "worker-alpha", second).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "last_heartbeat_at", "created_at", "updated_at"}).
			AddRow("worker-a", "worker-alpha", second, first, second))

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

	mock.ExpectQuery("SELECT id, name, last_heartbeat_at, created_at, updated_at").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "last_heartbeat_at", "created_at", "updated_at"}).
			AddRow("worker-a", "worker-alpha", second, first, second))

	workers, err := repo.List(context.Background())
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(workers) != 1 {
		t.Fatalf("expected 1 worker, got %d", len(workers))
	}
	if workers[0].Name != "worker-alpha" {
		t.Fatalf("expected listed name worker-alpha, got %q", workers[0].Name)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}
