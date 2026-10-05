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
