package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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
		WITH observed_build_number AS (
			UPDATE jobs
			SET next_build_number = GREATEST(next_build_number, $4 + 1)
			WHERE id = $3 AND $4 > 0
			RETURNING next_build_number
		),
		next_build_number AS (
			UPDATE jobs
			SET next_build_number = next_build_number + 1
			WHERE id = $3 AND $4 <= 0
			RETURNING next_build_number - 1 AS build_number
		)
		INSERT INTO builds (id, build_number, project_id, job_id, priority, status, created_at, current_step_index, attempt_number, rerun_of_build_id, rerun_from_step_index, pipeline_config_yaml, pipeline_name, pipeline_source, pipeline_path, repo_url, ref, commit_sha, trigger_kind, scm_provider, event_type, trigger_repository_owner, trigger_repository_name, trigger_repository_url, trigger_raw_ref, trigger_ref, trigger_ref_type, trigger_ref_name, trigger_deleted, trigger_commit_sha, trigger_delivery_id, trigger_actor, requested_image_ref, resolved_image_ref, image_source_kind, managed_image_id, managed_image_version_id)
		VALUES ($1, COALESCE(NULLIF($4, 0), (SELECT build_number FROM next_build_number), CASE WHEN $3 IS NULL THEN nextval('builds_build_number_seq') ELSE NULL END), $2, $3, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29, $30, $31, $32, $33, $34, $35, $36, $37)
		RETURNING ` + buildColumns + `
	`

	if build.CurrentStepIndex < 0 {
		build.CurrentStepIndex = 0
	}
	if build.AttemptNumber <= 0 {
		build.AttemptNumber = 1
	}
	build.Priority = domain.NormalizePriority(build.Priority)
	build.Trigger = domain.NormalizeBuildTrigger(build.Trigger)
	build = domain.NormalizeBuildMetadata(build)

	build, err := scanBuild(r.db.QueryRowContext(
		ctx,
		query,
		build.ID,
		build.ProjectID,
		build.JobID,
		build.BuildNumber,
		build.Priority,
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
		build.RequestedImageRef,
		build.ResolvedImageRef,
		string(defaultBuildImageSourceKind(build.ImageSourceKind)),
		build.ManagedImageID,
		build.ManagedImageVersionID,
	))
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
		WITH observed_build_number AS (
			UPDATE jobs
			SET next_build_number = GREATEST(next_build_number, $4 + 1)
			WHERE id = $3 AND $4 > 0
			RETURNING next_build_number
		),
		next_build_number AS (
			UPDATE jobs
			SET next_build_number = next_build_number + 1
			WHERE id = $3 AND $4 <= 0
			RETURNING next_build_number - 1 AS build_number
		)
		INSERT INTO builds (id, build_number, project_id, job_id, priority, status, created_at, queued_at, current_step_index, attempt_number, rerun_of_build_id, rerun_from_step_index, error_message, pipeline_config_yaml, pipeline_name, pipeline_source, pipeline_path, repo_url, ref, commit_sha, trigger_kind, scm_provider, event_type, trigger_repository_owner, trigger_repository_name, trigger_repository_url, trigger_raw_ref, trigger_ref, trigger_ref_type, trigger_ref_name, trigger_deleted, trigger_commit_sha, trigger_delivery_id, trigger_actor, requested_image_ref, resolved_image_ref, image_source_kind, managed_image_id, managed_image_version_id)
		VALUES ($1, COALESCE(NULLIF($4, 0), (SELECT build_number FROM next_build_number), CASE WHEN $3 IS NULL THEN nextval('builds_build_number_seq') ELSE NULL END), $2, $3, $5, 'queued', $6, COALESCE($7, NOW()), 0, $8, $9, $10, NULL, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29, $30, $31, $32, $33, $34, $35, $36)
		RETURNING ` + buildColumns + `
	`
	if build.AttemptNumber <= 0 {
		build.AttemptNumber = 1
	}
	build.Priority = domain.NormalizePriority(build.Priority)
	build.Trigger = domain.NormalizeBuildTrigger(build.Trigger)
	build = domain.NormalizeBuildMetadata(build)

	build, err = scanBuild(tx.QueryRowContext(ctx, createQuery, build.ID, build.ProjectID, build.JobID, build.BuildNumber, build.Priority, build.CreatedAt, build.QueuedAt, build.AttemptNumber, build.RerunOfBuildID, build.RerunFromStepIdx, build.PipelineConfigYAML, build.PipelineName, build.PipelineSource, build.PipelinePath, build.RepoURL, build.Ref, build.CommitSHA, string(build.Trigger.Kind), build.Trigger.SCMProvider, build.Trigger.EventType, build.Trigger.RepositoryOwner, build.Trigger.RepositoryName, build.Trigger.RepositoryURL, build.Trigger.RawRef, build.Trigger.Ref, build.Trigger.RefType, build.Trigger.RefName, build.Trigger.Deleted, build.Trigger.CommitSHA, build.Trigger.DeliveryID, build.Trigger.Actor, build.RequestedImageRef, build.ResolvedImageRef, string(defaultBuildImageSourceKind(build.ImageSourceKind)), build.ManagedImageID, build.ManagedImageVersionID))
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

