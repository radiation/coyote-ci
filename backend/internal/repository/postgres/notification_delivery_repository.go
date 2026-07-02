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

type NotificationDeliveryRepository struct {
	db *sql.DB
}

func NewNotificationDeliveryRepository(db *sql.DB) *NotificationDeliveryRepository {
	return &NotificationDeliveryRepository{db: db}
}

const notificationDeliveryColumns = `id, build_id, event_type, transport, destination_kind, destination_key, notification_target_id::text, recipient_user_id::text, slack_workspace_integration_id::text, recipient, status, attempts, max_attempts, last_attempt_at, next_attempt_at, claimed_at, claim_expires_at, claimed_by, failure_category, failure_reason, last_error, created_at, updated_at, sent_at`

const notificationDeliveryInsertClaimQuery = `
	INSERT INTO notification_deliveries (
		id,
		build_id,
		event_type,
		transport,
		destination_kind,
		destination_key,
		notification_target_id,
		recipient_user_id,
		slack_workspace_integration_id,
		recipient,
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
		created_at,
		updated_at,
		sent_at
	)
	VALUES (
		$1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
		$11, $12, $13, $14, NULL, $15, $16, $17, NULL, NULL, NULL,
		$18, $19, NULL
	)
	ON CONFLICT (build_id, event_type, transport, destination_key) DO NOTHING
	RETURNING ` + notificationDeliveryColumns + `
`

const notificationDeliverySelectForClaimQuery = `
	SELECT ` + notificationDeliveryColumns + `
	FROM notification_deliveries
	WHERE build_id = $1 AND event_type = $2 AND transport = $3 AND destination_key = $4
	FOR UPDATE
`

const notificationDeliveryUpdateClaimQuery = `
	UPDATE notification_deliveries
	SET status = $2,
		attempts = attempts + 1,
		last_attempt_at = $3,
		next_attempt_at = NULL,
		claimed_at = $3,
		claim_expires_at = $4,
		claimed_by = $5,
		updated_at = $3
	WHERE id = $1
	RETURNING ` + notificationDeliveryColumns + `
`

const notificationDeliverySelectByIDQuery = `
	SELECT ` + notificationDeliveryColumns + `
	FROM notification_deliveries
	WHERE id = $1
`

const notificationDeliverySelectByRecipientQuery = `
	SELECT ` + notificationDeliveryColumns + `
	FROM notification_deliveries
	WHERE build_id = $1 AND event_type = $2 AND recipient = $3
`

const notificationDeliveryListRecoverableQuery = `
	WITH recoverable AS (
		SELECT ` + notificationDeliveryColumns + `,
			next_attempt_at AS recover_at
		FROM notification_deliveries
		WHERE status = 'retry_waiting'
		  AND next_attempt_at IS NOT NULL
		  AND next_attempt_at <= $1
		UNION ALL
		SELECT ` + notificationDeliveryColumns + `,
			claim_expires_at AS recover_at
		FROM notification_deliveries
		WHERE status = 'sending'
		  AND claim_expires_at IS NOT NULL
		  AND claim_expires_at <= $1
	)
	SELECT ` + notificationDeliveryColumns + `
	FROM recoverable
	ORDER BY recover_at ASC, id ASC
	LIMIT $2
`

const notificationDeliveryMarkSentQuery = `
	UPDATE notification_deliveries
	SET status = 'sent',
		updated_at = $4,
		sent_at = $4,
		next_attempt_at = NULL,
		claimed_at = NULL,
		claim_expires_at = NULL,
		claimed_by = NULL,
		failure_category = NULL,
		failure_reason = NULL,
		last_error = NULL
	WHERE id = $1
	  AND status = 'sending'
	  AND claimed_by = $2
	  AND claimed_at = $3
	RETURNING ` + notificationDeliveryColumns + `
`

const notificationDeliveryRecordFailureQuery = `
	UPDATE notification_deliveries
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
		last_error = $9
	WHERE id = $1
	  AND status = 'sending'
	  AND claimed_by = $2
	  AND claimed_at = $3
	RETURNING ` + notificationDeliveryColumns + `
`

const notificationDeliveryAcquireLookupDelay = 10 * time.Millisecond

