package build

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	memoryrepo "github.com/radiation/coyote-ci/backend/internal/repository/memory"
)

func TestBuildNotificationService_NotifyTerminalBuild_ExecutionPaths(t *testing.T) {
	t.Run("records sent delivery state", func(t *testing.T) {
		deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
		sender := &recordingEmailSender{}
		notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com", Sender: sender, DeliveryRepo: deliveryRepo, SubscriptionRepo: memoryrepo.NewNotificationSubscriptionRepository()})
		if err != nil {
			t.Fatalf("create notifier failed: %v", err)
		}
		if notifyErr := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", Status: domain.BuildStatusSuccess}); notifyErr != nil {
			t.Fatalf("notify terminal build failed: %v", notifyErr)
		}

		delivery := mustGetNotificationDelivery(t, deliveryRepo, "build-1", domain.NotificationEventTypeBuildSucceeded, "<dev@example.com>")
		if delivery.Status != domain.NotificationDeliveryStatusSent {
			t.Fatalf("expected sent delivery status, got %q", delivery.Status)
		}
		if delivery.Attempts != 1 {
			t.Fatalf("expected one attempt, got %d", delivery.Attempts)
		}
		if delivery.SentAt == nil || delivery.SentAt.IsZero() {
			t.Fatal("expected sent_at to be set")
		}
	})

	t.Run("records failed delivery state", func(t *testing.T) {
		deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
		sender := &recordingEmailSender{err: errors.New("smtp unavailable")}
		notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com", Sender: sender, DeliveryRepo: deliveryRepo, SubscriptionRepo: memoryrepo.NewNotificationSubscriptionRepository()})
		if err != nil {
			t.Fatalf("create notifier failed: %v", err)
		}
		if notifyErr := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", Status: domain.BuildStatusFailed}); notifyErr == nil {
			t.Fatal("expected sender failure")
		}

		delivery := mustGetNotificationDelivery(t, deliveryRepo, "build-1", domain.NotificationEventTypeBuildFailed, "<dev@example.com>")
		if delivery.Status != domain.NotificationDeliveryStatusRetryWaiting {
			t.Fatalf("expected retry_waiting delivery status, got %q", delivery.Status)
		}
		if delivery.Attempts != 1 {
			t.Fatalf("expected one attempt, got %d", delivery.Attempts)
		}
		if delivery.LastError == nil || !strings.Contains(*delivery.LastError, "smtp unavailable") {
			t.Fatalf("expected last_error to be recorded, got %v", delivery.LastError)
		}
		if delivery.SentAt != nil {
			t.Fatalf("expected no sent_at for failed delivery, got %v", delivery.SentAt)
		}
	})

	t.Run("duplicate terminal hook does not send twice", func(t *testing.T) {
		deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
		sender := &recordingEmailSender{}
		notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com", Sender: sender, DeliveryRepo: deliveryRepo, SubscriptionRepo: memoryrepo.NewNotificationSubscriptionRepository()})
		if err != nil {
			t.Fatalf("create notifier failed: %v", err)
		}

		build := domain.Build{ID: "build-1", Status: domain.BuildStatusFailed}
		if notifyErr := notifier.NotifyTerminalBuild(context.Background(), build); notifyErr != nil {
			t.Fatalf("first notify failed: %v", notifyErr)
		}
		if notifyErr := notifier.NotifyTerminalBuild(context.Background(), build); notifyErr != nil {
			t.Fatalf("duplicate notify should skip cleanly, got %v", notifyErr)
		}
		if len(sender.messages) != 1 {
			t.Fatalf("expected one sent email after duplicate hook, got %d", len(sender.messages))
		}

		delivery := mustGetNotificationDelivery(t, deliveryRepo, "build-1", domain.NotificationEventTypeBuildFailed, "<dev@example.com>")
		if delivery.Attempts != 1 {
			t.Fatalf("expected attempts to remain 1 after duplicate hook, got %d", delivery.Attempts)
		}
	})

	t.Run("existing failed delivery record is skipped without retry", func(t *testing.T) {
		deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
		subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
		sharedTarget, err := subscriptionRepo.EnsureConfigEmailTarget(context.Background(), repository.EnsureConfigNotificationEmailTargetInput{Name: "dev@example.com", Recipient: "dev@example.com"})
		if err != nil {
			t.Fatalf("ensure shared target failed: %v", err)
		}
		message := "smtp unavailable"
		if _, createErr := deliveryRepo.Create(context.Background(), domain.NotificationDelivery{BuildID: "build-1", EventType: domain.NotificationEventTypeBuildFailed, Transport: domain.NotificationTransportEmail, DestinationKind: domain.NotificationDestinationKindSharedTarget, DestinationKey: "email-target:" + sharedTarget.ID, NotificationTargetID: &sharedTarget.ID, Recipient: "<dev@example.com>", Status: domain.NotificationDeliveryStatusFailedPermanent, Attempts: 1, MaxAttempts: 3, FailureCategory: failureCategoryPtr(domain.NotificationDeliveryFailureCategoryPermanent), LastError: &message}); createErr != nil {
			t.Fatalf("seed failed delivery record failed: %v", createErr)
		}

		sender := &recordingEmailSender{}
		notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com", Sender: sender, DeliveryRepo: deliveryRepo, SubscriptionRepo: subscriptionRepo})
		if err != nil {
			t.Fatalf("create notifier failed: %v", err)
		}
		if notifyErr := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", Status: domain.BuildStatusFailed}); notifyErr != nil {
			t.Fatalf("notify with existing failed record should skip cleanly, got %v", notifyErr)
		}
		if len(sender.messages) != 0 {
			t.Fatalf("expected no resend when failed delivery record already exists, got %d", len(sender.messages))
		}

		delivery := mustGetNotificationDelivery(t, deliveryRepo, "build-1", domain.NotificationEventTypeBuildFailed, "<dev@example.com>")
		if delivery.Status != domain.NotificationDeliveryStatusFailedPermanent {
			t.Fatalf("expected failed delivery status to remain unchanged, got %q", delivery.Status)
		}
		if delivery.Attempts != 1 {
			t.Fatalf("expected attempts to remain 1 for skipped failed record, got %d", delivery.Attempts)
		}
	})

	t.Run("delivery create error is returned", func(t *testing.T) {
		repo := &scriptedNotificationDeliveryRepo{createFunc: func(context.Context, domain.NotificationDelivery) (domain.NotificationDelivery, error) {
			return domain.NotificationDelivery{}, errors.New("create failed")
		}}
		notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com", Sender: &recordingEmailSender{}, DeliveryRepo: repo, SubscriptionRepo: memoryrepo.NewNotificationSubscriptionRepository()})
		if err != nil {
			t.Fatalf("create notifier failed: %v", err)
		}
		if notifyErr := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", Status: domain.BuildStatusFailed}); notifyErr == nil || notifyErr.Error() != "create failed" {
			t.Fatalf("expected create failure, got %v", notifyErr)
		}
	})

	t.Run("duplicate record lookup error is returned", func(t *testing.T) {
		repo := &scriptedNotificationDeliveryRepo{createFunc: func(context.Context, domain.NotificationDelivery) (domain.NotificationDelivery, error) {
			return domain.NotificationDelivery{}, repository.ErrNotificationDeliveryDuplicate
		}, getFunc: func(context.Context, string, domain.NotificationEventType, string) (domain.NotificationDelivery, error) {
			return domain.NotificationDelivery{}, errors.New("lookup failed")
		}}
		notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com", Sender: &recordingEmailSender{}, DeliveryRepo: repo, SubscriptionRepo: memoryrepo.NewNotificationSubscriptionRepository()})
		if err != nil {
			t.Fatalf("create notifier failed: %v", err)
		}
		if notifyErr := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", Status: domain.BuildStatusFailed}); notifyErr == nil || notifyErr.Error() != "lookup failed" {
			t.Fatalf("expected lookup failure, got %v", notifyErr)
		}
	})

	t.Run("sent state persistence failure marks delivery failed", func(t *testing.T) {
		updateCalls := 0
		repo := &scriptedNotificationDeliveryRepo{createFunc: func(_ context.Context, delivery domain.NotificationDelivery) (domain.NotificationDelivery, error) {
			delivery.ID = "delivery-1"
			delivery.CreatedAt = time.Now().UTC()
			return delivery, nil
		}, updateFunc: func(_ context.Context, delivery domain.NotificationDelivery) (domain.NotificationDelivery, error) {
			updateCalls++
			if updateCalls == 1 {
				if delivery.Status != domain.NotificationDeliveryStatusSent {
					t.Fatalf("expected first update to persist sent status, got %q", delivery.Status)
				}
				return domain.NotificationDelivery{}, errors.New("write sent failed")
			}
			if delivery.Status != domain.NotificationDeliveryStatusRetryWaiting {
				t.Fatalf("expected fallback update to schedule retry, got %q", delivery.Status)
			}
			if delivery.LastError == nil || !strings.Contains(*delivery.LastError, "persist sent delivery state failed") {
				t.Fatalf("expected fallback last error to describe sent-state persistence failure, got %v", delivery.LastError)
			}
			return delivery, nil
		}}
		notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com", Sender: &recordingEmailSender{}, DeliveryRepo: repo, SubscriptionRepo: memoryrepo.NewNotificationSubscriptionRepository()})
		if err != nil {
			t.Fatalf("create notifier failed: %v", err)
		}
		err = notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", Status: domain.BuildStatusSuccess})
		if err == nil || !strings.Contains(err.Error(), "persist sent delivery state failed") {
			t.Fatalf("expected sent-state persistence failure, got %v", err)
		}
		if updateCalls != 2 {
			t.Fatalf("expected two update attempts, got %d", updateCalls)
		}
	})

	t.Run("failed fallback persistence returns joined error", func(t *testing.T) {
		repo := &scriptedNotificationDeliveryRepo{createFunc: func(_ context.Context, delivery domain.NotificationDelivery) (domain.NotificationDelivery, error) {
			delivery.ID = "delivery-1"
			delivery.CreatedAt = time.Now().UTC()
			return delivery, nil
		}, updateFunc: func(_ context.Context, delivery domain.NotificationDelivery) (domain.NotificationDelivery, error) {
			if delivery.Status == domain.NotificationDeliveryStatusSent {
				return domain.NotificationDelivery{}, errors.New("write sent failed")
			}
			return domain.NotificationDelivery{}, errors.New("write failed failed")
		}}
		notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com", Sender: &recordingEmailSender{}, DeliveryRepo: repo, SubscriptionRepo: memoryrepo.NewNotificationSubscriptionRepository()})
		if err != nil {
			t.Fatalf("create notifier failed: %v", err)
		}
		err = notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", Status: domain.BuildStatusSuccess})
		if err == nil {
			t.Fatal("expected joined persistence error")
		}
		message := err.Error()
		if !strings.Contains(message, "persist sent delivery state failed") || !strings.Contains(message, "write failed failed") {
			t.Fatalf("expected joined error message, got %q", message)
		}
	})

	t.Run("multiple recipients create separate delivery records", func(t *testing.T) {
		deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
		sender := &recordingEmailSender{}
		notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com, qa@example.com", Sender: sender, DeliveryRepo: deliveryRepo, SubscriptionRepo: memoryrepo.NewNotificationSubscriptionRepository()})
		if err != nil {
			t.Fatalf("create notifier failed: %v", err)
		}
		if notifyErr := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", Status: domain.BuildStatusSuccess}); notifyErr != nil {
			t.Fatalf("notify terminal build failed: %v", notifyErr)
		}
		if len(sender.messages) != 2 {
			t.Fatalf("expected one message per recipient, got %d", len(sender.messages))
		}

		for _, recipient := range []string{"<dev@example.com>", "<qa@example.com>"} {
			delivery := mustGetNotificationDelivery(t, deliveryRepo, "build-1", domain.NotificationEventTypeBuildSucceeded, recipient)
			if delivery.Status != domain.NotificationDeliveryStatusSent {
				t.Fatalf("expected sent delivery for %s, got %q", recipient, delivery.Status)
			}
		}
	})
}

