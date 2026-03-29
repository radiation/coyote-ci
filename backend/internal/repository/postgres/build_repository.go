package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

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
		INSERT INTO builds (id, project_id, status, created_at, current_step_index, pipeline_config_yaml, pipeline_name, pipeline_source, repo_url, ref, commit_sha)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
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
		build.PipelineConfigYAML,
		build.PipelineName,
		build.PipelineSource,
		build.RepoURL,
		build.Ref,
		build.CommitSHA,
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
		INSERT INTO builds (id, project_id, status, created_at, queued_at, current_step_index, error_message, pipeline_config_yaml, pipeline_name, pipeline_source, repo_url, ref, commit_sha)
		VALUES ($1, $2, 'queued', $3, COALESCE($4, NOW()), 0, NULL, $5, $6, $7, $8, $9, $10)
		RETURNING ` + buildColumns + `
	`

	build, err = scanBuild(tx.QueryRowContext(ctx, createQuery, build.ID, build.ProjectID, build.CreatedAt, build.QueuedAt, build.PipelineConfigYAML, build.PipelineName, build.PipelineSource, build.RepoURL, build.Ref, build.CommitSHA))
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
				claim_token,
				claimed_at,
				lease_expires_at,
				started_at,
				finished_at,
				exit_code,
				stdout,
				stderr,
				error_message
			)
			VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7::jsonb, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20)
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
				step.ClaimToken,
				step.ClaimedAt,
				step.LeaseExpiresAt,
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
	query := `
		SELECT ` + buildListColumns + `
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
		build, err := scanBuildList(rows)
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
	query := `
		SELECT ` + buildColumns + `
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
	query := `
		UPDATE builds
		SET status = $2,
			queued_at = CASE WHEN $2 = 'queued' THEN COALESCE(queued_at, NOW()) ELSE queued_at END,
			started_at = CASE WHEN $2 = 'running' THEN COALESCE(started_at, NOW()) ELSE started_at END,
			finished_at = CASE WHEN $2 IN ('success', 'failed') THEN NOW() ELSE finished_at END,
			error_message = CASE WHEN $2 = 'failed' THEN $3 ELSE NULL END
		WHERE id = $1
		RETURNING ` + buildColumns + `
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

	queueQuery := `
		UPDATE builds
		SET status = 'queued',
			queued_at = COALESCE(queued_at, NOW()),
			current_step_index = 0,
			error_message = NULL
		WHERE id = $1
		RETURNING ` + buildColumns + `
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
				claim_token,
				claimed_at,
				lease_expires_at,
				started_at,
				finished_at,
				exit_code,
				stdout,
				stderr,
				error_message
			)
			VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7::jsonb, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20)
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
				step.ClaimToken,
				step.ClaimedAt,
				step.LeaseExpiresAt,
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

func (r *BuildRepository) UpdateCurrentStepIndex(ctx context.Context, id string, currentStepIndex int) (domain.Build, error) {
	query := `
		UPDATE builds
		SET current_step_index = $2
		WHERE id = $1
		RETURNING ` + buildColumns + `
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
