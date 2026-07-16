package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func TestArtifactTriggerDeliveryRepository_CreateDuplicateGetAndUpdate(t *testing.T) {
	repo := NewArtifactTriggerDeliveryRepository()

	created, err := repo.Create(context.Background(), domain.ArtifactTriggerDelivery{
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
	if created.ID == "" {
		t.Fatal("expected generated delivery id")
	}
	if created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Fatalf("expected timestamps to be populated, got %#v", created)
	}

	_, err = repo.Create(context.Background(), domain.ArtifactTriggerDelivery{
		ArtifactID:    "artifact-1",
		ConsumerJobID: "job-1",
		Status:        domain.ArtifactTriggerDeliveryStatusPending,
	})
	if !errors.Is(err, repository.ErrArtifactTriggerDeliveryDuplicate) {
		t.Fatalf("expected ErrArtifactTriggerDeliveryDuplicate, got %v", err)
	}

	got, err := repo.GetByArtifactIDAndConsumerJobID(context.Background(), "artifact-1", "job-1")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if got.ID != created.ID {
		t.Fatalf("expected delivery %q, got %#v", created.ID, got)
	}

	byID, err := repo.GetByID(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("get by id failed: %v", err)
	}
	if byID.ID != created.ID {
		t.Fatalf("expected delivery %q from get by id, got %#v", created.ID, byID)
	}

	queuedBuildID := "build-2"
	created.Status = domain.ArtifactTriggerDeliveryStatusQueued
	created.QueuedBuildID = &queuedBuildID
	updated, err := repo.Update(context.Background(), created)
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}
	if updated.QueuedBuildID == nil || *updated.QueuedBuildID != queuedBuildID {
		t.Fatalf("expected queued build id %q, got %#v", queuedBuildID, updated.QueuedBuildID)
	}

	_, err = repo.GetByArtifactIDAndConsumerJobID(context.Background(), "missing", "job-1")
	if !errors.Is(err, repository.ErrArtifactTriggerDeliveryNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}

	_, err = repo.Update(context.Background(), domain.ArtifactTriggerDelivery{ID: "missing"})
	if !errors.Is(err, repository.ErrArtifactTriggerDeliveryNotFound) {
		t.Fatalf("expected update not found, got %v", err)
	}

	other, err := repo.Create(context.Background(), domain.ArtifactTriggerDelivery{
		ArtifactID:        "artifact-2",
		ConsumerJobID:     "job-2",
		ProducerBuildID:   "build-1",
		ProducerProjectID: "project-1",
		ProducerJobID:     "producer-1",
		ArtifactPath:      "dist/docs.tgz",
		Status:            domain.ArtifactTriggerDeliveryStatusFailed,
	})
	if err != nil {
		t.Fatalf("create second delivery failed: %v", err)
	}
	third, err := repo.Create(context.Background(), domain.ArtifactTriggerDelivery{
		ArtifactID:        "artifact-3",
		ConsumerJobID:     "job-3",
		ProducerBuildID:   "build-2",
		ProducerProjectID: "project-1",
		ProducerJobID:     "producer-1",
		ArtifactPath:      "dist/other.tgz",
		Status:            domain.ArtifactTriggerDeliveryStatusQueued,
	})
	if err != nil {
		t.Fatalf("create third delivery failed: %v", err)
	}

	deliveries, err := repo.ListByProducerBuildID(context.Background(), "build-1")
	if err != nil {
		t.Fatalf("list by producer build failed: %v", err)
	}
	if len(deliveries) != 2 {
		t.Fatalf("expected 2 deliveries for build-1, got %d", len(deliveries))
	}
	if deliveries[0].ID != created.ID || deliveries[1].ID != other.ID {
		t.Fatalf("unexpected delivery order: %+v", deliveries)
	}

	deliveries, err = repo.ListByProducerBuildID(context.Background(), "build-2")
	if err != nil {
		t.Fatalf("list by second producer build failed: %v", err)
	}
	if len(deliveries) != 1 || deliveries[0].ID != third.ID {
		t.Fatalf("unexpected build-2 deliveries: %+v", deliveries)
	}

	paddedDeliveries, err := repo.ListByProducerBuildID(context.Background(), " build-1 ")
	if err != nil {
		t.Fatalf("list by padded producer build failed: %v", err)
	}
	if len(paddedDeliveries) != 2 || paddedDeliveries[0].ID != created.ID || paddedDeliveries[1].ID != other.ID {
		t.Fatalf("unexpected padded build-1 deliveries: %+v", paddedDeliveries)
	}

	deliveries, err = repo.ListByProducerBuildID(context.Background(), "missing-build")
	if err != nil {
		t.Fatalf("list by missing producer build failed: %v", err)
	}
	if len(deliveries) != 0 {
		t.Fatalf("expected no deliveries for missing build, got %+v", deliveries)
	}
}

func TestArtifactTriggerDeliveryRepository_ClaimFailedForRetry(t *testing.T) {
	repo := NewArtifactTriggerDeliveryRepository()
	created, err := repo.Create(context.Background(), domain.ArtifactTriggerDelivery{
		ID:                "delivery-1",
		ArtifactID:        "artifact-1",
		ConsumerJobID:     "job-1",
		ProducerBuildID:   "build-1",
		ProducerProjectID: "project-1",
		ProducerJobID:     "producer-1",
		ArtifactPath:      "dist/app.tgz",
		Status:            domain.ArtifactTriggerDeliveryStatusFailed,
	})
	if err != nil {
		t.Fatalf("create failed delivery: %v", err)
	}

	claimed, err := repo.ClaimFailedForRetry(context.Background(), created.ID, created.UpdatedAt.Add(time.Second))
	if err != nil {
		t.Fatalf("claim failed for retry: %v", err)
	}
	if claimed.Status != domain.ArtifactTriggerDeliveryStatusPending {
		t.Fatalf("expected pending after claim, got %q", claimed.Status)
	}
	if claimed.ErrorMessage != nil {
		t.Fatalf("expected cleared error message, got %#v", claimed.ErrorMessage)
	}

	_, err = repo.ClaimFailedForRetry(context.Background(), created.ID, time.Time{})
	if !errors.Is(err, repository.ErrArtifactTriggerDeliveryRetryNotClaimable) {
		t.Fatalf("expected retry not claimable, got %v", err)
	}

	queuedBuildID := "build-2"
	if _, createQueuedErr := repo.Create(context.Background(), domain.ArtifactTriggerDelivery{
		ID:                "delivery-queued",
		ArtifactID:        "artifact-queued",
		ConsumerJobID:     "job-queued",
		ProducerBuildID:   "build-1",
		ProducerProjectID: "project-1",
		ProducerJobID:     "producer-1",
		ArtifactPath:      "dist/queued.tgz",
		QueuedBuildID:     &queuedBuildID,
		Status:            domain.ArtifactTriggerDeliveryStatusFailed,
	}); createQueuedErr != nil {
		t.Fatalf("create queued delivery failed: %v", createQueuedErr)
	}
	_, err = repo.ClaimFailedForRetry(context.Background(), "delivery-queued", time.Time{})
	if !errors.Is(err, repository.ErrArtifactTriggerDeliveryRetryNotClaimable) {
		t.Fatalf("expected queued delivery retry not claimable, got %v", err)
	}

	_, err = repo.ClaimFailedForRetry(context.Background(), "missing", time.Time{})
	if !errors.Is(err, repository.ErrArtifactTriggerDeliveryNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}