func (r *NotificationDeliveryRepository) AcquireForDelivery(ctx context.Context, input repository.NotificationDeliveryClaimInput) (repository.NotificationDeliveryClaimResult, error) {
	delivery, now, claimOwner, claimDuration, err := normalizePostgresNotificationClaimInput(input)
	if err != nil {
		return repository.NotificationDeliveryClaimResult{}, err
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return repository.NotificationDeliveryClaimResult{}, err
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
	created, err := scanNotificationDelivery(tx.QueryRowContext(
		ctx,
		notificationDeliveryInsertClaimQuery,
		delivery.ID,
		delivery.BuildID,
		string(delivery.EventType),
		string(delivery.Transport),
		string(delivery.DestinationKind),
		delivery.DestinationKey,
		nullableOptionalStringLocal(delivery.NotificationTargetID),
		nullableOptionalStringLocal(delivery.RecipientUserID),
		nullableOptionalStringLocal(delivery.SlackWorkspaceIntegrationID),
		delivery.Recipient,
		string(domain.NotificationDeliveryStatusSending),
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
			return repository.NotificationDeliveryClaimResult{}, commitErr
		}
		return repository.NotificationDeliveryClaimResult{Delivery: created, Outcome: repository.NotificationDeliveryClaimOutcomeCreatedClaimed}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return repository.NotificationDeliveryClaimResult{}, err
	}

	existing, err := scanNotificationDelivery(tx.QueryRowContext(
		ctx,
		notificationDeliverySelectForClaimQuery,
		delivery.BuildID,
		string(delivery.EventType),
		string(delivery.Transport),
		delivery.DestinationKey,
	))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return repository.NotificationDeliveryClaimResult{}, fmt.Errorf("notification delivery acquire conflict did not resolve to an existing row")
		}
		return repository.NotificationDeliveryClaimResult{}, err
	}

	claimOutcome := repository.NotificationDeliveryClaimOutcomeFromExisting(existing, now)
	if claimOutcome != repository.NotificationDeliveryClaimOutcomeRetryClaimed && claimOutcome != repository.NotificationDeliveryClaimOutcomeStaleClaimReclaimed {
		return repository.NotificationDeliveryClaimResult{Delivery: existing, Outcome: claimOutcome}, nil
	}

	claimed, err := scanNotificationDelivery(tx.QueryRowContext(
		ctx,
		notificationDeliveryUpdateClaimQuery,
		existing.ID,
		string(domain.NotificationDeliveryStatusSending),
		now,
		claimExpiresAt,
		claimOwner,
	))
	if err != nil {
		return repository.NotificationDeliveryClaimResult{}, err
	}
	if commitErr := tx.Commit(); commitErr != nil {
		return repository.NotificationDeliveryClaimResult{}, commitErr
	}
	return repository.NotificationDeliveryClaimResult{Delivery: claimed, Outcome: claimOutcome}, nil
}

func (r *NotificationDeliveryRepository) ListRecoverable(ctx context.Context, input repository.NotificationDeliveryRecoverableScanInput) (result []domain.NotificationDelivery, err error) {
	now := input.Now.UTC()
	if now.IsZero() {
		return nil, errors.New("notification delivery recoverable scan time is required")
	}
	if input.Limit <= 0 {
		return nil, errors.New("notification delivery recoverable scan limit must be positive")
	}

	rows, err := r.db.QueryContext(ctx, notificationDeliveryListRecoverableQuery, now, input.Limit)
	if err != nil {
		return nil, err
	}
	defer func() {
		closeErr := rows.Close()
		if err == nil && closeErr != nil {
			err = closeErr
		}
	}()

	result = make([]domain.NotificationDelivery, 0, input.Limit)
	for rows.Next() {
		delivery, scanErr := scanNotificationDelivery(rows)
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

func (r *NotificationDeliveryRepository) MarkSent(ctx context.Context, input repository.NotificationDeliveryMarkSentInput) (repository.NotificationDeliveryUpdateResult, error) {
	updated, err := scanNotificationDelivery(r.db.QueryRowContext(
		ctx,
		notificationDeliveryMarkSentQuery,
		strings.TrimSpace(input.DeliveryID),
		strings.TrimSpace(input.ClaimOwner),
		input.ClaimedAt.UTC(),
		input.SentAt.UTC(),
	))
	if err == nil {
		return repository.NotificationDeliveryUpdateResult{Delivery: updated, Outcome: repository.NotificationDeliveryUpdateOutcomeUpdated}, nil
	}
	return r.resolveNotificationDeliveryUpdateConflict(ctx, input.DeliveryID, err)
}

func (r *NotificationDeliveryRepository) RecordRetryableFailure(ctx context.Context, input repository.NotificationDeliveryRecordFailureInput) (repository.NotificationDeliveryUpdateResult, error) {
	return r.recordFailure(ctx, input, domain.NotificationDeliveryStatusRetryWaiting)
}

func (r *NotificationDeliveryRepository) RecordPermanentFailure(ctx context.Context, input repository.NotificationDeliveryRecordFailureInput) (repository.NotificationDeliveryUpdateResult, error) {
	return r.recordFailure(ctx, input, domain.NotificationDeliveryStatusFailedPermanent)
}

func (r *NotificationDeliveryRepository) RecordExhaustedFailure(ctx context.Context, input repository.NotificationDeliveryRecordFailureInput) (repository.NotificationDeliveryUpdateResult, error) {
	return r.recordFailure(ctx, input, domain.NotificationDeliveryStatusFailedExhausted)
}

func (r *NotificationDeliveryRepository) GetByBuildEventRecipient(ctx context.Context, buildID string, eventType domain.NotificationEventType, recipient string) (domain.NotificationDelivery, error) {
	delivery, err := scanNotificationDelivery(r.db.QueryRowContext(
		ctx,
		notificationDeliverySelectByRecipientQuery,
		strings.TrimSpace(buildID),
		strings.TrimSpace(string(eventType)),
		strings.TrimSpace(recipient),
	))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.NotificationDelivery{}, repository.ErrNotificationDeliveryNotFound
		}
		return domain.NotificationDelivery{}, err
	}
	return delivery, nil
}

