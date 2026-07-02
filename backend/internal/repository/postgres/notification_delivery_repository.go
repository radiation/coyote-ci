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

const notificationDeliveryColumns = `id, build_id, event_type, transport, destination_kind, destination_key, notification_target_id::text, recipient_user_id::text, slack_workspace_integration_id::text, recipient, status, attempts, last_error, created_at, updated_at, sent_at`

const notificationDeliveryAcquireInsertQuery = `
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
		last_error,
		created_at,
		updated_at,
		sent_at
	)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, COALESCE($14, NOW()), COALESCE($15, NOW()), $16)
	ON CONFLICT (build_id, event_type, transport, destination_key) DO NOTHING
	RETURNING ` + notificationDeliveryColumns + `
`

const notificationDeliveryAcquireSelectQuery = `
	SELECT ` + notificationDeliveryColumns + `
	FROM notification_deliveries
	WHERE build_id = $1 AND event_type = $2 AND transport = $3 AND destination_key = $4
`

const notificationDeliveryAcquireLookupAttempts = 8
const notificationDeliveryAcquireLookupDelay = 10 * time.Millisecond

func (r *NotificationDeliveryRepository) Create(ctx context.Context, delivery domain.NotificationDelivery) (domain.NotificationDelivery, error) {
	result, err := r.Acquire(ctx, delivery)
	if err != nil {
		return domain.NotificationDelivery{}, err
	}
	if result.Outcome != repository.NotificationDeliveryAcquireOutcomeCreated {
		return domain.NotificationDelivery{}, repository.ErrNotificationDeliveryDuplicate
	}
	return result.Delivery, nil
}

func (r *NotificationDeliveryRepository) Acquire(ctx context.Context, delivery domain.NotificationDelivery) (repository.NotificationDeliveryAcquireResult, error) {
	delivery = delivery.Normalize()
	if err := delivery.ValidateIdentity(); err != nil {
		return repository.NotificationDeliveryAcquireResult{}, err
	}
	if strings.TrimSpace(delivery.ID) == "" {
		delivery.ID = uuid.NewString()
	}
	if strings.TrimSpace(string(delivery.Status)) == "" {
		delivery.Status = domain.NotificationDeliveryStatusPending
	}

	created, err := scanNotificationDelivery(r.db.QueryRowContext(
		ctx,
		notificationDeliveryAcquireInsertQuery,
		delivery.ID,
		delivery.BuildID,
		string(delivery.EventType),
		string(delivery.Transport),
		string(delivery.DestinationKind),
		delivery.DestinationKey,
		nullableOptionalString(delivery.NotificationTargetID),
		nullableOptionalString(delivery.RecipientUserID),
		nullableOptionalString(delivery.SlackWorkspaceIntegrationID),
		delivery.Recipient,
		string(delivery.Status),
		delivery.Attempts,
		nullableOptionalString(delivery.LastError),
		nullableTime(delivery.CreatedAt),
		nullableTime(delivery.UpdatedAt),
		nullableTimestamp(delivery.SentAt),
	))
	if err == nil {
		return repository.NotificationDeliveryAcquireResult{Delivery: created, Outcome: repository.NotificationDeliveryAcquireOutcomeCreated}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return repository.NotificationDeliveryAcquireResult{}, err
	}

	resolved, err := r.lookupAcquiredDelivery(ctx, delivery.BuildID, delivery.EventType, delivery.Transport, delivery.DestinationKey)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return repository.NotificationDeliveryAcquireResult{}, fmt.Errorf("notification delivery acquire conflict did not resolve to an existing row")
		}
		return repository.NotificationDeliveryAcquireResult{}, err
	}
	return repository.NotificationDeliveryAcquireResult{Delivery: resolved, Outcome: repository.NotificationDeliveryAcquireOutcomeFromStatus(resolved.Status)}, nil
}

func (r *NotificationDeliveryRepository) lookupAcquiredDelivery(ctx context.Context, buildID string, eventType domain.NotificationEventType, transport domain.NotificationTransport, destinationKey string) (domain.NotificationDelivery, error) {
	for attempt := 0; attempt < notificationDeliveryAcquireLookupAttempts; attempt++ {
		resolved, err := scanNotificationDelivery(r.db.QueryRowContext(
			ctx,
			notificationDeliveryAcquireSelectQuery,
			strings.TrimSpace(buildID),
			strings.TrimSpace(string(eventType)),
			strings.TrimSpace(string(transport)),
			strings.TrimSpace(destinationKey),
		))
		if err == nil {
			return resolved, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return domain.NotificationDelivery{}, err
		}
		if attempt == notificationDeliveryAcquireLookupAttempts-1 {
			break
		}
		if waitErr := waitForNotificationDeliveryAcquireRetry(ctx, notificationDeliveryAcquireLookupDelay); waitErr != nil {
			return domain.NotificationDelivery{}, waitErr
		}
	}

	return domain.NotificationDelivery{}, sql.ErrNoRows
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

func (r *NotificationDeliveryRepository) GetByBuildEventRecipient(ctx context.Context, buildID string, eventType domain.NotificationEventType, recipient string) (domain.NotificationDelivery, error) {
	const query = `
		SELECT ` + notificationDeliveryColumns + `
		FROM notification_deliveries
		WHERE build_id = $1 AND event_type = $2 AND recipient = $3
	`

	delivery, err := scanNotificationDelivery(r.db.QueryRowContext(
		ctx,
		query,
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

func (r *NotificationDeliveryRepository) Update(ctx context.Context, delivery domain.NotificationDelivery) (domain.NotificationDelivery, error) {
	const query = `
		UPDATE notification_deliveries
		SET status = $2,
			attempts = $3,
			last_error = $4,
			updated_at = COALESCE($5, NOW()),
			sent_at = $6
		WHERE id = $1
		RETURNING ` + notificationDeliveryColumns + `
	`

	updated, err := scanNotificationDelivery(r.db.QueryRowContext(
		ctx,
		query,
		delivery.ID,
		string(delivery.Status),
		delivery.Attempts,
		delivery.LastError,
		nullableTime(delivery.UpdatedAt),
		nullableTimestamp(delivery.SentAt),
	))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.NotificationDelivery{}, repository.ErrNotificationDeliveryNotFound
		}
		return domain.NotificationDelivery{}, err
	}

	return updated, nil
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
	var lastError sql.NullString
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
	if lastError.Valid {
		value := lastError.String
		delivery.LastError = &value
	}
	if sentAt.Valid {
		value := sentAt.Time.UTC()
		delivery.SentAt = &value
	}

	return delivery, nil
}

func nullableTimestamp(value *time.Time) any {
	if value == nil || value.IsZero() {
		return nil
	}
	return *value
}
