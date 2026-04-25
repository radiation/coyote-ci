package memory

import (
	"context"
	"errors"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func TestVersionTagRepository_CreateForTargets_AllowsSharedVersionAcrossTargets(t *testing.T) {
	repo := NewVersionTagRepository()
	jobID := "job-1"
	buildID := "build-1"
	repo.SeedBuilds(domain.Build{ID: buildID, ProjectID: "project-1", JobID: &jobID})
	repo.SeedArtifacts(
		domain.BuildArtifact{ID: "artifact-1", BuildID: buildID},
		domain.BuildArtifact{ID: "artifact-2", BuildID: buildID},
	)
	repo.SeedManagedImages(domain.ManagedImage{ID: "image-1", ProjectID: "project-1", Name: "go"})
	repo.SeedManagedImageVersions(domain.ManagedImageVersion{ID: "image-version-1", ManagedImageID: "image-1"})

	tags, err := repo.CreateForTargets(context.Background(), repository.CreateVersionTagsParams{
		JobID:                  jobID,
		Version:                "2026.04.22",
		ArtifactIDs:            []string{"artifact-1", "artifact-2"},
		ManagedImageVersionIDs: []string{"image-version-1"},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(tags) != 3 {
		t.Fatalf("expected 3 tags, got %d", len(tags))
	}
}

func TestVersionTagRepository_CreateForTargets_RejectsDuplicateOnSameTarget(t *testing.T) {
	repo := NewVersionTagRepository()
	jobID := "job-1"
	buildID := "build-1"
	repo.SeedBuilds(domain.Build{ID: buildID, JobID: &jobID})
	repo.SeedArtifacts(domain.BuildArtifact{ID: "artifact-1", BuildID: buildID})

	_, err := repo.CreateForTargets(context.Background(), repository.CreateVersionTagsParams{
		JobID:       jobID,
		Version:     "v1",
		ArtifactIDs: []string{"artifact-1"},
	})
	if err != nil {
		t.Fatalf("expected initial create to succeed, got %v", err)
	}

	_, err = repo.CreateForTargets(context.Background(), repository.CreateVersionTagsParams{
		JobID:       jobID,
		Version:     "v1",
		ArtifactIDs: []string{"artifact-1"},
	})
	if !errors.Is(err, repository.ErrVersionTagConflict) {
		t.Fatalf("expected ErrVersionTagConflict, got %v", err)
	}
}

func TestVersionTagRepository_CreateForTargets_RejectsArtifactFromDifferentJob(t *testing.T) {
	repo := NewVersionTagRepository()
	ownerJobID := "job-1"
	otherJobID := "job-2"
	buildID := "build-1"
	repo.SeedBuilds(domain.Build{ID: buildID, JobID: &ownerJobID})
	repo.SeedArtifacts(domain.BuildArtifact{ID: "artifact-1", BuildID: buildID})

	_, err := repo.CreateForTargets(context.Background(), repository.CreateVersionTagsParams{
		JobID:       otherJobID,
		Version:     "v1",
		ArtifactIDs: []string{"artifact-1"},
	})
	if !errors.Is(err, repository.ErrVersionTagTargetJobMismatch) {
		t.Fatalf("expected ErrVersionTagTargetJobMismatch, got %v", err)
	}
}

func TestVersionTagRepository_CreateForTargets_RejectsManagedImageVersionFromDifferentProject(t *testing.T) {
	repo := NewVersionTagRepository()
	jobID := "job-1"
	buildID := "build-1"
	repo.SeedBuilds(domain.Build{ID: buildID, ProjectID: "project-1", JobID: &jobID})
	repo.SeedManagedImages(domain.ManagedImage{ID: "image-1", ProjectID: "project-2", Name: "go"})
	repo.SeedManagedImageVersions(domain.ManagedImageVersion{ID: "image-version-1", ManagedImageID: "image-1"})

	_, err := repo.CreateForTargets(context.Background(), repository.CreateVersionTagsParams{
		JobID:                  jobID,
		Version:                "v1",
		ManagedImageVersionIDs: []string{"image-version-1"},
	})
	if !errors.Is(err, repository.ErrVersionTagTargetJobMismatch) {
		t.Fatalf("expected ErrVersionTagTargetJobMismatch, got %v", err)
	}
}
