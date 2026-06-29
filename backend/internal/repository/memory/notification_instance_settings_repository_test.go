package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func TestNotificationInstanceSettingsRepository_GetAndUpsert(t *testing.T) {
	ctx := context.Background()
	repo := NewNotificationInstanceSettingsRepository()

	_, err := repo.Get(ctx)
	if !errors.Is(err, repository.ErrNotificationInstanceSettingsNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}

	now := time.Date(2026, 6, 28, 18, 0, 0, 0, time.UTC)
	stored, err := repo.Upsert(ctx, domain.NotificationInstanceSettings{
		DefaultCommitAuthorFailureEmailEnabled: true,
		CreatedAt:                              now,
		UpdatedAt:                              now,
	})
	if err != nil {
		t.Fatalf("upsert settings failed: %v", err)
	}
	if !stored.DefaultCommitAuthorFailureEmailEnabled {
		t.Fatalf("expected enabled settings, got %+v", stored)
	}

	fetched, err := repo.Get(ctx)
	if err != nil {
		t.Fatalf("get settings failed: %v", err)
	}
	if !fetched.DefaultCommitAuthorFailureEmailEnabled || !fetched.CreatedAt.Equal(now) {
		t.Fatalf("unexpected fetched settings %+v", fetched)
	}

	updatedAt := now.Add(time.Minute)
	updated, err := repo.Upsert(ctx, domain.NotificationInstanceSettings{
		DefaultCommitAuthorFailureEmailEnabled: false,
		CreatedAt:                              now,
		UpdatedAt:                              updatedAt,
	})
	if err != nil {
		t.Fatalf("update settings failed: %v", err)
	}
	if updated.DefaultCommitAuthorFailureEmailEnabled {
		t.Fatalf("expected disabled settings, got %+v", updated)
	}

	fetchedUpdated, err := repo.Get(ctx)
	if err != nil {
		t.Fatalf("get updated settings failed: %v", err)
	}
	if fetchedUpdated.DefaultCommitAuthorFailureEmailEnabled {
		t.Fatalf("expected disabled fetched settings, got %+v", fetchedUpdated)
	}
	if !fetchedUpdated.CreatedAt.Equal(now) || !fetchedUpdated.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("unexpected timestamps %+v", fetchedUpdated)
	}
}
