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

const webhookDeliveryColumns = `id, provider, delivery_id, event_type, repository_owner, repository_name, trigger_raw_ref, trigger_ref_type, trigger_ref_name, trigger_ref, trigger_deleted, commit_sha, actor, status, matched_job_id, queued_build_id, reason, received_at, updated_at`

func (r *WebhookDeliveryRepository) Create(ctx context.Context, delivery domain.WebhookDelivery) (domain.WebhookDelivery, error) {
	const query = `
		INSERT INTO webhook_deliveries (id, provider, delivery_id, event_type, repository_owner, repository_name, trigger_raw_ref, trigger_ref_type, trigger_ref_name, trigger_ref, trigger_deleted, commit_sha, actor, status, matched_job_id, queued_build_id, reason, received_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, COALESCE($18, NOW()), COALESCE($19, NOW()))
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
		delivery.RawRef,
		delivery.RefType,
		delivery.RefName,
		delivery.TriggerRef,
		delivery.Deleted,
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
			trigger_raw_ref = $5,
			trigger_ref_type = $6,
			trigger_ref_name = $7,
			trigger_ref = $8,
			trigger_deleted = $9,
			commit_sha = $10,
			actor = $11,
			status = $12,
			matched_job_id = $13,
			queued_build_id = $14,
			reason = $15,
			updated_at = COALESCE($16, NOW())
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
		delivery.RawRef,
		delivery.RefType,
		delivery.RefName,
		delivery.TriggerRef,
		delivery.Deleted,
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
	var rawRef sql.NullString
	var refType sql.NullString
	var refName sql.NullString
	var triggerRef sql.NullString
	var deleted sql.NullBool
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
		&rawRef,
		&refType,
		&refName,
		&triggerRef,
		&deleted,
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
	if rawRef.Valid {
		v := rawRef.String
		delivery.RawRef = &v
	}
	if refType.Valid {
		v := refType.String
		delivery.RefType = &v
	}
	if refName.Valid {
		v := refName.String
		delivery.RefName = &v
	}
	if triggerRef.Valid {
		v := triggerRef.String
		delivery.TriggerRef = &v
	}
	if deleted.Valid {
		v := deleted.Bool
		delivery.Deleted = &v
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
