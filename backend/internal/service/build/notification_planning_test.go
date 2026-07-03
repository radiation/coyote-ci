package build

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	memoryrepo "github.com/radiation/coyote-ci/backend/internal/repository/memory"
)

func TestBuildNotificationService_CommitAuthorSuccessPreferenceDelivery(t *testing.T) {
	type successFixture struct {
		notifier         *BuildNotificationService
		sender           *recordingEmailSender
		deliveryRepo     *memoryrepo.NotificationDeliveryRepository
		subscriptionRepo *memoryrepo.NotificationSubscriptionRepository
		userRepo         *memoryrepo.UserRepository
		preferenceRepo   *memoryrepo.UserNotificationPreferenceRepository
		authorUser       domain.User
	}

	newFixture := func(t *testing.T, authorEmail string, createOwnedTarget bool, targetEnabled bool, failureEnabled bool, successEnabled bool) successFixture {
		t.Helper()

		sender := &recordingEmailSender{}
		deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
		userRepo := memoryrepo.NewUserRepository()
		preferenceRepo := memoryrepo.NewUserNotificationPreferenceRepository()
		subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
		authorUser := mustCreateNotificationUser(t, userRepo, authorEmail)
		if createOwnedTarget {
			mustEnsureOwnedNotificationTarget(t, subscriptionRepo, authorUser.ID, authorEmail, targetEnabled)
		}
		mustUpsertNotificationPreferenceFlags(t, preferenceRepo, authorUser.ID, failureEnabled, successEnabled)

		notifier, err := NewBuildNotificationService(BuildNotificationConfig{
			Enabled:          true,
			Sender:           sender,
			DeliveryRepo:     deliveryRepo,
			SubscriptionRepo: subscriptionRepo,
			UserRepo:         userRepo,
			PreferenceRepo:   preferenceRepo,
		})
		if err != nil {
			t.Fatalf("create notifier failed: %v", err)
		}

		return successFixture{
			notifier:         notifier,
			sender:           sender,
			deliveryRepo:     deliveryRepo,
			subscriptionRepo: subscriptionRepo,
			userRepo:         userRepo,
			preferenceRepo:   preferenceRepo,
			authorUser:       authorUser,
		}
	}

	t.Run("successful build sends to owned enabled personal target when success preference is enabled", func(t *testing.T) {
		authorEmail := "author@example.com"
		fixture := newFixture(t, authorEmail, true, true, false, true)

		if notifyErr := fixture.notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-success-1", Status: domain.BuildStatusSuccess, SourceAuthorEmail: &authorEmail}); notifyErr != nil {
			t.Fatalf("notify terminal build failed: %v", notifyErr)
		}
		if len(fixture.sender.messages) != 1 || fixture.sender.messages[0].To != "<author@example.com>" {
			t.Fatalf("expected one personal success delivery to author target, got %+v", fixture.sender.messages)
		}
		delivery := mustGetNotificationDelivery(t, fixture.deliveryRepo, "build-success-1", domain.NotificationEventTypeBuildSucceeded, "<author@example.com>")
		if delivery.Status != domain.NotificationDeliveryStatusSent {
			t.Fatalf("expected sent success delivery, got %q", delivery.Status)
		}
	})

	t.Run("success-disabled author receives no personal success delivery", func(t *testing.T) {
		authorEmail := "author@example.com"
		fixture := newFixture(t, authorEmail, true, true, false, false)

		if notifyErr := fixture.notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-success-2", Status: domain.BuildStatusSuccess, SourceAuthorEmail: &authorEmail}); notifyErr != nil {
			t.Fatalf("notify terminal build failed: %v", notifyErr)
		}
		if len(fixture.sender.messages) != 0 {
			t.Fatalf("expected no personal success delivery, got %+v", fixture.sender.messages)
		}
	})

	t.Run("failure preference alone does not cause success delivery and remains independently authoritative", func(t *testing.T) {
		authorEmail := "author@example.com"
		fixture := newFixture(t, authorEmail, true, true, true, false)

		if notifyErr := fixture.notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-success-3", Status: domain.BuildStatusSuccess, SourceAuthorEmail: &authorEmail}); notifyErr != nil {
			t.Fatalf("notify success build failed: %v", notifyErr)
		}
		if len(fixture.sender.messages) != 0 {
			t.Fatalf("expected no success delivery when only failure preference is enabled, got %+v", fixture.sender.messages)
		}

		if notifyErr := fixture.notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-failure-3", Status: domain.BuildStatusFailed, SourceAuthorEmail: &authorEmail}); notifyErr != nil {
			t.Fatalf("notify failure build failed: %v", notifyErr)
		}
		if len(fixture.sender.messages) != 1 || fixture.sender.messages[0].To != "<author@example.com>" {
			t.Fatalf("expected failure delivery to remain enabled independently, got %+v", fixture.sender.messages)
		}
	})

	t.Run("success preference alone does not change existing failure delivery behavior", func(t *testing.T) {
		authorEmail := "author@example.com"
		fixture := newFixture(t, authorEmail, true, true, false, true)

		if notifyErr := fixture.notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-failure-4", Status: domain.BuildStatusFailed, SourceAuthorEmail: &authorEmail}); notifyErr != nil {
			t.Fatalf("notify failure build failed: %v", notifyErr)
		}
		if len(fixture.sender.messages) != 0 {
			t.Fatalf("expected failure delivery to remain disabled, got %+v", fixture.sender.messages)
		}

		if notifyErr := fixture.notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-success-4", Status: domain.BuildStatusSuccess, SourceAuthorEmail: &authorEmail}); notifyErr != nil {
			t.Fatalf("notify success build failed: %v", notifyErr)
		}
		if len(fixture.sender.messages) != 1 || fixture.sender.messages[0].To != "<author@example.com>" {
			t.Fatalf("expected success delivery to remain enabled independently, got %+v", fixture.sender.messages)
		}
	})

	t.Run("disabled owned personal target prevents success delivery", func(t *testing.T) {
		authorEmail := "author@example.com"
		fixture := newFixture(t, authorEmail, true, false, false, true)

		if notifyErr := fixture.notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-success-5", Status: domain.BuildStatusSuccess, SourceAuthorEmail: &authorEmail}); notifyErr != nil {
			t.Fatalf("notify terminal build failed: %v", notifyErr)
		}
		if len(fixture.sender.messages) != 0 {
			t.Fatalf("expected disabled target to prevent success delivery, got %+v", fixture.sender.messages)
		}
	})

	t.Run("missing owned personal target prevents success delivery", func(t *testing.T) {
		authorEmail := "author@example.com"
		fixture := newFixture(t, authorEmail, false, false, false, true)

		if notifyErr := fixture.notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-success-6", Status: domain.BuildStatusSuccess, SourceAuthorEmail: &authorEmail}); notifyErr != nil {
			t.Fatalf("notify terminal build failed: %v", notifyErr)
		}
		if len(fixture.sender.messages) != 0 {
			t.Fatalf("expected missing target to prevent success delivery, got %+v", fixture.sender.messages)
		}
	})

	t.Run("target owned by another user is not used", func(t *testing.T) {
		authorEmail := "author@example.com"
		fixture := newFixture(t, authorEmail, false, false, false, true)
		otherOwner := mustCreateNotificationUser(t, fixture.userRepo, "owner@example.com")
		mustEnsureOwnedNotificationTarget(t, fixture.subscriptionRepo, otherOwner.ID, authorEmail, true)

		if notifyErr := fixture.notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-success-7", Status: domain.BuildStatusSuccess, SourceAuthorEmail: &authorEmail}); notifyErr != nil {
			t.Fatalf("notify terminal build failed: %v", notifyErr)
		}
		if len(fixture.sender.messages) != 0 {
			t.Fatalf("expected foreign owned target to be ignored, got %+v", fixture.sender.messages)
		}
	})

	t.Run("unknown commit author is skipped safely", func(t *testing.T) {
		fixture := newFixture(t, "author@example.com", true, true, false, true)
		unknown := "unknown@example.com"

		if notifyErr := fixture.notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-success-8", Status: domain.BuildStatusSuccess, SourceAuthorEmail: &unknown}); notifyErr != nil {
			t.Fatalf("notify terminal build failed: %v", notifyErr)
		}
		if len(fixture.sender.messages) != 0 {
			t.Fatalf("expected unknown author to be skipped, got %+v", fixture.sender.messages)
		}
	})

	t.Run("case-normalized author email matching works for success delivery", func(t *testing.T) {
		fixture := newFixture(t, "author@example.com", true, true, false, true)
		mixedCase := "AUTHOR@EXAMPLE.COM"

		if notifyErr := fixture.notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-success-9", Status: domain.BuildStatusSuccess, SourceAuthorEmail: &mixedCase}); notifyErr != nil {
			t.Fatalf("notify terminal build failed: %v", notifyErr)
		}
		if len(fixture.sender.messages) != 1 || fixture.sender.messages[0].To != "<author@example.com>" {
			t.Fatalf("expected normalized success delivery, got %+v", fixture.sender.messages)
		}
	})

	t.Run("manual success subscription plus personal success preference dedupes same target", func(t *testing.T) {
		authorEmail := "author@example.com"
		fixture := newFixture(t, authorEmail, true, true, false, true)
		targets, err := fixture.subscriptionRepo.ListTargets(context.Background())
		if err != nil {
			t.Fatalf("list targets failed: %v", err)
		}
		projectID := "project-1"
		mustCreateNotificationSubscription(t, fixture.subscriptionRepo, targets[0].ID, &projectID, nil, domain.NotificationEventTypeBuildSucceeded, true)

		if notifyErr := fixture.notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-success-10", ProjectID: projectID, Status: domain.BuildStatusSuccess, SourceAuthorEmail: &authorEmail}); notifyErr != nil {
			t.Fatalf("notify terminal build failed: %v", notifyErr)
		}
		if len(fixture.sender.messages) != 1 || fixture.sender.messages[0].To != "<author@example.com>" {
			t.Fatalf("expected same-target success dedupe, got %+v", fixture.sender.messages)
		}
		delivery := mustGetNotificationDelivery(t, fixture.deliveryRepo, "build-success-10", domain.NotificationEventTypeBuildSucceeded, "<author@example.com>")
		if delivery.Attempts != 1 {
			t.Fatalf("expected one success delivery attempt after same-target dedupe, got %d", delivery.Attempts)
		}
	})

	t.Run("distinct targets still each receive one success delivery", func(t *testing.T) {
		authorEmail := "author@example.com"
		fixture := newFixture(t, authorEmail, true, true, false, true)
		jobID := "job-1"
		manualTarget := mustCreateNotificationTarget(t, fixture.subscriptionRepo, "ops@example.com", true)
		mustCreateNotificationSubscription(t, fixture.subscriptionRepo, manualTarget.ID, nil, &jobID, domain.NotificationEventTypeBuildSucceeded, true)

		if notifyErr := fixture.notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-success-11", JobID: &jobID, Status: domain.BuildStatusSuccess, SourceAuthorEmail: &authorEmail}); notifyErr != nil {
			t.Fatalf("notify terminal build failed: %v", notifyErr)
		}
		if len(fixture.sender.messages) != 2 {
			t.Fatalf("expected distinct success targets to receive delivery, got %+v", fixture.sender.messages)
		}
	})

	t.Run("success and failure events remain distinct for dedupe", func(t *testing.T) {
		authorEmail := "author@example.com"
		fixture := newFixture(t, authorEmail, true, true, true, true)

		if notifyErr := fixture.notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-shared-12", Status: domain.BuildStatusSuccess, SourceAuthorEmail: &authorEmail}); notifyErr != nil {
			t.Fatalf("notify success build failed: %v", notifyErr)
		}
		if notifyErr := fixture.notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-shared-12", Status: domain.BuildStatusFailed, SourceAuthorEmail: &authorEmail}); notifyErr != nil {
			t.Fatalf("notify failure build failed: %v", notifyErr)
		}
		if len(fixture.sender.messages) != 2 {
			t.Fatalf("expected distinct success and failure deliveries, got %+v", fixture.sender.messages)
		}
		mustGetNotificationDelivery(t, fixture.deliveryRepo, "build-shared-12", domain.NotificationEventTypeBuildSucceeded, "<author@example.com>")
		mustGetNotificationDelivery(t, fixture.deliveryRepo, "build-shared-12", domain.NotificationEventTypeBuildFailed, "<author@example.com>")
	})

	t.Run("existing failed success delivery record is skipped without retry", func(t *testing.T) {
		authorEmail := "author@example.com"
		fixture := newFixture(t, authorEmail, true, true, false, true)
		message := "smtp unavailable"
		if _, err := fixture.deliveryRepo.Create(context.Background(), domain.NotificationDelivery{
			BuildID:         "build-success-13",
			EventType:       domain.NotificationEventTypeBuildSucceeded,
			Transport:       domain.NotificationTransportEmail,
			DestinationKind: domain.NotificationDestinationKindPersonalEmail,
			DestinationKey:  "email-personal:" + mustGetOwnedEmailTargetID(t, fixture.subscriptionRepo, fixture.authorUser.ID),
			RecipientUserID: &fixture.authorUser.ID,
			Recipient:       "<author@example.com>",
			Status:          domain.NotificationDeliveryStatusFailedPermanent,
			Attempts:        1,
			MaxAttempts:     3,
			FailureCategory: failureCategoryPtr(domain.NotificationDeliveryFailureCategoryPermanent),
			LastError:       &message,
		}); err != nil {
			t.Fatalf("seed failed success delivery record failed: %v", err)
		}

		if notifyErr := fixture.notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-success-13", Status: domain.BuildStatusSuccess, SourceAuthorEmail: &authorEmail}); notifyErr != nil {
			t.Fatalf("notify with existing failed success record should skip cleanly, got %v", notifyErr)
		}
		if len(fixture.sender.messages) != 0 {
			t.Fatalf("expected no resend when failed success delivery record already exists, got %+v", fixture.sender.messages)
		}
		delivery := mustGetNotificationDelivery(t, fixture.deliveryRepo, "build-success-13", domain.NotificationEventTypeBuildSucceeded, "<author@example.com>")
		if delivery.Status != domain.NotificationDeliveryStatusFailedPermanent {
			t.Fatalf("expected failed success delivery status to remain unchanged, got %q", delivery.Status)
		}
		if delivery.Attempts != 1 {
			t.Fatalf("expected success delivery attempts to remain 1, got %d", delivery.Attempts)
		}
	})
}

