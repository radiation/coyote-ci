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

func TestSlackWorkspaceIntegrationRepository_CRUD(t *testing.T) {
	repo := NewSlackWorkspaceIntegrationRepository()
	ctx := context.Background()

	_, err := repo.Get(ctx)
	if !errors.Is(err, repository.ErrSlackWorkspaceIntegrationNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}

	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	stored, err := repo.ConnectOrReplace(ctx, domain.SlackWorkspaceIntegration{
		ID:             "int-1",
		WorkspaceID:    "T123",
		BotTokenSecret: "xoxb-secret",
		Enabled:        true,
		ConnectedAt:    now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, false)
	if err != nil {
		t.Fatalf("connect integration: %v", err)
	}
	if stored.WorkspaceID != "T123" {
		t.Fatalf("unexpected workspace id %q", stored.WorkspaceID)
	}

	updatedAt := now.Add(time.Minute)
	updated, err := repo.SetEnabled(ctx, false, updatedAt)
	if err != nil {
		t.Fatalf("set enabled: %v", err)
	}
	if updated.Enabled {
		t.Fatalf("expected disabled integration")
	}

	testedAt := now.Add(2 * time.Minute)
	tested, err := repo.UpdateLastTestResult(ctx, testedAt, false)
	if err != nil {
		t.Fatalf("update test result: %v", err)
	}
	if tested.LastTestSucceeded == nil || *tested.LastTestSucceeded {
		t.Fatalf("expected failed test state, got %+v", tested.LastTestSucceeded)
	}

	if err := repo.Delete(ctx); err != nil {
		t.Fatalf("delete integration: %v", err)
	}
	if err := repo.Delete(ctx); !errors.Is(err, repository.ErrSlackWorkspaceIntegrationNotFound) {
		t.Fatalf("expected not found delete error, got %v", err)
	}
}

func TestSlackWorkspaceIntegrationRepository_RejectsDifferentWorkspaceWithoutReplace(t *testing.T) {
	repo := NewSlackWorkspaceIntegrationRepository()
	ctx := context.Background()
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)

	_, err := repo.ConnectOrReplace(ctx, domain.SlackWorkspaceIntegration{
		ID:             "int-1",
		WorkspaceID:    "T123",
		BotTokenSecret: "xoxb-one",
		Enabled:        true,
		ConnectedAt:    now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, false)
	if err != nil {
		t.Fatalf("seed integration: %v", err)
	}

	_, err = repo.ConnectOrReplace(ctx, domain.SlackWorkspaceIntegration{
		ID:             "int-2",
		WorkspaceID:    "T999",
		BotTokenSecret: "xoxb-two",
		Enabled:        true,
		ConnectedAt:    now.Add(time.Minute),
		CreatedAt:      now.Add(time.Minute),
		UpdatedAt:      now.Add(time.Minute),
	}, false)
	if !errors.Is(err, repository.ErrSlackWorkspaceIntegrationReplaceRequired) {
		t.Fatalf("expected replace required error, got %v", err)
	}

	stored, getErr := repo.Get(ctx)
	if getErr != nil {
		t.Fatalf("get integration: %v", getErr)
	}
	if stored.WorkspaceID != "T123" || stored.BotTokenSecret != "xoxb-one" {
		t.Fatalf("expected original integration to remain, got %+v", stored)
	}
}

func TestSlackWorkspaceIntegrationRepository_ConcurrentDifferentWorkspaceFirstConnect(t *testing.T) {
	repo := NewSlackWorkspaceIntegrationRepository()
	ctx := context.Background()
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)

	candidates := []domain.SlackWorkspaceIntegration{
		{ID: "int-1", WorkspaceID: "T123", BotTokenSecret: "xoxb-one", Enabled: true, ConnectedAt: now, CreatedAt: now, UpdatedAt: now},
		{ID: "int-2", WorkspaceID: "T999", BotTokenSecret: "xoxb-two", Enabled: true, ConnectedAt: now.Add(time.Second), CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second)},
	}

	results := make(chan error, len(candidates))
	var wg sync.WaitGroup
	for _, candidate := range candidates {
		candidate := candidate
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := repo.ConnectOrReplace(ctx, candidate, false)
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
		case errors.Is(err, repository.ErrSlackWorkspaceIntegrationReplaceRequired):
			conflicts++
		default:
			t.Fatalf("unexpected concurrent connect error: %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("expected 1 success and 1 conflict, got successes=%d conflicts=%d", successes, conflicts)
	}
}

func TestSlackWorkspaceIntegrationRepository_ConcurrentSameWorkspaceRotation(t *testing.T) {
	repo := NewSlackWorkspaceIntegrationRepository()
	ctx := context.Background()
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)

	_, err := repo.ConnectOrReplace(ctx, domain.SlackWorkspaceIntegration{
		ID:             "int-1",
		WorkspaceID:    "T123",
		BotTokenSecret: "xoxb-original",
		Enabled:        true,
		ConnectedAt:    now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, false)
	if err != nil {
		t.Fatalf("seed integration: %v", err)
	}

	tokens := []string{"xoxb-rotate-a", "xoxb-rotate-b"}
	var wg sync.WaitGroup
	results := make(chan error, len(tokens))
	for index, token := range tokens {
		candidate := domain.SlackWorkspaceIntegration{
			ID:             "ignored",
			WorkspaceID:    "T123",
			BotTokenSecret: token,
			Enabled:        true,
			ConnectedAt:    now.Add(time.Duration(index+1) * time.Minute),
			CreatedAt:      now.Add(time.Duration(index+1) * time.Minute),
			UpdatedAt:      now.Add(time.Duration(index+1) * time.Minute),
		}
		wg.Add(1)
		go func(candidate domain.SlackWorkspaceIntegration) {
			defer wg.Done()
			_, rotateErr := repo.ConnectOrReplace(ctx, candidate, false)
			results <- rotateErr
		}(candidate)
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatalf("unexpected rotation error: %v", err)
		}
	}

	stored, err := repo.Get(ctx)
	if err != nil {
		t.Fatalf("get integration: %v", err)
	}
	if stored.WorkspaceID != "T123" {
		t.Fatalf("expected workspace T123, got %q", stored.WorkspaceID)
	}
	if stored.BotTokenSecret != "xoxb-rotate-a" && stored.BotTokenSecret != "xoxb-rotate-b" {
		t.Fatalf("expected one of rotated tokens, got %q", stored.BotTokenSecret)
	}
}
