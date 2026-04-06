package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

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
		INSERT INTO builds (id, project_id, job_id, status, created_at, current_step_index, attempt_number, rerun_of_build_id, rerun_from_step_index, pipeline_config_yaml, pipeline_name, pipeline_source, pipeline_path, repo_url, ref, commit_sha, trigger_kind, scm_provider, event_type, trigger_repository_owner, trigger_repository_name, trigger_repository_url, trigger_raw_ref, trigger_ref, trigger_ref_type, trigger_ref_name, trigger_deleted, trigger_commit_sha, trigger_delivery_id, trigger_actor)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29, $30)
	`

	if build.CurrentStepIndex < 0 {
		build.CurrentStepIndex = 0
	}
	if build.AttemptNumber <= 0 {
		build.AttemptNumber = 1
	}
	build.Trigger = domain.NormalizeBuildTrigger(build.Trigger)

	_, err := r.db.ExecContext(
		ctx,
		query,
		build.ID,
		build.ProjectID,
		build.JobID,
		string(build.Status),
		build.CreatedAt,
		build.CurrentStepIndex,
		build.AttemptNumber,
		build.RerunOfBuildID,
		build.RerunFromStepIdx,
		build.PipelineConfigYAML,
		build.PipelineName,
		build.PipelineSource,
		build.PipelinePath,
		build.RepoURL,
		build.Ref,
		build.CommitSHA,
		string(build.Trigger.Kind),
		build.Trigger.SCMProvider,
		build.Trigger.EventType,
		build.Trigger.RepositoryOwner,
		build.Trigger.RepositoryName,
		build.Trigger.RepositoryURL,
		build.Trigger.RawRef,
		build.Trigger.Ref,
		build.Trigger.RefType,
		build.Trigger.RefName,
		build.Trigger.Deleted,
		build.Trigger.CommitSHA,
		build.Trigger.DeliveryID,
		build.Trigger.Actor,
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
		INSERT INTO builds (id, project_id, job_id, status, created_at, queued_at, current_step_index, attempt_number, rerun_of_build_id, rerun_from_step_index, error_message, pipeline_config_yaml, pipeline_name, pipeline_source, pipeline_path, repo_url, ref, commit_sha, trigger_kind, scm_provider, event_type, trigger_repository_owner, trigger_repository_name, trigger_repository_url, trigger_raw_ref, trigger_ref, trigger_ref_type, trigger_ref_name, trigger_deleted, trigger_commit_sha, trigger_delivery_id, trigger_actor)
		VALUES ($1, $2, $3, 'queued', $4, COALESCE($5, NOW()), 0, $6, $7, $8, NULL, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29)
		RETURNING ` + buildColumns + `
	`
	if build.AttemptNumber <= 0 {
		build.AttemptNumber = 1
	}
	build.Trigger = domain.NormalizeBuildTrigger(build.Trigger)

	build, err = scanBuild(tx.QueryRowContext(ctx, createQuery, build.ID, build.ProjectID, build.JobID, build.CreatedAt, build.QueuedAt, build.AttemptNumber, build.RerunOfBuildID, build.RerunFromStepIdx, build.PipelineConfigYAML, build.PipelineName, build.PipelineSource, build.PipelinePath, build.RepoURL, build.Ref, build.CommitSHA, string(build.Trigger.Kind), build.Trigger.SCMProvider, build.Trigger.EventType, build.Trigger.RepositoryOwner, build.Trigger.RepositoryName, build.Trigger.RepositoryURL, build.Trigger.RawRef, build.Trigger.Ref, build.Trigger.RefType, build.Trigger.RefName, build.Trigger.Deleted, build.Trigger.CommitSHA, build.Trigger.DeliveryID, build.Trigger.Actor))
	if err != nil {
		return domain.Build{}, err
	}

	if len(steps) > 0 {
		if err = insertSteps(ctx, tx, build.ID, steps); err != nil {
			return domain.Build{}, err
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

func (r *BuildRepository) ListPaged(ctx context.Context, params repository.ListParams) (builds []domain.Build, err error) {
	limit, offset := clampPageParams(params)
	query := `
		SELECT ` + buildListColumns + `
		FROM builds
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
	`

	rows, err := r.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	builds = make([]domain.Build, 0, limit)
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

func (r *BuildRepository) ListByJobID(ctx context.Context, jobID string) (builds []domain.Build, err error) {
	query := `
		SELECT ` + buildListColumns + `
		FROM builds
		WHERE job_id = $1
		ORDER BY created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, jobID)
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

func (r *BuildRepository) UpdateSourceCommitSHA(ctx context.Context, id string, commitSHA string) (domain.Build, error) {
	query := `
		UPDATE builds
		SET commit_sha = $2
		WHERE id = $1
		RETURNING ` + buildColumns + `
	`

	build, err := scanBuild(r.db.QueryRowContext(ctx, query, id, strings.TrimSpace(commitSHA)))
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
		if err = insertSteps(ctx, tx, id, steps); err != nil {
			return domain.Build{}, err
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

// insertSteps inserts build steps within an existing transaction.
func insertSteps(ctx context.Context, tx *sql.Tx, buildID string, steps []domain.BuildStep) error {
	const insertStepQuery = `
		INSERT INTO build_steps (
			id,
			build_id,
			step_index,
			name,
			image,
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
			error_message,
			artifact_paths
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8::jsonb, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22::jsonb)
	`

	for _, step := range steps {
		argsJSON, marshalErr := json.Marshal(step.Args)
		if marshalErr != nil {
			return marshalErr
		}
		envJSON, marshalErr := json.Marshal(step.Env)
		if marshalErr != nil {
			return marshalErr
		}
		artifactPaths := step.ArtifactPaths
		if artifactPaths == nil {
			artifactPaths = []string{}
		}
		artifactPathsJSON, marshalErr := json.Marshal(artifactPaths)
		if marshalErr != nil {
			return marshalErr
		}

		if _, err := tx.ExecContext(
			ctx,
			insertStepQuery,
			step.ID,
			buildID,
			step.StepIndex,
			step.Name,
			step.Image,
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
			string(artifactPathsJSON),
		); err != nil {
			return err
		}
	}

	return nil
}
