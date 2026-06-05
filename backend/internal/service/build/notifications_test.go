package build

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	platformemail "github.com/radiation/coyote-ci/backend/internal/platform/email"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	memoryrepo "github.com/radiation/coyote-ci/backend/internal/repository/memory"
	steprunner "github.com/radiation/coyote-ci/backend/internal/runner"
)

type recordingEmailSender struct {
	messages []platformemail.Message
	err      error
}

func (s *recordingEmailSender) SendText(_ context.Context, message platformemail.Message) error {
	s.messages = append(s.messages, message)
	return s.err
}

type scriptedNotificationDeliveryRepo struct {
	createFunc func(context.Context, domain.NotificationDelivery) (domain.NotificationDelivery, error)
	getFunc    func(context.Context, string, domain.NotificationEventType, string) (domain.NotificationDelivery, error)
	updateFunc func(context.Context, domain.NotificationDelivery) (domain.NotificationDelivery, error)
}

func (r *scriptedNotificationDeliveryRepo) Create(ctx context.Context, delivery domain.NotificationDelivery) (domain.NotificationDelivery, error) {
	if r.createFunc != nil {
		return r.createFunc(ctx, delivery)
	}
	return domain.NotificationDelivery{}, nil
}

func (r *scriptedNotificationDeliveryRepo) GetByBuildEventRecipient(ctx context.Context, buildID string, eventType domain.NotificationEventType, recipient string) (domain.NotificationDelivery, error) {
	if r.getFunc != nil {
		return r.getFunc(ctx, buildID, eventType, recipient)
	}
	return domain.NotificationDelivery{}, nil
}

func (r *scriptedNotificationDeliveryRepo) Update(ctx context.Context, delivery domain.NotificationDelivery) (domain.NotificationDelivery, error) {
	if r.updateFunc != nil {
		return r.updateFunc(ctx, delivery)
	}
	return delivery, nil
}

func mustGetNotificationDelivery(t *testing.T, repo repository.NotificationDeliveryRepository, buildID string, eventType domain.NotificationEventType, recipient string) domain.NotificationDelivery {
	t.Helper()

	delivery, err := repo.GetByBuildEventRecipient(context.Background(), buildID, eventType, recipient)
	if err != nil {
		t.Fatalf("get notification delivery failed: %v", err)
	}

	return delivery
}

