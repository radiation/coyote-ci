package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

var errGuardedBuildUpdateNoop = errors.New("guarded build update no-op")

func (r *BuildRepository) CompleteClaimedStepAndAdvanceBuild(ctx context.Context, buildID string, stepIndex int, claimToken string, update repository.StepUpdate) (domain.BuildStep, repository.StepCompletionOutcome, error) {
	request := repository.DecideStepCompletion(domain.BuildStepStatusRunning, update.Status, true, true)
	if !request.AllowUpdate {
		return domain.BuildStep{}, request.Outcome, nil
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

	step, updated, err := completeClaimedStepRow(ctx, tx, buildID, stepIndex, claimToken, update)
	if err != nil {
		return domain.BuildStep{}, repository.StepCompletionInvalidTransition, err
	}

	if !updated {
		existingStep, outcome, resolveErr := resolveCompletionConflictTx(ctx, tx, buildID, stepIndex, true)
		if resolveErr != nil {
			return domain.BuildStep{}, repository.StepCompletionInvalidTransition, resolveErr
		}

		if outcome == repository.StepCompletionDuplicateTerminal || outcome == repository.StepCompletionStaleClaim {
			if commitErr := tx.Commit(); commitErr != nil {
				return domain.BuildStep{}, repository.StepCompletionInvalidTransition, commitErr
			}
			rollback = false
			return existingStep, outcome, nil
		}

		return domain.BuildStep{}, repository.StepCompletionInvalidTransition, nil
	}

	if err := advanceBuildAfterStepCompletionTx(ctx, tx, buildID, stepIndex, update.Status, step.ErrorMessage); err != nil {
		if errors.Is(err, errGuardedBuildUpdateNoop) {
			return domain.BuildStep{}, repository.StepCompletionInvalidTransition, nil
		}
		return domain.BuildStep{}, repository.StepCompletionInvalidTransition, err
	}

	if err := tx.Commit(); err != nil {
		return domain.BuildStep{}, repository.StepCompletionInvalidTransition, err
	}
	rollback = false
	return step, repository.StepCompletionCompleted, nil
}

func (r *BuildRepository) CompleteStepAndAdvanceBuild(ctx context.Context, buildID string, stepIndex int, update repository.StepUpdate) (domain.BuildStep, repository.StepCompletionOutcome, error) {
	request := repository.DecideStepCompletion(domain.BuildStepStatusRunning, update.Status, false, true)
	if !request.AllowUpdate {
		return domain.BuildStep{}, request.Outcome, nil
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

	step, updated, err := completeStepRow(ctx, tx, buildID, stepIndex, update)
	if err != nil {
		return domain.BuildStep{}, repository.StepCompletionInvalidTransition, err
	}

	if !updated {
		existingStep, outcome, resolveErr := resolveCompletionConflictTx(ctx, tx, buildID, stepIndex, false)
		if resolveErr != nil {
			return domain.BuildStep{}, repository.StepCompletionInvalidTransition, resolveErr
		}

		if outcome == repository.StepCompletionDuplicateTerminal {
			if commitErr := tx.Commit(); commitErr != nil {
				return domain.BuildStep{}, repository.StepCompletionInvalidTransition, commitErr
			}
			rollback = false
			return existingStep, outcome, nil
		}

		return domain.BuildStep{}, repository.StepCompletionInvalidTransition, nil
	}

	if err := advanceBuildAfterStepCompletionTx(ctx, tx, buildID, stepIndex, update.Status, step.ErrorMessage); err != nil {
		if errors.Is(err, errGuardedBuildUpdateNoop) {
			return domain.BuildStep{}, repository.StepCompletionInvalidTransition, nil
		}
		return domain.BuildStep{}, repository.StepCompletionInvalidTransition, err
	}

	if err := tx.Commit(); err != nil {
		return domain.BuildStep{}, repository.StepCompletionInvalidTransition, err
	}
	rollback = false
	return step, repository.StepCompletionCompleted, nil
}

func completeClaimedStepRow(ctx context.Context, tx *sql.Tx, buildID string, stepIndex int, claimToken string, update repository.StepUpdate) (domain.BuildStep, bool, error) {
	const query = `
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

	step, err := scanStep(tx.QueryRowContext(ctx, query, buildID, stepIndex, string(update.Status), update.WorkerID, update.StartedAt, update.FinishedAt, update.ExitCode, update.Stdout, update.Stderr, update.ErrorMessage, claimToken))
	if err == nil {
		return step, true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return domain.BuildStep{}, false, nil
	}
	return domain.BuildStep{}, false, err
}

func completeStepRow(ctx context.Context, tx *sql.Tx, buildID string, stepIndex int, update repository.StepUpdate) (domain.BuildStep, bool, error) {
	const query = `
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

	step, err := scanStep(tx.QueryRowContext(ctx, query, buildID, stepIndex, string(update.Status), update.WorkerID, update.StartedAt, update.FinishedAt, update.ExitCode, update.Stdout, update.Stderr, update.ErrorMessage))
	if err == nil {
		return step, true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return domain.BuildStep{}, false, nil
	}
	return domain.BuildStep{}, false, err
}

func resolveCompletionConflictTx(ctx context.Context, tx *sql.Tx, buildID string, stepIndex int, claimRequired bool) (domain.BuildStep, repository.StepCompletionOutcome, error) {
	const query = `
		SELECT id, build_id, step_index, name, command, args, env, working_dir, timeout_seconds, status, worker_id, claim_token, claimed_at, lease_expires_at, started_at, finished_at, exit_code, stdout, stderr, error_message
		FROM build_steps
		WHERE build_id = $1 AND step_index = $2
	`

	existingStep, err := scanStep(tx.QueryRowContext(ctx, query, buildID, stepIndex))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.BuildStep{}, repository.StepCompletionInvalidTransition, repository.ErrBuildNotFound
		}
		return domain.BuildStep{}, repository.StepCompletionInvalidTransition, err
	}

	outcome := repository.ClassifyCompletionConflict(existingStep.Status, claimRequired)
	if outcome == repository.StepCompletionDuplicateTerminal || outcome == repository.StepCompletionStaleClaim {
		return existingStep, outcome, nil
	}
	return domain.BuildStep{}, outcome, nil
}

