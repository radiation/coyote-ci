package versiontag

import (
	"context"
	"errors"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	repositorymemory "github.com/radiation/coyote-ci/backend/internal/repository/memory"
)

func TestService_CreateVersionTags(t *testing.T) {
	repo := repositorymemory.NewVersionTagRepository()
	jobID := "job-1"
	buildID := "build-1"
	repo.SeedBuilds(domain.Build{ID: buildID, JobID: &jobID})
	repo.SeedArtifacts(domain.BuildArtifact{ID: "artifact-1", BuildID: buildID})
	repo.SeedManagedImageVersions(domain.ManagedImageVersion{ID: "image-version-1", ManagedImageID: "image-1"})

	svc := NewService(repo)
	tags, err := svc.CreateVersionTags(context.Background(), jobID, CreateVersionTagsInput{
		Version:                "  1.2.3  ",
		ArtifactIDs:            []string{"artifact-1", "artifact-1"},
		ManagedImageVersionIDs: []string{"image-version-1"},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(tags) != 2 {
		t.Fatalf("expected deduplicated target count of 2, got %d", len(tags))
	}
	if tags[0].Version != "1.2.3" {
		t.Fatalf("expected trimmed version, got %q", tags[0].Version)
	}
}

func TestService_CreateVersionTags_ValidatesInput(t *testing.T) {
	svc := NewService(repositorymemory.NewVersionTagRepository())

	_, err := svc.CreateVersionTags(context.Background(), "", CreateVersionTagsInput{Version: "v1", ArtifactIDs: []string{"artifact-1"}})
	if !errors.Is(err, ErrJobIDRequired) {
		t.Fatalf("expected ErrJobIDRequired, got %v", err)
	}

	_, err = svc.CreateVersionTags(context.Background(), "job-1", CreateVersionTagsInput{Version: "\n", ArtifactIDs: []string{"artifact-1"}})
	if !errors.Is(err, ErrVersionRequired) {
		t.Fatalf("expected ErrVersionRequired, got %v", err)
	}

	_, err = svc.CreateVersionTags(context.Background(), "job-1", CreateVersionTagsInput{Version: "v1"})
	if !errors.Is(err, ErrTargetRequired) {
		t.Fatalf("expected ErrTargetRequired, got %v", err)
	}

	_, err = svc.CreateVersionTags(context.Background(), "job-1", CreateVersionTagsInput{Version: "bad\x01version", ArtifactIDs: []string{"artifact-1"}})
	if !errors.Is(err, ErrVersionContainsControlChars) {
		t.Fatalf("expected ErrVersionContainsControlChars, got %v", err)
	}
}

func TestService_ListJobVersionTags(t *testing.T) {
	repo := repositorymemory.NewVersionTagRepository()
	jobID := "job-1"
	buildID := "build-1"
	repo.SeedBuilds(domain.Build{ID: buildID, JobID: &jobID})
	repo.SeedArtifacts(domain.BuildArtifact{ID: "artifact-1", BuildID: buildID})
	_, _ = repo.CreateForTargets(context.Background(), repository.CreateVersionTagsParams{JobID: jobID, Version: "v1", ArtifactIDs: []string{"artifact-1"}})

	svc := NewService(repo)
	tags, err := svc.ListJobVersionTags(context.Background(), jobID, "v1")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(tags) != 1 {
		t.Fatalf("expected 1 tag, got %d", len(tags))
	}
}
