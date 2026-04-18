package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type ExecutionJobRepository struct {
	db *sql.DB
}

func NewExecutionJobRepository(db *sql.DB) *ExecutionJobRepository {
	return &ExecutionJobRepository{db: db}
}

func (r *ExecutionJobRepository) CreateJobsForBuild(ctx context.Context, jobs []domain.ExecutionJob) ([]domain.ExecutionJob, error) {
	if len(jobs) == 0 {
		return []domain.ExecutionJob{}, nil
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	const query = `
		INSERT INTO build_jobs (
			id, build_id, step_id, node_id, group_name, depends_on_node_ids, name, step_index, attempt_number, retry_of_job_id, lineage_root_job_id, status, queue_name, image, working_dir,
			command_json, env_json, timeout_seconds, pipeline_file_path, context_dir,
			source_repo_url, source_commit_sha, source_ref_name, source_archive_uri, source_archive_digest,
			spec_version, spec_digest, resolved_spec_json, claim_token, claimed_by, claim_expires_at,
			created_at, started_at, finished_at, error_message, exit_code, output_refs_json
		)
		VALUES (
			$1, $2, $3, $4, $5, $6::jsonb, $7, $8, $9, $10, $11, $12, $13, $14, $15,
			$16::jsonb, $17::jsonb, $18, $19, $20,
			$21, $22, $23, $24, $25,
			$26, $27, $28::jsonb, $29, $30, $31,
			$32, $33, $34, $35, $36, $37::jsonb
		)
		RETURNING ` + executionJobColumns + `
	`

	out := make([]domain.ExecutionJob, 0, len(jobs))
	for _, job := range jobs {
		commandJSON, marshalErr := json.Marshal(job.Command)
		if marshalErr != nil {
			return nil, marshalErr
		}
		envJSON, marshalErr := json.Marshal(job.Environment)
		if marshalErr != nil {
			return nil, marshalErr
		}
		specJSON := strings.TrimSpace(job.ResolvedSpecJSON)
		if specJSON == "" {
			specJSON = "{}"
		}
		outputRefsJSON, marshalErr := json.Marshal(normalizeOutputRefs(job.OutputRefs))
		if marshalErr != nil {
			return nil, marshalErr
		}
		nodeDepsJSON, marshalErr := json.Marshal(normalizeNodeIDSlice(job.DependsOnNodeIDs))
		if marshalErr != nil {
			return nil, marshalErr
		}
		nodeID := normalizeNodeID(job.NodeID, job.StepIndex)

		created, scanErr := scanExecutionJob(tx.QueryRowContext(ctx, query,
			job.ID,
			job.BuildID,
			job.StepID,
			nodeID,
			job.GroupName,
			string(nodeDepsJSON),
			job.Name,
			job.StepIndex,
			normalizeAttemptNumber(job.AttemptNumber),
			job.RetryOfJobID,
			job.LineageRootJobID,
			string(job.Status),
			job.QueueName,
			job.Image,
			job.WorkingDir,
			string(commandJSON),
			string(envJSON),
			job.TimeoutSeconds,
			job.PipelineFilePath,
			job.ContextDir,
			nullableString(job.Source.RepositoryURL),
			job.Source.CommitSHA,
			job.Source.RefName,
			job.Source.ArchiveURI,
			job.Source.ArchiveDigest,
			job.SpecVersion,
			job.SpecDigest,
			specJSON,
			job.ClaimToken,
			job.ClaimedBy,
			job.ClaimExpiresAt,
			job.CreatedAt,
			job.StartedAt,
			job.FinishedAt,
			job.ErrorMessage,
			job.ExitCode,
			string(outputRefsJSON),
		))
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, created)
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}

	return out, nil
}