func (r *BuildRepository) ListQueue(ctx context.Context, params repository.QueueListParams) (entries []domain.QueueEntry, err error) {
	query := `
		SELECT ` + queueEntryColumns + `
		FROM builds AS b
		LEFT JOIN projects AS p ON p.id::text = b.project_id
		LEFT JOIN jobs AS j ON j.id = b.job_id
		LEFT JOIN LATERAL (
			SELECT bj.claimed_by, bj.claim_expires_at
			FROM build_jobs AS bj
			WHERE bj.build_id = b.id
			  AND bj.status = 'running'
			ORDER BY bj.started_at ASC NULLS LAST, bj.created_at ASC, bj.id ASC
			LIMIT 1
		) AS running_job ON TRUE
		WHERE b.status IN ('queued', 'running')
	`
	args := make([]any, 0, 2)
	if projectID := strings.TrimSpace(params.ProjectID); projectID != "" {
		args = append(args, projectID)
		query += fmt.Sprintf("\n\t\tAND b.project_id = $%d", len(args))
	}
	if status := strings.TrimSpace(params.Status); status != "" {
		args = append(args, status)
		query += fmt.Sprintf("\n\t\tAND b.status = $%d", len(args))
	}
	query += `
		ORDER BY
			CASE b.status WHEN 'queued' THEN 0 WHEN 'running' THEN 1 ELSE 2 END ASC,
			CASE WHEN b.status = 'queued' THEN b.priority END DESC,
			CASE WHEN b.status = 'queued' THEN COALESCE(b.queued_at, b.created_at) END ASC,
			CASE WHEN b.status = 'running' THEN COALESCE(b.started_at, b.created_at) END ASC,
			b.created_at ASC,
			b.id ASC
	`

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	entries = make([]domain.QueueEntry, 0)
	for rows.Next() {
		entry, scanErr := scanQueueEntry(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		entries = append(entries, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return entries, nil
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

func (r *BuildRepository) ListActive(ctx context.Context) (builds []domain.Build, err error) {
	query := `
		SELECT ` + buildListColumns + `
		FROM builds
		WHERE status IN ($1, $2, $3)
		ORDER BY created_at DESC
	`

	rows, err := r.db.QueryContext(
		ctx,
		query,
		string(domain.BuildStatusPreparing),
		string(domain.BuildStatusQueued),
		string(domain.BuildStatusRunning),
	)
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
	`
	args := make([]any, 0, 3)
	if strings.TrimSpace(params.ProjectID) != "" {
		query += `
		WHERE project_id = $1`
		args = append(args, params.ProjectID)
	}
	query += `
		ORDER BY created_at DESC`
	args = append(args, limit, offset)
	query += fmt.Sprintf("\n\t\tLIMIT $%d OFFSET $%d", len(args)-1, len(args))

	rows, err := r.db.QueryContext(ctx, query, args...)
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

func (r *BuildRepository) ListLatestByJobIDs(ctx context.Context, jobIDs []string) (latest map[string]domain.Build, err error) {
	jobIDs = uniqueNonBlankStrings(jobIDs)
	if len(jobIDs) == 0 {
		return map[string]domain.Build{}, nil
	}

	args := make([]any, 0, len(jobIDs))
	placeholders := make([]string, 0, len(jobIDs))
	for idx, jobID := range jobIDs {
		args = append(args, jobID)
		placeholders = append(placeholders, fmt.Sprintf("$%d", idx+1))
	}

	query := `
		SELECT DISTINCT ON (b.job_id) ` + qualifyColumns("b", buildListColumns) + `
		FROM builds b
		WHERE b.job_id IN (` + strings.Join(placeholders, ", ") + `)
		ORDER BY b.job_id, b.created_at DESC, b.id ASC
	`

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	latest = make(map[string]domain.Build, len(jobIDs))
	for rows.Next() {
		build, scanErr := scanBuildList(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		if build.JobID != nil {
			latest[*build.JobID] = build
		}
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return latest, nil
}

func uniqueNonBlankStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	unique := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		unique = append(unique, trimmed)
	}
	return unique
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

func (r *BuildRepository) UpdateImageExecution(ctx context.Context, id string, requestedRef *string, resolvedRef *string, sourceKind domain.ImageSourceKind, managedImageID *string, managedImageVersionID *string) (domain.Build, error) {
	query := `
		UPDATE builds
		SET requested_image_ref = $2,
			resolved_image_ref = $3,
			image_source_kind = $4,
			managed_image_id = $5,
			managed_image_version_id = $6
		WHERE id = $1
		RETURNING ` + buildColumns + `
	`

	build, err := scanBuild(r.db.QueryRowContext(ctx, query,
		id,
		requestedRef,
		resolvedRef,
		string(defaultBuildImageSourceKind(sourceKind)),
		managedImageID,
		managedImageVersionID,
	))
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
			node_id,
			group_name,
			depends_on_node_ids,
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
			artifact_paths,
			cache_config,
			requested_image_ref,
			resolved_image_ref,
			image_source_kind,
			managed_image_id,
			managed_image_version_id
		)
		VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7, $8, $9, $10::jsonb, $11::jsonb, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25::jsonb, $26::jsonb, $27, $28, $29, $30, $31)
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
		dependsOnJSON, marshalErr := json.Marshal(normalizeNodeIDSlice(step.DependsOnNodes))
		if marshalErr != nil {
			return marshalErr
		}
		var cacheJSON []byte
		if step.Cache != nil {
			cacheJSON, marshalErr = json.Marshal(step.Cache)
			if marshalErr != nil {
				return marshalErr
			}
		} else {
			cacheJSON = []byte("null")
		}

		if _, err := tx.ExecContext(
			ctx,
			insertStepQuery,
			step.ID,
			buildID,
			step.StepIndex,
			normalizeNodeID(step.NodeID, step.StepIndex),
			step.GroupName,
			string(dependsOnJSON),
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
			string(cacheJSON),
			step.RequestedImageRef,
			step.ResolvedImageRef,
			string(defaultStepImageSourceKind(step.ImageSourceKind)),
			step.ManagedImageID,
			step.ManagedImageVersionID,
		); err != nil {
			return err
		}
	}

	return nil
}

func defaultBuildImageSourceKind(kind domain.ImageSourceKind) domain.ImageSourceKind {
	if strings.TrimSpace(string(kind)) == "" {
		return domain.ImageSourceKindExternal
	}
	return kind
}

func defaultStepImageSourceKind(kind domain.ImageSourceKind) domain.ImageSourceKind {
	if strings.TrimSpace(string(kind)) == "" {
		return domain.ImageSourceKindExternal
	}
	return kind
}