func TestBuildService_FailBuild_SendsNotificationWhenConfigured(t *testing.T) {
	now := time.Now().UTC()
	jobID := "job-1"
	deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
	buildRepo := &fakeBuildRepository{
		build: domain.Build{
			ID:          "build-1",
			ProjectID:   "project-1",
			JobID:       &jobID,
			BuildNumber: 42,
			Status:      domain.BuildStatusRunning,
			CreatedAt:   now,
		},
	}
	jobRepo := memoryrepo.NewJobRepository()
	projectRepo := memoryrepo.NewProjectRepository(jobRepo)
	if _, err := projectRepo.Create(context.Background(), domain.Project{ID: "project-1", Name: "Payments API", Slug: "payments-api", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create project failed: %v", err)
	}
	if _, err := jobRepo.Create(context.Background(), domain.Job{ID: jobID, ProjectID: "project-1", Name: "backend-ci", RepositoryURL: "https://github.com/example/payments.git", DefaultRef: "main", PipelineYAML: "version: 1", Enabled: true, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create job failed: %v", err)
	}
	sender := &recordingEmailSender{}
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{
		Enabled:      true,
		Recipients:   "dev@example.com",
		Sender:       sender,
		JobRepo:      jobRepo,
		ProjectRepo:  projectRepo,
		DeliveryRepo: deliveryRepo,
	})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	svc := NewBuildServiceFromConfig(buildRepo, nil, nil, BuildServiceConfig{BuildNotifier: notifier})
	build, err := svc.FailBuild(context.Background(), "build-1")
	if err != nil {
		t.Fatalf("fail build returned error: %v", err)
	}
	if build.Status != domain.BuildStatusFailed {
		t.Fatalf("expected failed build, got %q", build.Status)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("expected one notification email, got %d", len(sender.messages))
	}
	message := sender.messages[0]
	if message.To != "<dev@example.com>" {
		t.Fatalf("expected normalized recipient <dev@example.com>, got %q", message.To)
	}
	if !strings.Contains(message.Subject, "backend-ci (job-1)") || !strings.Contains(message.Subject, "build-1") || !strings.Contains(message.Subject, "failed") {
		t.Fatalf("expected subject to include job/build/status context, got %q", message.Subject)
	}
	for _, want := range []string{
		"A Coyote CI build failed.",
		"Build ID: build-1",
		"Status: failed",
		"Project: Payments API (project-1)",
		"Build number: 42",
		"Job: backend-ci (job-1)",
	} {
		if !strings.Contains(message.Body, want) {
			t.Fatalf("expected body to contain %q, got %q", want, message.Body)
		}
	}

	delivery := mustGetNotificationDelivery(t, deliveryRepo, "build-1", domain.NotificationEventTypeBuildFailed, "<dev@example.com>")
	if delivery.Status != domain.NotificationDeliveryStatusSent {
		t.Fatalf("expected sent delivery status, got %q", delivery.Status)
	}
	if delivery.SentAt == nil || delivery.SentAt.IsZero() {
		t.Fatal("expected sent_at to be recorded")
	}
}

func TestBuildService_CompleteBuild_SendsNotificationWhenConfigured(t *testing.T) {
	now := time.Now().UTC()
	jobID := "job-1"
	deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
	buildRepo := &fakeBuildRepository{
		build: domain.Build{
			ID:          "build-1",
			ProjectID:   "project-1",
			JobID:       &jobID,
			BuildNumber: 42,
			Status:      domain.BuildStatusRunning,
			CreatedAt:   now,
		},
	}
	jobRepo := memoryrepo.NewJobRepository()
	projectRepo := memoryrepo.NewProjectRepository(jobRepo)
	if _, err := projectRepo.Create(context.Background(), domain.Project{ID: "project-1", Name: "Payments API", Slug: "payments-api", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create project failed: %v", err)
	}
	if _, err := jobRepo.Create(context.Background(), domain.Job{ID: jobID, ProjectID: "project-1", Name: "backend-ci", RepositoryURL: "https://github.com/example/payments.git", DefaultRef: "main", PipelineYAML: "version: 1", Enabled: true, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create job failed: %v", err)
	}
	sender := &recordingEmailSender{}
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{
		Enabled:      true,
		Recipients:   "dev@example.com",
		Sender:       sender,
		JobRepo:      jobRepo,
		ProjectRepo:  projectRepo,
		DeliveryRepo: deliveryRepo,
	})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	svc := NewBuildServiceFromConfig(buildRepo, nil, nil, BuildServiceConfig{BuildNotifier: notifier})
	build, err := svc.CompleteBuild(context.Background(), "build-1")
	if err != nil {
		t.Fatalf("complete build returned error: %v", err)
	}
	if build.Status != domain.BuildStatusSuccess {
		t.Fatalf("expected successful build, got %q", build.Status)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("expected one notification email, got %d", len(sender.messages))
	}
	message := sender.messages[0]
	if message.To != "<dev@example.com>" {
		t.Fatalf("expected normalized recipient <dev@example.com>, got %q", message.To)
	}
	if !strings.Contains(message.Subject, "backend-ci (job-1)") || !strings.Contains(message.Subject, "build-1") || !strings.Contains(message.Subject, "succeeded") {
		t.Fatalf("expected subject to include job/build/success context, got %q", message.Subject)
	}
	for _, want := range []string{
		"A Coyote CI build succeeded.",
		"Build ID: build-1",
		"Status: success",
		"Project: Payments API (project-1)",
		"Build number: 42",
		"Job: backend-ci (job-1)",
	} {
		if !strings.Contains(message.Body, want) {
			t.Fatalf("expected body to contain %q, got %q", want, message.Body)
		}
	}

	delivery := mustGetNotificationDelivery(t, deliveryRepo, "build-1", domain.NotificationEventTypeBuildSucceeded, "<dev@example.com>")
	if delivery.Status != domain.NotificationDeliveryStatusSent {
		t.Fatalf("expected sent delivery status, got %q", delivery.Status)
	}
	if delivery.SentAt == nil || delivery.SentAt.IsZero() {
		t.Fatal("expected sent_at to be recorded")
	}
}

func TestBuildService_FailBuild_DoesNotSendNotificationWhenDisabled(t *testing.T) {
	buildRepo := &fakeBuildRepository{build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: time.Now().UTC()}}
	sender := &recordingEmailSender{}
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{
		Enabled:    false,
		Recipients: "dev@example.com",
		Sender:     sender,
	})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	svc := NewBuildServiceFromConfig(buildRepo, nil, nil, BuildServiceConfig{BuildNotifier: notifier})
	if _, err := svc.FailBuild(context.Background(), "build-1"); err != nil {
		t.Fatalf("fail build returned error: %v", err)
	}
	if len(sender.messages) != 0 {
		t.Fatalf("expected no notification emails when disabled, got %d", len(sender.messages))
	}
}

