package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	memoryrepo "github.com/radiation/coyote-ci/backend/internal/repository/memory"
)

func TestNotificationService_TargetCreateValidateAndUpdate(t *testing.T) {
	ctx := context.Background()
	repo := memoryrepo.NewNotificationSubscriptionRepository()
	service := NewNotificationService(repo)
	now := time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	target, err := service.CreateEmailTarget(ctx, CreateNotificationTargetInput{
		Name:    " Build Alerts ",
		Address: "dev@example.com",
	})
	if err != nil {
		t.Fatalf("create target: %v", err)
	}
	if target.Name != "Build Alerts" {
		t.Fatalf("expected trimmed name, got %q", target.Name)
	}
	if target.Type != domain.NotificationTargetTypeEmail {
		t.Fatalf("expected email type, got %q", target.Type)
	}
	if !target.Enabled {
		t.Fatal("expected default enabled=true")
	}
	if !target.CreatedAt.Equal(now) || !target.UpdatedAt.Equal(now) {
		t.Fatalf("expected deterministic timestamps, got created=%s updated=%s", target.CreatedAt, target.UpdatedAt)
	}

	_, invalidAddressErr := service.CreateEmailTarget(ctx, CreateNotificationTargetInput{Name: "Alerts", Address: "bad-email"})
	if !errors.Is(invalidAddressErr, ErrNotificationTargetAddressInvalid) {
		t.Fatalf("expected invalid email error, got %v", invalidAddressErr)
	}

	_, missingNameErr := service.CreateEmailTarget(ctx, CreateNotificationTargetInput{Name: " ", Address: "dev@example.com"})
	if !errors.Is(missingNameErr, ErrNotificationTargetNameRequired) {
		t.Fatalf("expected missing name error, got %v", missingNameErr)
	}

	updatedTime := now.Add(time.Hour)
	service.now = func() time.Time { return updatedTime }
	enabled := false
	updated, err := service.UpdateTarget(ctx, target.ID, UpdateNotificationTargetInput{Enabled: &enabled})
	if err != nil {
		t.Fatalf("update target: %v", err)
	}
	if updated.Enabled {
		t.Fatal("expected target to be disabled")
	}
	if !updated.UpdatedAt.Equal(updatedTime) {
		t.Fatalf("expected updated timestamp %s, got %s", updatedTime, updated.UpdatedAt)
	}
}

func TestNotificationService_CreateSubscriptionValidationAndFiltering(t *testing.T) {
	ctx := context.Background()
	repo := memoryrepo.NewNotificationSubscriptionRepository()
	service := NewNotificationService(repo)
	now := time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	target, err := service.CreateEmailTarget(ctx, CreateNotificationTargetInput{Name: "Alerts", Address: "dev@example.com"})
	if err != nil {
		t.Fatalf("create target: %v", err)
	}

	projectID := "project-1"
	projectSub, err := service.CreateSubscription(ctx, CreateNotificationSubscriptionInput{
		TargetID:  target.ID,
		ProjectID: &projectID,
		EventType: string(domain.NotificationEventTypeBuildFailed),
	})
	if err != nil {
		t.Fatalf("create project subscription: %v", err)
	}
	if projectSub.ProjectID == nil || *projectSub.ProjectID != projectID {
		t.Fatalf("expected project scope, got %+v", projectSub)
	}

	jobID := "job-1"
	jobSub, err := service.CreateSubscription(ctx, CreateNotificationSubscriptionInput{
		TargetID:  target.ID,
		JobID:     &jobID,
		EventType: string(domain.NotificationEventTypeBuildSucceeded),
	})
	if err != nil {
		t.Fatalf("create job subscription: %v", err)
	}
	if jobSub.JobID == nil || *jobSub.JobID != jobID {
		t.Fatalf("expected job scope, got %+v", jobSub)
	}

	_, bothScopeErr := service.CreateSubscription(ctx, CreateNotificationSubscriptionInput{
		TargetID:  target.ID,
		ProjectID: &projectID,
		JobID:     &jobID,
		EventType: string(domain.NotificationEventTypeBuildFailed),
	})
	if !errors.Is(bothScopeErr, ErrNotificationSubscriptionScopeRequired) {
		t.Fatalf("expected both-scope error, got %v", bothScopeErr)
	}

	_, missingScopeErr := service.CreateSubscription(ctx, CreateNotificationSubscriptionInput{
		TargetID:  target.ID,
		EventType: string(domain.NotificationEventTypeBuildFailed),
	})
	if !errors.Is(missingScopeErr, ErrNotificationSubscriptionScopeRequired) {
		t.Fatalf("expected missing-scope error, got %v", missingScopeErr)
	}

	_, duplicateSubscriptionErr := service.CreateSubscription(ctx, CreateNotificationSubscriptionInput{
		TargetID:  target.ID,
		ProjectID: &projectID,
		EventType: string(domain.NotificationEventTypeBuildFailed),
	})
	if !errors.Is(duplicateSubscriptionErr, repository.ErrNotificationSubscriptionDuplicate) {
		t.Fatalf("expected duplicate subscription error, got %v", duplicateSubscriptionErr)
	}

	projectList, err := service.ListSubscriptions(ctx, ListNotificationSubscriptionsInput{ProjectID: &projectID})
	if err != nil {
		t.Fatalf("list project subscriptions: %v", err)
	}
	if len(projectList) != 1 || projectList[0].ID != projectSub.ID {
		t.Fatalf("expected only project subscription, got %+v", projectList)
	}

	jobList, err := service.ListSubscriptions(ctx, ListNotificationSubscriptionsInput{JobID: &jobID})
	if err != nil {
		t.Fatalf("list job subscriptions: %v", err)
	}
	if len(jobList) != 1 || jobList[0].ID != jobSub.ID {
		t.Fatalf("expected only job subscription, got %+v", jobList)
	}

	disabled := false
	updated, err := service.UpdateSubscription(ctx, projectSub.ID, UpdateNotificationSubscriptionInput{Enabled: &disabled})
	if err != nil {
		t.Fatalf("update subscription: %v", err)
	}
	if updated.Enabled {
		t.Fatal("expected subscription to be disabled")
	}
}
