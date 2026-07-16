package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type SCMStatusDeliveryRepository struct {
	db *sql.DB
}

func NewSCMStatusDeliveryRepository(db *sql.DB) *SCMStatusDeliveryRepository {
	return &SCMStatusDeliveryRepository{db: db}
}

const scmStatusDeliveryColumns = `id, build_id, build_attempt_number, build_created_at, provider, repository_owner, repository_name, commit_sha, context_name, desired_state, last_sent_state, description, details_url, status, attempts, max_attempts, last_attempt_at, next_attempt_at, claimed_at, claim_expires_at, claimed_by, failure_category, failure_reason, last_error, sent_at, superseded_at, created_at, updated_at`

const scmStatusDeliveryInsertClaimQuery = `
	INSERT INTO scm_status_deliveries (
		id,
		build_id,
		build_attempt_number,
		build_created_at,
		provider,
		repository_owner,
		repository_name,
		commit_sha,
		context_name,
		desired_state,
		last_sent_state,
		description,
		details_url,
		status,
		attempts,
		max_attempts,
		last_attempt_at,
		next_attempt_at,
		claimed_at,
		claim_expires_at,
		claimed_by,
		failure_category,
		failure_reason,
		last_error,
		sent_at,
		superseded_at,
		created_at,
		updated_at
	)
	VALUES (
		$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NULL, $11, $12,
		$13, $14, $15, $16, NULL, $17, $18, $19, NULL, NULL, NULL, NULL, NULL,
		$20, $21
	)
	ON CONFLICT (provider, repository_owner, repository_name, commit_sha, context_name) DO NOTHING
	RETURNING ` + scmStatusDeliveryColumns + `
`

const scmStatusDeliverySelectForClaimQuery = `
	SELECT ` + scmStatusDeliveryColumns + `
	FROM scm_status_deliveries
	WHERE provider = $1 AND repository_owner = $2 AND repository_name = $3 AND commit_sha = $4 AND context_name = $5
	FOR UPDATE
`

const scmStatusDeliveryUpdateClaimQuery = `
	UPDATE scm_status_deliveries
	SET status = $2,
		attempts = attempts + 1,
		last_attempt_at = $3,
		next_attempt_at = NULL,
		claimed_at = $3,
		claim_expires_at = $4,
		claimed_by = $5,
		updated_at = $3
	WHERE id = $1
	RETURNING ` + scmStatusDeliveryColumns + `
`

const scmStatusDeliveryReplaceClaimQuery = `
	UPDATE scm_status_deliveries
	SET build_id = $2,
		build_attempt_number = $3,
		build_created_at = $4,
		desired_state = $5,
		description = $6,
		details_url = $7,
		status = 'sending',
		attempts = 1,
		max_attempts = $8,
		last_attempt_at = $9,
		next_attempt_at = NULL,
		claimed_at = $9,
		claim_expires_at = $10,
		claimed_by = $11,
		failure_category = NULL,
		failure_reason = NULL,
		last_error = NULL,
		sent_at = NULL,
		superseded_at = NULL,
		last_sent_state = $12,
		updated_at = $9
	WHERE id = $1
	RETURNING ` + scmStatusDeliveryColumns + `
`

const scmStatusDeliverySelectByIDQuery = `
	SELECT ` + scmStatusDeliveryColumns + `
	FROM scm_status_deliveries
	WHERE id = $1
`

const scmStatusDeliverySelectByKeyQuery = `
	SELECT ` + scmStatusDeliveryColumns + `
	FROM scm_status_deliveries
	WHERE provider = $1 AND repository_owner = $2 AND repository_name = $3 AND commit_sha = $4 AND context_name = $5
`

const scmStatusDeliveryListRecoverableQuery = `
	WITH recoverable AS (
		SELECT ` + scmStatusDeliveryColumns + `,
			next_attempt_at AS recover_at
		FROM scm_status_deliveries
		WHERE status = 'retry_waiting'
		  AND next_attempt_at IS NOT NULL
		  AND next_attempt_at <= $1
		UNION ALL
		SELECT ` + scmStatusDeliveryColumns + `,
			claim_expires_at AS recover_at
		FROM scm_status_deliveries
		WHERE status = 'sending'
		  AND claim_expires_at IS NOT NULL
		  AND claim_expires_at <= $1
	)
	SELECT ` + scmStatusDeliveryColumns + `
	FROM recoverable
	ORDER BY recover_at ASC, id ASC
	LIMIT $2
`