func TestBuildService_NotificationsDisabled_DoesNotSendForSuccessOrFailure(t *testing.T) {
	tests := []struct {
		name   string
		action func(*BuildService, context.Context, string) (domain.Build, error)
		want   domain.BuildStatus
	}{
		{name: "success", action: (*BuildService).CompleteBuild, want: domain.BuildStatusSuccess},
		{name: "failure", action: (*BuildService).FailBuild, want: domain.BuildStatusFailed},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			buildRepo := &fakeBuildRepository{build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: time.Now().UTC()}}
			sender := &recordingEmailSender{}
			notifier, err := NewBuildNotificationService(BuildNotificationConfig{
				Enabled:    false,
				Recipients: "dev@example.com",
				Sender:     sender,
			})
			if err != nil {
				t.Fatalf("create notifier failed: %v", err)
			}

			svc := NewBuildServiceFromConfig(buildRepo, nil, nil, BuildServiceConfig{BuildNotifier: notifier})
			build, err := tc.action(svc, context.Background(), "build-1")
			if err != nil {
				t.Fatalf("terminal transition returned error: %v", err)
			}
			if build.Status != tc.want {
				t.Fatalf("expected build status %q, got %q", tc.want, build.Status)
			}
			if len(sender.messages) != 0 {
				t.Fatalf("expected no notification emails when disabled, got %d", len(sender.messages))
			}
		})
	}
}

func TestBuildService_FailBuild_DoesNotSendNotificationWithoutRecipients(t *testing.T) {
	buildRepo := &fakeBuildRepository{build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: time.Now().UTC()}}
	sender := &recordingEmailSender{}
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{
		Enabled: true,
		Sender:  sender,
	})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	svc := NewBuildServiceFromConfig(buildRepo, nil, nil, BuildServiceConfig{BuildNotifier: notifier})
	build, err := svc.FailBuild(context.Background(), "build-1")
	if err != nil {
		t.Fatalf("fail build returned error: %v", err)
	}
	if build.Status != domain.BuildStatusFailed {
		t.Fatalf("expected failed build, got %q", build.Status)
	}
	if len(sender.messages) != 0 {
		t.Fatalf("expected no notification emails without recipients, got %d", len(sender.messages))
	}
}

func TestBuildService_FailBuild_SenderFailureDoesNotBreakPersistence(t *testing.T) {
	buildRepo := &fakeBuildRepository{build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: time.Now().UTC()}}
	deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
	sender := &recordingEmailSender{err: errors.New("smtp unavailable")}
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{
		Enabled:      true,
		Recipients:   "dev@example.com",
		Sender:       sender,
		DeliveryRepo: deliveryRepo,
	})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	svc := NewBuildServiceFromConfig(buildRepo, nil, nil, BuildServiceConfig{BuildNotifier: notifier})
	build, err := svc.FailBuild(context.Background(), "build-1")
	if err != nil {
		t.Fatalf("fail build returned error: %v", err)
	}
	if build.Status != domain.BuildStatusFailed {
		t.Fatalf("expected failed build to remain persisted, got %q", build.Status)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("expected attempted notification email, got %d", len(sender.messages))
	}
	delivery := mustGetNotificationDelivery(t, deliveryRepo, "build-1", domain.NotificationEventTypeBuildFailed, "<dev@example.com>")
	if delivery.Status != domain.NotificationDeliveryStatusFailed {
		t.Fatalf("expected failed delivery status, got %q", delivery.Status)
	}
	if delivery.LastError == nil || !strings.Contains(*delivery.LastError, "smtp unavailable") {
		t.Fatalf("expected last_error to contain smtp failure, got %v", delivery.LastError)
	}
}