func (r *NotificationDeliveryRepository) recordFailure(ctx context.Context, input repository.NotificationDeliveryRecordFailureInput, status domain.NotificationDeliveryStatus) (repository.NotificationDeliveryUpdateResult, error) {
	updated, err := scanNotificationDelivery(r.db.QueryRowContext(
		ctx,
		notificationDeliveryRecordFailureQuery,
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
		return repository.NotificationDeliveryUpdateResult{Delivery: updated, Outcome: repository.NotificationDeliveryUpdateOutcomeUpdated}, nil
	}
	return r.resolveNotificationDeliveryUpdateConflict(ctx, input.DeliveryID, err)
}

func (r *NotificationDeliveryRepository) resolveNotificationDeliveryUpdateConflict(ctx context.Context, deliveryID string, updateErr error) (repository.NotificationDeliveryUpdateResult, error) {
	if !errors.Is(updateErr, sql.ErrNoRows) {
		return repository.NotificationDeliveryUpdateResult{}, updateErr
	}
	current, err := scanNotificationDelivery(r.db.QueryRowContext(ctx, notificationDeliverySelectByIDQuery, strings.TrimSpace(deliveryID)))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return repository.NotificationDeliveryUpdateResult{}, repository.ErrNotificationDeliveryNotFound
		}
		return repository.NotificationDeliveryUpdateResult{}, err
	}
	return repository.NotificationDeliveryUpdateResult{Delivery: current, Outcome: repository.NotificationDeliveryUpdateOutcomeLostClaim}, nil
}

func normalizePostgresNotificationClaimInput(input repository.NotificationDeliveryClaimInput) (domain.NotificationDelivery, time.Time, string, time.Duration, error) {
	delivery := input.Delivery.Normalize()
	if err := delivery.ValidateIdentity(); err != nil {
		return domain.NotificationDelivery{}, time.Time{}, "", 0, err
	}
	now := input.Now.UTC()
	if now.IsZero() {
		return domain.NotificationDelivery{}, time.Time{}, "", 0, errors.New("notification delivery claim time is required")
	}
	claimOwner := strings.TrimSpace(input.ClaimOwner)
	if claimOwner == "" {
		return domain.NotificationDelivery{}, time.Time{}, "", 0, errors.New("notification delivery claim owner is required")
	}
	if input.ClaimDuration <= 0 {
		return domain.NotificationDelivery{}, time.Time{}, "", 0, errors.New("notification delivery claim duration must be positive")
	}
	if input.MaxAttempts <= 0 {
		return domain.NotificationDelivery{}, time.Time{}, "", 0, errors.New("notification delivery max attempts must be positive")
	}
	return delivery, now, claimOwner, input.ClaimDuration, nil
}

func scanNotificationDelivery(scanner rowScanner) (domain.NotificationDelivery, error) {
	var delivery domain.NotificationDelivery
	var eventType string
	var transport string
	var destinationKind sql.NullString
	var destinationKey sql.NullString
	var notificationTargetID sql.NullString
	var recipientUserID sql.NullString
	var slackWorkspaceIntegrationID sql.NullString
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

	err := scanner.Scan(
		&delivery.ID,
		&delivery.BuildID,
		&eventType,
		&transport,
		&destinationKind,
		&destinationKey,
		&notificationTargetID,
		&recipientUserID,
		&slackWorkspaceIntegrationID,
		&delivery.Recipient,
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
		&delivery.CreatedAt,
		&delivery.UpdatedAt,
		&sentAt,
	)
	if err != nil {
		return domain.NotificationDelivery{}, err
	}

	delivery.EventType = domain.NotificationEventType(eventType)
	delivery.Transport = domain.NotificationTransport(transport)
	if destinationKind.Valid {
		delivery.DestinationKind = domain.NotificationDestinationKind(destinationKind.String)
	}
	if destinationKey.Valid {
		delivery.DestinationKey = destinationKey.String
	}
	if notificationTargetID.Valid {
		value := notificationTargetID.String
		delivery.NotificationTargetID = &value
	}
	if recipientUserID.Valid {
		value := recipientUserID.String
		delivery.RecipientUserID = &value
	}
	if slackWorkspaceIntegrationID.Valid {
		value := slackWorkspaceIntegrationID.String
		delivery.SlackWorkspaceIntegrationID = &value
	}
	delivery.Status = domain.NotificationDeliveryStatus(status)
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
		value := domain.NotificationDeliveryFailureCategory(failureCategory.String)
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

	return delivery.Normalize(), nil
}

func nullableOptionalStringLocal(value *string) any {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return trimmed
}

func nullableTrimmedStringLocal(value string) any {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return trimmed
}

func nullableTimePointerLocal(value *time.Time) any {
	if value == nil || value.IsZero() {
		return nil
	}
	return value.UTC()
}

func waitForNotificationDeliveryAcquireRetry(ctx context.Context, delay time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
