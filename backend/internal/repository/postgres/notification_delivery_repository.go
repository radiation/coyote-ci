package postgres

import (
	"context"
	"database/sql"
	"errors"
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

const notificationDeliveryColumns = `id, build_id, event_type, recipient, status, attempts, last_error, created_at, updated_at, sent_at`

func (r *NotificationDeliveryRepository) Create(ctx context.Context, delivery domain.NotificationDelivery) (domain.NotificationDelivery, error) {
	const query = `
		INSERT INTO notification_deliveries (id, build_id, event_type, recipient, status, attempts, last_error, created_at, updated_at, sent_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, COALESCE($8, NOW()), COALESCE($9, NOW()), $10)
		RETURNING ` + notificationDeliveryColumns + `
	`

	if strings.TrimSpace(delivery.ID) == "" {
		delivery.ID = uuid.NewString()
	}
	created, err := scanNotificationDelivery(r.db.QueryRowContext(
		ctx,
		query,
		delivery.ID,
		strings.TrimSpace(delivery.BuildID),
		strings.TrimSpace(string(delivery.EventType)),
		strings.TrimSpace(delivery.Recipient),
		string(delivery.Status),
		delivery.Attempts,
		delivery.LastError,
		nullableTime(delivery.CreatedAt),
		nullableTime(delivery.UpdatedAt),
		nullableTimestamp(delivery.SentAt),
	))
	if err != nil {
		if isUniqueViolation(err) {
			return domain.NotificationDelivery{}, repository.ErrNotificationDeliveryDuplicate
		}
		return domain.NotificationDelivery{}, err
	}

	return created, nil
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
	var status string
	var lastError sql.NullString
	var sentAt sql.NullTime

	err := scanner.Scan(
		&delivery.ID,
		&delivery.BuildID,
		&eventType,
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