func TestBuildService_CompleteBuild_SenderFailureDoesNotBreakPersistence(t *testing.T) {
	buildRepo := &fakeBuildRepository{build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: time.Now().UTC()}}
	sender := &recordingEmailSender{err: errors.New("smtp unavailable")}
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{
		Enabled:    true,
		Recipients: "dev@example.com",
		Sender:     sender,
	})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	svc := NewBuildServiceFromConfig(buildRepo, nil, nil, BuildServiceConfig{BuildNotifier: notifier})
	build, err := svc.CompleteBuild(context.Background(), "build-1")
	if err != nil {
		t.Fatalf("complete build returned error: %v", err)
	}
	if build.Status != domain.BuildStatusSuccess {
		t.Fatalf("expected successful build to remain persisted, got %q", build.Status)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("expected attempted notification email, got %d", len(sender.messages))
	}
}

func TestBuildService_CancelBuild_DoesNotSendNotificationWhenConfigured(t *testing.T) {
	buildRepo := &fakeBuildRepository{build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: time.Now().UTC()}}
	sender := &recordingEmailSender{}
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{
		Enabled:    true,
		Recipients: "dev@example.com",
		Sender:     sender,
	})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	svc := NewBuildServiceFromConfig(buildRepo, nil, nil, BuildServiceConfig{BuildNotifier: notifier})
	build, err := svc.CancelBuild(context.Background(), "build-1")
	if err != nil {
		t.Fatalf("cancel build returned error: %v", err)
	}
	if build.Status != domain.BuildStatusCanceled {
		t.Fatalf("expected canceled build, got %q", build.Status)
	}
	if len(sender.messages) != 0 {
		t.Fatalf("expected no notification emails for canceled builds, got %d", len(sender.messages))
	}
}

func TestBuildService_HandleStepResult_FailedStepThenFailBuild_DoesNotDoubleSendEmail(t *testing.T) {
	claimToken := "claim-active"
	buildRepo := &fakeBuildRepository{
		build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusRunning, CurrentStepIndex: 0, CreatedAt: time.Now().UTC()},
		steps: []domain.BuildStep{
			{StepIndex: 0, Name: "step-1", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken},
			{StepIndex: 1, Name: "step-2", Status: domain.BuildStepStatusPending},
		},
	}
	sender := &recordingEmailSender{}
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{
		Enabled:    true,
		Recipients: "dev@example.com",
		Sender:     sender,
	})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	svc := NewBuildServiceFromConfig(buildRepo, nil, nil, BuildServiceConfig{BuildNotifier: notifier})
	report, err := svc.HandleStepResult(context.Background(), steprunner.RunStepRequest{BuildID: "build-1", StepIndex: 0, StepName: "step-1", ClaimToken: claimToken}, steprunner.RunStepResult{Status: steprunner.RunStepStatusFailed, ExitCode: 7, Stderr: "boom", StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC()})
	if err != nil {
		t.Fatalf("handle step result returned error: %v", err)
	}
	if report.CompletionOutcome != repository.StepCompletionCompleted {
		t.Fatalf("expected completion to persist, got %q", report.CompletionOutcome)
	}
	if buildRepo.build.Status != domain.BuildStatusFailed {
		t.Fatalf("expected build failed after step completion, got %q", buildRepo.build.Status)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("expected one notification email after failed step completion, got %d", len(sender.messages))
	}

	if _, err := svc.FailBuild(context.Background(), "build-1"); !errors.Is(err, ErrInvalidBuildStatusTransition) {
		t.Fatalf("expected invalid transition when failing an already failed build, got %v", err)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("expected no second notification email after invalid FailBuild, got %d", len(sender.messages))
	}
}

