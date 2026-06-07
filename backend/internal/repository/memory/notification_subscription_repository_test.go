package memory

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func TestNotificationSubscriptionRepository_CreateAndListMatches(t *testing.T) {
	repo := NewNotificationSubscriptionRepository()
	target, err := repo.CreateTarget(context.Background(), domain.NotificationTarget{
		Type:      domain.NotificationTargetTypeEmail,
		Name:      "Dev Mailbox",
		Recipient: "dev@example.com",
		Enabled:   true,
	})
	if err != nil {
		t.Fatalf("create target failed: %v", err)
	}
	if target.Recipient != "<dev@example.com>" {
		t.Fatalf("expected normalized recipient, got %q", target.Recipient)
	}

	projectID := "project-1"
	subscription, err := repo.CreateSubscription(context.Background(), domain.NotificationSubscription{
		TargetID:  target.ID,
		ProjectID: &projectID,
		EventType: domain.NotificationEventTypeBuildFailed,
		Enabled:   true,
	})
	if err != nil {
		t.Fatalf("create subscription failed: %v", err)
	}
	if subscription.ProjectID == nil || *subscription.ProjectID != projectID {
		t.Fatalf("expected project subscription, got %+v", subscription)
	}

	matches, err := repo.ListEnabledMatchesForBuildEvent(context.Background(), domain.Build{ID: "build-1", ProjectID: projectID, Status: domain.BuildStatusFailed}, domain.NotificationEventTypeBuildFailed)
	if err != nil {
		t.Fatalf("list matches failed: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected one match, got %d", len(matches))
	}
	if matches[0].Target.Recipient != "<dev@example.com>" {
		t.Fatalf("unexpected recipient %q", matches[0].Target.Recipient)
	}

	_, err = repo.CreateTarget(context.Background(), domain.NotificationTarget{Type: domain.NotificationTargetTypeEmail, Recipient: "dev@example.com", Enabled: true})
	if !errors.Is(err, repository.ErrNotificationTargetDuplicate) {
		t.Fatalf("expected duplicate target error, got %v", err)
	}

	_, err = repo.CreateSubscription(context.Background(), domain.NotificationSubscription{TargetID: target.ID, ProjectID: &projectID, EventType: domain.NotificationEventTypeBuildFailed, Enabled: true})
	if !errors.Is(err, repository.ErrNotificationSubscriptionDuplicate) {
		t.Fatalf("expected duplicate subscription error, got %v", err)
	}

	jobID := "job-1"
	_, err = repo.CreateSubscription(context.Background(), domain.NotificationSubscription{TargetID: target.ID, ProjectID: &projectID, JobID: &jobID, EventType: domain.NotificationEventTypeBuildFailed, Enabled: true})
	if err == nil {
		t.Fatal("expected mixed-scope subscription error")
	}

	disabledTarget, err := repo.CreateTarget(context.Background(), domain.NotificationTarget{Type: domain.NotificationTargetTypeEmail, Recipient: "qa@example.com", Enabled: false})
	if err != nil {
		t.Fatalf("create disabled target failed: %v", err)
	}
	_, err = repo.CreateSubscription(context.Background(), domain.NotificationSubscription{TargetID: disabledTarget.ID, ProjectID: &projectID, EventType: domain.NotificationEventTypeBuildFailed, Enabled: true})
	if err != nil {
		t.Fatalf("create disabled-target subscription failed: %v", err)
	}

	matches, err = repo.ListEnabledMatchesForBuildEvent(context.Background(), domain.Build{ID: "build-1", ProjectID: projectID, Status: domain.BuildStatusFailed}, domain.NotificationEventTypeBuildFailed)
	if err != nil {
		t.Fatalf("list matches failed: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected disabled target to be excluded, got %d matches", len(matches))
	}
}