func TestBuildNotificationService_CommitAuthorPreferenceDelivery(t *testing.T) {
	newNotifier := func(t *testing.T, authorEmail string, targetEnabled bool, preferenceEnabled bool) (*BuildNotificationService, *recordingEmailSender, *memoryrepo.NotificationSubscriptionRepository) {
		t.Helper()

		sender := &recordingEmailSender{}
		deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
		userRepo := memoryrepo.NewUserRepository()
		preferenceRepo := memoryrepo.NewUserNotificationPreferenceRepository()
		subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
		user := mustCreateNotificationUser(t, userRepo, authorEmail)
		mustEnsureOwnedNotificationTarget(t, subscriptionRepo, user.ID, authorEmail, targetEnabled)
		mustUpsertNotificationPreference(t, preferenceRepo, user.ID, preferenceEnabled)

		notifier, err := NewBuildNotificationService(BuildNotificationConfig{
			Enabled:          true,
			Sender:           sender,
			DeliveryRepo:     deliveryRepo,
			SubscriptionRepo: subscriptionRepo,
			UserRepo:         userRepo,
			PreferenceRepo:   preferenceRepo,
		})
		if err != nil {
			t.Fatalf("create notifier failed: %v", err)
		}

		return notifier, sender, subscriptionRepo
	}

	t.Run("non-opted-in author does not receive personal author notification", func(t *testing.T) {
		authorEmail := "author@example.com"
		notifier, sender, _ := newNotifier(t, authorEmail, true, false)
		if notifyErr := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", Status: domain.BuildStatusFailed, SourceAuthorEmail: &authorEmail}); notifyErr != nil {
			t.Fatalf("notify terminal build failed: %v", notifyErr)
		}
		if len(sender.messages) != 0 {
			t.Fatalf("expected no personal author notification, got %+v", sender.messages)
		}
	})

	t.Run("unknown commit author is skipped safely", func(t *testing.T) {
		notifier, sender, _ := newNotifier(t, "author@example.com", true, true)
		unknown := "unknown@example.com"
		if notifyErr := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", Status: domain.BuildStatusFailed, SourceAuthorEmail: &unknown}); notifyErr != nil {
			t.Fatalf("notify terminal build failed: %v", notifyErr)
		}
		if len(sender.messages) != 0 {
			t.Fatalf("expected unknown author to be skipped, got %+v", sender.messages)
		}
	})

	t.Run("disabled personal target prevents delivery", func(t *testing.T) {
		authorEmail := "author@example.com"
		notifier, sender, _ := newNotifier(t, authorEmail, false, true)
		if notifyErr := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", Status: domain.BuildStatusFailed, SourceAuthorEmail: &authorEmail}); notifyErr != nil {
			t.Fatalf("notify terminal build failed: %v", notifyErr)
		}
		if len(sender.messages) != 0 {
			t.Fatalf("expected disabled target to prevent delivery, got %+v", sender.messages)
		}
	})

	t.Run("case-normalized email matching works", func(t *testing.T) {
		notifier, sender, _ := newNotifier(t, "author@example.com", true, true)
		mixedCase := "AUTHOR@EXAMPLE.COM"
		if notifyErr := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", Status: domain.BuildStatusFailed, SourceAuthorEmail: &mixedCase}); notifyErr != nil {
			t.Fatalf("notify terminal build failed: %v", notifyErr)
		}
		if len(sender.messages) != 1 || sender.messages[0].To != "<author@example.com>" {
			t.Fatalf("expected normalized author delivery, got %+v", sender.messages)
		}
	})

	t.Run("manual project subscription plus personal author preference produces one delivery to same target", func(t *testing.T) {
		authorEmail := "author@example.com"
		notifier, sender, subscriptionRepo := newNotifier(t, authorEmail, true, true)
		targets, err := subscriptionRepo.ListTargets(context.Background())
		if err != nil {
			t.Fatalf("list targets failed: %v", err)
		}
		projectID := "project-1"
		mustCreateNotificationSubscription(t, subscriptionRepo, targets[0].ID, &projectID, nil, domain.NotificationEventTypeBuildFailed, true)

		if notifyErr := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", ProjectID: projectID, Status: domain.BuildStatusFailed, SourceAuthorEmail: &authorEmail}); notifyErr != nil {
			t.Fatalf("notify terminal build failed: %v", notifyErr)
		}
		if len(sender.messages) != 1 || sender.messages[0].To != "<author@example.com>" {
			t.Fatalf("expected same-target dedupe, got %+v", sender.messages)
		}
	})

	t.Run("distinct targets still each receive delivery", func(t *testing.T) {
		authorEmail := "author@example.com"
		notifier, sender, subscriptionRepo := newNotifier(t, authorEmail, true, true)
		jobID := "job-1"
		manualTarget := mustCreateNotificationTarget(t, subscriptionRepo, "ops@example.com", true)
		mustCreateNotificationSubscription(t, subscriptionRepo, manualTarget.ID, nil, &jobID, domain.NotificationEventTypeBuildFailed, true)

		if notifyErr := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", JobID: &jobID, Status: domain.BuildStatusFailed, SourceAuthorEmail: &authorEmail}); notifyErr != nil {
			t.Fatalf("notify terminal build failed: %v", notifyErr)
		}
		if len(sender.messages) != 2 {
			t.Fatalf("expected distinct targets to receive delivery, got %+v", sender.messages)
		}
	})
}

