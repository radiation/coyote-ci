package service

import (
	"context"
	"errors"
	"sync"
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
	if got, err := normalizeNotificationWebhookURL(" https://hooks.slack.example/services/abc "); err != nil || got != "https://hooks.slack.example/services/abc" {
		t.Fatalf("expected normalized webhook url, got %q err=%v", got, err)
	}
	if _, err := normalizeNotificationWebhookURL("http://hooks.slack.example/services/abc"); !errors.Is(err, ErrNotificationTargetWebhookURLInvalid) {
		t.Fatalf("expected invalid webhook scheme error, got %v", err)
	}
	if _, err := normalizeNotificationWebhookURL("   "); !errors.Is(err, ErrNotificationTargetWebhookURLRequired) {
		t.Fatalf("expected required webhook url error, got %v", err)
	}
	if got, err := normalizeNotificationTargetType("slack_webhook"); err != nil || got != domain.NotificationTargetTypeSlackWebhook {
		t.Fatalf("expected slack_webhook type, got %q err=%v", got, err)
	}
	if _, err := normalizeNotificationTargetType("sms"); !errors.Is(err, ErrNotificationTargetTypeInvalid) {
		t.Fatalf("expected invalid type error, got %v", err)
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

func TestNotificationService_CreateSlackTargetAndDeleteTarget(t *testing.T) {
	ctx := context.Background()
	repo := memoryrepo.NewNotificationSubscriptionRepository()
	service := NewNotificationService(repo)
	now := time.Date(2026, 6, 7, 14, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	target, err := service.CreateTarget(ctx, CreateNotificationTargetInput{
		Type:       "slack_webhook",
		Name:       "Build Alerts",
		WebhookURL: "https://hooks.slack.example/services/T/B/X",
	})
	if err != nil {
		t.Fatalf("create slack target: %v", err)
	}
	if target.Type != domain.NotificationTargetTypeSlackWebhook {
		t.Fatalf("expected slack target type, got %q", target.Type)
	}
	if target.Recipient != "https://hooks.slack.example/services/T/B/X" {
		t.Fatalf("unexpected webhook storage %q", target.Recipient)
	}

	updatedURL := "https://hooks.slack.example/services/T/B/Y"
	updated, err := service.UpdateTarget(ctx, target.ID, UpdateNotificationTargetInput{WebhookURL: &updatedURL})
	if err != nil {
		t.Fatalf("update slack target: %v", err)
	}
	if updated.Recipient != updatedURL {
		t.Fatalf("expected updated webhook url, got %q", updated.Recipient)
	}

	projectID := uuid.NewString()
	_, err = service.CreateSubscription(ctx, CreateNotificationSubscriptionInput{
		TargetID:  target.ID,
		ProjectID: &projectID,
		EventType: string(domain.NotificationEventTypeBuildFailed),
	})
	if err != nil {
		t.Fatalf("create slack subscription: %v", err)
	}

	deleteErr := service.DeleteTarget(ctx, target.ID)
	if deleteErr != nil {
		t.Fatalf("delete slack target: %v", deleteErr)
	}
	_, listSubscriptionsErr := service.ListSubscriptions(ctx, ListNotificationSubscriptionsInput{})
	if listSubscriptionsErr != nil {
		t.Fatalf("list subscriptions after delete: %v", listSubscriptionsErr)
	}
	targets, err := service.ListTargets(ctx)
	if err != nil {
		t.Fatalf("list targets after delete: %v", err)
	}
	if len(targets) != 0 {
		t.Fatalf("expected no targets after delete, got %+v", targets)
	}
}

func TestNotificationService_EnsureOwnedEmailTarget_IdempotentAndReusable(t *testing.T) {
	ctx := context.Background()
	repo := memoryrepo.NewNotificationSubscriptionRepository()
	svc := NewNotificationService(repo)
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }

	owner := domain.User{ID: uuid.NewString(), Email: "Owner@Example.com", DisplayName: strPtr("Owner User")}
	target, err := svc.EnsureOwnedEmailTarget(ctx, owner)
	if err != nil {
		t.Fatalf("ensure target failed: %v", err)
	}
	if target.OwnerUserID == nil || *target.OwnerUserID != owner.ID {
		t.Fatalf("expected target owner %q, got %+v", owner.ID, target.OwnerUserID)
	}
	if target.Name != "Owner User" {
		t.Fatalf("expected display name-backed target name, got %q", target.Name)
	}
	if target.Recipient != "<owner@example.com>" {
		t.Fatalf("expected normalized recipient, got %q", target.Recipient)
	}

	second, err := svc.EnsureOwnedEmailTarget(ctx, owner)
	if err != nil {
		t.Fatalf("second ensure target failed: %v", err)
	}
	if second.ID != target.ID {
		t.Fatalf("expected idempotent ensure target id %q, got %q", target.ID, second.ID)
	}

	otherOwner := domain.User{ID: uuid.NewString(), Email: "Owner@example.com", DisplayName: strPtr("Different")}
	_, err = svc.EnsureOwnedEmailTarget(ctx, otherOwner)
	if !errors.Is(err, repository.ErrNotificationTargetOwnershipConflict) {
		t.Fatalf("expected ownership conflict, got %v", err)
	}
}

func TestNotificationService_EnsureOwnedEmailTarget_ClaimsUnownedTargetOnlyWhenSafe(t *testing.T) {
	ctx := context.Background()
	repo := memoryrepo.NewNotificationSubscriptionRepository()
	svc := NewNotificationService(repo)
	now := time.Date(2026, 6, 24, 13, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }

	unowned, err := svc.CreateEmailTarget(ctx, CreateNotificationTargetInput{
		Name:    "Shared inbox",
		Address: "shared@example.com",
	})
	if err != nil {
		t.Fatalf("create unowned target failed: %v", err)
	}

	user := domain.User{ID: uuid.NewString(), Email: "shared@example.com"}
	claimed, err := svc.EnsureOwnedEmailTarget(ctx, user)
	if err != nil {
		t.Fatalf("claim unowned target failed: %v", err)
	}
	if claimed.ID != unowned.ID {
		t.Fatalf("expected existing unowned target to be claimed, got %q", claimed.ID)
	}
	if claimed.OwnerUserID == nil || *claimed.OwnerUserID != user.ID {
		t.Fatalf("expected claimed owner %q, got %+v", user.ID, claimed.OwnerUserID)
	}

	unownedWithSub, err := svc.CreateEmailTarget(ctx, CreateNotificationTargetInput{
		Name:    "Admin shared",
		Address: "admin-shared@example.com",
	})
	if err != nil {
		t.Fatalf("create second unowned target failed: %v", err)
	}
	projectID := uuid.NewString()
	_, err = svc.CreateSubscription(ctx, CreateNotificationSubscriptionInput{
		TargetID:  unownedWithSub.ID,
		ProjectID: &projectID,
		EventType: string(domain.NotificationEventTypeBuildFailed),
	})
	if err != nil {
		t.Fatalf("create shared subscription failed: %v", err)
	}

	_, err = svc.EnsureOwnedEmailTarget(ctx, domain.User{ID: uuid.NewString(), Email: "admin-shared@example.com"})
	if !errors.Is(err, repository.ErrNotificationTargetOwnershipConflict) {
		t.Fatalf("expected shared target conflict, got %v", err)
	}
}

func TestNotificationService_EnsureOwnedEmailTarget_ConcurrentRequestsReturnSingleTarget(t *testing.T) {
	ctx := context.Background()
	repo := memoryrepo.NewNotificationSubscriptionRepository()
	preferenceRepo := memoryrepo.NewUserNotificationPreferenceRepository()
	settingsRepo := memoryrepo.NewNotificationInstanceSettingsRepository()
	svc := NewNotificationService(repo).WithPreferenceRepository(preferenceRepo).WithInstanceSettingsRepository(settingsRepo)
	now := time.Date(2026, 6, 24, 14, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }

	user := domain.User{ID: uuid.NewString(), Email: "race@example.com", DisplayName: strPtr("Race User")}
	const workers = 8
	ids := make(chan string, workers)
	errCh := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			target, err := svc.EnsureOwnedEmailTarget(ctx, user)
			if err != nil {
				errCh <- err
				return
			}
			ids <- target.ID
		}()
	}
	wg.Wait()
	close(ids)
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatalf("unexpected concurrent ensure error: %v", err)
		}
	}

	first := ""
	for id := range ids {
		if first == "" {
			first = id
			continue
		}
		if id != first {
			t.Fatalf("expected all requests to return same target id %q, got %q", first, id)
		}
	}

	targets, err := svc.ListTargets(ctx)
	if err != nil {
		t.Fatalf("list targets failed: %v", err)
	}
	count := 0
	for _, target := range targets {
		if target.Type == domain.NotificationTargetTypeEmail && target.OwnerUserID != nil && *target.OwnerUserID == user.ID {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly one owned personal target, got %d", count)
	}

	preference, err := preferenceRepo.GetByUserID(ctx, user.ID)
	if err != nil {
		t.Fatalf("expected initialized preference, got %v", err)
	}
	if !preference.CommitAuthorFailureEnabled || preference.Source != domain.UserNotificationPreferenceSourceInstanceDefault {
		t.Fatalf("unexpected initialized preference %+v", preference)
	}
}