const scmStatusDeliveryMarkSentQuery = `
	UPDATE scm_status_deliveries
	SET status = 'sent',
		last_sent_state = desired_state,
		updated_at = $4,
		sent_at = $4,
		next_attempt_at = NULL,
		claimed_at = NULL,
		claim_expires_at = NULL,
		claimed_by = NULL,
		failure_category = NULL,
		failure_reason = NULL,
		last_error = NULL,
		superseded_at = NULL
	WHERE id = $1
	  AND status = 'sending'
	  AND claimed_by = $2
	  AND claimed_at = $3
	RETURNING ` + scmStatusDeliveryColumns + `
`

const scmStatusDeliveryRecordFailureQuery = `
	UPDATE scm_status_deliveries
	SET status = $4,
		attempts = CASE
			WHEN $4 = 'failed_exhausted' AND attempts < max_attempts THEN max_attempts
			ELSE attempts
		END,
		updated_at = $5,
		next_attempt_at = $6,
		claimed_at = NULL,
		claim_expires_at = NULL,
		claimed_by = NULL,
		failure_category = $7,
		failure_reason = $8,
		last_error = $9,
		superseded_at = NULL
	WHERE id = $1
	  AND status = 'sending'
	  AND claimed_by = $2
	  AND claimed_at = $3
	RETURNING ` + scmStatusDeliveryColumns + `
`

const scmStatusDeliveryMarkSupersededQuery = `
	UPDATE scm_status_deliveries
	SET status = 'superseded',
		updated_at = $4,
		next_attempt_at = NULL,
		claimed_at = NULL,
		claim_expires_at = NULL,
		claimed_by = NULL,
		failure_category = 'permanent',
		failure_reason = $5,
		last_error = $6,
		sent_at = NULL,
		superseded_at = $4
	WHERE id = $1
	  AND ($2::text IS NULL OR claimed_by = $2)
	  AND ($3::timestamptz IS NULL OR claimed_at = $3)
	RETURNING ` + scmStatusDeliveryColumns + `
`