func (r *ExecutionJobRepository) GetJobsByBuildID(ctx context.Context, buildID string) (jobs []domain.ExecutionJob, err error) {
	const query = `
		SELECT ` + executionJobColumns + `
		FROM build_jobs
		WHERE build_id = $1
		ORDER BY step_index ASC, attempt_number ASC, created_at ASC
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

	jobs = make([]domain.ExecutionJob, 0)
	for rows.Next() {
		job, scanErr := scanExecutionJob(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return jobs, nil
}

func (r *ExecutionJobRepository) GetJobByID(ctx context.Context, id string) (domain.ExecutionJob, error) {
	const query = `SELECT ` + executionJobColumns + ` FROM build_jobs WHERE id = $1`
	job, err := scanExecutionJob(r.db.QueryRowContext(ctx, query, id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ExecutionJob{}, repository.ErrExecutionJobNotFound
		}
		return domain.ExecutionJob{}, err
	}
	return job, nil
}

func (r *ExecutionJobRepository) GetJobByStepID(ctx context.Context, stepID string) (domain.ExecutionJob, error) {
	const query = `SELECT ` + executionJobColumns + ` FROM build_jobs WHERE step_id = $1 ORDER BY attempt_number DESC, created_at DESC LIMIT 1`
	job, err := scanExecutionJob(r.db.QueryRowContext(ctx, query, stepID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ExecutionJob{}, repository.ErrExecutionJobNotFound
		}
		return domain.ExecutionJob{}, err
	}
	return job, nil
}

func (r *ExecutionJobRepository) ClaimNextRunnableJob(ctx context.Context, claim repository.StepClaim) (domain.ExecutionJob, bool, error) {
	query := `
		WITH candidate AS (
			SELECT bj.id
			FROM build_jobs AS bj
			INNER JOIN builds AS b ON b.id = bj.build_id
			WHERE b.status = 'running'
			  AND (bj.status = 'queued' OR (bj.status = 'running' AND bj.claim_expires_at IS NOT NULL AND bj.claim_expires_at <= $1))
			  AND (
					(
						NULLIF(BTRIM(COALESCE(bj.node_id, '')), '') IS NOT NULL
						AND NOT EXISTS (
							SELECT 1
							FROM jsonb_array_elements_text(COALESCE(bj.depends_on_node_ids, '[]'::jsonb)) AS dep(node_id)
							LEFT JOIN build_steps upstream
								ON upstream.build_id = bj.build_id
							   AND upstream.node_id = dep.node_id
							WHERE upstream.id IS NULL OR upstream.status <> 'success'
						)
					)
					OR (
						NULLIF(BTRIM(COALESCE(bj.node_id, '')), '') IS NULL
						AND NOT EXISTS (
							SELECT 1
							FROM build_steps previous
							WHERE previous.build_id = bj.build_id
							  AND previous.step_index < bj.step_index
							  AND previous.status <> 'success'
						)
					)
			  )
			ORDER BY bj.created_at ASC, bj.step_index ASC, bj.attempt_number ASC, bj.id ASC
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		UPDATE build_jobs AS j
		SET status = 'running',
			claimed_by = $2,
			claim_token = $3,
			claim_expires_at = $4,
			started_at = COALESCE(j.started_at, $1)
		FROM candidate AS c
		WHERE j.id = c.id
		RETURNING ` + executionJobColumnsQualifiedWithJ + `
	`

	job, err := scanExecutionJob(r.db.QueryRowContext(ctx, query, claim.ClaimedAt, claim.WorkerID, claim.ClaimToken, claim.LeaseExpiresAt))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ExecutionJob{}, false, nil
		}
		return domain.ExecutionJob{}, false, err
	}
	return job, true, nil
}

func (r *ExecutionJobRepository) ClaimJobByStepID(ctx context.Context, stepID string, claim repository.StepClaim) (domain.ExecutionJob, bool, error) {
	query := `
		WITH candidate AS (
			SELECT bj.id
			FROM build_jobs AS bj
			WHERE bj.step_id = $1
			  AND bj.status IN ('queued', 'running')
			ORDER BY bj.attempt_number DESC, bj.created_at DESC
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		UPDATE build_jobs AS j
		SET status = 'running',
			claimed_by = $2,
			claim_token = $3,
			claim_expires_at = $4,
			started_at = COALESCE(j.started_at, $5)
		FROM candidate AS c
		WHERE j.id = c.id
		RETURNING ` + executionJobColumnsQualifiedWithJ + `
	`

	job, err := scanExecutionJob(r.db.QueryRowContext(ctx, query, stepID, claim.WorkerID, claim.ClaimToken, claim.LeaseExpiresAt, claim.ClaimedAt))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ExecutionJob{}, false, nil
		}
		return domain.ExecutionJob{}, false, err
	}
	return job, true, nil
}

func (r *ExecutionJobRepository) RenewJobLease(ctx context.Context, jobID string, claimToken string, leaseExpiresAt time.Time) (domain.ExecutionJob, repository.StepCompletionOutcome, error) {
	const renewQuery = `
		UPDATE build_jobs
		SET claim_expires_at = $3
		WHERE id = $1
		  AND status = 'running'
		  AND claim_token = $2
		RETURNING ` + executionJobColumns + `
	`

	job, err := scanExecutionJob(r.db.QueryRowContext(ctx, renewQuery, jobID, claimToken, leaseExpiresAt))
	if err == nil {
		return job, repository.StepCompletionCompleted, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return domain.ExecutionJob{}, repository.StepCompletionInvalidTransition, err
	}

	existing, currentErr := r.GetJobByID(ctx, jobID)
	if currentErr != nil {
		if errors.Is(currentErr, repository.ErrExecutionJobNotFound) {
			return domain.ExecutionJob{}, repository.StepCompletionInvalidTransition, repository.ErrExecutionJobNotFound
		}
		return domain.ExecutionJob{}, repository.StepCompletionInvalidTransition, currentErr
	}
	if existing.Status == domain.ExecutionJobStatusSuccess || existing.Status == domain.ExecutionJobStatusFailed {
		return existing, repository.StepCompletionDuplicateTerminal, nil
	}
	if existing.Status == domain.ExecutionJobStatusRunning {
		return existing, repository.StepCompletionStaleClaim, nil
	}
	return existing, repository.StepCompletionInvalidTransition, nil
}

func (r *ExecutionJobRepository) CompleteJobSuccess(ctx context.Context, jobID string, claimToken string, finishedAt time.Time, exitCode int, outputRefs []domain.ArtifactRef) (domain.ExecutionJob, repository.StepCompletionOutcome, error) {
	return r.completeJob(ctx, jobID, claimToken, domain.ExecutionJobStatusSuccess, nil, &exitCode, finishedAt, outputRefs)
}

func (r *ExecutionJobRepository) CompleteJobFailure(ctx context.Context, jobID string, claimToken string, finishedAt time.Time, errorMessage string, exitCode *int, outputRefs []domain.ArtifactRef) (domain.ExecutionJob, repository.StepCompletionOutcome, error) {
	msg := strings.TrimSpace(errorMessage)
	if msg == "" {
		msg = "step execution failed"
	}
	return r.completeJob(ctx, jobID, claimToken, domain.ExecutionJobStatusFailed, &msg, exitCode, finishedAt, outputRefs)
}

func (r *ExecutionJobRepository) completeJob(ctx context.Context, jobID string, claimToken string, status domain.ExecutionJobStatus, errorMessage *string, exitCode *int, finishedAt time.Time, outputRefs []domain.ArtifactRef) (domain.ExecutionJob, repository.StepCompletionOutcome, error) {
	outputRefsJSON, err := json.Marshal(normalizeOutputRefs(outputRefs))
	if err != nil {
		return domain.ExecutionJob{}, repository.StepCompletionInvalidTransition, err
	}

	const query = `
		UPDATE build_jobs
		SET status = $3,
			finished_at = $4,
			error_message = $5,
			exit_code = $6,
			output_refs_json = $7::jsonb,
			claim_token = NULL,
			claimed_by = NULL,
			claim_expires_at = NULL
		WHERE id = $1
		  AND status = 'running'
		  AND claim_token = $2
		RETURNING ` + executionJobColumns + `
	`

	job, scanErr := scanExecutionJob(r.db.QueryRowContext(ctx, query, jobID, claimToken, string(status), finishedAt, errorMessage, exitCode, string(outputRefsJSON)))
	if scanErr == nil {
		return job, repository.StepCompletionCompleted, nil
	}
	if !errors.Is(scanErr, sql.ErrNoRows) {
		return domain.ExecutionJob{}, repository.StepCompletionInvalidTransition, scanErr
	}

	existing, currentErr := r.GetJobByID(ctx, jobID)
	if currentErr != nil {
		if errors.Is(currentErr, repository.ErrExecutionJobNotFound) {
			return domain.ExecutionJob{}, repository.StepCompletionInvalidTransition, repository.ErrExecutionJobNotFound
		}
		return domain.ExecutionJob{}, repository.StepCompletionInvalidTransition, currentErr
	}
	if existing.Status == domain.ExecutionJobStatusSuccess || existing.Status == domain.ExecutionJobStatusFailed {
		return existing, repository.StepCompletionDuplicateTerminal, nil
	}
	if existing.Status == domain.ExecutionJobStatusRunning {
		return existing, repository.StepCompletionStaleClaim, nil
	}
	return existing, repository.StepCompletionInvalidTransition, nil
}

func nullableString(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func normalizeOutputRefs(refs []domain.ArtifactRef) []domain.ArtifactRef {
	if refs == nil {
		return []domain.ArtifactRef{}
	}
	return refs
}

func normalizeAttemptNumber(value int) int {
	if value < 1 {
		return 1
	}
	return value
}
