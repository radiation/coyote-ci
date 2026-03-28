package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func (r *BuildRepository) GetStepsByBuildID(ctx context.Context, buildID string) (steps []domain.BuildStep, err error) {
	const query = `
		SELECT id, build_id, step_index, name, command, args, env, working_dir, timeout_seconds, status, worker_id, claim_token, claimed_at, lease_expires_at, started_at, finished_at, exit_code, stdout, stderr, error_message
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
		RETURNING id, build_id, step_index, name, command, args, env, working_dir, timeout_seconds, status, worker_id, claim_token, claimed_at, lease_expires_at, started_at, finished_at, exit_code, stdout, stderr, error_message
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

func (r *BuildRepository) ClaimPendingStep(ctx context.Context, buildID string, stepIndex int, claim repository.StepClaim) (domain.BuildStep, bool, error) {
	const query = `
		UPDATE build_steps
		SET status = 'running',
			worker_id = $3,
			claim_token = $4,
			claimed_at = $5,
			lease_expires_at = $6,
			started_at = COALESCE(started_at, $5)
		WHERE build_id = $1
		  AND step_index = $2
		  AND status = 'pending'
		RETURNING id, build_id, step_index, name, command, args, env, working_dir, timeout_seconds, status, worker_id, claim_token, claimed_at, lease_expires_at, started_at, finished_at, exit_code, stdout, stderr, error_message
	`

	step, err := scanStep(r.db.QueryRowContext(ctx, query, buildID, stepIndex, claim.WorkerID, claim.ClaimToken, claim.ClaimedAt, claim.LeaseExpiresAt))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.BuildStep{}, false, nil
		}
		return domain.BuildStep{}, false, err
	}

	return step, true, nil
}

func (r *BuildRepository) ReclaimExpiredStep(ctx context.Context, buildID string, stepIndex int, reclaimBefore time.Time, claim repository.StepClaim) (domain.BuildStep, bool, error) {
	const query = `
		UPDATE build_steps
		SET worker_id = $4,
			claim_token = $5,
			claimed_at = $6,
			lease_expires_at = $7
		WHERE build_id = $1
		  AND step_index = $2
		  AND status = 'running'
		  AND lease_expires_at IS NOT NULL
		  AND lease_expires_at <= $3
		RETURNING id, build_id, step_index, name, command, args, env, working_dir, timeout_seconds, status, worker_id, claim_token, claimed_at, lease_expires_at, started_at, finished_at, exit_code, stdout, stderr, error_message
	`

	step, err := scanStep(r.db.QueryRowContext(ctx, query, buildID, stepIndex, reclaimBefore, claim.WorkerID, claim.ClaimToken, claim.ClaimedAt, claim.LeaseExpiresAt))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.BuildStep{}, false, nil
		}
		return domain.BuildStep{}, false, err
	}

	return step, true, nil
}

func (r *BuildRepository) RenewStepLease(ctx context.Context, buildID string, stepIndex int, claimToken string, leaseExpiresAt time.Time) (domain.BuildStep, repository.StepCompletionOutcome, error) {
	const renewQuery = `
		UPDATE build_steps
		SET lease_expires_at = $4
		WHERE build_id = $1
		  AND step_index = $2
		  AND status = 'running'
		  AND claim_token = $3
		RETURNING id, build_id, step_index, name, command, args, env, working_dir, timeout_seconds, status, worker_id, claim_token, claimed_at, lease_expires_at, started_at, finished_at, exit_code, stdout, stderr, error_message
	`

	step, err := scanStep(r.db.QueryRowContext(ctx, renewQuery, buildID, stepIndex, claimToken, leaseExpiresAt))
	if err == nil {
		return step, repository.StepCompletionCompleted, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return domain.BuildStep{}, repository.StepCompletionInvalidTransition, err
	}

	const currentStepQuery = `
		SELECT id, build_id, step_index, name, command, args, env, working_dir, timeout_seconds, status, worker_id, claim_token, claimed_at, lease_expires_at, started_at, finished_at, exit_code, stdout, stderr, error_message
		FROM build_steps
		WHERE build_id = $1 AND step_index = $2
	`

	existingStep, currentErr := scanStep(r.db.QueryRowContext(ctx, currentStepQuery, buildID, stepIndex))
	if currentErr != nil {
		if errors.Is(currentErr, sql.ErrNoRows) {
			return domain.BuildStep{}, repository.StepCompletionInvalidTransition, repository.ErrBuildNotFound
		}
		return domain.BuildStep{}, repository.StepCompletionInvalidTransition, currentErr
	}

	if domain.IsTerminalStepStatus(existingStep.Status) {
		return existingStep, repository.StepCompletionDuplicateTerminal, nil
	}
	if existingStep.Status == domain.BuildStepStatusRunning {
		return existingStep, repository.StepCompletionStaleClaim, nil
	}

	return domain.BuildStep{}, repository.StepCompletionInvalidTransition, nil
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
		RETURNING id, build_id, step_index, name, command, args, env, working_dir, timeout_seconds, status, worker_id, claim_token, claimed_at, lease_expires_at, started_at, finished_at, exit_code, stdout, stderr, error_message
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

func (r *BuildRepository) CompleteStepIfRunning(ctx context.Context, buildID string, stepIndex int, update repository.StepUpdate) (domain.BuildStep, bool, error) {
	if !domain.CanTransitionStep(domain.BuildStepStatusRunning, update.Status) {
		return domain.BuildStep{}, false, repository.ErrInvalidBuildStepTransition
	}

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
		WHERE build_id = $1
		  AND step_index = $2
		  AND status = 'running'
		RETURNING id, build_id, step_index, name, command, args, env, working_dir, timeout_seconds, status, worker_id, claim_token, claimed_at, lease_expires_at, started_at, finished_at, exit_code, stdout, stderr, error_message
	`

	step, err := scanStep(r.db.QueryRowContext(ctx, query, buildID, stepIndex, string(update.Status), update.WorkerID, update.StartedAt, update.FinishedAt, update.ExitCode, update.Stdout, update.Stderr, update.ErrorMessage))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.BuildStep{}, false, nil
		}
		return domain.BuildStep{}, false, err
	}

	return step, true, nil
}
