package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type BuildRepository struct {
	db *sql.DB
}

func NewBuildRepository(db *sql.DB) *BuildRepository {
	return &BuildRepository{db: db}
}

func (r *BuildRepository) Create(ctx context.Context, build domain.Build) (domain.Build, error) {
	const query = `
		INSERT INTO builds (id, project_id, status, created_at, current_step_index)
		VALUES ($1, $2, $3, $4, $5)
	`

	if build.CurrentStepIndex < 0 {
		build.CurrentStepIndex = 0
	}

	_, err := r.db.ExecContext(
		ctx,
		query,
		build.ID,
		build.ProjectID,
		string(build.Status),
		build.CreatedAt,
		build.CurrentStepIndex,
	)
	if err != nil {
		return domain.Build{}, err
	}

	return build, nil
}

func (r *BuildRepository) CreateQueuedBuild(ctx context.Context, build domain.Build, steps []domain.BuildStep) (domain.Build, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Build{}, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	const createQuery = `
		INSERT INTO builds (id, project_id, status, created_at, queued_at, current_step_index, error_message)
		VALUES ($1, $2, 'queued', $3, COALESCE($4, NOW()), 0, NULL)
		RETURNING id, project_id, status, created_at, queued_at, started_at, finished_at, current_step_index, error_message
	`

	build, err = scanBuild(tx.QueryRowContext(ctx, createQuery, build.ID, build.ProjectID, build.CreatedAt, build.QueuedAt))
	if err != nil {
		return domain.Build{}, err
	}

	if len(steps) > 0 {
		const insertStepQuery = `
			INSERT INTO build_steps (
				id,
				build_id,
				step_index,
				name,
				command,
				args,
				env,
				working_dir,
				timeout_seconds,
				status,
				worker_id,
				started_at,
				finished_at,
				exit_code,
				stdout,
				stderr,
				error_message
			)
			VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7::jsonb, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
		`

		for _, step := range steps {
			argsJSON, marshalErr := json.Marshal(step.Args)
			if marshalErr != nil {
				return domain.Build{}, marshalErr
			}
			envJSON, marshalErr := json.Marshal(step.Env)
			if marshalErr != nil {
				return domain.Build{}, marshalErr
			}

			if _, err = tx.ExecContext(
				ctx,
				insertStepQuery,
				step.ID,
				build.ID,
				step.StepIndex,
				step.Name,
				step.Command,
				string(argsJSON),
				string(envJSON),
				step.WorkingDir,
				step.TimeoutSeconds,
				string(step.Status),
				step.WorkerID,
				step.StartedAt,
				step.FinishedAt,
				step.ExitCode,
				step.Stdout,
				step.Stderr,
				step.ErrorMessage,
			); err != nil {
				return domain.Build{}, err
			}
		}
	}

	if err = tx.Commit(); err != nil {
		return domain.Build{}, err
	}

	return build, nil
}

