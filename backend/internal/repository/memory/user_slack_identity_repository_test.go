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
