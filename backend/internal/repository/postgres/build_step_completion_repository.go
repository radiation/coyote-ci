package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func (r *BuildRepository) CompleteClaimedStepAndAdvanceBuild(ctx context.Context, buildID string, stepIndex int, claimToken string, update repository.StepUpdate) (domain.BuildStep, repository.StepCompletionOutcome, error) {
	if !domain.CanTransitionStep(domain.BuildStepStatusRunning, update.Status) {
		return domain.BuildStep{}, repository.StepCompletionInvalidTransition, nil
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.BuildStep{}, repository.StepCompletionInvalidTransition, err
	}

	rollback := true
	defer func() {
		if rollback {
			_ = tx.Rollback()
		}
	}()

	const completeQuery = `
		UPDATE build_steps
		SET status = $3,
			worker_id = COALESCE($4, worker_id),
			claim_token = NULL,
			claimed_at = NULL,
			lease_expires_at = NULL,
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
		  AND claim_token = $11
		RETURNING id, build_id, step_index, name, command, args, env, working_dir, timeout_seconds, status, worker_id, claim_token, claimed_at, lease_expires_at, started_at, finished_at, exit_code, stdout, stderr, error_message
	`

	step, err := scanStep(tx.QueryRowContext(ctx, completeQuery, buildID, stepIndex, string(update.Status), update.WorkerID, update.StartedAt, update.FinishedAt, update.ExitCode, update.Stdout, update.Stderr, update.ErrorMessage, claimToken))
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return domain.BuildStep{}, repository.StepCompletionInvalidTransition, err
		}

		const currentStepQuery = `
			SELECT id, build_id, step_index, name, command, args, env, working_dir, timeout_seconds, status, worker_id, claim_token, claimed_at, lease_expires_at, started_at, finished_at, exit_code, stdout, stderr, error_message
			FROM build_steps
			WHERE build_id = $1 AND step_index = $2
		`

		existingStep, currentErr := scanStep(tx.QueryRowContext(ctx, currentStepQuery, buildID, stepIndex))
		if currentErr != nil {
			if errors.Is(currentErr, sql.ErrNoRows) {
				return domain.BuildStep{}, repository.StepCompletionInvalidTransition, repository.ErrBuildNotFound
			}
			return domain.BuildStep{}, repository.StepCompletionInvalidTransition, currentErr
		}

		if domain.IsTerminalStepStatus(existingStep.Status) {
			if commitErr := tx.Commit(); commitErr != nil {
				return domain.BuildStep{}, repository.StepCompletionInvalidTransition, commitErr
			}
			rollback = false
			return existingStep, repository.StepCompletionDuplicateTerminal, nil
		}

		if existingStep.Status == domain.BuildStepStatusRunning {
			if commitErr := tx.Commit(); commitErr != nil {
				return domain.BuildStep{}, repository.StepCompletionInvalidTransition, commitErr
			}
			rollback = false
			return existingStep, repository.StepCompletionStaleClaim, nil
		}

		return domain.BuildStep{}, repository.StepCompletionInvalidTransition, nil
	}

	if update.Status == domain.BuildStepStatusFailed {
		if !domain.CanTransitionBuild(domain.BuildStatusRunning, domain.BuildStatusFailed) {
			return domain.BuildStep{}, repository.StepCompletionInvalidTransition, nil
		}

		const failBuildQuery = `
			UPDATE builds
			SET status = 'failed',
				finished_at = COALESCE(finished_at, NOW()),
				error_message = CASE WHEN $2 IS NOT NULL THEN $2 ELSE error_message END
			WHERE id = $1
			  AND status = 'running'
		`

		result, execErr := tx.ExecContext(ctx, failBuildQuery, buildID, step.ErrorMessage)
		if execErr != nil {
			return domain.BuildStep{}, repository.StepCompletionInvalidTransition, execErr
		}

		affected, rowsErr := result.RowsAffected()
		if rowsErr != nil {
			return domain.BuildStep{}, repository.StepCompletionInvalidTransition, rowsErr
		}
		if affected == 0 {
			return domain.BuildStep{}, repository.StepCompletionInvalidTransition, nil
		}
	} else {
		const hasNextQuery = `
			SELECT EXISTS (
				SELECT 1 FROM build_steps
				WHERE build_id = $1 AND step_index > $2
			)
		`

		var hasNext bool
		if scanErr := tx.QueryRowContext(ctx, hasNextQuery, buildID, stepIndex).Scan(&hasNext); scanErr != nil {
			return domain.BuildStep{}, repository.StepCompletionInvalidTransition, scanErr
		}

		nextIndex := stepIndex + 1
		if hasNext {
			const advanceQuery = `
				UPDATE builds
				SET current_step_index = GREATEST(current_step_index, $2)
				WHERE id = $1
				  AND status = 'running'
			`

			result, execErr := tx.ExecContext(ctx, advanceQuery, buildID, nextIndex)
			if execErr != nil {
				return domain.BuildStep{}, repository.StepCompletionInvalidTransition, execErr
			}

			affected, rowsErr := result.RowsAffected()
			if rowsErr != nil {
				return domain.BuildStep{}, repository.StepCompletionInvalidTransition, rowsErr
			}
			if affected == 0 {
				return domain.BuildStep{}, repository.StepCompletionInvalidTransition, nil
			}
		} else {
			if !domain.CanTransitionBuild(domain.BuildStatusRunning, domain.BuildStatusSuccess) {
				return domain.BuildStep{}, repository.StepCompletionInvalidTransition, nil
			}

			const successQuery = `
				UPDATE builds
				SET status = 'success',
					finished_at = COALESCE(finished_at, NOW()),
					error_message = NULL,
					current_step_index = GREATEST(current_step_index, $2)
				WHERE id = $1
				  AND status = 'running'
			`

			result, execErr := tx.ExecContext(ctx, successQuery, buildID, nextIndex)
			if execErr != nil {
				return domain.BuildStep{}, repository.StepCompletionInvalidTransition, execErr
			}

			affected, rowsErr := result.RowsAffected()
			if rowsErr != nil {
				return domain.BuildStep{}, repository.StepCompletionInvalidTransition, rowsErr
			}
			if affected == 0 {
				return domain.BuildStep{}, repository.StepCompletionInvalidTransition, nil
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return domain.BuildStep{}, repository.StepCompletionInvalidTransition, err
	}
	rollback = false
	return step, repository.StepCompletionCompleted, nil
}

func (r *BuildRepository) CompleteStepAndAdvanceBuild(ctx context.Context, buildID string, stepIndex int, update repository.StepUpdate) (domain.BuildStep, repository.StepCompletionOutcome, error) {
	if !domain.CanTransitionStep(domain.BuildStepStatusRunning, update.Status) {
		return domain.BuildStep{}, repository.StepCompletionInvalidTransition, nil
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.BuildStep{}, repository.StepCompletionInvalidTransition, err
	}

	rollback := true
	defer func() {
		if rollback {
			_ = tx.Rollback()
		}
	}()

	const completeQuery = `
		UPDATE build_steps
		SET status = $3,
			worker_id = COALESCE($4, worker_id),
			claim_token = CASE WHEN $3 IN ('success', 'failed') THEN NULL ELSE claim_token END,
			claimed_at = CASE WHEN $3 IN ('success', 'failed') THEN NULL ELSE claimed_at END,
			lease_expires_at = CASE WHEN $3 IN ('success', 'failed') THEN NULL ELSE lease_expires_at END,
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

	step, err := scanStep(tx.QueryRowContext(ctx, completeQuery, buildID, stepIndex, string(update.Status), update.WorkerID, update.StartedAt, update.FinishedAt, update.ExitCode, update.Stdout, update.Stderr, update.ErrorMessage))
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return domain.BuildStep{}, repository.StepCompletionInvalidTransition, err
		}

		const currentStepQuery = `
			SELECT id, build_id, step_index, name, command, args, env, working_dir, timeout_seconds, status, worker_id, claim_token, claimed_at, lease_expires_at, started_at, finished_at, exit_code, stdout, stderr, error_message
			FROM build_steps
			WHERE build_id = $1 AND step_index = $2
		`

		existingStep, currentErr := scanStep(tx.QueryRowContext(ctx, currentStepQuery, buildID, stepIndex))
		if currentErr != nil {
			if errors.Is(currentErr, sql.ErrNoRows) {
				return domain.BuildStep{}, repository.StepCompletionInvalidTransition, repository.ErrBuildNotFound
			}
			return domain.BuildStep{}, repository.StepCompletionInvalidTransition, currentErr
		}

		if !domain.IsTerminalStepStatus(existingStep.Status) {
			return domain.BuildStep{}, repository.StepCompletionInvalidTransition, nil
		}

		if commitErr := tx.Commit(); commitErr != nil {
			return domain.BuildStep{}, repository.StepCompletionInvalidTransition, commitErr
		}
		rollback = false
		return existingStep, repository.StepCompletionDuplicateTerminal, nil
	}

	if update.Status == domain.BuildStepStatusFailed {
		if !domain.CanTransitionBuild(domain.BuildStatusRunning, domain.BuildStatusFailed) {
			return domain.BuildStep{}, repository.StepCompletionInvalidTransition, nil
		}

		const failBuildQuery = `
			UPDATE builds
			SET status = 'failed',
				finished_at = COALESCE(finished_at, NOW()),
				error_message = CASE WHEN $2 IS NOT NULL THEN $2 ELSE error_message END
			WHERE id = $1
			  AND status = 'running'
		`

		result, execErr := tx.ExecContext(ctx, failBuildQuery, buildID, step.ErrorMessage)
		if execErr != nil {
			return domain.BuildStep{}, repository.StepCompletionInvalidTransition, execErr
		}

		affected, rowsErr := result.RowsAffected()
		if rowsErr != nil {
			return domain.BuildStep{}, repository.StepCompletionInvalidTransition, rowsErr
		}
		if affected == 0 {
			return domain.BuildStep{}, repository.StepCompletionInvalidTransition, nil
		}
	} else {
		const hasNextQuery = `
			SELECT EXISTS (
				SELECT 1 FROM build_steps
				WHERE build_id = $1 AND step_index > $2
			)
		`

		var hasNext bool
		if scanErr := tx.QueryRowContext(ctx, hasNextQuery, buildID, stepIndex).Scan(&hasNext); scanErr != nil {
			return domain.BuildStep{}, repository.StepCompletionInvalidTransition, scanErr
		}

		nextIndex := stepIndex + 1
		if hasNext {
			const advanceQuery = `
				UPDATE builds
				SET current_step_index = GREATEST(current_step_index, $2)
				WHERE id = $1
				  AND status = 'running'
			`

			result, execErr := tx.ExecContext(ctx, advanceQuery, buildID, nextIndex)
			if execErr != nil {
				return domain.BuildStep{}, repository.StepCompletionInvalidTransition, execErr
			}

			affected, rowsErr := result.RowsAffected()
			if rowsErr != nil {
				return domain.BuildStep{}, repository.StepCompletionInvalidTransition, rowsErr
			}
			if affected == 0 {
				return domain.BuildStep{}, repository.StepCompletionInvalidTransition, nil
			}
		} else {
			if !domain.CanTransitionBuild(domain.BuildStatusRunning, domain.BuildStatusSuccess) {
				return domain.BuildStep{}, repository.StepCompletionInvalidTransition, nil
			}

			const successQuery = `
				UPDATE builds
				SET status = 'success',
					finished_at = COALESCE(finished_at, NOW()),
					error_message = NULL,
					current_step_index = GREATEST(current_step_index, $2)
				WHERE id = $1
				  AND status = 'running'
			`

			result, execErr := tx.ExecContext(ctx, successQuery, buildID, nextIndex)
			if execErr != nil {
				return domain.BuildStep{}, repository.StepCompletionInvalidTransition, execErr
			}

			affected, rowsErr := result.RowsAffected()
			if rowsErr != nil {
				return domain.BuildStep{}, repository.StepCompletionInvalidTransition, rowsErr
			}
			if affected == 0 {
				return domain.BuildStep{}, repository.StepCompletionInvalidTransition, nil
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return domain.BuildStep{}, repository.StepCompletionInvalidTransition, err
	}
	rollback = false
	return step, repository.StepCompletionCompleted, nil
}