func TestNotificationSubscriptionRepository_CreateTarget_ValidationAndDefaults(t *testing.T) {
	repo := NewNotificationSubscriptionRepository()

	created, err := repo.CreateTarget(context.Background(), domain.NotificationTarget{
		Name:      " Dev Mailbox ",
		Recipient: " dev@example.com ",
		Enabled:   true,
	})
	if err != nil {
		t.Fatalf("create target failed: %v", err)
	}
	if created.Type != domain.NotificationTargetTypeEmail {
		t.Fatalf("expected default email type, got %q", created.Type)
	}
	if created.Name != "Dev Mailbox" {
		t.Fatalf("expected trimmed name, got %q", created.Name)
	}
	if created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Fatal("expected timestamps to be populated")
	}

	customTime := time.Now().UTC().Add(-time.Hour)
	preserved, err := repo.CreateTarget(context.Background(), domain.NotificationTarget{
		ID:        "target-2",
		Type:      domain.NotificationTargetTypeEmail,
		Name:      "QA",
		Recipient: "qa@example.com",
		Enabled:   true,
		CreatedAt: customTime,
		UpdatedAt: customTime,
	})
	if err != nil {
		t.Fatalf("create target with explicit fields failed: %v", err)
	}
	if preserved.ID != "target-2" {
		t.Fatalf("expected explicit id to be preserved, got %q", preserved.ID)
	}
	if !preserved.CreatedAt.Equal(customTime) || !preserved.UpdatedAt.Equal(customTime) {
		t.Fatalf("expected explicit timestamps to be preserved, got created=%v updated=%v", preserved.CreatedAt, preserved.UpdatedAt)
	}

	_, err = repo.CreateTarget(context.Background(), domain.NotificationTarget{
		Type:      domain.NotificationTargetType("sms"),
		Recipient: "sms@example.com",
		Enabled:   true,
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported notification target type") {
		t.Fatalf("expected unsupported type error, got %v", err)
	}

	_, err = repo.CreateTarget(context.Background(), domain.NotificationTarget{
		Type:      domain.NotificationTargetTypeEmail,
		Recipient: "not-an-email",
		Enabled:   true,
	})
	if err == nil || !strings.Contains(err.Error(), "invalid notification target recipient") {
		t.Fatalf("expected invalid recipient error, got %v", err)
	}
}

func TestNotificationSubscriptionRepository_CreateSubscription_ValidationAndJobScope(t *testing.T) {
	repo := NewNotificationSubscriptionRepository()
	target, err := repo.CreateTarget(context.Background(), domain.NotificationTarget{
		Type:      domain.NotificationTargetTypeEmail,
		Recipient: "dev@example.com",
		Enabled:   true,
	})
	if err != nil {
		t.Fatalf("create target failed: %v", err)
	}

	_, err = repo.CreateSubscription(context.Background(), domain.NotificationSubscription{})
	if err == nil || !strings.Contains(err.Error(), "target_id is required") {
		t.Fatalf("expected missing target id error, got %v", err)
	}

	_, err = repo.CreateSubscription(context.Background(), domain.NotificationSubscription{
		TargetID:  "missing",
		EventType: domain.NotificationEventTypeBuildFailed,
		ProjectID: stringPtr("project-1"),
		Enabled:   true,
	})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected target not found error, got %v", err)
	}

	_, err = repo.CreateSubscription(context.Background(), domain.NotificationSubscription{
		TargetID:  target.ID,
		EventType: domain.NotificationEventTypeBuildFailed,
		Enabled:   true,
	})
	if err == nil || !strings.Contains(err.Error(), "exactly one scope") {
		t.Fatalf("expected missing scope error, got %v", err)
	}

	_, err = repo.CreateSubscription(context.Background(), domain.NotificationSubscription{
		TargetID:  target.ID,
		ProjectID: stringPtr("project-1"),
		Enabled:   true,
	})
	if err == nil || !strings.Contains(err.Error(), "event_type is required") {
		t.Fatalf("expected missing event type error, got %v", err)
	}

	_, err = repo.CreateSubscription(context.Background(), domain.NotificationSubscription{
		TargetID:  target.ID,
		ProjectID: stringPtr("project-1"),
		EventType: domain.NotificationEventType("build_canceled"),
		Enabled:   true,
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported notification event type") {
		t.Fatalf("expected unsupported event type error, got %v", err)
	}

	jobID := " job-1 "
	created, err := repo.CreateSubscription(context.Background(), domain.NotificationSubscription{
		ID:        "subscription-1",
		TargetID:  " " + target.ID + " ",
		JobID:     &jobID,
		EventType: domain.NotificationEventTypeBuildSucceeded,
		Enabled:   true,
	})
	if err != nil {
		t.Fatalf("create job-scoped subscription failed: %v", err)
	}
	if created.ID != "subscription-1" {
		t.Fatalf("expected explicit id to be preserved, got %q", created.ID)
	}
	if created.JobID == nil || *created.JobID != "job-1" {
		t.Fatalf("expected trimmed job id, got %+v", created.JobID)
	}
	if created.TargetID != target.ID {
		t.Fatalf("expected trimmed target id, got %q", created.TargetID)
	}

	_, err = repo.CreateSubscription(context.Background(), domain.NotificationSubscription{
		TargetID:  target.ID,
		JobID:     stringPtr("job-1"),
		EventType: domain.NotificationEventTypeBuildSucceeded,
		Enabled:   true,
	})
	if !errors.Is(err, repository.ErrNotificationSubscriptionDuplicate) {
		t.Fatalf("expected duplicate job subscription error, got %v", err)
	}

	blankScope := "   "
	_, err = repo.CreateSubscription(context.Background(), domain.NotificationSubscription{
		TargetID:  target.ID,
		ProjectID: &blankScope,
		EventType: domain.NotificationEventTypeBuildFailed,
		Enabled:   true,
	})
	if err == nil || !strings.Contains(err.Error(), "exactly one scope") {
		t.Fatalf("expected blank scope to collapse to missing scope, got %v", err)
	}
}

