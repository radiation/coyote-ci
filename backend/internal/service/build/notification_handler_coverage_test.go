package build

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	platformemail "github.com/radiation/coyote-ci/backend/internal/platform/email"
	memoryrepo "github.com/radiation/coyote-ci/backend/internal/repository/memory"
)

func TestBuildNotificationService_NotifyTerminalBuild_NoSenderReturnsError(t *testing.T) {
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com", SubscriptionRepo: memoryrepo.NewNotificationSubscriptionRepository()})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	err = notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", Status: domain.BuildStatusFailed})
	if err == nil || !strings.Contains(err.Error(), "email sender is not configured") {
		t.Fatalf("expected missing sender error, got %v", err)
	}
}

func TestBuildNotificationService_NotifyTerminalBuild_CanceledIgnoresMissingSenderAndRecipients(t *testing.T) {
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	err = notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", Status: domain.BuildStatusCanceled})
	if err != nil {
		t.Fatalf("expected canceled status to noop before sender/recipient validation, got %v", err)
	}
}

func TestBuildNotificationService_NotifyTerminalBuild_SendsOnlyForConfiguredStatuses(t *testing.T) {
	sender := &recordingEmailSender{}
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com", Sender: sender, SubscriptionRepo: memoryrepo.NewNotificationSubscriptionRepository()})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	states := []struct {
		status    domain.BuildStatus
		wantCount int
	}{
		{status: domain.BuildStatusSuccess, wantCount: 1},
		{status: domain.BuildStatusCanceled, wantCount: 1},
	}
	for _, state := range states {
		if notifyErr := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", Status: state.status}); notifyErr != nil {
			t.Fatalf("expected nil error for state %q, got %v", state.status, notifyErr)
		}
		if len(sender.messages) != state.wantCount {
			t.Fatalf("expected %d emails after status %q, got %d", state.wantCount, state.status, len(sender.messages))
		}
	}
	if !strings.Contains(sender.messages[0].Subject, "succeeded") {
		t.Fatalf("expected success email subject, got %q", sender.messages[0].Subject)
	}
}

func TestBuildNotificationService_SendSampleBuildFailure_NoSender(t *testing.T) {
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com"})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	_, err = notifier.SendSampleBuildFailure(context.Background())
	if err == nil || !strings.Contains(err.Error(), "email sender is not configured") {
		t.Fatalf("expected missing sender error, got %v", err)
	}
}

func TestBuildNotificationService_IsActive_ZeroAndConfiguredCases(t *testing.T) {
	if (*BuildNotificationService)(nil).isActive() {
		t.Fatal("expected nil notifier to be inactive")
	}
	inactive := &BuildNotificationService{}
	if inactive.isActive() {
		t.Fatal("expected zero-value notifier to be inactive")
	}
	active := &BuildNotificationService{enabled: true, defaultRecipients: []string{"<dev@example.com>"}, sender: &recordingEmailSender{}, subscriptionRepo: memoryrepo.NewNotificationSubscriptionRepository()}
	if !active.isActive() {
		t.Fatal("expected configured notifier to be active")
	}
}

func TestParseNotificationRecipients_RejectsInvalidRecipient(t *testing.T) {
	_, err := parseNotificationRecipients("bad-email")
	if err == nil {
		t.Fatal("expected parse error")
	}
}

func TestBuildNotificationService_FormatBuildStatusEmail_FallsBackWithoutLookups(t *testing.T) {
	errMessage := "boom"
	jobID := "job-1"
	notifier := &BuildNotificationService{}
	build := domain.Build{
		ID:           "build-1",
		ProjectID:    "project-1",
		JobID:        &jobID,
		BuildNumber:  7,
		Status:       domain.BuildStatusFailed,
		ErrorMessage: &errMessage,
	}
	subject, body := notifier.formatBuildStatusEmail(build, notifier.buildNotificationDetails(context.Background(), build))
	if !strings.Contains(subject, "job-1") || !strings.Contains(subject, "failed") {
		t.Fatalf("unexpected subject %q", subject)
	}
	for _, want := range []string{"A Coyote CI build failed.", "Project: project-1", "Job: job-1", "Build number: 7", "Error: boom"} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected body to contain %q, got %q", want, body)
		}
	}
}

func TestBuildNotificationService_Send_StopsOnFirstError(t *testing.T) {
	sender := &recordingEmailSender{err: errors.New("smtp unavailable")}
	notifier := &BuildNotificationService{defaultRecipients: []string{"<dev@example.com>", "<qa@example.com>"}, sender: sender}
	err := notifier.send(context.Background(), notifier.defaultRecipients, "subject", "body")
	if err == nil {
		t.Fatal("expected send error")
	}
	if len(sender.messages) != 1 {
		t.Fatalf("expected send to stop on first error, got %d attempted messages", len(sender.messages))
	}
	if sender.messages[0] != (platformemail.Message{To: "<dev@example.com>", Subject: "subject", Body: "body"}) {
		t.Fatalf("unexpected first message %#v", sender.messages[0])
	}
}