func advanceBuildAfterStepCompletionTx(ctx context.Context, tx *sql.Tx, buildID string, stepIndex int, stepStatus domain.BuildStepStatus, errorMessage *string) error {
	if stepStatus == domain.BuildStepStatusFailed {
		decision := repository.DecideBuildAdvancement(stepStatus, stepIndex, true)
		if decision.FailBuild {
			if !domain.CanTransitionBuild(domain.BuildStatusRunning, domain.BuildStatusFailed) {
				return nil
			}
			return updateBuildFailedTx(ctx, tx, buildID, errorMessage)
		}
		return nil
	}

	const hasNextQuery = `
		SELECT EXISTS (
			SELECT 1 FROM build_steps
			WHERE build_id = $1 AND step_index > $2
		)
	`

	var hasNext bool
	if err := tx.QueryRowContext(ctx, hasNextQuery, buildID, stepIndex).Scan(&hasNext); err != nil {
		return err
	}

	decision := repository.DecideBuildAdvancement(stepStatus, stepIndex, hasNext)
	if decision.FailBuild {
		if !domain.CanTransitionBuild(domain.BuildStatusRunning, domain.BuildStatusFailed) {
			return nil
		}
		return updateBuildFailedTx(ctx, tx, buildID, errorMessage)
	}
	if decision.SucceedBuild {
		if !domain.CanTransitionBuild(domain.BuildStatusRunning, domain.BuildStatusSuccess) {
			return nil
		}
		return updateBuildSuccessTx(ctx, tx, buildID, decision.NextStepIndex)
	}
	if decision.AdvanceCurrentStepIndex {
		return advanceBuildCurrentStepTx(ctx, tx, buildID, decision.NextStepIndex)
	}

	return nil
}

func updateBuildFailedTx(ctx context.Context, tx *sql.Tx, buildID string, errorMessage *string) error {
	const query = `
		UPDATE builds
		SET status = 'failed',
			finished_at = COALESCE(finished_at, NOW()),
			error_message = CASE WHEN $2 IS NOT NULL THEN $2 ELSE error_message END
		WHERE id = $1
		  AND status = 'running'
	`

	result, err := tx.ExecContext(ctx, query, buildID, errorMessage)
	if err != nil {
		return err
	}
	return expectRowsAffected(result)
}

func updateBuildSuccessTx(ctx context.Context, tx *sql.Tx, buildID string, nextIndex int) error {
	const query = `
		UPDATE builds
		SET status = 'success',
			finished_at = COALESCE(finished_at, NOW()),
			error_message = NULL,
			current_step_index = GREATEST(current_step_index, $2)
		WHERE id = $1
		  AND status = 'running'
	`

	result, err := tx.ExecContext(ctx, query, buildID, nextIndex)
	if err != nil {
		return err
	}
	return expectRowsAffected(result)
}

func advanceBuildCurrentStepTx(ctx context.Context, tx *sql.Tx, buildID string, nextIndex int) error {
	const query = `
		UPDATE builds
		SET current_step_index = GREATEST(current_step_index, $2)
		WHERE id = $1
		  AND status = 'running'
	`

	result, err := tx.ExecContext(ctx, query, buildID, nextIndex)
	if err != nil {
		return err
	}
	return expectRowsAffected(result)
}

func expectRowsAffected(result sql.Result) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return errGuardedBuildUpdateNoop
	}
	return nil
}