func TestBuildNotificationService_NotifyTerminalBuild_SlackFailureDoesNotBlockEmail(t *testing.T) {
	emailSender := &recordingEmailSender{}
	slackSender := &recordingSlackSender{err: errors.New("webhook unavailable")}
	subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
	deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
	projectID := "project-1"
	emailTarget := mustCreateNotificationTarget(t, subscriptionRepo, "dev@example.com", true)
	slackTarget := mustCreateSlackNotificationTarget(t, subscriptionRepo, "https://hooks.slack.example/services/T/B/X", true)
	mustCreateNotificationSubscription(t, subscriptionRepo, emailTarget.ID, &projectID, nil, domain.NotificationEventTypeBuildFailed, true)
	mustCreateNotificationSubscription(t, subscriptionRepo, slackTarget.ID, &projectID, nil, domain.NotificationEventTypeBuildFailed, true)
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Sender: emailSender, SlackSender: slackSender, DeliveryRepo: deliveryRepo, SubscriptionRepo: subscriptionRepo})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	err = notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", ProjectID: projectID, Status: domain.BuildStatusFailed})
	if err == nil || !strings.Contains(err.Error(), "webhook unavailable") {
		t.Fatalf("expected slack failure error, got %v", err)
	}
	if len(emailSender.messages) != 1 {
		t.Fatalf("expected email delivery to proceed, got %d emails", len(emailSender.messages))
	}
	emailDelivery := mustGetNotificationDelivery(t, deliveryRepo, "build-1", domain.NotificationEventTypeBuildFailed, "<dev@example.com>")
	if emailDelivery.Status != domain.NotificationDeliveryStatusSent {
		t.Fatalf("expected email delivery sent, got %q", emailDelivery.Status)
	}
	slackDelivery := mustGetNotificationDelivery(t, deliveryRepo, "build-1", domain.NotificationEventTypeBuildFailed, "slack_webhook:"+slackTarget.ID)
	if slackDelivery.Status != domain.NotificationDeliveryStatusRetryWaiting {
		t.Fatalf("expected slack delivery retry_waiting, got %q", slackDelivery.Status)
	}
}

