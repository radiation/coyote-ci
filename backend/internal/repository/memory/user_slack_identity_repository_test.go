package memory

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func TestUserSlackIdentityRepository_UpsertIsIdempotentForSameUserAndSlackMember(t *testing.T) {
	repo := NewUserSlackIdentityRepository()
	now := time.Date(2026, 7, 1, 16, 0, 0, 0, time.UTC)

	first, err := repo.Upsert(context.Background(), domain.UserSlackIdentity{
		ID:                          "identity-1",
		UserID:                      "user-1",
		SlackWorkspaceIntegrationID: "workspace-1",
		SlackUserID:                 "U123",
		Enabled:                     true,
		LinkedAt:                    now,
		CreatedAt:                   now,
		UpdatedAt:                   now,
	})
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	second, err := repo.Upsert(context.Background(), domain.UserSlackIdentity{
		ID:                          "identity-2",
		UserID:                      "user-1",
		SlackWorkspaceIntegrationID: "workspace-1",
		SlackUserID:                 "U123",
		Enabled:                     true,
		LinkedAt:                    now.Add(time.Hour),
		CreatedAt:                   now.Add(time.Hour),
		UpdatedAt:                   now.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("expected idempotent upsert to keep identity id %q, got %q", first.ID, second.ID)
	}
	if !second.LinkedAt.Equal(first.LinkedAt) {
		t.Fatalf("expected linked_at to be preserved, got first=%s second=%s", first.LinkedAt, second.LinkedAt)
	}
}

func TestUserSlackIdentityRepository_ConcurrentClaimSameSlackUserReturnsOneConflict(t *testing.T) {
	repo := NewUserSlackIdentityRepository()
	now := time.Date(2026, 7, 1, 16, 5, 0, 0, time.UTC)

	inputs := []domain.UserSlackIdentity{
		{ID: "identity-1", UserID: "user-1", SlackWorkspaceIntegrationID: "workspace-1", SlackUserID: "U123", Enabled: true, LinkedAt: now, CreatedAt: now, UpdatedAt: now},
		{ID: "identity-2", UserID: "user-2", SlackWorkspaceIntegrationID: "workspace-1", SlackUserID: "U123", Enabled: true, LinkedAt: now, CreatedAt: now, UpdatedAt: now},
	}

	results := make(chan error, len(inputs))
	var wg sync.WaitGroup
	for _, input := range inputs {
		input := input
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := repo.Upsert(context.Background(), input)
			results <- err
		}()
	}
	wg.Wait()
	close(results)

	successes := 0
	conflicts := 0
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, repository.ErrUserSlackIdentityConflict):
			conflicts++
		default:
			t.Fatalf("unexpected concurrent upsert error: %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("expected 1 success and 1 conflict, got successes=%d conflicts=%d", successes, conflicts)
	}
}

func TestUserSlackIdentityRepository_CountByWorkspaceIntegrationID_PropagatesContextCancellation(t *testing.T) {
	repo := NewUserSlackIdentityRepository()
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := repo.CountByWorkspaceIntegrationID(canceledCtx, "workspace-1")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
}

func TestUserSlackIdentityRepository_CRUDAndConflictBehavior(t *testing.T) {
	repo := NewUserSlackIdentityRepository()
	now := time.Date(2026, 7, 1, 16, 15, 0, 0, time.UTC)
	displayName := "  Bryan  "
	realName := " Bryan Choate "
	handle := " bryan "
	email := " bryan@example.com "
	avatar := " https://images.example/avatar.png "
	verifiedAt := now.Add(time.Minute)

	stored, err := repo.Upsert(context.Background(), domain.UserSlackIdentity{
		ID:                          "identity-1",
		UserID:                      " user-1 ",
		SlackWorkspaceIntegrationID: " workspace-1 ",
		SlackUserID:                 " U123 ",
		SlackDisplayName:            &displayName,
		SlackRealName:               &realName,
		SlackHandle:                 &handle,
		SlackEmail:                  &email,
		ProfileImageURL:             &avatar,
		Enabled:                     true,
		LinkedAt:                    now,
		LastVerifiedAt:              &verifiedAt,
		CreatedAt:                   now,
		UpdatedAt:                   now,
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	if stored.UserID != "user-1" || stored.SlackWorkspaceIntegrationID != "workspace-1" || stored.SlackUserID != "U123" {
		t.Fatalf("expected trimmed ids, got %+v", stored)
	}
	if stored.SlackDisplayName == nil || *stored.SlackDisplayName != "Bryan" {
		t.Fatalf("expected trimmed display name, got %+v", stored.SlackDisplayName)
	}

	got, err := repo.GetByUserID(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("get by user id: %v", err)
	}
	if got.SlackEmail == nil || *got.SlackEmail != "bryan@example.com" {
		t.Fatalf("expected cloned trimmed email, got %+v", got.SlackEmail)
	}

	updated, err := repo.SetEnabled(context.Background(), "user-1", false, now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("set enabled: %v", err)
	}
	if updated.Enabled {
		t.Fatalf("expected identity to be disabled")
	}

	count, err := repo.CountByWorkspaceIntegrationID(context.Background(), "workspace-1")
	if err != nil {
		t.Fatalf("count identities: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected count 1, got %d", count)
	}

	_, err = repo.Upsert(context.Background(), domain.UserSlackIdentity{
		ID:                          "identity-2",
		UserID:                      "user-2",
		SlackWorkspaceIntegrationID: "workspace-1",
		SlackUserID:                 "U123",
		Enabled:                     true,
		LinkedAt:                    now,
		CreatedAt:                   now,
		UpdatedAt:                   now,
	})
	if !errors.Is(err, repository.ErrUserSlackIdentityConflict) {
		t.Fatalf("expected conflict for reused slack member, got %v", err)
	}

	if err := repo.DeleteByUserID(context.Background(), "user-1"); err != nil {
		t.Fatalf("delete identity: %v", err)
	}
	if err := repo.DeleteByUserID(context.Background(), "user-1"); !errors.Is(err, repository.ErrUserSlackIdentityNotFound) {
		t.Fatalf("expected delete not found after removal, got %v", err)
	}
	if _, err := repo.GetByUserID(context.Background(), "user-1"); !errors.Is(err, repository.ErrUserSlackIdentityNotFound) {
		t.Fatalf("expected get not found after delete, got %v", err)
	}
	if _, err := repo.SetEnabled(context.Background(), "user-1", true, now); !errors.Is(err, repository.ErrUserSlackIdentityNotFound) {
		t.Fatalf("expected set enabled not found after delete, got %v", err)
	}
}
