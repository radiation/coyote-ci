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
	if stepStatus == domain.BuildStepStatusSuccess {
		nextIndex := stepIndex + 1
		if err := advanceBuildCurrentStepTx(ctx, tx, buildID, nextIndex); err != nil && !errors.Is(err, errGuardedBuildUpdateNoop) {
			return err
		}
	}

	stats, err := loadBuildStepProgressTx(ctx, tx, buildID)
	if err != nil {
		return err
	}

	if stats.totalCount > 0 && stats.successCount == stats.totalCount {
		if !domain.CanTransitionBuild(domain.BuildStatusRunning, domain.BuildStatusSuccess) {
			return nil
		}
		return updateBuildSuccessTx(ctx, tx, buildID, stepIndex+1)
	}

	if stats.runningCount > 0 {
		return nil
	}

	runnableExists, err := hasRunnablePendingStepTx(ctx, tx, buildID)
	if err != nil {
		return err
	}
	if runnableExists {
		return nil
	}

	if stats.failedCount > 0 || stats.pendingCount > 0 {
		if !domain.CanTransitionBuild(domain.BuildStatusRunning, domain.BuildStatusFailed) {
			return nil
		}
		message := errorMessage
		if message == nil {
			defaultMessage := "build cannot make further progress toward success"
			message = &defaultMessage
		}
		return updateBuildFailedTx(ctx, tx, buildID, message)
	}

	return nil
}

type buildStepProgressStats struct {
	totalCount   int
	successCount int
	failedCount  int
	pendingCount int
	runningCount int
}

func loadBuildStepProgressTx(ctx context.Context, tx *sql.Tx, buildID string) (buildStepProgressStats, error) {
	const query = `
		SELECT
			COUNT(*)::int AS total_count,
			COUNT(*) FILTER (WHERE status = 'success')::int AS success_count,
			COUNT(*) FILTER (WHERE status = 'failed')::int AS failed_count,
			COUNT(*) FILTER (WHERE status = 'pending')::int AS pending_count,
			COUNT(*) FILTER (WHERE status = 'running')::int AS running_count
		FROM build_steps
		WHERE build_id = $1
	`

	var stats buildStepProgressStats
	err := tx.QueryRowContext(ctx, query, buildID).Scan(
		&stats.totalCount,
		&stats.successCount,
		&stats.failedCount,
		&stats.pendingCount,
		&stats.runningCount,
	)
	if err != nil {
		return buildStepProgressStats{}, err
	}

	return stats, nil
}

func hasRunnablePendingStepTx(ctx context.Context, tx *sql.Tx, buildID string) (bool, error) {
	const query = `
		SELECT EXISTS (
			SELECT 1
			FROM build_steps AS bs
			WHERE bs.build_id = $1
			  AND bs.status = 'pending'
			  AND (
					(
						NULLIF(BTRIM(COALESCE(bs.node_id, '')), '') IS NOT NULL
						AND NOT EXISTS (
							SELECT 1
							FROM jsonb_array_elements_text(COALESCE(bs.depends_on_node_ids, '[]'::jsonb)) AS dep(node_id)
							LEFT JOIN build_steps upstream
								ON upstream.build_id = bs.build_id
							   AND upstream.node_id = dep.node_id
							WHERE upstream.id IS NULL OR upstream.status <> 'success'
						)
					)
					OR (
						NULLIF(BTRIM(COALESCE(bs.node_id, '')), '') IS NULL
						AND NOT EXISTS (
							SELECT 1
							FROM build_steps previous
							WHERE previous.build_id = bs.build_id
							  AND previous.step_index < bs.step_index
							  AND previous.status <> 'success'
						)
					)
			  )
		)
	`

	var exists bool
	if err := tx.QueryRowContext(ctx, query, buildID).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
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