func TestBuildNotificationService_NotifyTerminalBuild_UsesProjectSubscriptionRecipients(t *testing.T) {
	sender := &recordingEmailSender{}
	subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
	target := mustCreateNotificationTarget(t, subscriptionRepo, "dev@example.com", true)
	projectID := "project-1"
	mustCreateNotificationSubscription(t, subscriptionRepo, target.ID, &projectID, nil, domain.NotificationEventTypeBuildFailed, true)
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{
		Enabled:          true,
		Recipients:       "fallback@example.com",
		Sender:           sender,
		SubscriptionRepo: subscriptionRepo,
	})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	err = notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", ProjectID: projectID, Status: domain.BuildStatusFailed})
	if err != nil {
		t.Fatalf("notify terminal build failed: %v", err)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("expected one subscription email, got %d", len(sender.messages))
	}
	if sender.messages[0].To != "<dev@example.com>" {
		t.Fatalf("expected project subscription recipient, got %q", sender.messages[0].To)
	}
}

func TestBuildNotificationService_NotifyTerminalBuild_UsesJobSubscriptionRecipients(t *testing.T) {
	sender := &recordingEmailSender{}
	subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
	target := mustCreateNotificationTarget(t, subscriptionRepo, "job@example.com", true)
	jobID := "job-1"
	mustCreateNotificationSubscription(t, subscriptionRepo, target.ID, nil, &jobID, domain.NotificationEventTypeBuildSucceeded, true)
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{
		Enabled:          true,
		Sender:           sender,
		SubscriptionRepo: subscriptionRepo,
	})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	err = notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", ProjectID: "project-1", JobID: &jobID, Status: domain.BuildStatusSuccess})
	if err != nil {
		t.Fatalf("notify terminal build failed: %v", err)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("expected one job subscription email, got %d", len(sender.messages))
	}
	if sender.messages[0].To != "<job@example.com>" {
		t.Fatalf("expected job subscription recipient, got %q", sender.messages[0].To)
	}
}

