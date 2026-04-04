package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func TestWebhookDeliveryRepository_CreateDuplicateAndUpdate(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}

	repo := NewWebhookDeliveryRepository(db)
	now := time.Now().UTC()
	row := []string{"id", "provider", "delivery_id", "event_type", "repository_owner", "repository_name", "trigger_raw_ref", "trigger_ref_type", "trigger_ref_name", "trigger_ref", "trigger_deleted", "commit_sha", "actor", "status", "matched_job_id", "queued_build_id", "reason", "received_at", "updated_at"}

	mock.ExpectQuery("INSERT INTO webhook_deliveries").WillReturnRows(sqlmock.NewRows(row).AddRow(
		"delivery-row-1", "github", "delivery-1", "push", "example", "backend", "refs/heads/main", "branch", "main", "main", false, "abc123", "octocat", "received", nil, nil, nil, now, now,
	))

	created, err := repo.Create(context.Background(), domain.WebhookDelivery{
		ID:         "delivery-row-1",
		Provider:   "github",
		DeliveryID: "delivery-1",
		Status:     domain.WebhookDeliveryStatusReceived,
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if created.ID != "delivery-row-1" {
		t.Fatalf("unexpected id: %s", created.ID)
	}

	mock.ExpectQuery("INSERT INTO webhook_deliveries").WillReturnError(errors.New("duplicate key value violates unique constraint webhook_deliveries_provider_delivery_key"))
	_, err = repo.Create(context.Background(), domain.WebhookDelivery{Provider: "github", DeliveryID: "delivery-1", Status: domain.WebhookDeliveryStatusReceived})
	if !errors.Is(err, repository.ErrWebhookDeliveryDuplicate) {
		t.Fatalf("expected ErrWebhookDeliveryDuplicate, got %v", err)
	}

	mock.ExpectQuery("UPDATE webhook_deliveries").WillReturnRows(sqlmock.NewRows(row).AddRow(
		"delivery-row-1", "github", "delivery-1", "push", "example", "backend", "refs/heads/main", "branch", "main", "main", false, "abc123", "octocat", "queued", "job-1", "build-1", nil, now, now,
	))

	jobID := "job-1"
	buildID := "build-1"
	updated, err := repo.Update(context.Background(), domain.WebhookDelivery{
		ID:            "delivery-row-1",
		Provider:      "github",
		DeliveryID:    "delivery-1",
		Status:        domain.WebhookDeliveryStatusQueued,
		MatchedJobID:  &jobID,
		QueuedBuildID: &buildID,
	})
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}
	if updated.Status != domain.WebhookDeliveryStatusQueued {
		t.Fatalf("expected queued status, got %q", updated.Status)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}
