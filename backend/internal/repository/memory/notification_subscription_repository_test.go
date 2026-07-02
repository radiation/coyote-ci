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

type nonMemoryPreferenceRepo struct{}

func (r *nonMemoryPreferenceRepo) GetByUserID(context.Context, string) (domain.UserNotificationPreference, error) {
	return domain.UserNotificationPreference{}, repository.ErrUserNotificationPreferenceNotFound
}

func (r *nonMemoryPreferenceRepo) InitializeIfAbsent(context.Context, domain.UserNotificationPreference) (domain.UserNotificationPreference, bool, error) {
	return domain.UserNotificationPreference{}, false, nil
}

func (r *nonMemoryPreferenceRepo) Upsert(context.Context, domain.UserNotificationPreference) (domain.UserNotificationPreference, error) {
	return domain.UserNotificationPreference{}, nil
}

type nonMemorySettingsRepo struct{}

func (r *nonMemorySettingsRepo) Get(context.Context) (domain.NotificationInstanceSettings, error) {
	return domain.NotificationInstanceSettings{}, repository.ErrNotificationInstanceSettingsNotFound
}

func (r *nonMemorySettingsRepo) Upsert(context.Context, domain.NotificationInstanceSettings) (domain.NotificationInstanceSettings, error) {
	return domain.NotificationInstanceSettings{}, nil
}

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

	normalized, err := normalizeNotificationTargetRecipient(domain.NotificationTargetTypeEmail, " dev@example.com ")
	if err != nil {
		t.Fatalf("normalize recipient failed: %v", err)
	}
	if normalized != "<dev@example.com>" {
		t.Fatalf("unexpected normalized recipient %q", normalized)
	}

	_, err = normalizeNotificationTargetRecipient(domain.NotificationTargetTypeEmail, "bad-email")
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