func TestBuildNotificationService_NotifyTerminalBuild_NonMatchingSubscriptionEventDoesNotSend(t *testing.T) {
	sender := &recordingEmailSender{}
	subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
	target := mustCreateNotificationTarget(t, subscriptionRepo, "dev@example.com", true)
	projectID := "project-1"
	mustCreateNotificationSubscription(t, subscriptionRepo, target.ID, &projectID, nil, domain.NotificationEventTypeBuildSucceeded, true)
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{
		Enabled:          true,
		Sender:           sender,
		SubscriptionRepo: subscriptionRepo,
	})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	err = notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", ProjectID: projectID, Status: domain.BuildStatusFailed})
	if err != nil {
		t.Fatalf("notify terminal build failed: %v", err)
	}
	if len(sender.messages) != 0 {
		t.Fatalf("expected no email for non-matching event, got %d", len(sender.messages))
	}
}

func TestBuildNotificationService_NotifyTerminalBuild_DisabledTargetDoesNotSend(t *testing.T) {
	sender := &recordingEmailSender{}
	subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
	target := mustCreateNotificationTarget(t, subscriptionRepo, "dev@example.com", false)
	projectID := "project-1"
	mustCreateNotificationSubscription(t, subscriptionRepo, target.ID, &projectID, nil, domain.NotificationEventTypeBuildFailed, true)
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{
		Enabled:          true,
		Sender:           sender,
		SubscriptionRepo: subscriptionRepo,
	})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	err = notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", ProjectID: projectID, Status: domain.BuildStatusFailed})
	if err != nil {
		t.Fatalf("notify terminal build failed: %v", err)
	}
	if len(sender.messages) != 0 {
		t.Fatalf("expected disabled target to suppress send, got %d", len(sender.messages))
	}
}