func (r *SCMStatusDeliveryRepository) AcquireForDelivery(ctx context.Context, input repository.SCMStatusDeliveryClaimInput) (repository.SCMStatusDeliveryClaimResult, error) {
	delivery, now, claimOwner, claimDuration, err := normalizePostgresSCMStatusDeliveryClaimInput(input)
	if err != nil {
		return repository.SCMStatusDeliveryClaimResult{}, err
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return repository.SCMStatusDeliveryClaimResult{}, err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if strings.TrimSpace(delivery.ID) == "" {
		delivery.ID = uuid.NewString()
	}
	if delivery.CreatedAt.IsZero() {
		delivery.CreatedAt = now
	}
	claimExpiresAt := now.Add(claimDuration)
	created, err := scanSCMStatusDelivery(tx.QueryRowContext(
		ctx,
		scmStatusDeliveryInsertClaimQuery,
		delivery.ID,
		delivery.BuildID,
		delivery.BuildAttempt,
		delivery.BuildCreatedAt,
		delivery.Provider,
		delivery.RepositoryOwner,
		delivery.RepositoryName,
		delivery.CommitSHA,
		delivery.Context,
		string(delivery.DesiredState),
		delivery.Description,
		nullableOptionalStringLocal(delivery.DetailsURL),
		string(domain.SCMStatusDeliveryStatusSending),
		1,
		input.MaxAttempts,
		now,
		now,
		claimExpiresAt,
		claimOwner,
		delivery.CreatedAt,
		now,
	))
	if err == nil {
		if commitErr := tx.Commit(); commitErr != nil {
			return repository.SCMStatusDeliveryClaimResult{}, commitErr
		}
		return repository.SCMStatusDeliveryClaimResult{Delivery: created, Outcome: repository.SCMStatusDeliveryClaimOutcomeCreatedClaimed}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return repository.SCMStatusDeliveryClaimResult{}, err
	}

	existing, err := scanSCMStatusDelivery(tx.QueryRowContext(
		ctx,
		scmStatusDeliverySelectForClaimQuery,
		delivery.Provider,
		delivery.RepositoryOwner,
		delivery.RepositoryName,
		delivery.CommitSHA,
		delivery.Context,
	))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return repository.SCMStatusDeliveryClaimResult{}, fmt.Errorf("scm status delivery acquire conflict did not resolve to an existing row")
		}
		return repository.SCMStatusDeliveryClaimResult{}, err
	}

	ownerComparison := repository.CompareSCMStatusDeliveryOwners(existing, delivery)
	if ownerComparison > 0 {
		return repository.SCMStatusDeliveryClaimResult{Delivery: existing, Outcome: repository.SCMStatusDeliveryClaimOutcomeSuperseded}, nil
	}
	if ownerComparison < 0 {
		reassertAfter := repository.SCMStatusDeliveryReassertAfterReplacement(existing, now)
		replaced, replaceErr := scanSCMStatusDelivery(tx.QueryRowContext(
			ctx,
			scmStatusDeliveryReplaceClaimQuery,
			existing.ID,
			delivery.BuildID,
			delivery.BuildAttempt,
			delivery.BuildCreatedAt,
			string(delivery.DesiredState),
			delivery.Description,
			nullableOptionalStringLocal(delivery.DetailsURL),
			input.MaxAttempts,
			now,
			claimExpiresAt,
			claimOwner,
			nil,
		))
		if replaceErr != nil {
			return repository.SCMStatusDeliveryClaimResult{}, replaceErr
		}
		if commitErr := tx.Commit(); commitErr != nil {
			return repository.SCMStatusDeliveryClaimResult{}, commitErr
		}
		return repository.SCMStatusDeliveryClaimResult{Delivery: replaced, Outcome: repository.SCMStatusDeliveryClaimOutcomeCreatedClaimed, ReassertAfter: reassertAfter}, nil
	}
	if repository.SCMStatusDeliveryIncomingStateObsolete(existing, delivery) {
		return repository.SCMStatusDeliveryClaimResult{Delivery: existing, Outcome: repository.SCMStatusDeliveryClaimOutcomeSuperseded}, nil
	}
	if repository.SCMStatusDeliveryShouldReplaceCurrentState(existing, delivery) {
		reassertAfter := repository.SCMStatusDeliveryReassertAfterReplacement(existing, now)
		replaced, replaceErr := scanSCMStatusDelivery(tx.QueryRowContext(
			ctx,
			scmStatusDeliveryReplaceClaimQuery,
			existing.ID,
			delivery.BuildID,
			delivery.BuildAttempt,
			delivery.BuildCreatedAt,
			string(delivery.DesiredState),
			delivery.Description,
			nullableOptionalStringLocal(delivery.DetailsURL),
			input.MaxAttempts,
			now,
			claimExpiresAt,
			claimOwner,
			nullableSCMCommitStatusStateLocal(existing.LastSentState),
		))
		if replaceErr != nil {
			return repository.SCMStatusDeliveryClaimResult{}, replaceErr
		}
		if commitErr := tx.Commit(); commitErr != nil {
			return repository.SCMStatusDeliveryClaimResult{}, commitErr
		}
		return repository.SCMStatusDeliveryClaimResult{Delivery: replaced, Outcome: repository.SCMStatusDeliveryClaimOutcomeCreatedClaimed, ReassertAfter: reassertAfter}, nil
	}

	claimOutcome := repository.SCMStatusDeliveryClaimOutcomeFromExisting(existing, now)
	if claimOutcome != repository.SCMStatusDeliveryClaimOutcomeRetryClaimed && claimOutcome != repository.SCMStatusDeliveryClaimOutcomeStaleClaimReclaimed {
		return repository.SCMStatusDeliveryClaimResult{Delivery: existing, Outcome: claimOutcome}, nil
	}

	claimed, err := scanSCMStatusDelivery(tx.QueryRowContext(
		ctx,
		scmStatusDeliveryUpdateClaimQuery,
		existing.ID,
		string(domain.SCMStatusDeliveryStatusSending),
		now,
		claimExpiresAt,
		claimOwner,
	))
	if err != nil {
		return repository.SCMStatusDeliveryClaimResult{}, err
	}
	if commitErr := tx.Commit(); commitErr != nil {
		return repository.SCMStatusDeliveryClaimResult{}, commitErr
	}
	return repository.SCMStatusDeliveryClaimResult{Delivery: claimed, Outcome: claimOutcome}, nil
}

