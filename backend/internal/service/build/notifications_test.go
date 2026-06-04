package build

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	platformemail "github.com/radiation/coyote-ci/backend/internal/platform/email"
	memoryrepo "github.com/radiation/coyote-ci/backend/internal/repository/memory"
)

type recordingEmailSender struct {
	messages []platformemail.Message
	err      error
}

func (s *recordingEmailSender) SendText(_ context.Context, message platformemail.Message) error {
	s.messages = append(s.messages, message)
	return s.err
}

func TestBuildService_FailBuild_SendsNotificationWhenConfigured(t *testing.T) {
	now := time.Now().UTC()
	jobID := "job-1"
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
		Enabled:     true,
		Recipients:  "dev@example.com",
		Sender:      sender,
		JobRepo:     jobRepo,
		ProjectRepo: projectRepo,
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