func TestBuildNotificationService_NotifyTerminalBuild_DisabledSubscriptionDoesNotSend(t *testing.T) {
	sender := &recordingEmailSender{}
	subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
	target := mustCreateNotificationTarget(t, subscriptionRepo, "dev@example.com", true)
	projectID := "project-1"
	mustCreateNotificationSubscription(t, subscriptionRepo, target.ID, &projectID, nil, domain.NotificationEventTypeBuildFailed, false)
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{
		Enabled:          true,
		Sender:           sender,
		SubscriptionRepo: subscriptionRepo,
	})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	err = notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", ProjectID: projectID, Status: domain.BuildStatusFailed})
	if err != nil {
		t.Fatalf("notify terminal build failed: %v", err)
	}
	if len(sender.messages) != 0 {
		t.Fatalf("expected disabled subscription to suppress send, got %d", len(sender.messages))
	}
}

func TestBuildNotificationService_NotifyTerminalBuild_DedupesProjectAndJobSubscriptionsForSameRecipient(t *testing.T) {
	sender := &recordingEmailSender{}
	deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
	subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
	target := mustCreateNotificationTarget(t, subscriptionRepo, "dev@example.com", true)
	projectID := "project-1"
	jobID := "job-1"
	mustCreateNotificationSubscription(t, subscriptionRepo, target.ID, &projectID, nil, domain.NotificationEventTypeBuildFailed, true)
	mustCreateNotificationSubscription(t, subscriptionRepo, target.ID, nil, &jobID, domain.NotificationEventTypeBuildFailed, true)
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{
		Enabled:          true,
		Sender:           sender,
		DeliveryRepo:     deliveryRepo,
		SubscriptionRepo: subscriptionRepo,
	})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	build := domain.Build{ID: "build-1", ProjectID: projectID, JobID: &jobID, Status: domain.BuildStatusFailed}
	err = notifier.NotifyTerminalBuild(context.Background(), build)
	if err != nil {
		t.Fatalf("notify terminal build failed: %v", err)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("expected one deduped email, got %d", len(sender.messages))
	}
	delivery := mustGetNotificationDelivery(t, deliveryRepo, "build-1", domain.NotificationEventTypeBuildFailed, "<dev@example.com>")
	if delivery.Attempts != 1 {
		t.Fatalf("expected one delivery attempt, got %d", delivery.Attempts)
	}
}