func TestNotificationSubscriptionRepository_ListEnabledMatchesForBuildEvent_Filters(t *testing.T) {
	repo := NewNotificationSubscriptionRepository()
	projectTarget, err := repo.CreateTarget(context.Background(), domain.NotificationTarget{
		Type:      domain.NotificationTargetTypeEmail,
		Recipient: "project@example.com",
		Enabled:   true,
	})
	if err != nil {
		t.Fatalf("create project target failed: %v", err)
	}
	jobTarget, err := repo.CreateTarget(context.Background(), domain.NotificationTarget{
		Type:      domain.NotificationTargetTypeEmail,
		Recipient: "job@example.com",
		Enabled:   true,
	})
	if err != nil {
		t.Fatalf("create job target failed: %v", err)
	}
	disabledTarget, err := repo.CreateTarget(context.Background(), domain.NotificationTarget{
		Type:      domain.NotificationTargetTypeEmail,
		Recipient: "disabled@example.com",
		Enabled:   false,
	})
	if err != nil {
		t.Fatalf("create disabled target failed: %v", err)
	}

	projectID := "project-1"
	otherProjectID := "project-2"
	jobID := "job-1"
	otherJobID := "job-2"

	_, err = repo.CreateSubscription(context.Background(), domain.NotificationSubscription{
		TargetID:  projectTarget.ID,
		ProjectID: &projectID,
		EventType: domain.NotificationEventTypeBuildFailed,
		Enabled:   true,
	})
	if err != nil {
		t.Fatalf("create matching project subscription failed: %v", err)
	}
	_, err = repo.CreateSubscription(context.Background(), domain.NotificationSubscription{
		TargetID:  projectTarget.ID,
		ProjectID: &otherProjectID,
		EventType: domain.NotificationEventTypeBuildFailed,
		Enabled:   true,
	})
	if err != nil {
		t.Fatalf("create non-matching project subscription failed: %v", err)
	}
	_, err = repo.CreateSubscription(context.Background(), domain.NotificationSubscription{
		TargetID:  jobTarget.ID,
		JobID:     &jobID,
		EventType: domain.NotificationEventTypeBuildFailed,
		Enabled:   true,
	})
	if err != nil {
		t.Fatalf("create matching job subscription failed: %v", err)
	}
	_, err = repo.CreateSubscription(context.Background(), domain.NotificationSubscription{
		TargetID:  jobTarget.ID,
		JobID:     &otherJobID,
		EventType: domain.NotificationEventTypeBuildFailed,
		Enabled:   true,
	})
	if err != nil {
		t.Fatalf("create non-matching job subscription failed: %v", err)
	}
	_, err = repo.CreateSubscription(context.Background(), domain.NotificationSubscription{
		TargetID:  disabledTarget.ID,
		ProjectID: &projectID,
		EventType: domain.NotificationEventTypeBuildFailed,
		Enabled:   true,
	})
	if err != nil {
		t.Fatalf("create disabled-target subscription failed: %v", err)
	}
	_, err = repo.CreateSubscription(context.Background(), domain.NotificationSubscription{
		TargetID:  projectTarget.ID,
		ProjectID: stringPtr("project-3"),
		EventType: domain.NotificationEventTypeBuildSucceeded,
		Enabled:   true,
	})
	if err != nil {
		t.Fatalf("create event-mismatch subscription failed: %v", err)
	}
	_, err = repo.CreateSubscription(context.Background(), domain.NotificationSubscription{
		TargetID:  projectTarget.ID,
		ProjectID: stringPtr("project-4"),
		EventType: domain.NotificationEventTypeBuildFailed,
		Enabled:   false,
	})
	if err != nil {
		t.Fatalf("create disabled subscription failed: %v", err)
	}

	missingTargetProjectID := "project-missing"
	missingTargetID := "missing-target"
	repo.subscriptions["missing-subscription"] = domain.NotificationSubscription{
		ID:        "missing-subscription",
		TargetID:  missingTargetID,
		ProjectID: &missingTargetProjectID,
		EventType: domain.NotificationEventTypeBuildFailed,
		Enabled:   true,
	}

	matches, err := repo.ListEnabledMatchesForBuildEvent(context.Background(), domain.Build{
		ID:        "build-1",
		ProjectID: projectID,
		JobID:     stringPtr(jobID),
	}, domain.NotificationEventTypeBuildFailed)
	if err != nil {
		t.Fatalf("list matches failed: %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("expected one project match and one job match, got %d", len(matches))
	}

	matches, err = repo.ListEnabledMatchesForBuildEvent(context.Background(), domain.Build{
		ID:        "build-2",
		ProjectID: projectID,
	}, domain.NotificationEventTypeBuildFailed)
	if err != nil {
		t.Fatalf("list matches without job failed: %v", err)
	}
	if len(matches) != 1 || matches[0].Target.Recipient != "<project@example.com>" {
		t.Fatalf("expected only project match without job, got %+v", matches)
	}

	matches, err = repo.ListEnabledMatchesForBuildEvent(context.Background(), domain.Build{}, domain.NotificationEventTypeBuildFailed)
	if err != nil {
		t.Fatalf("list matches for empty build failed: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("expected no matches for empty build, got %d", len(matches))
	}
}

func TestNotificationSubscriptionRepository_HelperFunctions(t *testing.T) {
	if got := notificationSubscriptionKey(" target ", domain.NotificationEventTypeBuildFailed, nil, nil); got != "target|build_failed|" {
		t.Fatalf("unexpected key without scope: %q", got)
	}
	if got := notificationSubscriptionKey("target", domain.NotificationEventTypeBuildFailed, stringPtr("project-1"), nil); got != "target|build_failed|project:project-1" {
		t.Fatalf("unexpected project key: %q", got)
	}
	if got := notificationSubscriptionKey("target", domain.NotificationEventTypeBuildFailed, nil, stringPtr("job-1")); got != "target|build_failed|job:job-1" {
		t.Fatalf("unexpected job key: %q", got)
	}

	normalized, err := normalizeNotificationTargetRecipient(" dev@example.com ")
	if err != nil {
		t.Fatalf("normalize recipient failed: %v", err)
	}
	if normalized != "<dev@example.com>" {
		t.Fatalf("unexpected normalized recipient %q", normalized)
	}

	_, err = normalizeNotificationTargetRecipient("bad-email")
	if err == nil {
		t.Fatal("expected invalid email error")
	}

	if got := trimOptionalString(nil); got != nil {
		t.Fatalf("expected nil optional string, got %v", got)
	}
	blank := "   "
	if got := trimOptionalString(&blank); got != nil {
		t.Fatalf("expected blank string to trim to nil, got %v", got)
	}
	value := " value "
	trimmed := trimOptionalString(&value)
	if trimmed == nil || *trimmed != "value" {
		t.Fatalf("expected trimmed value, got %v", trimmed)
	}
}
