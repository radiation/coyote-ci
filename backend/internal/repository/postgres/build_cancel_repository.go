package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

// CancelBuild terminalizes a cancelable build and any cancellable steps/jobs in one transaction.
func (r *BuildRepository) CancelBuild(ctx context.Context, id string, reason string, canceledAt time.Time) (domain.Build, int, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Build{}, 0, err
	}

	rollback := true
	defer func() {
		if rollback {
			_ = tx.Rollback()
		}
	}()

	lockedBuild, err := scanBuild(tx.QueryRowContext(ctx, `
		SELECT `+buildColumns+`
		FROM builds
		WHERE id = $1
		FOR UPDATE
	`, id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Build{}, 0, repository.ErrBuildNotFound
		}
		return domain.Build{}, 0, err
	}

	if !domain.CanCancelBuild(lockedBuild.Status) {
		return domain.Build{}, 0, repository.ErrInvalidBuildStatusTransition
	}

	trimmedReason := strings.TrimSpace(reason)
	var reasonPtr *string
	if trimmedReason != "" {
		reasonPtr = &trimmedReason
	}

	stepResult, err := tx.ExecContext(ctx, `
		UPDATE build_steps
		SET status = 'canceled',
			claim_token = NULL,
			claimed_at = NULL,
			lease_expires_at = NULL,
			started_at = COALESCE(started_at, $2),
			finished_at = COALESCE(finished_at, $2),
			error_message = COALESCE($3::text, error_message)
		WHERE build_id = $1
		  AND status IN ('pending', 'running')
	`, id, canceledAt, reasonPtr)
	if err != nil {
		return domain.Build{}, 0, err
	}

	if _, updateJobsErr := tx.ExecContext(ctx, `
		UPDATE build_jobs
		SET status = 'canceled',
			claim_token = NULL,
			claimed_by = NULL,
			claim_expires_at = NULL,
			started_at = COALESCE(started_at, $2),
			finished_at = COALESCE(finished_at, $2),
			error_message = COALESCE($3::text, error_message)
		WHERE build_id = $1
		  AND status IN ('queued', 'running')
	`, id, canceledAt, reasonPtr); updateJobsErr != nil {
		return domain.Build{}, 0, updateJobsErr
	}

	updatedSteps := 0
	affected, err := stepResult.RowsAffected()
	if err != nil {
		return domain.Build{}, 0, err
	}
	updatedSteps = int(affected)

	canceledBuild, err := scanBuild(tx.QueryRowContext(ctx, `
		UPDATE builds
		SET status = 'canceled',
			finished_at = COALESCE(finished_at, $2),
			error_message = COALESCE($3::text, error_message)
		WHERE id = $1
		  AND status IN ('queued', 'preparing', 'running')
		RETURNING `+buildColumns+`
	`, id, canceledAt, reasonPtr))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Build{}, 0, repository.ErrInvalidBuildStatusTransition
		}
		return domain.Build{}, 0, err
	}

	if commitErr := tx.Commit(); commitErr != nil {
		return domain.Build{}, 0, commitErr
	}
	rollback = false
	return canceledBuild, updatedSteps, nil
}