func TestBuildNotificationService_NotifyTerminalBuild_FallsBackToEnvRecipientsWithoutSubscriptions(t *testing.T) {
	sender := &recordingEmailSender{}
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{
		Enabled:          true,
		Recipients:       "fallback@example.com",
		Sender:           sender,
		SubscriptionRepo: memoryrepo.NewNotificationSubscriptionRepository(),
	})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	err = notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusFailed})
	if err != nil {
		t.Fatalf("notify terminal build failed: %v", err)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("expected fallback email, got %d", len(sender.messages))
	}
	if sender.messages[0].To != "<fallback@example.com>" {
		t.Fatalf("expected fallback recipient, got %q", sender.messages[0].To)
	}
}

func TestBuildNotificationService_NotifyTerminalBuild_DurableDedupeStillWorksForSubscriptionRecipients(t *testing.T) {
	sender := &recordingEmailSender{}
	deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
	subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
	target := mustCreateNotificationTarget(t, subscriptionRepo, "dev@example.com", true)
	projectID := "project-1"
	mustCreateNotificationSubscription(t, subscriptionRepo, target.ID, &projectID, nil, domain.NotificationEventTypeBuildFailed, true)
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{
		Enabled:          true,
		Sender:           sender,
		DeliveryRepo:     deliveryRepo,
		SubscriptionRepo: subscriptionRepo,
	})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	build := domain.Build{ID: "build-1", ProjectID: projectID, Status: domain.BuildStatusFailed}
	if notifyErr := notifier.NotifyTerminalBuild(context.Background(), build); notifyErr != nil {
		t.Fatalf("first notify failed: %v", notifyErr)
	}
	if notifyErr := notifier.NotifyTerminalBuild(context.Background(), build); notifyErr != nil {
		t.Fatalf("second notify failed: %v", notifyErr)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("expected one deduped subscription email, got %d", len(sender.messages))
	}
	delivery := mustGetNotificationDelivery(t, deliveryRepo, "build-1", domain.NotificationEventTypeBuildFailed, "<dev@example.com>")
	if delivery.Attempts != 1 {
		t.Fatalf("expected attempts to remain 1, got %d", delivery.Attempts)
	}
}