func TestNotificationService_EnsureOwnedEmailTarget_Validation(t *testing.T) {
	svc := NewNotificationService(memoryrepo.NewNotificationSubscriptionRepository())

	_, err := svc.EnsureOwnedEmailTarget(context.Background(), domain.User{ID: "", Email: "user@example.com"})
	if !errors.Is(err, ErrNotificationPersonalUserIDRequired) {
		t.Fatalf("expected missing user id error, got %v", err)
	}

	_, err = svc.EnsureOwnedEmailTarget(context.Background(), domain.User{ID: uuid.NewString(), Email: "   "})
	if !errors.Is(err, ErrNotificationPersonalEmailRequired) {
		t.Fatalf("expected missing email error, got %v", err)
	}
}

func TestNotificationService_GetOwnedEmailTarget(t *testing.T) {
	ctx := context.Background()
	repo := memoryrepo.NewNotificationSubscriptionRepository()
	svc := NewNotificationService(repo)

	_, err := svc.GetOwnedEmailTarget(ctx, domain.User{})
	if !errors.Is(err, ErrNotificationPersonalUserIDRequired) {
		t.Fatalf("expected missing user id error, got %v", err)
	}

	owner := domain.User{ID: uuid.NewString(), Email: "owner@example.com"}
	ensured, err := svc.EnsureOwnedEmailTarget(ctx, owner)
	if err != nil {
		t.Fatalf("ensure owned target failed: %v", err)
	}

	fetched, err := svc.GetOwnedEmailTarget(ctx, domain.User{ID: " " + owner.ID + " "})
	if err != nil {
		t.Fatalf("get owned target failed: %v", err)
	}
	if fetched.ID != ensured.ID {
		t.Fatalf("expected fetched target id %q, got %q", ensured.ID, fetched.ID)
	}

	_, err = svc.GetOwnedEmailTarget(ctx, domain.User{ID: uuid.NewString()})
	if !errors.Is(err, repository.ErrNotificationTargetNotFound) {
		t.Fatalf("expected owned target not found, got %v", err)
	}
}

