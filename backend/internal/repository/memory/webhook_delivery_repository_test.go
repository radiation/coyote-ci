package memory

import (
	"context"
	"errors"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func TestWebhookDeliveryRepository_CreateDuplicateAndUpdate(t *testing.T) {
	repo := NewWebhookDeliveryRepository()

	created, err := repo.Create(context.Background(), domain.WebhookDelivery{
		Provider:   "github",
		DeliveryID: "delivery-1",
		Status:     domain.WebhookDeliveryStatusReceived,
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if created.ID == "" {
		t.Fatal("expected generated delivery id")
	}

	_, err = repo.Create(context.Background(), domain.WebhookDelivery{
		Provider:   "github",
		DeliveryID: "delivery-1",
		Status:     domain.WebhookDeliveryStatusReceived,
	})
	if !errors.Is(err, repository.ErrWebhookDeliveryDuplicate) {
		t.Fatalf("expected ErrWebhookDeliveryDuplicate, got %v", err)
	}

	created.Status = domain.WebhookDeliveryStatusQueued
	buildID := "build-1"
	created.QueuedBuildID = &buildID
	updated, err := repo.Update(context.Background(), created)
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}
	if updated.Status != domain.WebhookDeliveryStatusQueued {
		t.Fatalf("expected queued status, got %q", updated.Status)
	}
	if updated.QueuedBuildID == nil || *updated.QueuedBuildID != "build-1" {
		t.Fatalf("expected queued build id, got %v", updated.QueuedBuildID)
	}

	got, err := repo.GetByProviderDeliveryID(context.Background(), "github", "delivery-1")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if got.Status != domain.WebhookDeliveryStatusQueued {
		t.Fatalf("expected queued status from get, got %q", got.Status)
	}
}