func TestBuildNotificationHelpers(t *testing.T) {
	if got, ok := buildStatusNotificationEventType(domain.BuildStatusSuccess); !ok || got != domain.NotificationEventTypeBuildSucceeded {
		t.Fatalf("expected success event type, got %q ok=%v", got, ok)
	}
	if got, ok := buildStatusNotificationEventType(domain.BuildStatusFailed); !ok || got != domain.NotificationEventTypeBuildFailed {
		t.Fatalf("expected failed event type, got %q ok=%v", got, ok)
	}
	if got, ok := buildStatusNotificationEventType(domain.BuildStatusCanceled); ok || got != "" {
		t.Fatalf("expected no event type for canceled build, got %q ok=%v", got, ok)
	}

	if got := buildStatusNotificationSummary(domain.BuildStatusCanceled); got != string(domain.BuildStatusCanceled) {
		t.Fatalf("expected fallback summary to echo status, got %q", got)
	}

	recipients, err := parseNotificationRecipients("")
	if err != nil {
		t.Fatalf("expected empty recipients to parse cleanly, got %v", err)
	}
	if len(recipients) != 0 {
		t.Fatalf("expected no recipients, got %v", recipients)
	}

	recipients, err = parseNotificationRecipients("dev@example.com, qa@example.com")
	if err != nil {
		t.Fatalf("expected recipients to parse, got %v", err)
	}
	if want := []string{"<dev@example.com>", "<qa@example.com>"}; fmt.Sprint(recipients) != fmt.Sprint(want) {
		t.Fatalf("unexpected parsed recipients: got %v want %v", recipients, want)
	}
}