func TestNotificationService_CommitAuthorFailureNotificationPreference(t *testing.T) {
	ctx := context.Background()
	targetRepo := memoryrepo.NewNotificationSubscriptionRepository()
	preferenceRepo := memoryrepo.NewUserNotificationPreferenceRepository()
	svc := NewNotificationService(targetRepo).WithPreferenceRepository(preferenceRepo)
	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }

	user := domain.User{ID: uuid.NewString(), Email: "user@example.com", DisplayName: strPtr("User Example")}

	state, err := svc.GetCommitAuthorFailureNotificationPreference(ctx, user)
	if err != nil {
		t.Fatalf("get default preference failed: %v", err)
	}
	if state.Enabled || state.Eligible || state.DeliveryActive {
		t.Fatalf("expected disabled ineligible default, got %+v", state)
	}
	if state.UnavailableReason == nil || *state.UnavailableReason != NotificationPreferenceUnavailableReasonPersonalTargetRequired {
		t.Fatalf("expected missing-target reason, got %+v", state.UnavailableReason)
	}

	if _, enableErr := svc.SetCommitAuthorFailureNotificationPreference(ctx, user, notificationBoolPtr(true)); !errors.Is(enableErr, ErrNotificationPreferencePersonalTargetRequired) {
		t.Fatalf("expected enable without target to fail, got %v", enableErr)
	}

	target, err := svc.EnsureOwnedEmailTarget(ctx, user)
	if err != nil {
		t.Fatalf("ensure owned target failed: %v", err)
	}

	enabledState, err := svc.SetCommitAuthorFailureNotificationPreference(ctx, user, notificationBoolPtr(true))
	if err != nil {
		t.Fatalf("enable preference failed: %v", err)
	}
	if !enabledState.Enabled || !enabledState.Eligible || !enabledState.DeliveryActive {
		t.Fatalf("unexpected enabled state %+v", enabledState)
	}
	if enabledState.Target == nil || enabledState.Target.ID != target.ID {
		t.Fatalf("expected preference target %q, got %+v", target.ID, enabledState.Target)
	}

	repeatedEnable, err := svc.SetCommitAuthorFailureNotificationPreference(ctx, user, notificationBoolPtr(true))
	if err != nil {
		t.Fatalf("repeat enable failed: %v", err)
	}
	if !repeatedEnable.Enabled {
		t.Fatal("expected repeat enable to remain enabled")
	}

	manualTarget, err := svc.CreateEmailTarget(ctx, CreateNotificationTargetInput{Name: "Project Alerts", Address: "alerts@example.com"})
	if err != nil {
		t.Fatalf("create manual target failed: %v", err)
	}
	projectID := uuid.NewString()
	_, err = svc.CreateSubscription(ctx, CreateNotificationSubscriptionInput{TargetID: manualTarget.ID, ProjectID: &projectID, EventType: string(domain.NotificationEventTypeBuildFailed)})
	if err != nil {
		t.Fatalf("create manual subscription failed: %v", err)
	}

	disabledState, err := svc.SetCommitAuthorFailureNotificationPreference(ctx, user, notificationBoolPtr(false))
	if err != nil {
		t.Fatalf("disable preference failed: %v", err)
	}
	if disabledState.Enabled || !disabledState.Eligible || disabledState.DeliveryActive {
		t.Fatalf("unexpected disabled state %+v", disabledState)
	}
	subscriptions, err := svc.ListSubscriptions(ctx, ListNotificationSubscriptionsInput{ProjectID: &projectID})
	if err != nil {
		t.Fatalf("list subscriptions failed: %v", err)
	}
	if len(subscriptions) != 1 {
		t.Fatalf("expected manual subscription to remain, got %+v", subscriptions)
	}

	falseValue := false
	_, err = svc.UpdateTarget(ctx, target.ID, UpdateNotificationTargetInput{Enabled: &falseValue})
	if err != nil {
		t.Fatalf("disable personal target failed: %v", err)
	}
	pausedState, err := svc.GetCommitAuthorFailureNotificationPreference(ctx, user)
	if err != nil {
		t.Fatalf("get paused state failed: %v", err)
	}
	if pausedState.UnavailableReason == nil || *pausedState.UnavailableReason != NotificationPreferenceUnavailableReasonPersonalTargetDisabled {
		t.Fatalf("expected disabled-target reason, got %+v", pausedState.UnavailableReason)
	}
	if pausedState.Eligible != true || pausedState.DeliveryActive {
		t.Fatalf("expected paused delivery state, got %+v", pausedState)
	}

	disabledWithPausedTarget, err := svc.SetCommitAuthorFailureNotificationPreference(ctx, user, notificationBoolPtr(false))
	if err != nil {
		t.Fatalf("disable preference with disabled target failed: %v", err)
	}
	if disabledWithPausedTarget.Enabled || disabledWithPausedTarget.DeliveryActive {
		t.Fatalf("expected disabled preference with paused target, got %+v", disabledWithPausedTarget)
	}

	enabledAgain, err := svc.SetCommitAuthorFailureNotificationPreference(ctx, user, notificationBoolPtr(true))
	if !errors.Is(err, ErrNotificationPreferencePersonalTargetRequired) {
		t.Fatalf("expected re-enable with disabled target to fail, got state=%+v err=%v", enabledAgain, err)
	}

	if deleteErr := svc.DeleteTarget(ctx, target.ID); deleteErr != nil {
		t.Fatalf("delete personal target failed: %v", deleteErr)
	}
	missingTargetEnabledState, err := preferenceRepo.Upsert(ctx, domain.UserNotificationPreference{
		UserID:                     user.ID,
		CommitAuthorFailureEnabled: true,
		CreatedAt:                  now,
		UpdatedAt:                  now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("restore enabled preference without target failed: %v", err)
	}
	if !missingTargetEnabledState.CommitAuthorFailureEnabled {
		t.Fatalf("expected stored enabled preference, got %+v", missingTargetEnabledState)
	}

	missingTargetState, err := svc.GetCommitAuthorFailureNotificationPreference(ctx, user)
	if err != nil {
		t.Fatalf("get missing-target enabled state failed: %v", err)
	}
	if !missingTargetState.Enabled || missingTargetState.Eligible || missingTargetState.DeliveryActive {
		t.Fatalf("expected enabled preference but inactive missing-target state, got %+v", missingTargetState)
	}
	if missingTargetState.UnavailableReason == nil || *missingTargetState.UnavailableReason != NotificationPreferenceUnavailableReasonPersonalTargetRequired {
		t.Fatalf("expected missing-target reason after target removal, got %+v", missingTargetState.UnavailableReason)
	}

	disabledWithoutTarget, err := svc.SetCommitAuthorFailureNotificationPreference(ctx, user, notificationBoolPtr(false))
	if err != nil {
		t.Fatalf("disable preference without target failed: %v", err)
	}
	if disabledWithoutTarget.Enabled || disabledWithoutTarget.DeliveryActive {
		t.Fatalf("expected disable without target to succeed, got %+v", disabledWithoutTarget)
	}
}

