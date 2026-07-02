package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func TestUserNotificationPreferenceRepository_GetByUserIDAndUpsert(t *testing.T) {
	repo := NewUserNotificationPreferenceRepository()
	ctx := context.Background()
	now := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)
	source := domain.UserNotificationPreferenceSourceUser

	_, err := repo.GetByUserID(ctx, "missing")
	if !errors.Is(err, repository.ErrUserNotificationPreferenceNotFound) {
		t.Fatalf("expected missing preference error, got %v", err)
	}

	stored, err := repo.Upsert(ctx, domain.UserNotificationPreference{
		UserID:                          "  user-1  ",
		CommitAuthorFailureEmailEnabled: true,
		CommitAuthorFailureEmailSource:  domain.UserNotificationPreferenceSourceUser,
		CommitAuthorSuccessEmailEnabled: true,
		CommitAuthorSuccessEmailSource:  &source,
		CreatedAt:                       now,
		UpdatedAt:                       now,
	})
	if err != nil {
		t.Fatalf("upsert preference failed: %v", err)
	}
	if stored.UserID != "user-1" {
		t.Fatalf("expected trimmed user id, got %q", stored.UserID)
	}
	source = domain.UserNotificationPreferenceSourceInstanceDefault
	if stored.CommitAuthorSuccessEmailSource == nil || *stored.CommitAuthorSuccessEmailSource != domain.UserNotificationPreferenceSourceUser {
		t.Fatalf("expected success source clone to preserve original value, got %+v", stored.CommitAuthorSuccessEmailSource)
	}

	fetched, err := repo.GetByUserID(ctx, " user-1 ")
	if err != nil {
		t.Fatalf("get preference failed: %v", err)
	}
	if !fetched.CommitAuthorFailureEmailEnabled {
		t.Fatalf("expected enabled preference, got %+v", fetched)
	}
	if !fetched.CommitAuthorSuccessEmailEnabled || fetched.CommitAuthorSuccessEmailSource == nil || *fetched.CommitAuthorSuccessEmailSource != domain.UserNotificationPreferenceSourceUser {
		t.Fatalf("expected success preference fields to persist, got %+v", fetched)
	}

	updated, err := repo.Upsert(ctx, domain.UserNotificationPreference{
		UserID:                         "user-1",
		CommitAuthorFailureEmailSource: domain.UserNotificationPreferenceSourceUser,
		CreatedAt:                      now,
		UpdatedAt:                      now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("update preference failed: %v", err)
	}
	if updated.CommitAuthorFailureEmailEnabled {
		t.Fatalf("expected updated disabled preference, got %+v", updated)
	}

	fetchedUpdated, err := repo.GetByUserID(ctx, "user-1")
	if err != nil {
		t.Fatalf("get updated preference failed: %v", err)
	}
	if fetchedUpdated.CommitAuthorFailureEmailEnabled {
		t.Fatalf("expected fetched updated preference to be disabled, got %+v", fetchedUpdated)
	}
}

func TestUserNotificationPreferenceRepository_InitializeIfAbsent(t *testing.T) {
	repo := NewUserNotificationPreferenceRepository()
	ctx := context.Background()
	now := time.Date(2026, 6, 28, 20, 10, 0, 0, time.UTC)
	source := domain.UserNotificationPreferenceSourceInstanceDefault

	initialized, created, err := repo.InitializeIfAbsent(ctx, domain.UserNotificationPreference{
		UserID:                          "  user-init  ",
		CommitAuthorFailureEmailEnabled: true,
		CommitAuthorFailureEmailSource:  domain.UserNotificationPreferenceSourceInstanceDefault,
		CommitAuthorSuccessEmailSource:  &source,
		CreatedAt:                       now,
		UpdatedAt:                       now,
	})
	if err != nil {
		t.Fatalf("initialize preference failed: %v", err)
	}
	if !created || initialized.UserID != "user-init" {
		t.Fatalf("unexpected initialized preference %+v created=%t", initialized, created)
	}
	source = domain.UserNotificationPreferenceSourceUser
	if initialized.CommitAuthorSuccessEmailSource == nil || *initialized.CommitAuthorSuccessEmailSource != domain.UserNotificationPreferenceSourceInstanceDefault {
		t.Fatalf("expected initialized success source clone, got %+v", initialized.CommitAuthorSuccessEmailSource)
	}

	existing, existingCreated, err := repo.InitializeIfAbsent(ctx, domain.UserNotificationPreference{
		UserID:                         "user-init",
		CommitAuthorFailureEmailSource: domain.UserNotificationPreferenceSourceUser,
		CreatedAt:                      now,
		UpdatedAt:                      now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("initialize existing preference failed: %v", err)
	}
	if existingCreated || !existing.CommitAuthorFailureEmailEnabled || existing.CommitAuthorFailureEmailSource != domain.UserNotificationPreferenceSourceInstanceDefault {
		t.Fatalf("expected existing initialized preference to be preserved, got %+v created=%t", existing, existingCreated)
	}
}
