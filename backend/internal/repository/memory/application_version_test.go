package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func TestBuildRepository_SetApplicationVersionIfUnset(t *testing.T) {
	repo := NewBuildRepository()
	created, createErr := repo.Create(context.Background(), domain.Build{
		ID:        "build-1",
		ProjectID: "project-1",
		Status:    domain.BuildStatusPending,
		CreatedAt: time.Now().UTC(),
	})
	if createErr != nil {
		t.Fatalf("create build: %v", createErr)
	}
	set, setErr := repo.SetApplicationVersionIfUnset(context.Background(), created.ID, "1.2.3")
	if setErr != nil {
		t.Fatalf("set application version: %v", setErr)
	}
	if set.ApplicationVersion == nil || *set.ApplicationVersion != "1.2.3" {
		t.Fatalf("application version=%v, want 1.2.3", set.ApplicationVersion)
	}
	if _, idempotentErr := repo.SetApplicationVersionIfUnset(context.Background(), created.ID, "1.2.3"); idempotentErr != nil {
		t.Fatalf("set same application version: %v", idempotentErr)
	}
	if _, conflictErr := repo.SetApplicationVersionIfUnset(context.Background(), created.ID, "1.2.4"); !errors.Is(conflictErr, repository.ErrApplicationVersionConflict) {
		t.Fatalf("expected conflict, got %v", conflictErr)
	}
}

func TestBuildRepository_ApplicationVersionReturnsAreIsolated(t *testing.T) {
	repo := NewBuildRepository()
	created, createErr := repo.Create(context.Background(), domain.Build{
		ID:        "build-1",
		ProjectID: "project-1",
		Status:    domain.BuildStatusPending,
		CreatedAt: time.Now().UTC(),
	})
	if createErr != nil {
		t.Fatalf("create build: %v", createErr)
	}

	set, setErr := repo.SetApplicationVersionIfUnset(context.Background(), created.ID, "1.2.3")
	if setErr != nil {
		t.Fatalf("set application version: %v", setErr)
	}
	*set.ApplicationVersion = "9.9.9"

	stored, getErr := repo.GetByID(context.Background(), created.ID)
	if getErr != nil {
		t.Fatalf("get build: %v", getErr)
	}
	if stored.ApplicationVersion == nil || *stored.ApplicationVersion != "1.2.3" {
		t.Fatalf("stored application version=%v, want 1.2.3", stored.ApplicationVersion)
	}

	*stored.ApplicationVersion = "8.8.8"
	builds, listErr := repo.List(context.Background())
	if listErr != nil {
		t.Fatalf("list builds: %v", listErr)
	}
	if len(builds) != 1 {
		t.Fatalf("build count=%d, want 1", len(builds))
	}
	if builds[0].ApplicationVersion == nil || *builds[0].ApplicationVersion != "1.2.3" {
		t.Fatalf("listed application version=%v, want 1.2.3", builds[0].ApplicationVersion)
	}
}
