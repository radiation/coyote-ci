package postgres

import (
	"context"
	"database/sql"
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

type WorkerRepository struct {
	db *sql.DB
}

func NewWorkerRepository(db *sql.DB) *WorkerRepository {
	return &WorkerRepository{db: db}
}

func (r *WorkerRepository) UpsertHeartbeat(ctx context.Context, heartbeat domain.WorkerHeartbeat) (domain.Worker, error) {
	const query = `
		INSERT INTO workers (id, name, last_heartbeat_at, created_at, updated_at)
		VALUES ($1, $2, $3, $3, $3)
		ON CONFLICT (id) DO UPDATE
		SET name = EXCLUDED.name,
			last_heartbeat_at = EXCLUDED.last_heartbeat_at,
			updated_at = EXCLUDED.updated_at
		RETURNING id, name, last_heartbeat_at, created_at, updated_at
	`

	return scanWorker(r.db.QueryRowContext(
		ctx,
		query,
		heartbeat.ID,
		normalizeWorkerName(heartbeat.Name, heartbeat.ID),
		heartbeat.HeartbeatAt.UTC(),
	))
}

func (r *WorkerRepository) List(ctx context.Context) (workers []domain.Worker, err error) {
	const query = `
		SELECT id, name, last_heartbeat_at, created_at, updated_at
		FROM workers
		ORDER BY name ASC, id ASC
	`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	workers = make([]domain.Worker, 0)
	for rows.Next() {
		worker, scanErr := scanWorker(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		workers = append(workers, worker)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return workers, nil
}

func scanWorker(scanner rowScanner) (domain.Worker, error) {
	var worker domain.Worker
	if err := scanner.Scan(
		&worker.ID,
		&worker.Name,
		&worker.LastHeartbeatAt,
		&worker.CreatedAt,
		&worker.UpdatedAt,
	); err != nil {
		return domain.Worker{}, err
	}
	return worker, nil
}

func normalizeWorkerName(name string, fallback string) string {
	if trimmed := strings.TrimSpace(name); trimmed != "" {
		return trimmed
	}
	return strings.TrimSpace(fallback)
}