func TestBuildNotificationService_SendSampleBuildFailure(t *testing.T) {
	sender := &recordingEmailSender{}
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{
		Enabled:    true,
		Recipients: "dev@example.com, qa@example.com",
		Sender:     sender,
	})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	recipients, err := notifier.SendSampleBuildFailure(context.Background())
	if err != nil {
		t.Fatalf("sample email returned error: %v", err)
	}
	if len(recipients) != 2 || recipients[0] != "<dev@example.com>" || recipients[1] != "<qa@example.com>" {
		t.Fatalf("unexpected recipients: %v", recipients)
	}
	if len(sender.messages) != 2 {
		t.Fatalf("expected one message per recipient, got %d", len(sender.messages))
	}
	if !strings.Contains(sender.messages[0].Subject, "sample build failure") {
		t.Fatalf("expected sample subject, got %q", sender.messages[0].Subject)
	}
	if !strings.Contains(sender.messages[0].Body, "POST /api/dev/notifications/sample-build") {
		t.Fatalf("expected sample endpoint hint in body, got %q", sender.messages[0].Body)
	}
}

func TestBuildNotificationService_SendSampleBuildFailureRequiresEnabledConfig(t *testing.T) {
	disabledNotifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: false, Recipients: "dev@example.com", Sender: &recordingEmailSender{}})
	if err != nil {
		t.Fatalf("create disabled notifier failed: %v", err)
	}
	if _, sendErr := disabledNotifier.SendSampleBuildFailure(context.Background()); !errors.Is(sendErr, ErrEmailNotificationsDisabled) {
		t.Fatalf("expected disabled error, got %v", sendErr)
	}

	noRecipientsNotifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Sender: &recordingEmailSender{}})
	if err != nil {
		t.Fatalf("create no-recipient notifier failed: %v", err)
	}
	if _, sendErr := noRecipientsNotifier.SendSampleBuildFailure(context.Background()); !errors.Is(sendErr, ErrEmailNotificationRecipientsNotConfigured) {
		t.Fatalf("expected no recipient error, got %v", sendErr)
	}
}

func TestNewBuildNotificationService_DisabledIgnoresInvalidRecipients(t *testing.T) {
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{
		Enabled:    false,
		Recipients: "not-an-email",
		Sender:     &recordingEmailSender{},
	})
	if err != nil {
		t.Fatalf("expected disabled notifier to ignore invalid recipients, got %v", err)
	}
	if notifier == nil {
		t.Fatal("expected notifier")
	}
	if len(notifier.recipients) != 0 {
		t.Fatalf("expected disabled notifier to keep no parsed recipients, got %v", notifier.recipients)
	}
}

func TestNewBuildNotificationService_EnabledRejectsInvalidRecipients(t *testing.T) {
	_, err := NewBuildNotificationService(BuildNotificationConfig{
		Enabled:    true,
		Recipients: "not-an-email",
		Sender:     &recordingEmailSender{},
	})
	if err == nil {
		t.Fatal("expected invalid recipient error")
	}
}

