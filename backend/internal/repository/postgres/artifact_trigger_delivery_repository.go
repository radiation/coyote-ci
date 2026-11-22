package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

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

func (r *ArtifactTriggerDeliveryRepository) ClaimFailedForRetry(ctx context.Context, id string, updatedAt time.Time) (domain.ArtifactTriggerDelivery, error) {
	const query = `
		UPDATE artifact_trigger_deliveries
		SET queued_build_id = NULL,
			error_message = NULL,
			status = $2,
			updated_at = GREATEST(COALESCE($3, NOW()), updated_at + interval '1 microsecond')
		WHERE id = $1 AND status = $4 AND queued_build_id IS NULL AND atomic_creation
		RETURNING ` + artifactTriggerDeliveryColumns + `
	`

	claimed, err := scanArtifactTriggerDelivery(r.db.QueryRowContext(ctx, query,
		strings.TrimSpace(id),
		string(domain.ArtifactTriggerDeliveryStatusPending),
		nullableTime(updatedAt),
		string(domain.ArtifactTriggerDeliveryStatusFailed),
	))
	if err == nil {
		return claimed, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return domain.ArtifactTriggerDelivery{}, err
	}
	if _, getErr := r.GetByID(ctx, id); getErr != nil {
		return domain.ArtifactTriggerDelivery{}, getErr
	}
	return domain.ArtifactTriggerDelivery{}, repository.ErrArtifactTriggerDeliveryRetryNotClaimable
}

func (r *ArtifactTriggerDeliveryRepository) Create(ctx context.Context, delivery domain.ArtifactTriggerDelivery) (domain.ArtifactTriggerDelivery, error) {
	const query = `
		INSERT INTO artifact_trigger_deliveries (
			id, artifact_id, consumer_job_id, producer_build_id, producer_project_id, producer_job_id, artifact_path, queued_build_id, error_message, status, created_at, updated_at, atomic_creation
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, COALESCE($11, NOW()), COALESCE($12, NOW()), true)
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

func (r *ArtifactTriggerDeliveryRepository) GetByID(ctx context.Context, id string) (domain.ArtifactTriggerDelivery, error) {
	const query = `
		SELECT ` + artifactTriggerDeliveryColumns + `
		FROM artifact_trigger_deliveries
		WHERE id = $1
	`

	delivery, err := scanArtifactTriggerDelivery(r.db.QueryRowContext(ctx, query, strings.TrimSpace(id)))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ArtifactTriggerDelivery{}, repository.ErrArtifactTriggerDeliveryNotFound
		}
		return domain.ArtifactTriggerDelivery{}, err
	}
	return delivery, nil
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

func (r *ArtifactTriggerDeliveryRepository) CreateArtifactBuild(ctx context.Context, id string, claim time.Time, plan repository.ArtifactBuildPlan) (domain.Build, error) {
	return r.createArtifactBuild(ctx, id, claim, plan, (*sql.Tx).Commit)
}

func (r *ArtifactTriggerDeliveryRepository) createArtifactBuild(ctx context.Context, id string, claim time.Time, plan repository.ArtifactBuildPlan, commit func(*sql.Tx) error) (domain.Build, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Build{}, err
	}
	defer func() { _ = tx.Rollback() }()
	delivery, err := scanArtifactTriggerDelivery(tx.QueryRowContext(ctx, `SELECT `+artifactTriggerDeliveryColumns+` FROM artifact_trigger_deliveries WHERE id=$1 AND atomic_creation FOR UPDATE`, id))
	if err != nil {
		return domain.Build{}, err
	}
	if delivery.Status == domain.ArtifactTriggerDeliveryStatusQueued && delivery.QueuedBuildID != nil {
		return scanBuild(tx.QueryRowContext(ctx, `SELECT `+buildColumns+` FROM builds WHERE id=$1`, *delivery.QueuedBuildID))
	}
	if !plan.Matches(delivery, claim) {
		return domain.Build{}, repository.ErrArtifactTriggerDeliveryRetryNotClaimable
	}
	var consumerID string
	err = tx.QueryRowContext(ctx, `SELECT id FROM jobs WHERE id=$1 AND project_id=$2 AND enabled AND updated_at=$3 FOR NO KEY UPDATE`, delivery.ConsumerJobID, delivery.ProducerProjectID, plan.ConsumerUpdatedAt).Scan(&consumerID)
	if err != nil {
		return domain.Build{}, fmt.Errorf("validate artifact consumer: %w", err)
	}
	created, err := insertQueuedBuild(ctx, tx, plan.Build, plan.Steps)
	if err != nil {
		return domain.Build{}, err
	}
	if _, jobsErr := insertExecutionJobs(ctx, tx, plan.Jobs); jobsErr != nil {
		return domain.Build{}, jobsErr
	}
	if outputsErr := insertExecutionOutputs(ctx, tx, plan.Outputs); outputsErr != nil {
		return domain.Build{}, outputsErr
	}
	if _, linkErr := tx.ExecContext(ctx, `UPDATE artifact_trigger_deliveries SET queued_build_id=$2,status='queued',error_message=NULL,updated_at=clock_timestamp() WHERE id=$1`, id, created.ID); linkErr != nil {
		return domain.Build{}, linkErr
	}
	if commitErr := commit(tx); commitErr != nil {
		return domain.Build{}, errors.Join(repository.ErrArtifactBuildCommitUnknown, commitErr)
	}
	return created, nil
}

func (r *ArtifactTriggerDeliveryRepository) FailPending(ctx context.Context, id string, claim time.Time, message string) (domain.ArtifactTriggerDelivery, error) {
	delivery, err := scanArtifactTriggerDelivery(r.db.QueryRowContext(ctx, `UPDATE artifact_trigger_deliveries SET status='failed',error_message=$3,updated_at=GREATEST(clock_timestamp(),updated_at + interval '1 microsecond') WHERE id=$1 AND atomic_creation AND status='pending' AND queued_build_id IS NULL AND updated_at=$2 RETURNING `+artifactTriggerDeliveryColumns, id, claim, nilIfBlank(message)))
	if errors.Is(err, sql.ErrNoRows) {
		return r.GetByID(ctx, id)
	}
	return delivery, err
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
