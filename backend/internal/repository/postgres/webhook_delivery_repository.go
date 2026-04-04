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

type WebhookDeliveryRepository struct {
	db *sql.DB
}

func NewWebhookDeliveryRepository(db *sql.DB) *WebhookDeliveryRepository {
	return &WebhookDeliveryRepository{db: db}
}

const webhookDeliveryColumns = `id, provider, delivery_id, event_type, repository_owner, repository_name, trigger_ref, commit_sha, actor, status, matched_job_id, queued_build_id, reason, received_at, updated_at`

func (r *WebhookDeliveryRepository) Create(ctx context.Context, delivery domain.WebhookDelivery) (domain.WebhookDelivery, error) {
	const query = `
		INSERT INTO webhook_deliveries (id, provider, delivery_id, event_type, repository_owner, repository_name, trigger_ref, commit_sha, actor, status, matched_job_id, queued_build_id, reason, received_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, COALESCE($14, NOW()), COALESCE($15, NOW()))
		RETURNING ` + webhookDeliveryColumns + `
	`

	created, err := scanWebhookDelivery(r.db.QueryRowContext(
		ctx,
		query,
		delivery.ID,
		strings.ToLower(strings.TrimSpace(delivery.Provider)),
		strings.TrimSpace(delivery.DeliveryID),
		delivery.EventType,
		delivery.RepositoryOwner,
		delivery.RepositoryName,
		delivery.TriggerRef,
		delivery.CommitSHA,
		delivery.Actor,
		string(delivery.Status),
		delivery.MatchedJobID,
		delivery.QueuedBuildID,
		delivery.Reason,
		nullableTime(delivery.ReceivedAt),
		nullableTime(delivery.UpdatedAt),
	))
	if err != nil {
		if isUniqueViolation(err) {
			return domain.WebhookDelivery{}, repository.ErrWebhookDeliveryDuplicate
		}
		return domain.WebhookDelivery{}, err
	}

	return created, nil
}

func (r *WebhookDeliveryRepository) GetByProviderDeliveryID(ctx context.Context, provider string, deliveryID string) (domain.WebhookDelivery, error) {
	const query = `
		SELECT ` + webhookDeliveryColumns + `
		FROM webhook_deliveries
		WHERE provider = $1 AND delivery_id = $2
	`

	delivery, err := scanWebhookDelivery(r.db.QueryRowContext(ctx, query, strings.ToLower(strings.TrimSpace(provider)), strings.TrimSpace(deliveryID)))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.WebhookDelivery{}, repository.ErrWebhookDeliveryNotFound
		}
		return domain.WebhookDelivery{}, err
	}

	return delivery, nil
}

func (r *WebhookDeliveryRepository) Update(ctx context.Context, delivery domain.WebhookDelivery) (domain.WebhookDelivery, error) {
	const query = `
		UPDATE webhook_deliveries
		SET event_type = $2,
			repository_owner = $3,
			repository_name = $4,
			trigger_ref = $5,
			commit_sha = $6,
			actor = $7,
			status = $8,
			matched_job_id = $9,
			queued_build_id = $10,
			reason = $11,
			updated_at = COALESCE($12, NOW())
		WHERE id = $1
		RETURNING ` + webhookDeliveryColumns + `
	`

	updated, err := scanWebhookDelivery(r.db.QueryRowContext(
		ctx,
		query,
		delivery.ID,
		delivery.EventType,
		delivery.RepositoryOwner,
		delivery.RepositoryName,
		delivery.TriggerRef,
		delivery.CommitSHA,
		delivery.Actor,
		string(delivery.Status),
		delivery.MatchedJobID,
		delivery.QueuedBuildID,
		delivery.Reason,
		nullableTime(delivery.UpdatedAt),
	))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.WebhookDelivery{}, repository.ErrWebhookDeliveryNotFound
		}
		return domain.WebhookDelivery{}, err
	}

	return updated, nil
}

func scanWebhookDelivery(scanner rowScanner) (domain.WebhookDelivery, error) {
	var delivery domain.WebhookDelivery
	var eventType sql.NullString
	var repositoryOwner sql.NullString
	var repositoryName sql.NullString
	var triggerRef sql.NullString
	var commitSHA sql.NullString
	var actor sql.NullString
	var status string
	var matchedJobID sql.NullString
	var queuedBuildID sql.NullString
	var reason sql.NullString

	err := scanner.Scan(
		&delivery.ID,
		&delivery.Provider,
		&delivery.DeliveryID,
		&eventType,
		&repositoryOwner,
		&repositoryName,
		&triggerRef,
		&commitSHA,
		&actor,
		&status,
		&matchedJobID,
		&queuedBuildID,
		&reason,
		&delivery.ReceivedAt,
		&delivery.UpdatedAt,
	)
	if err != nil {
		return domain.WebhookDelivery{}, err
	}

	delivery.Status = domain.WebhookDeliveryStatus(status)
	if eventType.Valid {
		v := eventType.String
		delivery.EventType = &v
	}
	if repositoryOwner.Valid {
		v := repositoryOwner.String
		delivery.RepositoryOwner = &v
	}
	if repositoryName.Valid {
		v := repositoryName.String
		delivery.RepositoryName = &v
	}
	if triggerRef.Valid {
		v := triggerRef.String
		delivery.TriggerRef = &v
	}
	if commitSHA.Valid {
		v := commitSHA.String
		delivery.CommitSHA = &v
	}
	if actor.Valid {
		v := actor.String
		delivery.Actor = &v
	}
	if matchedJobID.Valid {
		v := matchedJobID.String
		delivery.MatchedJobID = &v
	}
	if queuedBuildID.Valid {
		v := queuedBuildID.String
		delivery.QueuedBuildID = &v
	}
	if reason.Valid {
		v := reason.String
		delivery.Reason = &v
	}

	return delivery, nil
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "duplicate key") || strings.Contains(message, "unique constraint")
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}