func TestBuildNotificationService_NotifyTerminalBuild_SlackFailureEnrichmentErrorsAreRetryable(t *testing.T) {
	slackSender := &recordingSlackSender{}
	deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
	subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
	target := mustCreateSlackNotificationTarget(t, subscriptionRepo, "https://hooks.slack.example/services/T/B/X", true)
	projectID := "project-1"
	mustCreateNotificationSubscription(t, subscriptionRepo, target.ID, &projectID, nil, domain.NotificationEventTypeBuildFailed, true)
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, SlackSender: slackSender, BuildRepo: &fakeBuildRepository{stepsErr: errors.New("db unavailable")}, DeliveryRepo: deliveryRepo, SubscriptionRepo: subscriptionRepo, PublicBaseURL: "https://ci.example.com/"})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	err = notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", ProjectID: projectID, Status: domain.BuildStatusFailed})
	if err == nil || !strings.Contains(err.Error(), "notification failed-step enrichment failed") {
		t.Fatalf("expected retryable failed-step enrichment error, got %v", err)
	}
	if len(slackSender.messages) != 0 {
		t.Fatalf("expected slack delivery not to send on failed-step enrichment error, got %d messages", len(slackSender.messages))
	}
	delivery := mustGetNotificationDelivery(t, deliveryRepo, "build-1", domain.NotificationEventTypeBuildFailed, "slack_webhook:"+target.ID)
	if delivery.Status != domain.NotificationDeliveryStatusRetryWaiting {
		t.Fatalf("expected retry_waiting slack delivery, got %q", delivery.Status)
	}
	if delivery.LastError == nil || !strings.Contains(*delivery.LastError, "notification failed-step enrichment failed") {
		t.Fatalf("expected delivery last_error to record enrichment failure, got %v", delivery.LastError)
	}
}

