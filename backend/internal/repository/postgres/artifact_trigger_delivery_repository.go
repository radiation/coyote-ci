package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type ArtifactTriggerDeliveryRepository struct {
	db *sql.DB
}

func NewArtifactTriggerDeliveryRepository(db *sql.DB) *ArtifactTriggerDeliveryRepository {
	return &ArtifactTriggerDeliveryRepository{db: db}
}

const artifactTriggerDeliveryColumns = `id, artifact_id, consumer_job_id, producer_build_id, producer_project_id, producer_job_id, artifact_path, queued_build_id, error_message, status, created_at, updated_at`

func (r *ArtifactTriggerDeliveryRepository) Create(ctx context.Context, delivery domain.ArtifactTriggerDelivery) (domain.ArtifactTriggerDelivery, error) {
	const query = `
		INSERT INTO artifact_trigger_deliveries (
			id, artifact_id, consumer_job_id, producer_build_id, producer_project_id, producer_job_id, artifact_path, queued_build_id, error_message, status, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, COALESCE($11, NOW()), COALESCE($12, NOW()))
		RETURNING ` + artifactTriggerDeliveryColumns + `
	`

	created, err := scanArtifactTriggerDelivery(r.db.QueryRowContext(ctx, query,
		delivery.ID,
		delivery.ArtifactID,
		delivery.ConsumerJobID,
		delivery.ProducerBuildID,
		delivery.ProducerProjectID,
		delivery.ProducerJobID,
		delivery.ArtifactPath,
		delivery.QueuedBuildID,
		delivery.ErrorMessage,
		string(delivery.Status),
		nullableTime(delivery.CreatedAt),
		nullableTime(delivery.UpdatedAt),
	))
	if err != nil {
		if isUniqueViolation(err) {
			return domain.ArtifactTriggerDelivery{}, repository.ErrArtifactTriggerDeliveryDuplicate
		}
		return domain.ArtifactTriggerDelivery{}, err
	}
	return created, nil
}

func (r *ArtifactTriggerDeliveryRepository) GetByArtifactIDAndConsumerJobID(ctx context.Context, artifactID string, consumerJobID string) (domain.ArtifactTriggerDelivery, error) {
	const query = `
		SELECT ` + artifactTriggerDeliveryColumns + `
		FROM artifact_trigger_deliveries
		WHERE artifact_id = $1 AND consumer_job_id = $2
	`

	delivery, err := scanArtifactTriggerDelivery(r.db.QueryRowContext(ctx, query, strings.TrimSpace(artifactID), strings.TrimSpace(consumerJobID)))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ArtifactTriggerDelivery{}, repository.ErrArtifactTriggerDeliveryNotFound
		}
		return domain.ArtifactTriggerDelivery{}, err
	}
	return delivery, nil
}

func (r *ArtifactTriggerDeliveryRepository) ListByProducerBuildID(ctx context.Context, producerBuildID string) (result []domain.ArtifactTriggerDelivery, err error) {
	const query = `
		SELECT ` + artifactTriggerDeliveryColumns + `
		FROM artifact_trigger_deliveries
		WHERE producer_build_id = $1
		ORDER BY created_at ASC, id ASC
	`

	rows, err := r.db.QueryContext(ctx, query, strings.TrimSpace(producerBuildID))
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	result = make([]domain.ArtifactTriggerDelivery, 0)
	for rows.Next() {
		delivery, scanErr := scanArtifactTriggerDelivery(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, delivery)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, rowsErr
	}
	return result, nil
}

func (r *ArtifactTriggerDeliveryRepository) Update(ctx context.Context, delivery domain.ArtifactTriggerDelivery) (domain.ArtifactTriggerDelivery, error) {
	const query = `
		UPDATE artifact_trigger_deliveries
		SET queued_build_id = $2,
			error_message = $3,
			status = $4,
			updated_at = COALESCE($5, NOW())
		WHERE id = $1
		RETURNING ` + artifactTriggerDeliveryColumns + `
	`

	updated, err := scanArtifactTriggerDelivery(r.db.QueryRowContext(ctx, query,
		delivery.ID,
		delivery.QueuedBuildID,
		delivery.ErrorMessage,
		string(delivery.Status),
		nullableTime(delivery.UpdatedAt),
	))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ArtifactTriggerDelivery{}, repository.ErrArtifactTriggerDeliveryNotFound
		}
		return domain.ArtifactTriggerDelivery{}, err
	}
	return updated, nil
}

func scanArtifactTriggerDelivery(scanner rowScanner) (domain.ArtifactTriggerDelivery, error) {
	var delivery domain.ArtifactTriggerDelivery
	var queuedBuildID sql.NullString
	var errorMessage sql.NullString
	var status string
	err := scanner.Scan(
		&delivery.ID,
		&delivery.ArtifactID,
		&delivery.ConsumerJobID,
		&delivery.ProducerBuildID,
		&delivery.ProducerProjectID,
		&delivery.ProducerJobID,
		&delivery.ArtifactPath,
		&queuedBuildID,
		&errorMessage,
		&status,
		&delivery.CreatedAt,
		&delivery.UpdatedAt,
	)
	if err != nil {
		return domain.ArtifactTriggerDelivery{}, err
	}
	delivery.Status = domain.ArtifactTriggerDeliveryStatus(status)
	if queuedBuildID.Valid {
		value := queuedBuildID.String
		delivery.QueuedBuildID = &value
	}
	if errorMessage.Valid {
		value := errorMessage.String
		delivery.ErrorMessage = &value
	}
	return delivery, nil
}
