package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type ExecutionJobOutputRepository struct {
	db *sql.DB
}

func NewExecutionJobOutputRepository(db *sql.DB) *ExecutionJobOutputRepository {
	return &ExecutionJobOutputRepository{db: db}
}

func (r *ExecutionJobOutputRepository) CreateMany(ctx context.Context, outputs []domain.ExecutionJobOutput) ([]domain.ExecutionJobOutput, error) {
	if len(outputs) == 0 {
		return []domain.ExecutionJobOutput{}, nil
	}

	const query = `
		INSERT INTO build_job_outputs (
			id, job_id, build_id, name, kind, declared_path, destination_uri, content_type, size_bytes, digest, status, created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	for _, output := range outputs {
		if _, execErr := tx.ExecContext(ctx, query,
			output.ID,
			output.JobID,
			output.BuildID,
			output.Name,
			output.Kind,
			output.DeclaredPath,
			output.DestinationURI,
			output.ContentType,
			output.SizeBytes,
			output.Digest,
			string(output.Status),
			output.CreatedAt,
		); execErr != nil {
			return nil, execErr
		}
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return outputs, nil
}

func (r *ExecutionJobOutputRepository) ListByBuildID(ctx context.Context, buildID string) ([]domain.ExecutionJobOutput, error) {
	const query = `
		SELECT id, job_id, build_id, name, kind, declared_path, destination_uri, content_type, size_bytes, digest, status, created_at
		FROM build_job_outputs
		WHERE build_id = $1
		ORDER BY created_at ASC, id ASC
	`
	return r.list(ctx, query, buildID)
}

func (r *ExecutionJobOutputRepository) ListByJobID(ctx context.Context, jobID string) ([]domain.ExecutionJobOutput, error) {
	const query = `
		SELECT id, job_id, build_id, name, kind, declared_path, destination_uri, content_type, size_bytes, digest, status, created_at
		FROM build_job_outputs
		WHERE job_id = $1
		ORDER BY created_at ASC, id ASC
	`
	return r.list(ctx, query, jobID)
}

func (r *ExecutionJobOutputRepository) list(ctx context.Context, query string, arg string) (outputs []domain.ExecutionJobOutput, err error) {
	rows, err := r.db.QueryContext(ctx, query, arg)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	outputs = make([]domain.ExecutionJobOutput, 0)
	for rows.Next() {
		item, scanErr := scanExecutionJobOutput(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		outputs = append(outputs, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return outputs, nil
}

func scanExecutionJobOutput(scanner rowScanner) (domain.ExecutionJobOutput, error) {
	var item domain.ExecutionJobOutput
	var status string
	var destinationURI sql.NullString
	var contentType sql.NullString
	var sizeBytes sql.NullInt64
	var digest sql.NullString

	err := scanner.Scan(
		&item.ID,
		&item.JobID,
		&item.BuildID,
		&item.Name,
		&item.Kind,
		&item.DeclaredPath,
		&destinationURI,
		&contentType,
		&sizeBytes,
		&digest,
		&status,
		&item.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ExecutionJobOutput{}, repository.ErrExecutionJobOutputNotFound
		}
		return domain.ExecutionJobOutput{}, err
	}

	item.Status = domain.ExecutionJobOutputStatus(status)
	if destinationURI.Valid {
		v := destinationURI.String
		item.DestinationURI = &v
	}
	if contentType.Valid {
		v := contentType.String
		item.ContentType = &v
	}
	if sizeBytes.Valid {
		v := sizeBytes.Int64
		item.SizeBytes = &v
	}
	if digest.Valid {
		v := digest.String
		item.Digest = &v
	}

	return item, nil
}