func (r *BuildRepository) List(ctx context.Context) (builds []domain.Build, err error) {
	const query = `
		SELECT id, project_id, status, created_at, queued_at, started_at, finished_at, current_step_index, error_message
		FROM builds
		ORDER BY created_at DESC
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

	builds = make([]domain.Build, 0)
	for rows.Next() {
		build, err := scanBuild(rows)
		if err != nil {
			return nil, err
		}
		builds = append(builds, build)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return builds, nil
}

func (r *BuildRepository) GetByID(ctx context.Context, id string) (domain.Build, error) {
	const query = `
		SELECT id, project_id, status, created_at, queued_at, started_at, finished_at, current_step_index, error_message
		FROM builds
		WHERE id = $1
	`

	build, err := scanBuild(r.db.QueryRowContext(ctx, query, id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Build{}, repository.ErrBuildNotFound
		}
		return domain.Build{}, err
	}

	return build, nil
}

func (r *BuildRepository) UpdateStatus(ctx context.Context, id string, status domain.BuildStatus, errorMessage *string) (domain.Build, error) {
	const query = `
		UPDATE builds
		SET status = $2,
			queued_at = CASE WHEN $2 = 'queued' THEN COALESCE(queued_at, NOW()) ELSE queued_at END,
			started_at = CASE WHEN $2 = 'running' THEN COALESCE(started_at, NOW()) ELSE started_at END,
			finished_at = CASE WHEN $2 IN ('success', 'failed') THEN NOW() ELSE finished_at END,
			error_message = CASE WHEN $2 = 'failed' THEN $3 ELSE NULL END
		WHERE id = $1
		RETURNING id, project_id, status, created_at, queued_at, started_at, finished_at, current_step_index, error_message
	`

	build, err := scanBuild(r.db.QueryRowContext(ctx, query, id, string(status), errorMessage))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Build{}, repository.ErrBuildNotFound
		}
		return domain.Build{}, err
	}

	return build, nil
}

func (r *BuildRepository) QueueBuild(ctx context.Context, id string, steps []domain.BuildStep) (domain.Build, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Build{}, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	const queueQuery = `
		UPDATE builds
		SET status = 'queued',
			queued_at = COALESCE(queued_at, NOW()),
			current_step_index = 0,
			error_message = NULL
		WHERE id = $1
		RETURNING id, project_id, status, created_at, queued_at, started_at, finished_at, current_step_index, error_message
	`

	build, err := scanBuild(tx.QueryRowContext(ctx, queueQuery, id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Build{}, repository.ErrBuildNotFound
		}
		return domain.Build{}, err
	}

	const deleteStepsQuery = `
		DELETE FROM build_steps
		WHERE build_id = $1
	`
	if _, err = tx.ExecContext(ctx, deleteStepsQuery, id); err != nil {
		return domain.Build{}, err
	}

	if len(steps) > 0 {
		const insertStepQuery = `
			INSERT INTO build_steps (
				id,
				build_id,
				step_index,
				name,
				command,
				args,
				env,
				working_dir,
				timeout_seconds,
				status,
				worker_id,
				started_at,
				finished_at,
				exit_code,
				stdout,
				stderr,
				error_message
			)
			VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7::jsonb, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
		`

		for _, step := range steps {
			argsJSON, marshalErr := json.Marshal(step.Args)
			if marshalErr != nil {
				return domain.Build{}, marshalErr
			}
			envJSON, marshalErr := json.Marshal(step.Env)
			if marshalErr != nil {
				return domain.Build{}, marshalErr
			}

			if _, err = tx.ExecContext(
				ctx,
				insertStepQuery,
				step.ID,
				id,
				step.StepIndex,
				step.Name,
				step.Command,
				string(argsJSON),
				string(envJSON),
				step.WorkingDir,
				step.TimeoutSeconds,
				string(step.Status),
				step.WorkerID,
				step.StartedAt,
				step.FinishedAt,
				step.ExitCode,
				step.Stdout,
				step.Stderr,
				step.ErrorMessage,
			); err != nil {
				return domain.Build{}, err
			}
		}
	}

	if err = tx.Commit(); err != nil {
		return domain.Build{}, err
	}

	return build, nil
}

func (r *BuildRepository) GetStepsByBuildID(ctx context.Context, buildID string) (steps []domain.BuildStep, err error) {
	const query = `
		SELECT id, build_id, step_index, name, command, args, env, working_dir, timeout_seconds, status, worker_id, started_at, finished_at, exit_code, stdout, stderr, error_message
		FROM build_steps
		WHERE build_id = $1
		ORDER BY step_index ASC
	`

	rows, err := r.db.QueryContext(ctx, query, buildID)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	steps = make([]domain.BuildStep, 0)
	for rows.Next() {
		step, scanErr := scanStep(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		steps = append(steps, step)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(steps) == 0 {
		if _, err := r.GetByID(ctx, buildID); err != nil {
			return nil, err
		}
	}

	return steps, nil
}

func (r *BuildRepository) ClaimStepIfPending(ctx context.Context, buildID string, stepIndex int, workerID *string, startedAt time.Time) (domain.BuildStep, bool, error) {
	const query = `
		UPDATE build_steps
		SET status = 'running',
			worker_id = COALESCE($3, worker_id),
			started_at = COALESCE(started_at, $4)
		WHERE build_id = $1
		  AND step_index = $2
		  AND status = 'pending'
		RETURNING id, build_id, step_index, name, command, args, env, working_dir, timeout_seconds, status, worker_id, started_at, finished_at, exit_code, stdout, stderr, error_message
	`

	step, err := scanStep(r.db.QueryRowContext(ctx, query, buildID, stepIndex, workerID, startedAt))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.BuildStep{}, false, nil
		}
		return domain.BuildStep{}, false, err
	}

	return step, true, nil
}

func (r *BuildRepository) UpdateStepByIndex(ctx context.Context, buildID string, stepIndex int, update repository.StepUpdate) (domain.BuildStep, error) {
	const query = `
		UPDATE build_steps
		SET status = $3,
			worker_id = COALESCE($4, worker_id),
			started_at = COALESCE($5, started_at),
			finished_at = COALESCE($6, finished_at),
			exit_code = COALESCE($7, exit_code),
			stdout = COALESCE($8, stdout),
			stderr = COALESCE($9, stderr),
			error_message = CASE
				WHEN $3 = 'failed' THEN COALESCE($10, error_message)
				WHEN $10 IS NOT NULL THEN $10
				ELSE NULL
			END
		WHERE build_id = $1 AND step_index = $2
		RETURNING id, build_id, step_index, name, command, args, env, working_dir, timeout_seconds, status, worker_id, started_at, finished_at, exit_code, stdout, stderr, error_message
	`

	step, err := scanStep(r.db.QueryRowContext(ctx, query, buildID, stepIndex, string(update.Status), update.WorkerID, update.StartedAt, update.FinishedAt, update.ExitCode, update.Stdout, update.Stderr, update.ErrorMessage))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.BuildStep{}, repository.ErrBuildNotFound
		}
		return domain.BuildStep{}, err
	}

	return step, nil
}

func (r *BuildRepository) UpdateCurrentStepIndex(ctx context.Context, id string, currentStepIndex int) (domain.Build, error) {
	const query = `
		UPDATE builds
		SET current_step_index = $2
		WHERE id = $1
		RETURNING id, project_id, status, created_at, queued_at, started_at, finished_at, current_step_index, error_message
	`

	build, err := scanBuild(r.db.QueryRowContext(ctx, query, id, currentStepIndex))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Build{}, repository.ErrBuildNotFound
		}
		return domain.Build{}, err
	}

	return build, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanBuild(scanner rowScanner) (domain.Build, error) {
	var build domain.Build
	var status string
	var queuedAt sql.NullTime
	var startedAt sql.NullTime
	var finishedAt sql.NullTime
	var errorMessage sql.NullString

	err := scanner.Scan(
		&build.ID,
		&build.ProjectID,
		&status,
		&build.CreatedAt,
		&queuedAt,
		&startedAt,
		&finishedAt,
		&build.CurrentStepIndex,
		&errorMessage,
	)
	if err != nil {
		return domain.Build{}, err
	}

	build.Status = domain.BuildStatus(status)
	if queuedAt.Valid {
		queued := queuedAt.Time
		build.QueuedAt = &queued
	}
	if startedAt.Valid {
		started := startedAt.Time
		build.StartedAt = &started
	}
	if finishedAt.Valid {
		finished := finishedAt.Time
		build.FinishedAt = &finished
	}
	if errorMessage.Valid {
		errMsg := errorMessage.String
		build.ErrorMessage = &errMsg
	}

	return build, nil
}

func scanStep(scanner rowScanner) (domain.BuildStep, error) {
	var step domain.BuildStep
	var status string
	var command string
	var argsRaw []byte
	var envRaw []byte
	var workingDir string
	var timeoutSeconds int
	var workerID sql.NullString
	var startedAt sql.NullTime
	var finishedAt sql.NullTime
	var exitCode sql.NullInt64
	var stdout sql.NullString
	var stderr sql.NullString
	var errorMessage sql.NullString

	err := scanner.Scan(
		&step.ID,
		&step.BuildID,
		&step.StepIndex,
		&step.Name,
		&command,
		&argsRaw,
		&envRaw,
		&workingDir,
		&timeoutSeconds,
		&status,
		&workerID,
		&startedAt,
		&finishedAt,
		&exitCode,
		&stdout,
		&stderr,
		&errorMessage,
	)
	if err != nil {
		return domain.BuildStep{}, err
	}

	step.Command = command
	if len(argsRaw) > 0 {
		if err := json.Unmarshal(argsRaw, &step.Args); err != nil {
			return domain.BuildStep{}, err
		}
	} else {
		step.Args = []string{}
	}
	if len(envRaw) > 0 {
		if err := json.Unmarshal(envRaw, &step.Env); err != nil {
			return domain.BuildStep{}, err
		}
	} else {
		step.Env = map[string]string{}
	}
	step.WorkingDir = workingDir
	step.TimeoutSeconds = timeoutSeconds
	step.Status = domain.BuildStepStatus(status)
	if workerID.Valid {
		worker := workerID.String
		step.WorkerID = &worker
	}
	if startedAt.Valid {
		started := startedAt.Time
		step.StartedAt = &started
	}
	if finishedAt.Valid {
		finished := finishedAt.Time
		step.FinishedAt = &finished
	}
	if exitCode.Valid {
		exit := int(exitCode.Int64)
		step.ExitCode = &exit
	}
	if stdout.Valid {
		stdoutValue := stdout.String
		step.Stdout = &stdoutValue
	}
	if stderr.Valid {
		stderrValue := stderr.String
		step.Stderr = &stderrValue
	}
	if errorMessage.Valid {
		errMsg := errorMessage.String
		step.ErrorMessage = &errMsg
	}

	return step, nil
}