func TestBuildNotificationHelpers_DedupeRecipients(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  []string
	}{
		{name: "dedupes and trims", input: []string{"alice@example.com", " alice@example.com ", "bob@example.com"}, want: []string{"alice@example.com", "bob@example.com"}},
		{name: "skips empty strings", input: []string{"alice@example.com", "", "   ", "bob@example.com"}, want: []string{"alice@example.com", "bob@example.com"}},
		{name: "empty input", input: []string{}, want: nil},
		{name: "all duplicates", input: []string{"alice@example.com", "alice@example.com", "alice@example.com"}, want: []string{"alice@example.com"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := dedupeRecipients(tc.input)
			if len(got) != len(tc.want) {
				t.Fatalf("dedupeRecipients mismatch: got length %d, want %d (got %v, want %v)", len(got), len(tc.want), got, tc.want)
			}
			for i, item := range got {
				if item != tc.want[i] {
					t.Fatalf("dedupeRecipients mismatch at index %d: got %q, want %q", i, item, tc.want[i])
				}
			}
		})
	}
}

func TestBuildNotificationHelpers_ParseNotificationRecipient(t *testing.T) {
	valid := "alice@example.com"
	withName := "Alice <alice@example.com>"
	withSpace := "  bob@example.com  "
	invalid := "not-an-email"
	empty := ""

	tests := []struct {
		name      string
		value     *string
		wantOk    bool
		wantCheck func(got string) bool
	}{
		{name: "valid email", value: &valid, wantOk: true, wantCheck: func(got string) bool { return strings.Contains(got, "alice@example.com") }},
		{name: "email with name", value: &withName, wantOk: true, wantCheck: func(got string) bool { return strings.Contains(got, "alice@example.com") }},
		{name: "nil value", value: nil, wantOk: false, wantCheck: func(got string) bool { return got == "" }},
		{name: "empty string", value: &empty, wantOk: false, wantCheck: func(got string) bool { return got == "" }},
		{name: "whitespace only", value: &withSpace, wantOk: true, wantCheck: func(got string) bool { return strings.Contains(got, "bob@example.com") }},
		{name: "invalid email", value: &invalid, wantOk: false, wantCheck: func(got string) bool { return got == "" }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseNotificationRecipient(tc.value)
			if ok != tc.wantOk {
				t.Fatalf("parseNotificationRecipient ok mismatch: got %v, want %v", ok, tc.wantOk)
			}
			if !tc.wantCheck(got) {
				t.Fatalf("parseNotificationRecipient result failed check: got %q", got)
			}
		})
	}
}
