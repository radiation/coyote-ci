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

func TestArtifactRepository_BrowsePaginatesLogicalArtifacts(t *testing.T) {
	repo := NewArtifactRepository()
	now := time.Now().UTC().Truncate(time.Second)
	jobID := "job-1"
	repo.SeedBuilds(
		domain.Build{ID: "build-1", JobID: &jobID, ProjectID: "project-1", BuildNumber: 11, CreatedAt: now},
		domain.Build{ID: "build-2", JobID: &jobID, ProjectID: "project-1", BuildNumber: 12, CreatedAt: now.Add(time.Minute)},
		domain.Build{ID: "build-3", JobID: &jobID, ProjectID: "project-1", BuildNumber: 13, CreatedAt: now.Add(2 * time.Minute)},
	)

	_, err := repo.Create(context.Background(), domain.BuildArtifact{ID: "artifact-1", BuildID: "build-1", LogicalPath: "packages/pkg-a.tgz", ArtifactType: domain.ArtifactTypeNPMPackage, CreatedAt: now})
	if err != nil {
		t.Fatalf("create artifact-1 failed: %v", err)
	}
	_, err = repo.Create(context.Background(), domain.BuildArtifact{ID: "artifact-2", BuildID: "build-2", LogicalPath: "packages/pkg-a.tgz", ArtifactType: domain.ArtifactTypeNPMPackage, CreatedAt: now.Add(time.Minute)})
	if err != nil {
		t.Fatalf("create artifact-2 failed: %v", err)
	}
	_, err = repo.Create(context.Background(), domain.BuildArtifact{ID: "artifact-3", BuildID: "build-3", LogicalPath: "reports/junit.xml", ArtifactType: domain.ArtifactTypeGeneric, CreatedAt: now.Add(2 * time.Minute)})
	if err != nil {
		t.Fatalf("create artifact-3 failed: %v", err)
	}

	firstPage, err := repo.Browse(context.Background(), repository.BrowseArtifactsParams{Limit: 1, Offset: 0})
	if err != nil {
		t.Fatalf("Browse first page failed: %v", err)
	}
	if len(firstPage) != 1 {
		t.Fatalf("expected one record for first logical artifact, got %d", len(firstPage))
	}
	if firstPage[0].Artifact.LogicalPath != "reports/junit.xml" {
		t.Fatalf("expected newest logical artifact first, got %q", firstPage[0].Artifact.LogicalPath)
	}

	secondPage, err := repo.Browse(context.Background(), repository.BrowseArtifactsParams{Limit: 1, Offset: 1})
	if err != nil {
		t.Fatalf("Browse second page failed: %v", err)
	}
	if len(secondPage) != 2 {
		t.Fatalf("expected both versions for paged logical artifact, got %d", len(secondPage))
	}
	for _, record := range secondPage {
		if record.Artifact.LogicalPath != "packages/pkg-a.tgz" {
			t.Fatalf("expected grouped versions to stay together, got %q", record.Artifact.LogicalPath)
		}
	}
}

func TestArtifactRepository_BrowseFiltersBeforePagination(t *testing.T) {
	repo := NewArtifactRepository()
	now := time.Now().UTC().Truncate(time.Second)
	jobID := "job-1"
	repo.SeedBuilds(
		domain.Build{ID: "build-1", JobID: &jobID, ProjectID: "project-1", CreatedAt: now},
		domain.Build{ID: "build-2", JobID: &jobID, ProjectID: "project-1", CreatedAt: now.Add(time.Minute)},
	)

	_, err := repo.Create(context.Background(), domain.BuildArtifact{ID: "artifact-1", BuildID: "build-1", LogicalPath: "images/backend.tar", ArtifactType: domain.ArtifactTypeDockerImage, CreatedAt: now})
	if err != nil {
		t.Fatalf("create docker artifact failed: %v", err)
	}
	_, err = repo.Create(context.Background(), domain.BuildArtifact{ID: "artifact-2", BuildID: "build-2", LogicalPath: "reports/junit.xml", ArtifactType: domain.ArtifactTypeGeneric, CreatedAt: now.Add(time.Minute)})
	if err != nil {
		t.Fatalf("create generic artifact failed: %v", err)
	}

	records, err := repo.Browse(context.Background(), repository.BrowseArtifactsParams{Type: domain.ArtifactTypeDockerImage, Limit: 1})
	if err != nil {
		t.Fatalf("Browse failed: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected one docker artifact record, got %d", len(records))
	}
	if records[0].Artifact.LogicalPath != "images/backend.tar" {
		t.Fatalf("expected docker artifact after filtering, got %q", records[0].Artifact.LogicalPath)
	}
}