func (r *SCMStatusDeliveryRepository) ListRecoverable(ctx context.Context, input repository.SCMStatusDeliveryRecoverableScanInput) (result []domain.SCMStatusDelivery, err error) {
	now := input.Now.UTC()
	if now.IsZero() {
		return nil, errors.New("scm status delivery recoverable scan time is required")
	}
	if input.Limit <= 0 {
		return nil, errors.New("scm status delivery recoverable scan limit must be positive")
	}

	rows, err := r.db.QueryContext(ctx, scmStatusDeliveryListRecoverableQuery, now, input.Limit)
	if err != nil {
		return nil, err
	}
	defer func() {
		closeErr := rows.Close()
		if err == nil && closeErr != nil {
			err = closeErr
		}
	}()

	result = make([]domain.SCMStatusDelivery, 0, input.Limit)
	for rows.Next() {
		delivery, scanErr := scanSCMStatusDelivery(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, delivery)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, rowsErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *SCMStatusDeliveryRepository) MarkSent(ctx context.Context, input repository.SCMStatusDeliveryMarkSentInput) (repository.SCMStatusDeliveryUpdateResult, error) {
	updated, err := scanSCMStatusDelivery(r.db.QueryRowContext(
		ctx,
		scmStatusDeliveryMarkSentQuery,
		strings.TrimSpace(input.DeliveryID),
		strings.TrimSpace(input.ClaimOwner),
		input.ClaimedAt.UTC(),
		input.SentAt.UTC(),
	))
	if err == nil {
		return repository.SCMStatusDeliveryUpdateResult{Delivery: updated, Outcome: repository.SCMStatusDeliveryUpdateOutcomeUpdated}, nil
	}
	return r.resolveSCMStatusDeliveryUpdateConflict(ctx, input.DeliveryID, err)
}

func (r *SCMStatusDeliveryRepository) RecordRetryableFailure(ctx context.Context, input repository.SCMStatusDeliveryRecordFailureInput) (repository.SCMStatusDeliveryUpdateResult, error) {
	return r.recordFailure(ctx, input, domain.SCMStatusDeliveryStatusRetryWaiting)
}

func (r *SCMStatusDeliveryRepository) RecordPermanentFailure(ctx context.Context, input repository.SCMStatusDeliveryRecordFailureInput) (repository.SCMStatusDeliveryUpdateResult, error) {
	return r.recordFailure(ctx, input, domain.SCMStatusDeliveryStatusFailedPermanent)
}

func (r *SCMStatusDeliveryRepository) RecordExhaustedFailure(ctx context.Context, input repository.SCMStatusDeliveryRecordFailureInput) (repository.SCMStatusDeliveryUpdateResult, error) {
	return r.recordFailure(ctx, input, domain.SCMStatusDeliveryStatusFailedExhausted)
}

func (r *SCMStatusDeliveryRepository) MarkSuperseded(ctx context.Context, input repository.SCMStatusDeliveryMarkSupersededInput) (repository.SCMStatusDeliveryUpdateResult, error) {
	updated, err := scanSCMStatusDelivery(r.db.QueryRowContext(
		ctx,
		scmStatusDeliveryMarkSupersededQuery,
		strings.TrimSpace(input.DeliveryID),
		nullableOptionalStringLocal(input.ClaimOwner),
		nullableTimePointerLocal(input.ClaimedAt),
		input.SupersededAt.UTC(),
		nullableTrimmedStringLocal(input.Reason),
		nullableOptionalStringLocal(input.LastError),
	))
	if err == nil {
		return repository.SCMStatusDeliveryUpdateResult{Delivery: updated, Outcome: repository.SCMStatusDeliveryUpdateOutcomeUpdated}, nil
	}
	return r.resolveSCMStatusDeliveryUpdateConflict(ctx, input.DeliveryID, err)
}

func (r *SCMStatusDeliveryRepository) GetByKey(ctx context.Context, provider string, repositoryOwner string, repositoryName string, commitSHA string, contextName string) (domain.SCMStatusDelivery, error) {
	delivery, err := scanSCMStatusDelivery(r.db.QueryRowContext(
		ctx,
		scmStatusDeliverySelectByKeyQuery,
		strings.ToLower(strings.TrimSpace(provider)),
		strings.TrimSpace(repositoryOwner),
		strings.TrimSpace(repositoryName),
		strings.TrimSpace(commitSHA),
		strings.TrimSpace(contextName),
	))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.SCMStatusDelivery{}, repository.ErrSCMStatusDeliveryNotFound
		}
		return domain.SCMStatusDelivery{}, err
	}
	return delivery, nil
}

