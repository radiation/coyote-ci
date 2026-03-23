package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func TestNewBuildStore_Behavior(t *testing.T) {
	store := NewBuildStore()
	if store == nil {
		t.Fatal("expected store, got nil")
	}

	created, err := store.Create(context.Background(), domain.Build{
		ID:        "build-1",
		ProjectID: "project-1",
		Status:    domain.BuildStatusPending,
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("expected create to succeed, got %v", err)
	}

	fetched, err := store.GetByID(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("expected get by id to succeed, got %v", err)
	}
	if fetched.ID != created.ID {
		t.Fatalf("expected id %q, got %q", created.ID, fetched.ID)
	}

	updated, err := store.UpdateStatus(context.Background(), created.ID, domain.BuildStatusRunning, nil)
	if err != nil {
		t.Fatalf("expected update status to succeed, got %v", err)
	}
	if updated.Status != domain.BuildStatusRunning {
		t.Fatalf("expected status %q, got %q", domain.BuildStatusRunning, updated.Status)
	}

	builds, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("expected list to succeed, got %v", err)
	}
	if len(builds) != 1 {
		t.Fatalf("expected exactly one build, got %d", len(builds))
	}
}

func TestBuildStore_MissingBuildCases(t *testing.T) {
	store := NewBuildStore()

	_, err := store.GetByID(context.Background(), "")
	if !errors.Is(err, repository.ErrBuildNotFound) {
		t.Fatalf("expected not found for empty id, got %v", err)
	}

	_, err = store.GetByID(context.Background(), "missing")
	if !errors.Is(err, repository.ErrBuildNotFound) {
		t.Fatalf("expected not found for missing id, got %v", err)
	}

	_, err = store.UpdateStatus(context.Background(), "missing", domain.BuildStatusFailed, nil)
	if !errors.Is(err, repository.ErrBuildNotFound) {
		t.Fatalf("expected not found when updating missing build, got %v", err)
	}
}
