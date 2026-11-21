package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func TestNotificationPlanRejectsInvalidMembership(t *testing.T) {
	ctx := context.Background()
	builds := NewBuildRepository()
	ledger := NewNotificationDeliveryRepository()
	plans := NewNotificationPlanningRepository(builds, ledger)
	build, err := builds.Create(ctx, domain.Build{ID: "failed", Status: domain.BuildStatusFailed})
	if err != nil {
		t.Fatal(err)
	}
	valid := domain.NotificationDelivery{BuildID: build.ID, EventType: domain.NotificationEventTypeBuildFailed, Transport: domain.NotificationTransportEmail, DestinationKind: domain.NotificationDestinationKindSharedTarget, DestinationKey: "target", Status: domain.NotificationDeliveryStatusPending, MaxAttempts: 3}
	invalid := valid
	invalid.BuildID = "other"
	if won, planErr := plans.CommitNotificationPlan(ctx, build.ID, build.Status, []domain.NotificationDelivery{valid, invalid}); planErr == nil || won {
		t.Fatalf("invalid set committed: %v %v", won, planErr)
	}
	if _, claimErr := ledger.AcquireForDelivery(ctx, repository.NotificationDeliveryClaimInput{Delivery: valid, ExistingOnly: true, ClaimOwner: "server", Now: time.Now(), ClaimDuration: time.Minute, MaxAttempts: 3}); !errors.Is(claimErr, repository.ErrNotificationDeliveryNotFound) {
		t.Fatalf("unexpected claim: %v", claimErr)
	}
	if _, listErr := plans.ListUnplannedBuildIDs(ctx, "", 0); listErr == nil {
		t.Fatal("accepted invalid batch")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, listErr := plans.ListUnplannedBuildIDs(canceled, "", 1); !errors.Is(listErr, context.Canceled) {
		t.Fatal(listErr)
	}
	if _, planErr := plans.CommitNotificationPlan(canceled, build.ID, build.Status, nil); !errors.Is(planErr, context.Canceled) {
		t.Fatal(planErr)
	}
	if won, planErr := plans.CommitNotificationPlan(ctx, build.ID, build.Status, nil); !won || planErr != nil {
		t.Fatalf("empty retry: %v %v", won, planErr)
	}
}