func (r *SCMStatusDeliveryRepository) recordFailure(ctx context.Context, input repository.SCMStatusDeliveryRecordFailureInput, status domain.SCMStatusDeliveryStatus) (repository.SCMStatusDeliveryUpdateResult, error) {
	updated, err := scanSCMStatusDelivery(r.db.QueryRowContext(
		ctx,
		scmStatusDeliveryRecordFailureQuery,
		strings.TrimSpace(input.DeliveryID),
		strings.TrimSpace(input.ClaimOwner),
		input.ClaimedAt.UTC(),
		string(status),
		input.FailedAt.UTC(),
		nullableTimePointerLocal(input.NextAttemptAt),
		string(input.FailureCategory),
		nullableTrimmedStringLocal(input.FailureReason),
		nullableOptionalStringLocal(input.LastError),
	))
	if err == nil {
		return repository.SCMStatusDeliveryUpdateResult{Delivery: updated, Outcome: repository.SCMStatusDeliveryUpdateOutcomeUpdated}, nil
	}
	return r.resolveSCMStatusDeliveryUpdateConflict(ctx, input.DeliveryID, err)
}

func (r *SCMStatusDeliveryRepository) resolveSCMStatusDeliveryUpdateConflict(ctx context.Context, deliveryID string, updateErr error) (repository.SCMStatusDeliveryUpdateResult, error) {
	if !errors.Is(updateErr, sql.ErrNoRows) {
		return repository.SCMStatusDeliveryUpdateResult{}, updateErr
	}
	current, err := scanSCMStatusDelivery(r.db.QueryRowContext(ctx, scmStatusDeliverySelectByIDQuery, strings.TrimSpace(deliveryID)))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return repository.SCMStatusDeliveryUpdateResult{}, repository.ErrSCMStatusDeliveryNotFound
		}
		return repository.SCMStatusDeliveryUpdateResult{}, err
	}
	return repository.SCMStatusDeliveryUpdateResult{Delivery: current, Outcome: repository.SCMStatusDeliveryUpdateOutcomeLostClaim}, nil
}

func normalizePostgresSCMStatusDeliveryClaimInput(input repository.SCMStatusDeliveryClaimInput) (domain.SCMStatusDelivery, time.Time, string, time.Duration, error) {
	delivery := input.Delivery.Normalize()
	if err := delivery.ValidateIdentity(); err != nil {
		return domain.SCMStatusDelivery{}, time.Time{}, "", 0, err
	}
	now := input.Now.UTC()
	if now.IsZero() {
		return domain.SCMStatusDelivery{}, time.Time{}, "", 0, errors.New("scm status delivery claim time is required")
	}
	claimOwner := strings.TrimSpace(input.ClaimOwner)
	if claimOwner == "" {
		return domain.SCMStatusDelivery{}, time.Time{}, "", 0, errors.New("scm status delivery claim owner is required")
	}
	if input.ClaimDuration <= 0 {
		return domain.SCMStatusDelivery{}, time.Time{}, "", 0, errors.New("scm status delivery claim duration must be positive")
	}
	if input.MaxAttempts <= 0 {
		return domain.SCMStatusDelivery{}, time.Time{}, "", 0, errors.New("scm status delivery max attempts must be positive")
	}
	return delivery, now, claimOwner, input.ClaimDuration, nil
}

