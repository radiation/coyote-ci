package build

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	platformemail "github.com/radiation/coyote-ci/backend/internal/platform/email"
)

func TestBuildNotificationService_NotifyTerminalBuild_NoSenderReturnsError(t *testing.T) {
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com"})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	err = notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", Status: domain.BuildStatusFailed})
	if err == nil || !strings.Contains(err.Error(), "email sender is not configured") {
		t.Fatalf("expected missing sender error, got %v", err)
	}
}

func TestBuildNotificationService_NotifyTerminalBuild_IgnoresNonFailedTerminalStates(t *testing.T) {
	sender := &recordingEmailSender{}
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com", Sender: sender})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	states := []domain.BuildStatus{domain.BuildStatusSuccess, domain.BuildStatusCanceled}
	for _, state := range states {
		if notifyErr := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", Status: state}); notifyErr != nil {
			t.Fatalf("expected nil error for state %q, got %v", state, notifyErr)
		}
	}
	if len(sender.messages) != 0 {
		t.Fatalf("expected no emails for non-failed terminal states, got %d", len(sender.messages))
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
	active := &BuildNotificationService{enabled: true, recipients: []string{"<dev@example.com>"}, sender: &recordingEmailSender{}}
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
	subject, body := notifier.formatBuildStatusEmail(context.Background(), domain.Build{
		ID:           "build-1",
		ProjectID:    "project-1",
		JobID:        &jobID,
		BuildNumber:  7,
		Status:       domain.BuildStatusFailed,
		ErrorMessage: &errMessage,
	})
	if !strings.Contains(subject, "job-1") || !strings.Contains(subject, "failed") {
		t.Fatalf("unexpected subject %q", subject)
	}
	for _, want := range []string{"Project: project-1", "Job: job-1", "Build number: 7", "Error: boom"} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected body to contain %q, got %q", want, body)
		}
	}
}

func TestBuildNotificationService_Send_StopsOnFirstError(t *testing.T) {
	sender := &recordingEmailSender{err: errors.New("smtp unavailable")}
	notifier := &BuildNotificationService{recipients: []string{"<dev@example.com>", "<qa@example.com>"}, sender: sender}
	err := notifier.send(context.Background(), "subject", "body")
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
