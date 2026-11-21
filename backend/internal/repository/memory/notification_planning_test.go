package memory

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func TestNotificationPlanPagesUnfinishedBuildsAndFreezesMembership(t *testing.T) {
	ctx := context.Background()
	builds := NewBuildRepository()
	ledger := NewNotificationDeliveryRepository()
	plans := NewNotificationPlanningRepository(builds, ledger)
	for id, status := range map[string]domain.BuildStatus{"a": domain.BuildStatusSuccess, "b": domain.BuildStatusCanceled, "c": domain.BuildStatusFailed} {
		if _, err := builds.Create(ctx, domain.Build{ID: id, Status: status}); err != nil {
			t.Fatal(err)
		}
	}
	assertPage := func(after string, limit int, want ...string) {
		t.Helper()
		ids, err := plans.ListUnplannedBuildIDs(ctx, after, limit)
		if err != nil || !slices.Equal(ids, want) {
			t.Fatalf("page after %q: got %v, %v; want %v", after, ids, err, want)
		}
	}
	assertPage("", 1, "a")
	assertPage("a", 1, "c")
	delivery := domain.NotificationDelivery{BuildID: "a", EventType: domain.NotificationEventTypeBuildSucceeded, Transport: domain.NotificationTransportEmail, DestinationKind: domain.NotificationDestinationKindSharedTarget, DestinationKey: "original", Status: domain.NotificationDeliveryStatusPending, MaxAttempts: 3}
	if won, err := plans.CommitNotificationPlan(ctx, "a", domain.BuildStatusSuccess, []domain.NotificationDelivery{delivery, delivery}); err != nil || !won {
		t.Fatalf("first plan: won=%v, err=%v", won, err)
	}
	delivery.DestinationKey = "added-later"
	if won, err := plans.CommitNotificationPlan(ctx, "a", domain.BuildStatusSuccess, []domain.NotificationDelivery{delivery}); err != nil || won {
		t.Fatalf("replayed plan: won=%v, err=%v", won, err)
	}
	assertPage("", 3, "c")
	rows, err := ledger.ListRecoverable(ctx, repository.NotificationDeliveryRecoverableScanInput{Now: time.Now(), Limit: 3})
	if err != nil || len(rows) != 1 || rows[0].DestinationKey != "original" || rows[0].Status != domain.NotificationDeliveryStatusPending {
		t.Fatalf("expected only original pending delivery, got %+v, %v", rows, err)
	}
}

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