func TestBuildNotificationService_NotifyTerminalBuild_SlackSuccessEnrichmentErrorsAreRetryable(t *testing.T) {
	slackSender := &recordingSlackSender{}
	deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
	subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
	target := mustCreateSlackNotificationTarget(t, subscriptionRepo, "https://hooks.slack.example/services/T/B/X", true)
	projectID := "project-1"
	mustCreateNotificationSubscription(t, subscriptionRepo, target.ID, &projectID, nil, domain.NotificationEventTypeBuildSucceeded, true)
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, SlackSender: slackSender, ArtifactRepo: &fakeArtifactRepository{listErr: errors.New("storage unavailable")}, DeliveryRepo: deliveryRepo, SubscriptionRepo: subscriptionRepo, PublicBaseURL: "https://ci.example.com/"})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	err = notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", ProjectID: projectID, Status: domain.BuildStatusSuccess})
	if err == nil || !strings.Contains(err.Error(), "notification artifact enrichment failed") {
		t.Fatalf("expected retryable artifact enrichment error, got %v", err)
	}
	if len(slackSender.messages) != 0 {
		t.Fatalf("expected slack delivery not to send on artifact enrichment error, got %d messages", len(slackSender.messages))
	}
	delivery := mustGetNotificationDelivery(t, deliveryRepo, "build-1", domain.NotificationEventTypeBuildSucceeded, "slack_webhook:"+target.ID)
	if delivery.Status != domain.NotificationDeliveryStatusRetryWaiting {
		t.Fatalf("expected retry_waiting slack delivery, got %q", delivery.Status)
	}
	if delivery.LastError == nil || !strings.Contains(*delivery.LastError, "notification artifact enrichment failed") {
		t.Fatalf("expected delivery last_error to record artifact enrichment failure, got %v", delivery.LastError)
	}
}