func scanSCMStatusDelivery(scanner rowScanner) (domain.SCMStatusDelivery, error) {
	var delivery domain.SCMStatusDelivery
	var buildCreatedAt time.Time
	var provider string
	var desiredState string
	var lastSentState sql.NullString
	var detailsURL sql.NullString
	var status string
	var failureCategory sql.NullString
	var failureReason sql.NullString
	var lastError sql.NullString
	var lastAttemptAt sql.NullTime
	var nextAttemptAt sql.NullTime
	var claimedAt sql.NullTime
	var claimExpiresAt sql.NullTime
	var claimedBy sql.NullString
	var sentAt sql.NullTime
	var supersededAt sql.NullTime

	err := scanner.Scan(
		&delivery.ID,
		&delivery.BuildID,
		&delivery.BuildAttempt,
		&buildCreatedAt,
		&provider,
		&delivery.RepositoryOwner,
		&delivery.RepositoryName,
		&delivery.CommitSHA,
		&delivery.Context,
		&desiredState,
		&lastSentState,
		&delivery.Description,
		&detailsURL,
		&status,
		&delivery.Attempts,
		&delivery.MaxAttempts,
		&lastAttemptAt,
		&nextAttemptAt,
		&claimedAt,
		&claimExpiresAt,
		&claimedBy,
		&failureCategory,
		&failureReason,
		&lastError,
		&sentAt,
		&supersededAt,
		&delivery.CreatedAt,
		&delivery.UpdatedAt,
	)
	if err != nil {
		return domain.SCMStatusDelivery{}, err
	}

	delivery.Provider = provider
	delivery.BuildCreatedAt = buildCreatedAt.UTC()
	delivery.DesiredState = domain.SCMCommitStatusState(desiredState)
	delivery.Status = domain.SCMStatusDeliveryStatus(status)
	if lastSentState.Valid {
		value := domain.SCMCommitStatusState(lastSentState.String)
		delivery.LastSentState = &value
	}
	if detailsURL.Valid {
		value := detailsURL.String
		delivery.DetailsURL = &value
	}
	if lastAttemptAt.Valid {
		value := lastAttemptAt.Time.UTC()
		delivery.LastAttemptAt = &value
	}
	if nextAttemptAt.Valid {
		value := nextAttemptAt.Time.UTC()
		delivery.NextAttemptAt = &value
	}
	if claimedAt.Valid {
		value := claimedAt.Time.UTC()
		delivery.ClaimedAt = &value
	}
	if claimExpiresAt.Valid {
		value := claimExpiresAt.Time.UTC()
		delivery.ClaimExpiresAt = &value
	}
	if claimedBy.Valid {
		value := claimedBy.String
		delivery.ClaimedBy = &value
	}
	if failureCategory.Valid {
		value := domain.SCMStatusDeliveryFailureCategory(failureCategory.String)
		delivery.FailureCategory = &value
	}
	if failureReason.Valid {
		value := failureReason.String
		delivery.FailureReason = &value
	}
	if lastError.Valid {
		value := lastError.String
		delivery.LastError = &value
	}
	if sentAt.Valid {
		value := sentAt.Time.UTC()
		delivery.SentAt = &value
	}
	if supersededAt.Valid {
		value := supersededAt.Time.UTC()
		delivery.SupersededAt = &value
	}

	return delivery.Normalize(), nil
}

func nullableSCMCommitStatusStateLocal(value *domain.SCMCommitStatusState) any {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(string(*value))
	if trimmed == "" {
		return nil
	}
	return trimmed
}
