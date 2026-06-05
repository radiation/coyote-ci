package memory

import (
	"context"
	"errors"
	"testing"

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
	if !errors.Is(err, repository.ErrNotificationDeliveryDuplicate) {
		t.Fatalf("expected duplicate target error, got %v", err)
	}

	_, err = repo.CreateSubscription(context.Background(), domain.NotificationSubscription{TargetID: target.ID, ProjectID: &projectID, EventType: domain.NotificationEventTypeBuildFailed, Enabled: true})
	if !errors.Is(err, repository.ErrNotificationDeliveryDuplicate) {
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