func TestNotificationService_NotificationDefaultsAndInitialization(t *testing.T) {
	ctx := context.Background()
	targetRepo := memoryrepo.NewNotificationSubscriptionRepository()
	preferenceRepo := memoryrepo.NewUserNotificationPreferenceRepository()
	settingsRepo := memoryrepo.NewNotificationInstanceSettingsRepository()
	svc := NewNotificationService(targetRepo).WithPreferenceRepository(preferenceRepo).WithInstanceSettingsRepository(settingsRepo)
	now := time.Date(2026, 6, 26, 9, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }

	defaults, err := svc.GetNotificationDefaults(ctx)
	if err != nil {
		t.Fatalf("get defaults failed: %v", err)
	}
	if !defaults.DefaultCommitAuthorFailureEmailEnabled {
		t.Fatalf("expected unset defaults to resolve enabled, got %+v", defaults)
	}

	updatedDefaults, err := svc.SetNotificationDefaults(ctx, notificationBoolPtr(false))
	if err != nil {
		t.Fatalf("set defaults failed: %v", err)
	}
	if updatedDefaults.DefaultCommitAuthorFailureEmailEnabled {
		t.Fatalf("expected defaults to be disabled, got %+v", updatedDefaults)
	}

	user := domain.User{ID: uuid.NewString(), Email: "new-user@example.com"}
	if _, ensureErr := svc.EnsureOwnedEmailTarget(ctx, user); ensureErr != nil {
		t.Fatalf("ensure owned target failed: %v", ensureErr)
	}

	preference, err := preferenceRepo.GetByUserID(ctx, user.ID)
	if err != nil {
		t.Fatalf("expected initialized preference, got %v", err)
	}
	if preference.CommitAuthorFailureEnabled {
		t.Fatalf("expected disabled preference from instance default, got %+v", preference)
	}
	if preference.Source != domain.UserNotificationPreferenceSourceInstanceDefault {
		t.Fatalf("expected instance-default source, got %+v", preference)
	}

	if _, enableErr := svc.SetCommitAuthorFailureNotificationPreference(ctx, user, notificationBoolPtr(true)); enableErr != nil {
		t.Fatalf("explicit enable failed: %v", enableErr)
	}
	if _, defaultsErr := svc.SetNotificationDefaults(ctx, notificationBoolPtr(true)); defaultsErr != nil {
		t.Fatalf("re-enable defaults failed: %v", defaultsErr)
	}
	if _, repeatEnsureErr := svc.EnsureOwnedEmailTarget(ctx, user); repeatEnsureErr != nil {
		t.Fatalf("repeat ensure failed: %v", repeatEnsureErr)
	}

	explicitPreference, err := preferenceRepo.GetByUserID(ctx, user.ID)
	if err != nil {
		t.Fatalf("get explicit preference failed: %v", err)
	}
	if !explicitPreference.CommitAuthorFailureEnabled || explicitPreference.Source != domain.UserNotificationPreferenceSourceUser {
		t.Fatalf("expected explicit user preference to be preserved, got %+v", explicitPreference)
	}
}

