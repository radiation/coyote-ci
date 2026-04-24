package versiontag

import (
	"context"
	"errors"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	repositorymemory "github.com/radiation/coyote-ci/backend/internal/repository/memory"
	"github.com/radiation/coyote-ci/backend/internal/versioning"
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

func TestService_ResolveReleaseVersion(t *testing.T) {
	repo := repositorymemory.NewVersionTagRepository()
	jobID := "job-1"
	buildID := "build-1"
	repo.SeedBuilds(domain.Build{ID: buildID, JobID: &jobID})
	repo.SeedArtifacts(
		domain.BuildArtifact{ID: "artifact-1", BuildID: buildID},
		domain.BuildArtifact{ID: "artifact-2", BuildID: buildID},
	)
	_, _ = repo.CreateForTargets(context.Background(), repository.CreateVersionTagsParams{JobID: jobID, Version: "1.2.0", ArtifactIDs: []string{"artifact-1"}})
	_, _ = repo.CreateForTargets(context.Background(), repository.CreateVersionTagsParams{JobID: jobID, Version: "1.2.1", ArtifactIDs: []string{"artifact-2"}})

	svc := NewService(repo)
	resolved, err := svc.ResolveReleaseVersion(context.Background(), domain.Build{ID: buildID, JobID: &jobID, BuildNumber: 8}, versioning.Config{Strategy: "semver-patch", Version: "1.2"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resolved != "1.2.2" {
		t.Fatalf("expected next patch version 1.2.2, got %q", resolved)
	}

	explicit, err := svc.ResolveReleaseVersion(context.Background(), domain.Build{ID: buildID, JobID: &jobID}, versioning.Config{Version: "2.0.5"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if explicit != "2.0.5" {
		t.Fatalf("expected explicit version 2.0.5, got %q", explicit)
	}

	templateResolved, err := svc.ResolveReleaseVersion(context.Background(), domain.Build{ID: buildID, JobID: &jobID, BuildNumber: 12}, versioning.Config{Strategy: "template", Template: "0.1.{build_number}"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if templateResolved != "0.1.12" {
		t.Fatalf("expected template version 0.1.12, got %q", templateResolved)
	}
}
