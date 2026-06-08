package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

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

	projectID := uuid.NewString()
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

	jobID := uuid.NewString()
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

func TestNotificationService_TargetAndSubscriptionErrorBranches(t *testing.T) {
	ctx := context.Background()
	repo := memoryrepo.NewNotificationSubscriptionRepository()
	service := NewNotificationService(repo)
	now := time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	if _, err := service.UpdateTarget(ctx, " ", UpdateNotificationTargetInput{}); !errors.Is(err, repository.ErrNotificationTargetNotFound) {
		t.Fatalf("expected blank target id to be not found, got %v", err)
	}
	if _, err := service.UpdateTarget(ctx, "not-a-uuid", UpdateNotificationTargetInput{}); !errors.Is(err, ErrNotificationTargetIDInvalid) {
		t.Fatalf("expected invalid target id error, got %v", err)
	}
	if _, err := service.UpdateSubscription(ctx, " ", UpdateNotificationSubscriptionInput{}); !errors.Is(err, repository.ErrNotificationSubscriptionNotFound) {
		t.Fatalf("expected blank subscription id to be not found, got %v", err)
	}
	if _, err := service.UpdateSubscription(ctx, "not-a-uuid", UpdateNotificationSubscriptionInput{}); !errors.Is(err, ErrNotificationSubscriptionIDInvalid) {
		t.Fatalf("expected invalid subscription id error, got %v", err)
	}
	if err := service.DeleteSubscription(ctx, " "); !errors.Is(err, repository.ErrNotificationSubscriptionNotFound) {
		t.Fatalf("expected blank delete subscription id to be not found, got %v", err)
	}
	if err := service.DeleteSubscription(ctx, "not-a-uuid"); !errors.Is(err, ErrNotificationSubscriptionIDInvalid) {
		t.Fatalf("expected invalid delete subscription id error, got %v", err)
	}

	target, createErr := service.CreateEmailTarget(ctx, CreateNotificationTargetInput{Name: "Alerts", Address: "dev@example.com"})
	if createErr != nil {
		t.Fatalf("create target: %v", createErr)
	}
	if _, err := service.ListTargets(ctx); err != nil {
		t.Fatalf("list targets: %v", err)
	}

	blankName := "   "
	if _, err := service.UpdateTarget(ctx, target.ID, UpdateNotificationTargetInput{Name: &blankName}); !errors.Is(err, ErrNotificationTargetNameRequired) {
		t.Fatalf("expected blank update name error, got %v", err)
	}
	blankAddress := "   "
	if _, err := service.UpdateTarget(ctx, target.ID, UpdateNotificationTargetInput{Address: &blankAddress}); !errors.Is(err, ErrNotificationTargetAddressRequired) {
		t.Fatalf("expected blank update address error, got %v", err)
	}
	invalidAddress := "bad-email"
	if _, err := service.UpdateTarget(ctx, target.ID, UpdateNotificationTargetInput{Address: &invalidAddress}); !errors.Is(err, ErrNotificationTargetAddressInvalid) {
		t.Fatalf("expected invalid update address error, got %v", err)
	}
	updatedName := " Build Notifications "
	updatedAddress := "alerts@example.com"
	enabled := true
	updatedTarget, updateTargetErr := service.UpdateTarget(ctx, target.ID, UpdateNotificationTargetInput{Name: &updatedName, Address: &updatedAddress, Enabled: &enabled})
	if updateTargetErr != nil {
		t.Fatalf("update target with name/address failed: %v", updateTargetErr)
	}
	if updatedTarget.Name != "Build Notifications" || updatedTarget.Recipient != "<alerts@example.com>" {
		t.Fatalf("unexpected updated target %+v", updatedTarget)
	}

	missingTargetID := uuid.NewString()
	if _, err := service.CreateSubscription(ctx, CreateNotificationSubscriptionInput{TargetID: missingTargetID, EventType: string(domain.NotificationEventTypeBuildFailed), ProjectID: &missingTargetID}); !errors.Is(err, repository.ErrNotificationTargetNotFound) {
		t.Fatalf("expected missing target error, got %v", err)
	}
	if _, err := service.CreateSubscription(ctx, CreateNotificationSubscriptionInput{EventType: string(domain.NotificationEventTypeBuildFailed), ProjectID: &missingTargetID}); !errors.Is(err, ErrNotificationSubscriptionTargetIDRequired) {
		t.Fatalf("expected missing target id error, got %v", err)
	}
	invalidTargetID := "not-a-uuid"
	if _, err := service.CreateSubscription(ctx, CreateNotificationSubscriptionInput{TargetID: invalidTargetID, EventType: string(domain.NotificationEventTypeBuildFailed), ProjectID: &missingTargetID}); !errors.Is(err, ErrNotificationSubscriptionTargetIDInvalid) {
		t.Fatalf("expected invalid target id error, got %v", err)
	}
	invalidProjectID := "not-a-uuid"
	if _, err := service.CreateSubscription(ctx, CreateNotificationSubscriptionInput{TargetID: target.ID, EventType: string(domain.NotificationEventTypeBuildFailed), ProjectID: &invalidProjectID}); !errors.Is(err, ErrNotificationSubscriptionProjectIDInvalid) {
		t.Fatalf("expected invalid project id error, got %v", err)
	}
	invalidJobID := "not-a-uuid"
	if _, err := service.CreateSubscription(ctx, CreateNotificationSubscriptionInput{TargetID: target.ID, EventType: string(domain.NotificationEventTypeBuildFailed), JobID: &invalidJobID}); !errors.Is(err, ErrNotificationSubscriptionJobIDInvalid) {
		t.Fatalf("expected invalid job id error, got %v", err)
	}
	if _, err := service.CreateSubscription(ctx, CreateNotificationSubscriptionInput{TargetID: target.ID, EventType: "build_canceled", ProjectID: &missingTargetID}); !errors.Is(err, ErrNotificationSubscriptionEventTypeInvalid) {
		t.Fatalf("expected invalid event type error, got %v", err)
	}
	if _, err := service.ListSubscriptions(ctx, ListNotificationSubscriptionsInput{ProjectID: &invalidProjectID}); !errors.Is(err, ErrNotificationSubscriptionProjectIDInvalid) {
		t.Fatalf("expected invalid list project id error, got %v", err)
	}
	if _, err := service.ListSubscriptions(ctx, ListNotificationSubscriptionsInput{JobID: &invalidJobID}); !errors.Is(err, ErrNotificationSubscriptionJobIDInvalid) {
		t.Fatalf("expected invalid list job id error, got %v", err)
	}

	jobID := uuid.NewString()
	createdSub, createSubErr := service.CreateSubscription(ctx, CreateNotificationSubscriptionInput{TargetID: target.ID, JobID: &jobID, EventType: string(domain.NotificationEventTypeBuildFailed)})
	if createSubErr != nil {
		t.Fatalf("create subscription for delete failed: %v", createSubErr)
	}
	if deleteErr := service.DeleteSubscription(ctx, createdSub.ID); deleteErr != nil {
		t.Fatalf("delete subscription failed: %v", deleteErr)
	}
	if deleteMissingErr := service.DeleteSubscription(ctx, createdSub.ID); !errors.Is(deleteMissingErr, repository.ErrNotificationSubscriptionNotFound) {
		t.Fatalf("expected delete missing subscription error, got %v", deleteMissingErr)
	}

	if got, err := normalizeNotificationEmailAddress(" dev@example.com "); err != nil || got != "<dev@example.com>" {
		t.Fatalf("expected normalized email address, got %q err=%v", got, err)
	}
	if _, err := normalizeNotificationEmailAddress("   "); !errors.Is(err, ErrNotificationTargetAddressRequired) {
		t.Fatalf("expected required address error, got %v", err)
	}
	if _, err := normalizeNotificationEventType("build_succeeded"); err != nil {
		t.Fatalf("expected valid event type, got %v", err)
	}
	if _, err := normalizeNotificationEventType("invalid"); !errors.Is(err, ErrNotificationSubscriptionEventTypeInvalid) {
		t.Fatalf("expected invalid event type error, got %v", err)
	}
	if trimOptionalStringValue(nil) != nil {
		t.Fatal("expected nil optional string to stay nil")
	}
	blankValue := "   "
	if trimOptionalStringValue(&blankValue) != nil {
		t.Fatal("expected blank optional string to trim to nil")
	}
	trimmedValue := " value "
	trimmed := trimOptionalStringValue(&trimmedValue)
	if trimmed == nil || *trimmed != "value" {
		t.Fatalf("expected trimmed optional string, got %v", trimmed)
	}
	if normalizedID, err := normalizeRequiredNotificationUUID(" 550e8400-e29b-41d4-a716-446655440000 ", repository.ErrNotificationTargetNotFound, ErrNotificationTargetIDInvalid); err != nil || normalizedID != "550e8400-e29b-41d4-a716-446655440000" {
		t.Fatalf("expected normalized uuid, got %q err=%v", normalizedID, err)
	}
	if _, err := normalizeRequiredNotificationUUID("bad", repository.ErrNotificationTargetNotFound, ErrNotificationTargetIDInvalid); !errors.Is(err, ErrNotificationTargetIDInvalid) {
		t.Fatalf("expected invalid required uuid error, got %v", err)
	}
	validOptionalID := "550e8400-e29b-41d4-a716-446655440000"
	if normalizedOptionalID, err := normalizeOptionalNotificationUUID(&validOptionalID, ErrNotificationSubscriptionProjectIDInvalid); err != nil || normalizedOptionalID == nil || *normalizedOptionalID != validOptionalID {
		t.Fatalf("expected normalized optional uuid, got %v err=%v", normalizedOptionalID, err)
	}
	if _, err := normalizeOptionalNotificationUUID(&invalidProjectID, ErrNotificationSubscriptionProjectIDInvalid); !errors.Is(err, ErrNotificationSubscriptionProjectIDInvalid) {
		t.Fatalf("expected invalid optional uuid error, got %v", err)
	}
}
