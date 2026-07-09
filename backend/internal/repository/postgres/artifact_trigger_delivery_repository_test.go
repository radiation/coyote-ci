package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func TestArtifactTriggerDeliveryRepository_CreateGetAndUpdate(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}

	repo := NewArtifactTriggerDeliveryRepository(db)
	now := time.Now().UTC()
	row := []string{"id", "artifact_id", "consumer_job_id", "producer_build_id", "producer_project_id", "producer_job_id", "artifact_path", "queued_build_id", "error_message", "status", "created_at", "updated_at"}

	mock.ExpectQuery("INSERT INTO artifact_trigger_deliveries").WillReturnRows(sqlmock.NewRows(row).AddRow(
		"delivery-1", "artifact-1", "job-1", "build-1", "project-1", "producer-1", "dist/app.tgz", nil, nil, "pending", now, now,
	))

	created, err := repo.Create(context.Background(), domain.ArtifactTriggerDelivery{
		ID:                "delivery-1",
		ArtifactID:        "artifact-1",
		ConsumerJobID:     "job-1",
		ProducerBuildID:   "build-1",
		ProducerProjectID: "project-1",
		ProducerJobID:     "producer-1",
		ArtifactPath:      "dist/app.tgz",
		Status:            domain.ArtifactTriggerDeliveryStatusPending,
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if created.Status != domain.ArtifactTriggerDeliveryStatusPending {
		t.Fatalf("expected pending status, got %q", created.Status)
	}

	mock.ExpectQuery("INSERT INTO artifact_trigger_deliveries").WillReturnError(errors.New("duplicate key value violates unique constraint artifact_trigger_deliveries_artifact_id_consumer_job_id_key"))
	_, err = repo.Create(context.Background(), domain.ArtifactTriggerDelivery{ArtifactID: "artifact-1", ConsumerJobID: "job-1", Status: domain.ArtifactTriggerDeliveryStatusPending})
	if !errors.Is(err, repository.ErrArtifactTriggerDeliveryDuplicate) {
		t.Fatalf("expected duplicate error, got %v", err)
	}

	errorMessage := "queue failed"
	queuedBuildID := "build-2"
	mock.ExpectQuery("SELECT .*FROM artifact_trigger_deliveries").WillReturnRows(sqlmock.NewRows(row).AddRow(
		"delivery-1", "artifact-1", "job-1", "build-1", "project-1", "producer-1", "dist/app.tgz", queuedBuildID, errorMessage, "failed", now, now,
	))
	got, err := repo.GetByArtifactIDAndConsumerJobID(context.Background(), " artifact-1 ", " job-1 ")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if got.QueuedBuildID == nil || *got.QueuedBuildID != queuedBuildID {
		t.Fatalf("expected queued build id %q, got %#v", queuedBuildID, got.QueuedBuildID)
	}
	if got.ErrorMessage == nil || *got.ErrorMessage != errorMessage {
		t.Fatalf("expected error message %q, got %#v", errorMessage, got.ErrorMessage)
	}

	mock.ExpectQuery("SELECT .*FROM artifact_trigger_deliveries").WillReturnError(sql.ErrNoRows)
	_, err = repo.GetByArtifactIDAndConsumerJobID(context.Background(), "missing", "job-1")
	if !errors.Is(err, repository.ErrArtifactTriggerDeliveryNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}

	mock.ExpectQuery("UPDATE artifact_trigger_deliveries").WillReturnRows(sqlmock.NewRows(row).AddRow(
		"delivery-1", "artifact-1", "job-1", "build-1", "project-1", "producer-1", "dist/app.tgz", queuedBuildID, nil, "queued", now, now,
	))
	updated, err := repo.Update(context.Background(), domain.ArtifactTriggerDelivery{
		ID:            "delivery-1",
		QueuedBuildID: &queuedBuildID,
		Status:        domain.ArtifactTriggerDeliveryStatusQueued,
		UpdatedAt:     now,
	})
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}
	if updated.Status != domain.ArtifactTriggerDeliveryStatusQueued {
		t.Fatalf("expected queued status, got %q", updated.Status)
	}

	mock.ExpectQuery("UPDATE artifact_trigger_deliveries").WillReturnError(sql.ErrNoRows)
	_, err = repo.Update(context.Background(), domain.ArtifactTriggerDelivery{ID: "missing", Status: domain.ArtifactTriggerDeliveryStatusFailed})
	if !errors.Is(err, repository.ErrArtifactTriggerDeliveryNotFound) {
		t.Fatalf("expected update not found, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}
