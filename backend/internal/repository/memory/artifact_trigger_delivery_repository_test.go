package memory

import (
	"context"
	"errors"
	"testing"

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
}