func TestBuildNotificationService_NotifyTerminalBuild_SlackFailureSanitizedErrorDoesNotLeakWebhookToken(t *testing.T) {
	emailSender := &recordingEmailSender{}
	secretToken := "SECRET_TOKEN_123"
	slackSender := NewSlackWebhookSender(&recordingSlackHTTPDoer{err: errors.New("Post \"https://hooks.slack.example/services/T/B/" + secretToken + "\": dial tcp: lookup hooks.slack.example: i/o timeout")})
	subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
	deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
	projectID := "project-1"
	emailTarget := mustCreateNotificationTarget(t, subscriptionRepo, "dev@example.com", true)
	slackTarget := mustCreateSlackNotificationTarget(t, subscriptionRepo, "https://hooks.slack.example/services/T/B/"+secretToken, true)
	mustCreateNotificationSubscription(t, subscriptionRepo, emailTarget.ID, &projectID, nil, domain.NotificationEventTypeBuildFailed, true)
	mustCreateNotificationSubscription(t, subscriptionRepo, slackTarget.ID, &projectID, nil, domain.NotificationEventTypeBuildFailed, true)
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Sender: emailSender, SlackSender: slackSender, DeliveryRepo: deliveryRepo, SubscriptionRepo: subscriptionRepo})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	err = notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", ProjectID: projectID, Status: domain.BuildStatusFailed})
	if err == nil || !strings.Contains(err.Error(), "slack webhook request failed") {
		t.Fatalf("expected sanitized slack failure error, got %v", err)
	}
	if strings.Contains(err.Error(), secretToken) {
		t.Fatalf("expected returned error to hide webhook token, got %q", err.Error())
	}
	if len(emailSender.messages) != 1 {
		t.Fatalf("expected email delivery to proceed, got %d emails", len(emailSender.messages))
	}
	slackDelivery := mustGetNotificationDelivery(t, deliveryRepo, "build-1", domain.NotificationEventTypeBuildFailed, "slack_webhook:"+slackTarget.ID)
	if slackDelivery.LastError == nil || *slackDelivery.LastError == "" {
		t.Fatalf("expected slack delivery last_error to be recorded, got %v", slackDelivery.LastError)
	}
	if strings.Contains(*slackDelivery.LastError, secretToken) {
		t.Fatalf("expected persisted last_error to hide webhook token, got %q", *slackDelivery.LastError)
	}
	if *slackDelivery.LastError != "slack webhook request failed" {
		t.Fatalf("expected sanitized persisted last_error, got %q", *slackDelivery.LastError)
	}
}

func TestBuildNotificationService_ExecutionHelpers(t *testing.T) {
	t.Run("send destination requires configured transport clients", func(t *testing.T) {
		notifier := &BuildNotificationService{}
		if err := notifier.sendDestination(context.Background(), notificationDestination{transport: domain.NotificationTransportSlackWebhook, webhookURL: "https://hooks.slack.example/services/T/B/X"}, "", "", "payload", ""); err == nil || err.Error() != "slack sender is not configured" {
			t.Fatalf("expected missing slack sender error, got %v", err)
		}
		if err := notifier.sendDestination(context.Background(), notificationDestination{transport: domain.NotificationTransportSlackDM, slackBotToken: "xoxb-token", slackUserID: "U123"}, "", "", "", "payload"); err == nil || err.Error() != "slack client is not configured" {
			t.Fatalf("expected missing slack client error, got %v", err)
		}
		if err := notifier.sendDestination(context.Background(), notificationDestination{transport: domain.NotificationTransportEmail, emailRecipient: "<dev@example.com>"}, "subject", "body", "", ""); err == nil || err.Error() != "email sender is not configured" {
			t.Fatalf("expected missing email sender error, got %v", err)
		}
	})

	t.Run("context cancellation leaves claimed delivery active", func(t *testing.T) {
		claimedAt := time.Now().UTC()
		notifier := &BuildNotificationService{
			sender: &recordingEmailSender{err: context.Canceled},
			now:    func() time.Time { return claimedAt },
		}
		delivery := domain.NotificationDelivery{
			ID:          "delivery-1",
			BuildID:     "build-1",
			EventType:   domain.NotificationEventTypeBuildFailed,
			Transport:   domain.NotificationTransportEmail,
			ClaimedAt:   &claimedAt,
			Attempts:    1,
			MaxAttempts: 3,
		}

		outcome, err := notifier.executeClaimedDelivery(context.Background(), delivery, notificationDestination{transport: domain.NotificationTransportEmail, emailRecipient: "<dev@example.com>"}, notificationContent{subject: "subject", body: "body"}, notificationRecoveryReasonInline)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context canceled error, got %v", err)
		}
		if outcome != notificationExecutionOutcomeNone {
			t.Fatalf("expected no execution outcome on cancellation, got %q", outcome)
		}
	})
}