func TestNotificationSubscriptionRepository_OwnedEmailTargets(t *testing.T) {
	repo := NewNotificationSubscriptionRepository()
	ctx := context.Background()
	now := time.Now().UTC()

	claimable, err := repo.CreateTarget(ctx, domain.NotificationTarget{
		Type:      domain.NotificationTargetTypeEmail,
		Recipient: "shared@example.com",
		Enabled:   true,
	})
	if err != nil {
		t.Fatalf("create claimable target failed: %v", err)
	}

	claimed, err := repo.EnsureOwnedEmailTarget(ctx, repository.EnsureOwnedNotificationEmailTargetInput{
		ID:          "claimed-target",
		OwnerUserID: " user-1 ",
		Name:        "User One",
		Recipient:   claimable.Recipient,
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		t.Fatalf("claim target failed: %v", err)
	}
	if claimed.ID != claimable.ID {
		t.Fatalf("expected claimed target id %q, got %q", claimable.ID, claimed.ID)
	}
	if claimed.OwnerUserID == nil || *claimed.OwnerUserID != "user-1" {
		t.Fatalf("expected claimed owner user-1, got %+v", claimed.OwnerUserID)
	}

	fetched, err := repo.GetOwnedEmailTargetByUserID(ctx, " user-1 ")
	if err != nil {
		t.Fatalf("get owned target failed: %v", err)
	}
	if fetched.ID != claimable.ID {
		t.Fatalf("expected fetched owned target id %q, got %q", claimable.ID, fetched.ID)
	}

	again, err := repo.EnsureOwnedEmailTarget(ctx, repository.EnsureOwnedNotificationEmailTargetInput{
		ID:          "ignored-id",
		OwnerUserID: "user-1",
		Name:        "Ignored",
		Recipient:   "<different@example.com>",
		CreatedAt:   now.Add(time.Minute),
		UpdatedAt:   now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("re-ensure owned target failed: %v", err)
	}
	if again.ID != claimable.ID {
		t.Fatalf("expected idempotent owned target id %q, got %q", claimable.ID, again.ID)
	}

	otherOwner := "other-user"
	conflictingOwned, err := repo.CreateTarget(ctx, domain.NotificationTarget{
		Type:        domain.NotificationTargetTypeEmail,
		OwnerUserID: &otherOwner,
		Recipient:   "owned@example.com",
		Enabled:     true,
	})
	if err != nil {
		t.Fatalf("create conflicting owned target failed: %v", err)
	}

	_, err = repo.EnsureOwnedEmailTarget(ctx, repository.EnsureOwnedNotificationEmailTargetInput{
		OwnerUserID: "user-2",
		Name:        "User Two",
		Recipient:   conflictingOwned.Recipient,
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if !errors.Is(err, repository.ErrNotificationTargetOwnershipConflict) {
		t.Fatalf("expected owned target conflict, got %v", err)
	}

	sharedWithSubscription, err := repo.CreateTarget(ctx, domain.NotificationTarget{
		Type:      domain.NotificationTargetTypeEmail,
		Recipient: "subscribed@example.com",
		Enabled:   true,
	})
	if err != nil {
		t.Fatalf("create shared target failed: %v", err)
	}
	projectID := "project-1"
	_, err = repo.CreateSubscription(ctx, domain.NotificationSubscription{
		TargetID:  sharedWithSubscription.ID,
		ProjectID: &projectID,
		EventType: domain.NotificationEventTypeBuildFailed,
		Enabled:   true,
	})
	if err != nil {
		t.Fatalf("create shared target subscription failed: %v", err)
	}

	_, err = repo.EnsureOwnedEmailTarget(ctx, repository.EnsureOwnedNotificationEmailTargetInput{
		OwnerUserID: "user-3",
		Name:        "User Three",
		Recipient:   sharedWithSubscription.Recipient,
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if !errors.Is(err, repository.ErrNotificationTargetOwnershipConflict) {
		t.Fatalf("expected subscribed shared target conflict, got %v", err)
	}

	created, err := repo.EnsureOwnedEmailTarget(ctx, repository.EnsureOwnedNotificationEmailTargetInput{
		ID:          "fresh-target",
		OwnerUserID: "user-4",
		Recipient:   "<fresh@example.com>",
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		t.Fatalf("create fresh owned target failed: %v", err)
	}
	if created.ID != "fresh-target" {
		t.Fatalf("expected created target id fresh-target, got %q", created.ID)
	}
	if created.Name != "<fresh@example.com>" {
		t.Fatalf("expected fallback name to use recipient, got %q", created.Name)
	}
	if created.OwnerUserID == nil || *created.OwnerUserID != "user-4" {
		t.Fatalf("expected created owner user-4, got %+v", created.OwnerUserID)
	}

	_, err = repo.GetOwnedEmailTargetByUserID(ctx, "missing-user")
	if !errors.Is(err, repository.ErrNotificationTargetNotFound) {
		t.Fatalf("expected owned target not found, got %v", err)
	}
}

func TestNotificationSubscriptionRepository_SetOwnedEmailTargetEnabled(t *testing.T) {
	repo := NewNotificationSubscriptionRepository()
	ctx := context.Background()
	createdAt := time.Date(2026, 6, 29, 11, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(15 * time.Minute)
	ownerUserID := "user-1"

	owned, err := repo.CreateTarget(ctx, domain.NotificationTarget{
		ID:          "owned-target",
		OwnerUserID: &ownerUserID,
		Type:        domain.NotificationTargetTypeEmail,
		Name:        "User Example",
		Recipient:   "user@example.com",
		Enabled:     true,
		CreatedAt:   createdAt,
		UpdatedAt:   createdAt,
	})
	if err != nil {
		t.Fatalf("create owned target failed: %v", err)
	}

	shared, err := repo.CreateTarget(ctx, domain.NotificationTarget{
		ID:        "shared-target",
		Type:      domain.NotificationTargetTypeEmail,
		Name:      "Shared Inbox",
		Recipient: "shared@example.com",
		Enabled:   true,
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	})
	if err != nil {
		t.Fatalf("create shared target failed: %v", err)
	}

	otherOwner := "user-2"
	otherOwned, err := repo.CreateTarget(ctx, domain.NotificationTarget{
		ID:          "other-owned-target",
		OwnerUserID: &otherOwner,
		Type:        domain.NotificationTargetTypeEmail,
		Name:        "Other User",
		Recipient:   "other@example.com",
		Enabled:     true,
		CreatedAt:   createdAt,
		UpdatedAt:   createdAt,
	})
	if err != nil {
		t.Fatalf("create other owned target failed: %v", err)
	}

	disabled, err := repo.SetOwnedEmailTargetEnabled(ctx, " user-1 ", false, updatedAt)
	if err != nil {
		t.Fatalf("disable owned target failed: %v", err)
	}
	if disabled.Enabled {
		t.Fatalf("expected disabled target, got %+v", disabled)
	}
	if disabled.Name != owned.Name || disabled.Recipient != owned.Recipient || disabled.Type != owned.Type {
		t.Fatalf("expected identifying fields to remain unchanged, got %+v", disabled)
	}
	if disabled.OwnerUserID == nil || *disabled.OwnerUserID != ownerUserID {
		t.Fatalf("expected owner to remain unchanged, got %+v", disabled.OwnerUserID)
	}
	if !disabled.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("expected updated_at to change to %v, got %v", updatedAt, disabled.UpdatedAt)
	}

	reEnabled, err := repo.SetOwnedEmailTargetEnabled(ctx, ownerUserID, true, updatedAt.Add(5*time.Minute))
	if err != nil {
		t.Fatalf("re-enable owned target failed: %v", err)
	}
	if !reEnabled.Enabled {
		t.Fatalf("expected enabled target, got %+v", reEnabled)
	}

	repeated, err := repo.SetOwnedEmailTargetEnabled(ctx, ownerUserID, true, updatedAt.Add(10*time.Minute))
	if err != nil {
		t.Fatalf("repeat enable failed: %v", err)
	}
	if !repeated.Enabled {
		t.Fatalf("expected idempotent enabled target, got %+v", repeated)
	}

	if _, missingErr := repo.SetOwnedEmailTargetEnabled(ctx, "missing-user", false, updatedAt); !errors.Is(missingErr, repository.ErrNotificationTargetNotFound) {
		t.Fatalf("expected missing owned target error, got %v", missingErr)
	}
	if _, otherUpdateErr := repo.SetOwnedEmailTargetEnabled(ctx, otherOwner, false, updatedAt); otherUpdateErr != nil {
		t.Fatalf("expected other user to update only their own target, got %v", otherUpdateErr)
	}

	sharedFetched, err := repo.GetTargetByID(ctx, shared.ID)
	if err != nil {
		t.Fatalf("get shared target failed: %v", err)
	}
	if !sharedFetched.Enabled {
		t.Fatalf("expected unowned shared target to remain unchanged, got %+v", sharedFetched)
	}

	otherFetched, err := repo.GetTargetByID(ctx, otherOwned.ID)
	if err != nil {
		t.Fatalf("get other owned target failed: %v", err)
	}
	if otherFetched.Enabled {
		t.Fatalf("expected other user target to reflect only its own update, got %+v", otherFetched)
	}

	ownedFetched, err := repo.GetTargetByID(ctx, owned.ID)
	if err != nil {
		t.Fatalf("get owned target failed: %v", err)
	}
	if ownedFetched.Name != owned.Name || ownedFetched.Recipient != owned.Recipient || ownedFetched.OwnerUserID == nil || *ownedFetched.OwnerUserID != ownerUserID || ownedFetched.Type != owned.Type {
		t.Fatalf("expected owned target identifying fields to remain unchanged, got %+v", ownedFetched)
	}

	unownedOnlyRepo := NewNotificationSubscriptionRepository()
	if _, err := unownedOnlyRepo.CreateTarget(ctx, domain.NotificationTarget{
		ID:        "unowned-only",
		Type:      domain.NotificationTargetTypeEmail,
		Name:      "Unowned",
		Recipient: "unowned@example.com",
		Enabled:   true,
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	}); err != nil {
		t.Fatalf("create unowned-only target failed: %v", err)
	}
	if _, err := unownedOnlyRepo.SetOwnedEmailTargetEnabled(ctx, ownerUserID, false, updatedAt); !errors.Is(err, repository.ErrNotificationTargetNotFound) {
		t.Fatalf("expected unowned target update to behave as not found, got %v", err)
	}
}

func TestNotificationSubscriptionRepository_ListAndUpdateAdminViews(t *testing.T) {
	repo := NewNotificationSubscriptionRepository()
	ctx := context.Background()
	createdAt := time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC)
	targetOne, err := repo.CreateTarget(ctx, domain.NotificationTarget{
		ID:        "target-1",
		Type:      domain.NotificationTargetTypeEmail,
		Name:      "Ops",
		Recipient: "ops@example.com",
		Enabled:   true,
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	})
	if err != nil {
		t.Fatalf("create target one: %v", err)
	}
	targetTwo, err := repo.CreateTarget(ctx, domain.NotificationTarget{
		ID:        "target-2",
		Type:      domain.NotificationTargetTypeEmail,
		Name:      "QA",
		Recipient: "qa@example.com",
		Enabled:   true,
		CreatedAt: createdAt.Add(time.Minute),
		UpdatedAt: createdAt.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("create target two: %v", err)
	}

	targets, err := repo.ListTargets(ctx)
	if err != nil {
		t.Fatalf("list targets: %v", err)
	}
	if len(targets) != 2 || targets[0].ID != targetOne.ID || targets[1].ID != targetTwo.ID {
		t.Fatalf("expected ordered targets, got %+v", targets)
	}

	targetOne.Name = "Ops Pager"
	targetOne.Enabled = false
	targetOne.UpdatedAt = createdAt.Add(2 * time.Minute)
	updatedTarget, err := repo.UpdateTarget(ctx, targetOne)
	if err != nil {
		t.Fatalf("update target: %v", err)
	}
	if updatedTarget.Name != "Ops Pager" || updatedTarget.Enabled {
		t.Fatalf("expected updated target fields, got %+v", updatedTarget)
	}

	projectID := "project-1"
	jobID := "job-1"
	projectSub, err := repo.CreateSubscription(ctx, domain.NotificationSubscription{
		ID:        "subscription-project",
		TargetID:  targetOne.ID,
		ProjectID: &projectID,
		EventType: domain.NotificationEventTypeBuildFailed,
		Enabled:   true,
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	})
	if err != nil {
		t.Fatalf("create project subscription: %v", err)
	}
	jobSub, err := repo.CreateSubscription(ctx, domain.NotificationSubscription{
		ID:        "subscription-job",
		TargetID:  targetTwo.ID,
		JobID:     &jobID,
		EventType: domain.NotificationEventTypeBuildSucceeded,
		Enabled:   true,
		CreatedAt: createdAt.Add(time.Minute),
		UpdatedAt: createdAt.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("create job subscription: %v", err)
	}

	projectSubs, err := repo.ListSubscriptions(ctx, repository.NotificationSubscriptionListFilter{ProjectID: &projectID})
	if err != nil {
		t.Fatalf("list project subscriptions: %v", err)
	}
	if len(projectSubs) != 1 || projectSubs[0].ID != projectSub.ID {
		t.Fatalf("expected one project subscription, got %+v", projectSubs)
	}

	jobSubs, err := repo.ListSubscriptions(ctx, repository.NotificationSubscriptionListFilter{JobID: &jobID})
	if err != nil {
		t.Fatalf("list job subscriptions: %v", err)
	}
	if len(jobSubs) != 1 || jobSubs[0].ID != jobSub.ID {
		t.Fatalf("expected one job subscription, got %+v", jobSubs)
	}

	updatedProjectSub, err := repo.UpdateSubscription(ctx, domain.NotificationSubscription{
		ID:        projectSub.ID,
		TargetID:  projectSub.TargetID,
		ProjectID: projectSub.ProjectID,
		EventType: projectSub.EventType,
		Enabled:   false,
		CreatedAt: projectSub.CreatedAt,
		UpdatedAt: createdAt.Add(3 * time.Minute),
	})
	if err != nil {
		t.Fatalf("update subscription: %v", err)
	}
	if updatedProjectSub.Enabled {
		t.Fatalf("expected disabled subscription, got %+v", updatedProjectSub)
	}

	deleteErr := repo.DeleteSubscription(ctx, jobSub.ID)
	if deleteErr != nil {
		t.Fatalf("delete subscription: %v", deleteErr)
	}
	_, deletedSubscriptionErr := repo.GetSubscriptionByID(ctx, jobSub.ID)
	if !errors.Is(deletedSubscriptionErr, repository.ErrNotificationSubscriptionNotFound) {
		t.Fatalf("expected deleted subscription to be missing, got %v", deletedSubscriptionErr)
	}
	_, missingTargetErr := repo.GetTargetByID(ctx, "missing-target")
	if !errors.Is(missingTargetErr, repository.ErrNotificationTargetNotFound) {
		t.Fatalf("expected missing target error, got %v", missingTargetErr)
	}
	_, missingSubscriptionErr := repo.GetSubscriptionByID(ctx, "missing-subscription")
	if !errors.Is(missingSubscriptionErr, repository.ErrNotificationSubscriptionNotFound) {
		t.Fatalf("expected missing subscription error, got %v", missingSubscriptionErr)
	}

	_, updateTargetMissingErr := repo.UpdateTarget(ctx, domain.NotificationTarget{ID: "missing-target", Recipient: "missing@example.com"})
	if !errors.Is(updateTargetMissingErr, repository.ErrNotificationTargetNotFound) {
		t.Fatalf("expected update target missing error, got %v", updateTargetMissingErr)
	}

	duplicateTarget := targetOne
	duplicateTarget.Recipient = targetTwo.Recipient
	duplicateTarget.UpdatedAt = createdAt.Add(4 * time.Minute)
	_, updateTargetDuplicateErr := repo.UpdateTarget(ctx, duplicateTarget)
	if !errors.Is(updateTargetDuplicateErr, repository.ErrNotificationTargetDuplicate) {
		t.Fatalf("expected update target duplicate error, got %v", updateTargetDuplicateErr)
	}

	allSubs, err := repo.ListSubscriptions(ctx, repository.NotificationSubscriptionListFilter{})
	if err != nil {
		t.Fatalf("list all subscriptions: %v", err)
	}
	if len(allSubs) != 1 {
		t.Fatalf("expected one remaining subscription after delete, got %+v", allSubs)
	}

	duplicateProjectID := "project-dup"
	duplicateSubA, err := repo.CreateSubscription(ctx, domain.NotificationSubscription{
		ID:        "subscription-dup-a",
		TargetID:  targetOne.ID,
		ProjectID: &duplicateProjectID,
		EventType: domain.NotificationEventTypeBuildFailed,
		Enabled:   true,
		CreatedAt: createdAt.Add(4 * time.Minute),
		UpdatedAt: createdAt.Add(4 * time.Minute),
	})
	if err != nil {
		t.Fatalf("create duplicate test base subscription: %v", err)
	}
	duplicateSubB, err := repo.CreateSubscription(ctx, domain.NotificationSubscription{
		ID:        "subscription-dup-b",
		TargetID:  targetTwo.ID,
		ProjectID: &duplicateProjectID,
		EventType: domain.NotificationEventTypeBuildFailed,
		Enabled:   true,
		CreatedAt: createdAt.Add(5 * time.Minute),
		UpdatedAt: createdAt.Add(5 * time.Minute),
	})
	if err != nil {
		t.Fatalf("create duplicate test competing subscription: %v", err)
	}

	_, updateSubscriptionMissingErr := repo.UpdateSubscription(ctx, domain.NotificationSubscription{ID: "missing-subscription", TargetID: targetOne.ID, ProjectID: &duplicateProjectID, EventType: domain.NotificationEventTypeBuildFailed})
	if !errors.Is(updateSubscriptionMissingErr, repository.ErrNotificationSubscriptionNotFound) {
		t.Fatalf("expected update subscription missing error, got %v", updateSubscriptionMissingErr)
	}

	_, updateSubscriptionDuplicateErr := repo.UpdateSubscription(ctx, domain.NotificationSubscription{
		ID:        duplicateSubB.ID,
		TargetID:  duplicateSubA.TargetID,
		ProjectID: duplicateSubA.ProjectID,
		EventType: duplicateSubA.EventType,
		Enabled:   true,
		CreatedAt: duplicateSubB.CreatedAt,
		UpdatedAt: createdAt.Add(6 * time.Minute),
	})
	if !errors.Is(updateSubscriptionDuplicateErr, repository.ErrNotificationSubscriptionDuplicate) {
		t.Fatalf("expected update subscription duplicate error, got %v", updateSubscriptionDuplicateErr)
	}

	if err := repo.DeleteSubscription(ctx, "missing-subscription"); !errors.Is(err, repository.ErrNotificationSubscriptionNotFound) {
		t.Fatalf("expected delete missing subscription error, got %v", err)
	}
}

func TestNotificationSubscriptionRepository_EnsureOwnedEmailTargetInitialized(t *testing.T) {
	ctx := context.Background()
	repo := NewNotificationSubscriptionRepository()
	preferences := NewUserNotificationPreferenceRepository()
	settings := NewNotificationInstanceSettingsRepository()
	repo.SetNotificationPreferenceRepository(preferences)
	repo.SetNotificationInstanceSettingsRepository(settings)

	now := time.Date(2026, 6, 28, 19, 0, 0, 0, time.UTC)
	_, err := settings.Upsert(ctx, domain.NotificationInstanceSettings{
		DefaultCommitAuthorFailureEmailEnabled: false,
		CreatedAt:                              now,
		UpdatedAt:                              now,
	})
	if err != nil {
		t.Fatalf("seed settings failed: %v", err)
	}

	created, err := repo.EnsureOwnedEmailTargetInitialized(ctx, repository.EnsureOwnedNotificationEmailTargetInput{
		ID:          "target-init",
		OwnerUserID: "user-init",
		Name:        "User Init",
		Recipient:   "<user-init@example.com>",
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		t.Fatalf("ensure initialized target failed: %v", err)
	}
	if created.ID != "target-init" {
		t.Fatalf("expected created target id target-init, got %q", created.ID)
	}

	preference, err := preferences.GetByUserID(ctx, "user-init")
	if err != nil {
		t.Fatalf("expected initialized preference, got %v", err)
	}
	if preference.CommitAuthorFailureEmailEnabled {
		t.Fatalf("expected disabled initialized preference, got %+v", preference)
	}
	if preference.CommitAuthorFailureEmailSource != domain.UserNotificationPreferenceSourceInstanceDefault {
		t.Fatalf("expected instance-default preference source, got %+v", preference)
	}
	if preference.CommitAuthorSuccessEmailEnabled {
		t.Fatalf("expected success preference default to stay disabled, got %+v", preference)
	}
	if preference.CommitAuthorSuccessEmailSource == nil || *preference.CommitAuthorSuccessEmailSource != domain.UserNotificationPreferenceSourceInstanceDefault {
		t.Fatalf("expected success source to be initialized from instance default, got %+v", preference)
	}

	_, err = preferences.Upsert(ctx, domain.UserNotificationPreference{
		UserID:                          "user-init",
		CommitAuthorFailureEmailEnabled: true,
		CommitAuthorFailureEmailSource:  domain.UserNotificationPreferenceSourceUser,
		CreatedAt:                       now,
		UpdatedAt:                       now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("seed explicit preference failed: %v", err)
	}

	again, err := repo.EnsureOwnedEmailTargetInitialized(ctx, repository.EnsureOwnedNotificationEmailTargetInput{
		ID:          "ignored",
		OwnerUserID: "user-init",
		Name:        "Ignored",
		Recipient:   "<different@example.com>",
		CreatedAt:   now.Add(time.Minute),
		UpdatedAt:   now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("repeat ensure initialized failed: %v", err)
	}
	if again.ID != created.ID {
		t.Fatalf("expected repeat ensure to return %q, got %q", created.ID, again.ID)
	}

	explicitPreference, err := preferences.GetByUserID(ctx, "user-init")
	if err != nil {
		t.Fatalf("get explicit preference failed: %v", err)
	}
	if !explicitPreference.CommitAuthorFailureEmailEnabled || explicitPreference.CommitAuthorFailureEmailSource != domain.UserNotificationPreferenceSourceUser {
		t.Fatalf("expected explicit preference to be preserved, got %+v", explicitPreference)
	}

	legacyPreference, err := preferences.Upsert(ctx, domain.UserNotificationPreference{
		UserID:                          "legacy-claimed-user",
		CommitAuthorFailureEmailEnabled: true,
		CommitAuthorFailureEmailSource:  domain.UserNotificationPreferenceSourceUser,
		CreatedAt:                       now,
		UpdatedAt:                       now,
	})
	if err != nil {
		t.Fatalf("seed legacy preference without success source failed: %v", err)
	}
	if legacyPreference.CommitAuthorSuccessEmailSource != nil {
		t.Fatalf("expected seeded legacy preference to lack success source, got %+v", legacyPreference)
	}
	claimableLegacy, err := repo.CreateTarget(ctx, domain.NotificationTarget{
		ID:        "legacy-claimable-target",
		Type:      domain.NotificationTargetTypeEmail,
		Name:      "Legacy Claimable",
		Recipient: "legacy-claimed@example.com",
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("create legacy claimable target failed: %v", err)
	}
	claimedLegacy, err := repo.EnsureOwnedEmailTargetInitialized(ctx, repository.EnsureOwnedNotificationEmailTargetInput{
		ID:          "ignored-legacy-claim-id",
		OwnerUserID: "legacy-claimed-user",
		Name:        "Legacy Claimed User",
		Recipient:   claimableLegacy.Recipient,
		CreatedAt:   now,
		UpdatedAt:   now.Add(2 * time.Minute),
	})
	if err != nil {
		t.Fatalf("claim legacy target with backfill failed: %v", err)
	}
	if claimedLegacy.ID != claimableLegacy.ID {
		t.Fatalf("expected claimed legacy target id %q, got %q", claimableLegacy.ID, claimedLegacy.ID)
	}
	backfilledPreference, err := preferences.GetByUserID(ctx, "legacy-claimed-user")
	if err != nil {
		t.Fatalf("get backfilled legacy preference failed: %v", err)
	}
	if backfilledPreference.CommitAuthorSuccessEmailEnabled {
		t.Fatalf("expected backfilled success preference to remain disabled, got %+v", backfilledPreference)
	}
	if backfilledPreference.CommitAuthorSuccessEmailSource == nil || *backfilledPreference.CommitAuthorSuccessEmailSource != domain.UserNotificationPreferenceSourceInstanceDefault {
		t.Fatalf("expected legacy preference success source backfill, got %+v", backfilledPreference)
	}

	legacyOwner := "legacy-user"
	_, err = repo.CreateTarget(ctx, domain.NotificationTarget{
		ID:          "legacy-target",
		OwnerUserID: &legacyOwner,
		Type:        domain.NotificationTargetTypeEmail,
		Name:        "Legacy User",
		Recipient:   "legacy@example.com",
		Enabled:     true,
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		t.Fatalf("seed legacy target failed: %v", err)
	}

	legacy, err := repo.EnsureOwnedEmailTargetInitialized(ctx, repository.EnsureOwnedNotificationEmailTargetInput{
		ID:          "legacy-ignore",
		OwnerUserID: legacyOwner,
		Name:        "Legacy User",
		Recipient:   "legacy@example.com",
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		t.Fatalf("ensure legacy target failed: %v", err)
	}
	if legacy.ID != "legacy-target" {
		t.Fatalf("expected legacy target id legacy-target, got %q", legacy.ID)
	}
	_, err = preferences.GetByUserID(ctx, legacyOwner)
	if !errors.Is(err, repository.ErrUserNotificationPreferenceNotFound) {
		t.Fatalf("expected legacy preference to remain absent, got %v", err)
	}
}

func TestNotificationSubscriptionRepository_EnsureOwnedEmailTargetInitialized_NoConfiguredMemoryRepositories(t *testing.T) {
	ctx := context.Background()
	repo := NewNotificationSubscriptionRepository()
	repo.SetNotificationPreferenceRepository(&nonMemoryPreferenceRepo{})
	repo.SetNotificationInstanceSettingsRepository(&nonMemorySettingsRepo{})
	now := time.Date(2026, 6, 28, 20, 15, 0, 0, time.UTC)

	ensured, err := repo.EnsureOwnedEmailTargetInitialized(ctx, repository.EnsureOwnedNotificationEmailTargetInput{
		ID:          "target-nil-config",
		OwnerUserID: "user-no-pref",
		Name:        "No Pref",
		Recipient:   "<no-pref@example.com>",
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		t.Fatalf("ensure target without memory config failed: %v", err)
	}
	if ensured.ID != "target-nil-config" {
		t.Fatalf("expected ensured target id target-nil-config, got %q", ensured.ID)
	}
}
