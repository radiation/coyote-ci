package logs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

type PostgresSink struct {
	db *sql.DB
}

var _ LogSink = (*PostgresSink)(nil)
var _ LogReader = (*PostgresSink)(nil)
var _ StepLogChunkAppender = (*PostgresSink)(nil)
var _ StepLogChunkReader = (*PostgresSink)(nil)

func NewPostgresSink(db *sql.DB) *PostgresSink {
	return &PostgresSink{db: db}
}

func (s *PostgresSink) WriteStepLog(_ context.Context, _ string, _ string, _ string) error {
	// Legacy line-based writes are intentionally ignored for postgres sink.
	return nil
}

func (s *PostgresSink) AppendStepLogChunk(ctx context.Context, chunk StepLogChunk) (StepLogChunk, error) {
	if strings.TrimSpace(chunk.BuildID) == "" {
		return StepLogChunk{}, errors.New("build_id is required")
	}
	if chunk.StepIndex < 0 {
		return StepLogChunk{}, errors.New("step_index must be >= 0")
	}
	if strings.TrimSpace(chunk.ChunkText) == "" {
		return StepLogChunk{}, errors.New("chunk_text is required")
	}
	if chunk.Stream == "" {
		chunk.Stream = StepLogStreamSystem
	}
	if chunk.CreatedAt.IsZero() {
		chunk.CreatedAt = time.Now().UTC()
	}

	const query = `
		WITH target_step AS (
			SELECT id, name
			FROM build_steps
			WHERE build_id = $1 AND step_index = $2
		)
		INSERT INTO build_step_logs (build_id, step_id, step_index, step_name, stream, chunk_text, created_at)
		SELECT $1, target_step.id, $2, COALESCE($3, target_step.name), $4, $5, $6
		FROM target_step
		RETURNING sequence_no, build_id, step_id, step_index, step_name, stream, chunk_text, created_at
	`

	var persisted StepLogChunk
	if err := s.db.QueryRowContext(
		ctx,
		query,
		chunk.BuildID,
		chunk.StepIndex,
		nullIfEmpty(chunk.StepName),
		string(chunk.Stream),
		chunk.ChunkText,
		chunk.CreatedAt,
	).Scan(
		&persisted.SequenceNo,
		&persisted.BuildID,
		&persisted.StepID,
		&persisted.StepIndex,
		&persisted.StepName,
		&persisted.Stream,
		&persisted.ChunkText,
		&persisted.CreatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return StepLogChunk{}, fmt.Errorf("step not found for build_id=%s step_index=%d", chunk.BuildID, chunk.StepIndex)
		}
		return StepLogChunk{}, err
	}

	return persisted, nil
}

func (s *PostgresSink) ListStepLogChunks(ctx context.Context, buildID string, stepIndex int, afterSequence int64, limit int) (chunks []StepLogChunk, err error) {
	if strings.TrimSpace(buildID) == "" {
		return nil, errors.New("build_id is required")
	}
	if stepIndex < 0 {
		return nil, errors.New("step_index must be >= 0")
	}
	if limit <= 0 {
		limit = 200
	}
	if limit > 2000 {
		limit = 2000
	}

	const query = `
		SELECT sequence_no, build_id, step_id, step_index, step_name, stream, chunk_text, created_at
		FROM build_step_logs
		WHERE build_id = $1
		  AND step_index = $2
		  AND sequence_no > $3
		ORDER BY created_at ASC, id ASC
		LIMIT $4
	`

	rows, err := s.db.QueryContext(ctx, query, buildID, stepIndex, afterSequence, limit)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	chunks = make([]StepLogChunk, 0)
	for rows.Next() {
		var chunk StepLogChunk
		if scanErr := rows.Scan(
			&chunk.SequenceNo,
			&chunk.BuildID,
			&chunk.StepID,
			&chunk.StepIndex,
			&chunk.StepName,
			&chunk.Stream,
			&chunk.ChunkText,
			&chunk.CreatedAt,
		); scanErr != nil {
			return nil, scanErr
		}
		chunks = append(chunks, chunk)
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, rowsErr
	}

	return chunks, nil
}

func (s *PostgresSink) GetBuildLogs(ctx context.Context, buildID string) (out []BuildLogLine, err error) {
	const query = `
		SELECT step_name, created_at, chunk_text
		FROM build_step_logs
		WHERE build_id = $1
		ORDER BY created_at ASC, id ASC
	`

	rows, err := s.db.QueryContext(ctx, query, buildID)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	out = make([]BuildLogLine, 0)
	for rows.Next() {
		var line BuildLogLine
		if scanErr := rows.Scan(&line.StepName, &line.Timestamp, &line.Message); scanErr != nil {
			return nil, scanErr
		}
		out = append(out, line)
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, rowsErr
	}

	return out, nil
}

func nullIfEmpty(value string) any {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return trimmed
}
