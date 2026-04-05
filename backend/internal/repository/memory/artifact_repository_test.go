package memory

import (
	"context"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func TestArtifactRepository_CreateAndList(t *testing.T) {
	repo := NewArtifactRepository()
	ctx := context.Background()
	now := time.Now().UTC()
	stepID := "step-1"

	a := domain.BuildArtifact{
		ID:              "art-1",
		BuildID:         "build-1",
		StepID:          &stepID,
		LogicalPath:     "dist/app",
		StorageKey:      "builds/build-1/steps/step-1/art-1-app",
		StorageProvider: domain.StorageProviderFilesystem,
		SizeBytes:       42,
		CreatedAt:       now,
	}
	created, err := repo.Create(ctx, a)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if created.ID != "art-1" {
		t.Fatalf("expected id art-1, got %q", created.ID)
	}

	artifacts, err := repo.ListByBuildID(ctx, "build-1")
	if err != nil {
		t.Fatalf("list by build failed: %v", err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(artifacts))
	}

	artifacts, err = repo.ListByStepID(ctx, "step-1")
	if err != nil {
		t.Fatalf("list by step failed: %v", err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("expected 1 artifact by step, got %d", len(artifacts))
	}
	if artifacts[0].StorageProvider != domain.StorageProviderFilesystem {
		t.Fatalf("expected filesystem provider, got %q", artifacts[0].StorageProvider)
	}
}

func TestArtifactRepository_GetByID_NotFound(t *testing.T) {
	repo := NewArtifactRepository()
	_, err := repo.GetByID(context.Background(), "build-1", "missing")
	if err != repository.ErrArtifactNotFound {
		t.Fatalf("expected ErrArtifactNotFound, got %v", err)
	}
}

func TestArtifactRepository_ListByStepID_Empty(t *testing.T) {
	repo := NewArtifactRepository()
	artifacts, err := repo.ListByStepID(context.Background(), "nonexistent-step")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if len(artifacts) != 0 {
		t.Fatalf("expected 0 artifacts, got %d", len(artifacts))
	}
}