func TestBuildNotificationService_NotifyTerminalBuild(t *testing.T) {
	t.Run("nil service is noop", func(t *testing.T) {
		var notifier *BuildNotificationService
		if err := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", Status: domain.BuildStatusFailed}); err != nil {
			t.Fatalf("expected nil service to noop, got %v", err)
		}
	})

	t.Run("no sender returns error", func(t *testing.T) {
		notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com"})
		if err != nil {
			t.Fatalf("create notifier failed: %v", err)
		}
		if err := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", Status: domain.BuildStatusFailed}); err == nil {
			t.Fatal("expected missing sender error")
		}
	})

	t.Run("non failed terminal status is ignored", func(t *testing.T) {
		sender := &recordingEmailSender{}
		notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com", Sender: sender})
		if err != nil {
			t.Fatalf("create notifier failed: %v", err)
		}
		if err := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", Status: domain.BuildStatusCanceled}); err != nil {
			t.Fatalf("expected canceled status to be ignored, got %v", err)
		}
		if len(sender.messages) != 0 {
			t.Fatalf("expected no email for canceled status, got %d", len(sender.messages))
		}
	})

	t.Run("successful terminal status sends email", func(t *testing.T) {
		sender := &recordingEmailSender{}
		notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com", Sender: sender})
		if err != nil {
			t.Fatalf("create notifier failed: %v", err)
		}
		if err := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusSuccess}); err != nil {
			t.Fatalf("expected success status to send, got %v", err)
		}
		if len(sender.messages) != 1 {
			t.Fatalf("expected one email for success status, got %d", len(sender.messages))
		}
		if !strings.Contains(sender.messages[0].Subject, "succeeded") {
			t.Fatalf("expected success subject, got %q", sender.messages[0].Subject)
		}
	})

	t.Run("records sent delivery state", func(t *testing.T) {
		deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
		sender := &recordingEmailSender{}
		notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com", Sender: sender, DeliveryRepo: deliveryRepo})
		if err != nil {
			t.Fatalf("create notifier failed: %v", err)
		}
		if err := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", Status: domain.BuildStatusSuccess}); err != nil {
			t.Fatalf("notify terminal build failed: %v", err)
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
		notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com", Sender: sender, DeliveryRepo: deliveryRepo})
		if err != nil {
			t.Fatalf("create notifier failed: %v", err)
		}
		if err := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", Status: domain.BuildStatusFailed}); err == nil {
			t.Fatal("expected sender failure")
		}

		delivery := mustGetNotificationDelivery(t, deliveryRepo, "build-1", domain.NotificationEventTypeBuildFailed, "<dev@example.com>")
		if delivery.Status != domain.NotificationDeliveryStatusFailed {
			t.Fatalf("expected failed delivery status, got %q", delivery.Status)
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
		notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com", Sender: sender, DeliveryRepo: deliveryRepo})
		if err != nil {
			t.Fatalf("create notifier failed: %v", err)
		}

		build := domain.Build{ID: "build-1", Status: domain.BuildStatusFailed}
		if err := notifier.NotifyTerminalBuild(context.Background(), build); err != nil {
			t.Fatalf("first notify failed: %v", err)
		}
		if err := notifier.NotifyTerminalBuild(context.Background(), build); err != nil {
			t.Fatalf("duplicate notify should skip cleanly, got %v", err)
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
		message := "smtp unavailable"
		if _, err := deliveryRepo.Create(context.Background(), domain.NotificationDelivery{
			BuildID:   "build-1",
			EventType: domain.NotificationEventTypeBuildFailed,
			Recipient: "<dev@example.com>",
			Status:    domain.NotificationDeliveryStatusFailed,
			Attempts:  1,
			LastError: &message,
		}); err != nil {
			t.Fatalf("seed failed delivery record failed: %v", err)
		}

		sender := &recordingEmailSender{}
		notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com", Sender: sender, DeliveryRepo: deliveryRepo})
		if err != nil {
			t.Fatalf("create notifier failed: %v", err)
		}
		if err := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", Status: domain.BuildStatusFailed}); err != nil {
			t.Fatalf("notify with existing failed record should skip cleanly, got %v", err)
		}
		if len(sender.messages) != 0 {
			t.Fatalf("expected no resend when failed delivery record already exists, got %d", len(sender.messages))
		}

		delivery := mustGetNotificationDelivery(t, deliveryRepo, "build-1", domain.NotificationEventTypeBuildFailed, "<dev@example.com>")
		if delivery.Status != domain.NotificationDeliveryStatusFailed {
			t.Fatalf("expected failed delivery status to remain unchanged, got %q", delivery.Status)
		}
		if delivery.Attempts != 1 {
			t.Fatalf("expected attempts to remain 1 for skipped failed record, got %d", delivery.Attempts)
		}
	})

	t.Run("delivery create error is returned", func(t *testing.T) {
		repo := &scriptedNotificationDeliveryRepo{
			createFunc: func(context.Context, domain.NotificationDelivery) (domain.NotificationDelivery, error) {
				return domain.NotificationDelivery{}, errors.New("create failed")
			},
		}
		notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com", Sender: &recordingEmailSender{}, DeliveryRepo: repo})
		if err != nil {
			t.Fatalf("create notifier failed: %v", err)
		}
		if err := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", Status: domain.BuildStatusFailed}); err == nil || err.Error() != "create failed" {
			t.Fatalf("expected create failure, got %v", err)
		}
	})

	t.Run("duplicate record lookup error is returned", func(t *testing.T) {
		repo := &scriptedNotificationDeliveryRepo{
			createFunc: func(context.Context, domain.NotificationDelivery) (domain.NotificationDelivery, error) {
				return domain.NotificationDelivery{}, repository.ErrNotificationDeliveryDuplicate
			},
			getFunc: func(context.Context, string, domain.NotificationEventType, string) (domain.NotificationDelivery, error) {
				return domain.NotificationDelivery{}, errors.New("lookup failed")
			},
		}
		notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com", Sender: &recordingEmailSender{}, DeliveryRepo: repo})
		if err != nil {
			t.Fatalf("create notifier failed: %v", err)
		}
		if err := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", Status: domain.BuildStatusFailed}); err == nil || err.Error() != "lookup failed" {
			t.Fatalf("expected lookup failure, got %v", err)
		}
	})

	t.Run("sent state persistence failure marks delivery failed", func(t *testing.T) {
		updateCalls := 0
		repo := &scriptedNotificationDeliveryRepo{
			createFunc: func(_ context.Context, delivery domain.NotificationDelivery) (domain.NotificationDelivery, error) {
				delivery.ID = "delivery-1"
				delivery.CreatedAt = time.Now().UTC()
				return delivery, nil
			},
			updateFunc: func(_ context.Context, delivery domain.NotificationDelivery) (domain.NotificationDelivery, error) {
				updateCalls++
				if updateCalls == 1 {
					if delivery.Status != domain.NotificationDeliveryStatusSent {
						t.Fatalf("expected first update to persist sent status, got %q", delivery.Status)
					}
					return domain.NotificationDelivery{}, errors.New("write sent failed")
				}
				if delivery.Status != domain.NotificationDeliveryStatusFailed {
					t.Fatalf("expected fallback update to mark failed, got %q", delivery.Status)
				}
				if delivery.LastError == nil || !strings.Contains(*delivery.LastError, "persist sent delivery state failed") {
					t.Fatalf("expected fallback last error to describe sent-state persistence failure, got %v", delivery.LastError)
				}
				return delivery, nil
			},
		}
		notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com", Sender: &recordingEmailSender{}, DeliveryRepo: repo})
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
		repo := &scriptedNotificationDeliveryRepo{
			createFunc: func(_ context.Context, delivery domain.NotificationDelivery) (domain.NotificationDelivery, error) {
				delivery.ID = "delivery-1"
				delivery.CreatedAt = time.Now().UTC()
				return delivery, nil
			},
			updateFunc: func(_ context.Context, delivery domain.NotificationDelivery) (domain.NotificationDelivery, error) {
				if delivery.Status == domain.NotificationDeliveryStatusSent {
					return domain.NotificationDelivery{}, errors.New("write sent failed")
				}
				return domain.NotificationDelivery{}, errors.New("write failed failed")
			},
		}
		notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com", Sender: &recordingEmailSender{}, DeliveryRepo: repo})
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
		notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com, qa@example.com", Sender: sender, DeliveryRepo: deliveryRepo})
		if err != nil {
			t.Fatalf("create notifier failed: %v", err)
		}
		if err := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", Status: domain.BuildStatusSuccess}); err != nil {
			t.Fatalf("notify terminal build failed: %v", err)
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

func TestBuildNotificationService_SendSampleBuildFailureRequiresSender(t *testing.T) {
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com"})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}
	if _, sendErr := notifier.SendSampleBuildFailure(context.Background()); sendErr == nil {
		t.Fatal("expected missing sender error")
	}
}

func TestBuildNotificationService_IsActive(t *testing.T) {
	var nilNotifier *BuildNotificationService
	if nilNotifier.isActive() {
		t.Fatal("expected nil notifier to be inactive")
	}
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com", Sender: &recordingEmailSender{}})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}
	if !notifier.isActive() {
		t.Fatal("expected notifier to be active")
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
