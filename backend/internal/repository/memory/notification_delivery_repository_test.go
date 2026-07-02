package memory

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func TestNotificationDeliveryRepository_CreateGetUpdateAndErrors(t *testing.T) {
	repo := NewNotificationDeliveryRepository()

	created, err := repo.Create(context.Background(), domain.NotificationDelivery{
		BuildID:         " build-1 ",
		EventType:       domain.NotificationEventTypeBuildFailed,
		Transport:       domain.NotificationTransportEmail,
		DestinationKind: domain.NotificationDestinationKindSharedTarget,
		DestinationKey:  "email-target:target-1",
		Recipient:       " <dev@example.com> ",
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if strings.TrimSpace(created.ID) == "" {
		t.Fatal("expected generated id")
	}
	if created.BuildID != "build-1" {
		t.Fatalf("expected trimmed build id, got %q", created.BuildID)
	}
	if created.Recipient != "<dev@example.com>" {
		t.Fatalf("expected trimmed recipient, got %q", created.Recipient)
	}
	if created.Status != domain.NotificationDeliveryStatusPending {
		t.Fatalf("expected default pending status, got %q", created.Status)
	}
	if created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Fatal("expected timestamps to be set")
	}

	fetched, err := repo.GetByBuildEventRecipient(context.Background(), " build-1 ", domain.NotificationEventTypeBuildFailed, " <dev@example.com> ")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if fetched.ID != created.ID {
		t.Fatalf("expected same delivery id, got %q", fetched.ID)
	}

	updatedAt := time.Now().UTC()
	sentAt := updatedAt.Add(time.Second)
	updated, err := repo.Update(context.Background(), domain.NotificationDelivery{
		ID:              created.ID,
		BuildID:         " build-1 ",
		EventType:       domain.NotificationEventTypeBuildFailed,
		Transport:       domain.NotificationTransportEmail,
		DestinationKind: domain.NotificationDestinationKindSharedTarget,
		DestinationKey:  "email-target:target-1",
		Recipient:       " <dev@example.com> ",
		Status:          domain.NotificationDeliveryStatusSent,
		Attempts:        1,
		UpdatedAt:       updatedAt,
		SentAt:          &sentAt,
	})
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}
	if updated.CreatedAt != created.CreatedAt {
		t.Fatalf("expected create timestamp to be preserved, got %v want %v", updated.CreatedAt, created.CreatedAt)
	}
	if updated.Status != domain.NotificationDeliveryStatusSent {
		t.Fatalf("expected sent status, got %q", updated.Status)
	}

	_, err = repo.Create(context.Background(), domain.NotificationDelivery{
		BuildID:         "build-1",
		EventType:       domain.NotificationEventTypeBuildFailed,
		Transport:       domain.NotificationTransportEmail,
		DestinationKind: domain.NotificationDestinationKindSharedTarget,
		DestinationKey:  "email-target:target-1",
		Recipient:       "<dev@example.com>",
	})
	if !errors.Is(err, repository.ErrNotificationDeliveryDuplicate) {
		t.Fatalf("expected duplicate create error, got %v", err)
	}

	_, err = repo.Create(context.Background(), domain.NotificationDelivery{})
	if err == nil || !strings.Contains(err.Error(), "build id is required") {
		t.Fatalf("expected validation error for blank identity, got %v", err)
	}

	_, err = repo.GetByBuildEventRecipient(context.Background(), "missing", domain.NotificationEventTypeBuildFailed, "<dev@example.com>")
	if !errors.Is(err, repository.ErrNotificationDeliveryNotFound) {
		t.Fatalf("expected not found on get, got %v", err)
	}

	_, err = repo.Update(context.Background(), domain.NotificationDelivery{ID: "missing"})
	if !errors.Is(err, repository.ErrNotificationDeliveryNotFound) {
		t.Fatalf("expected not found on update, got %v", err)
	}
}

func TestNotificationDeliveryRepository_AcquireContract(t *testing.T) {
	repo := NewNotificationDeliveryRepository()
	base := domain.NotificationDelivery{
		BuildID:         "build-1",
		EventType:       domain.NotificationEventTypeBuildFailed,
		Transport:       domain.NotificationTransportEmail,
		DestinationKind: domain.NotificationDestinationKindSharedTarget,
		DestinationKey:  "email-target:target-1",
		Recipient:       "<dev@example.com>",
	}

	first, err := repo.Acquire(context.Background(), base)
	if err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	if first.Outcome != repository.NotificationDeliveryAcquireOutcomeCreated {
		t.Fatalf("expected created outcome, got %q", first.Outcome)
	}

	second, err := repo.Acquire(context.Background(), base)
	if err != nil {
		t.Fatalf("second acquire failed: %v", err)
	}
	if second.Outcome != repository.NotificationDeliveryAcquireOutcomePending {
		t.Fatalf("expected pending outcome for duplicate pending delivery, got %q", second.Outcome)
	}

	if _, updateErr := repo.Update(context.Background(), domain.NotificationDelivery{
		ID:              first.Delivery.ID,
		BuildID:         base.BuildID,
		EventType:       base.EventType,
		Transport:       base.Transport,
		DestinationKind: base.DestinationKind,
		DestinationKey:  base.DestinationKey,
		Recipient:       base.Recipient,
		Status:          domain.NotificationDeliveryStatusSent,
		Attempts:        1,
		UpdatedAt:       time.Now().UTC(),
	}); updateErr != nil {
		t.Fatalf("update sent delivery failed: %v", updateErr)
	}

	sent, err := repo.Acquire(context.Background(), base)
	if err != nil {
		t.Fatalf("sent acquire failed: %v", err)
	}
	if sent.Outcome != repository.NotificationDeliveryAcquireOutcomeSent {
		t.Fatalf("expected sent outcome, got %q", sent.Outcome)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := repo.Acquire(ctx, base); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled acquire, got %v", err)
	}
}

func TestNotificationDeliveryRepository_AcquireConcurrentIdenticalCreatesOneRow(t *testing.T) {
	repo := NewNotificationDeliveryRepository()
	base := domain.NotificationDelivery{
		BuildID:         "build-1",
		EventType:       domain.NotificationEventTypeBuildFailed,
		Transport:       domain.NotificationTransportEmail,
		DestinationKind: domain.NotificationDestinationKindSharedTarget,
		DestinationKey:  "email-target:target-1",
		Recipient:       "<dev@example.com>",
	}

	const workers = 8
	results := make(chan repository.NotificationDeliveryAcquireOutcome, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := repo.Acquire(context.Background(), base)
			if err != nil {
				t.Errorf("acquire failed: %v", err)
				return
			}
			results <- result.Outcome
		}()
	}
	wg.Wait()
	close(results)

	created := 0
	others := 0
	for outcome := range results {
		if outcome == repository.NotificationDeliveryAcquireOutcomeCreated {
			created++
			continue
		}
		others++
	}
	if created != 1 {
		t.Fatalf("expected exactly one created delivery, got %d", created)
	}
	if others != workers-1 {
		t.Fatalf("expected %d non-created duplicate outcomes, got %d", workers-1, others)
	}
}
