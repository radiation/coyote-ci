package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

var errGuardedBuildUpdateNoop = errors.New("guarded build update no-op")

func (r *BuildRepository) CompleteStep(ctx context.Context, request repository.CompleteStepRequest) (repository.CompleteStepResult, error) {
	completionReq := repository.DecideStepCompletion(domain.BuildStepStatusRunning, request.Update.Status, request.RequireClaim, true)
	if !completionReq.AllowUpdate {
		return repository.CompleteStepResult{Outcome: completionReq.Outcome}, nil
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return repository.CompleteStepResult{}, err
	}

	rollback := true
	defer func() {
		if rollback {
			_ = tx.Rollback()
		}
	}()

	claimToken := (*string)(nil)
	if request.RequireClaim {
		claimToken = &request.ClaimToken
	}

	step, updated, err := applyStepCompletionUpdateTx(ctx, tx, request.BuildID, request.StepIndex, claimToken, request.Update)
	if err != nil {
		return repository.CompleteStepResult{}, err
	}

	if !updated {
		existingStep, outcome, committed, resolveErr := resolveAndCommitConflictTx(ctx, tx, request.BuildID, request.StepIndex, request.RequireClaim, request.RequireClaim)
		if resolveErr != nil {
			return repository.CompleteStepResult{}, resolveErr
		}
		if committed {
			rollback = false
		}
		return repository.CompleteStepResult{Step: existingStep, Outcome: outcome}, nil
	}

	outcome, finalizeErr := advanceBuildAndCommitTx(ctx, tx, request.BuildID, request.StepIndex, request.Update.Status, step.ErrorMessage)
	if finalizeErr != nil {
		return repository.CompleteStepResult{}, finalizeErr
	}
	if outcome != repository.StepCompletionCompleted {
		return repository.CompleteStepResult{Outcome: outcome}, nil
	}
	rollback = false
	return repository.CompleteStepResult{Step: step, Outcome: outcome}, nil
}

func resolveAndCommitConflictTx(ctx context.Context, tx *sql.Tx, buildID string, stepIndex int, claimRequired bool, allowStale bool) (domain.BuildStep, repository.StepCompletionOutcome, bool, error) {
	existingStep, outcome, err := resolveCompletionConflictTx(ctx, tx, buildID, stepIndex, claimRequired)
	if err != nil {
		return domain.BuildStep{}, repository.StepCompletionInvalidTransition, false, err
	}

	if outcome == repository.StepCompletionDuplicateTerminal || (allowStale && outcome == repository.StepCompletionStaleClaim) {
		if commitErr := tx.Commit(); commitErr != nil {
			return domain.BuildStep{}, repository.StepCompletionInvalidTransition, false, commitErr
		}
		return existingStep, outcome, true, nil
	}

	return domain.BuildStep{}, repository.StepCompletionInvalidTransition, false, nil
}

func advanceBuildAndCommitTx(ctx context.Context, tx *sql.Tx, buildID string, stepIndex int, stepStatus domain.BuildStepStatus, errorMessage *string) (repository.StepCompletionOutcome, error) {
	if err := advanceBuildAfterStepCompletionTx(ctx, tx, buildID, stepIndex, stepStatus, errorMessage); err != nil {
		if errors.Is(err, errGuardedBuildUpdateNoop) {
			return repository.StepCompletionInvalidTransition, nil
		}
		return repository.StepCompletionInvalidTransition, err
	}

	if err := tx.Commit(); err != nil {
		return repository.StepCompletionInvalidTransition, err
	}

	return repository.StepCompletionCompleted, nil
}

func applyStepCompletionUpdateTx(ctx context.Context, tx *sql.Tx, buildID string, stepIndex int, claimToken *string, update repository.StepUpdate) (domain.BuildStep, bool, error) {
	claimTokenExpr := "CASE WHEN $3 IN ('success', 'failed') THEN NULL ELSE claim_token END"
	claimedAtExpr := "CASE WHEN $3 IN ('success', 'failed') THEN NULL ELSE claimed_at END"
	leaseExpr := "CASE WHEN $3 IN ('success', 'failed') THEN NULL ELSE lease_expires_at END"
	claimGuard := ""

	args := []any{buildID, stepIndex, string(update.Status), update.WorkerID, update.StartedAt, update.FinishedAt, update.ExitCode, update.Stdout, update.Stderr, update.ErrorMessage}
	if claimToken != nil {
		claimTokenExpr = "NULL"
		claimedAtExpr = "NULL"
		leaseExpr = "NULL"
		claimGuard = "\n\t\t  AND claim_token = $11"
		args = append(args, *claimToken)
	}

	query := fmt.Sprintf(`
		UPDATE build_steps
		SET status = $3,
			worker_id = COALESCE($4, worker_id),
			claim_token = %s,
			claimed_at = %s,
			lease_expires_at = %s,
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
		  AND status = 'running'%s
		RETURNING `+stepColumns+`
	`, claimTokenExpr, claimedAtExpr, leaseExpr, claimGuard)

	step, err := scanStep(tx.QueryRowContext(ctx, query, args...))
	if err == nil {
		return step, true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return domain.BuildStep{}, false, nil
	}
	return domain.BuildStep{}, false, err
}

func resolveCompletionConflictTx(ctx context.Context, tx *sql.Tx, buildID string, stepIndex int, claimRequired bool) (domain.BuildStep, repository.StepCompletionOutcome, error) {
	query := `
		SELECT ` + stepColumns + `
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
			error_message = COALESCE($2::text, error_message)
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