func TestNotificationService_ExistingOwnedTargetIsNotRetroactivelyInitialized(t *testing.T) {
	ctx := context.Background()
	targetRepo := memoryrepo.NewNotificationSubscriptionRepository()
	preferenceRepo := memoryrepo.NewUserNotificationPreferenceRepository()
	settingsRepo := memoryrepo.NewNotificationInstanceSettingsRepository()
	svc := NewNotificationService(targetRepo).WithPreferenceRepository(preferenceRepo).WithInstanceSettingsRepository(settingsRepo)
	now := time.Date(2026, 6, 26, 10, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }

	userID := uuid.NewString()
	_, err := targetRepo.EnsureOwnedEmailTarget(ctx, repository.EnsureOwnedNotificationEmailTargetInput{
		ID:          uuid.NewString(),
		OwnerUserID: userID,
		Name:        "Existing User",
		Recipient:   "<existing@example.com>",
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		t.Fatalf("seed owned target failed: %v", err)
	}

	state, err := svc.GetCommitAuthorFailureNotificationPreference(ctx, domain.User{ID: userID, Email: "existing@example.com"})
	if err != nil {
		t.Fatalf("get preference state failed: %v", err)
	}
	if state.Enabled || !state.Eligible {
		t.Fatalf("expected existing target without preference to stay disabled but eligible, got %+v", state)
	}

	if _, defaultsErr := svc.SetNotificationDefaults(ctx, notificationBoolPtr(false)); defaultsErr != nil {
		t.Fatalf("set defaults failed: %v", defaultsErr)
	}

	ensured, err := svc.EnsureOwnedEmailTarget(ctx, domain.User{ID: userID, Email: "existing@example.com"})
	if err != nil {
		t.Fatalf("ensure existing target failed: %v", err)
	}
	if ensured.ID == "" {
		t.Fatal("expected ensure to return the existing owned target")
	}

	_, err = preferenceRepo.GetByUserID(ctx, userID)
	if !errors.Is(err, repository.ErrUserNotificationPreferenceNotFound) {
		t.Fatalf("expected no retroactive preference row, got %v", err)
	}
}

func notificationBoolPtr(value bool) *bool {
	return &value
}
