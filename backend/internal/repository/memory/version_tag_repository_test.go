package memory

import (
	"context"
	"errors"
	"testing"
	"time"

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

func TestVersionTagRepository_ListFiltersTrimAndDeduplicateTargets(t *testing.T) {
	repo := NewVersionTagRepository()
	ctx := context.Background()
	jobID := "job-1"
	buildID := "build-1"
	repo.SeedBuilds(domain.Build{ID: buildID, ProjectID: "project-1", JobID: &jobID})
	repo.SeedArtifacts(
		domain.BuildArtifact{ID: "artifact-1", BuildID: buildID},
		domain.BuildArtifact{ID: "artifact-2", BuildID: buildID},
	)
	repo.SeedManagedImages(domain.ManagedImage{ID: "image-1", ProjectID: "project-1", Name: "go"})
	repo.SeedManagedImageVersions(domain.ManagedImageVersion{ID: "image-version-1", ManagedImageID: "image-1"})

	tags, err := repo.CreateForTargets(ctx, repository.CreateVersionTagsParams{
		JobID:                  " job-1 ",
		Version:                " v2 ",
		ArtifactIDs:            []string{" artifact-1 ", "", "artifact-1", "artifact-2"},
		ManagedImageVersionIDs: []string{" image-version-1 ", "image-version-1"},
	})
	if err != nil {
		t.Fatalf("create tags failed: %v", err)
	}
	if len(tags) != 3 {
		t.Fatalf("expected 3 unique tags, got %d", len(tags))
	}

	artifactTags, err := repo.ListByArtifactID(ctx, " artifact-1 ")
	if err != nil {
		t.Fatalf("list by artifact failed: %v", err)
	}
	assertVersionTagTargets(t, artifactTags, []string{"artifact:artifact-1"})

	artifactTags, err = repo.ListByArtifactIDs(ctx, []string{"artifact-2", "artifact-1", "artifact-2", " "})
	if err != nil {
		t.Fatalf("list by artifact ids failed: %v", err)
	}
	assertVersionTagTargets(t, artifactTags, []string{"artifact:artifact-1", "artifact:artifact-2"})

	managedImageTags, err := repo.ListByManagedImageVersionID(ctx, " image-version-1 ")
	if err != nil {
		t.Fatalf("list by managed image version failed: %v", err)
	}
	assertVersionTagTargets(t, managedImageTags, []string{"managed_image_version:image-version-1"})

	jobTags, err := repo.ListByJobID(ctx, " job-1 ")
	if err != nil {
		t.Fatalf("list by job failed: %v", err)
	}
	if len(jobTags) != 3 {
		t.Fatalf("expected 3 job tags, got %d", len(jobTags))
	}

	versionTags, err := repo.ListByJobIDAndVersion(ctx, " job-1 ", " v2 ")
	if err != nil {
		t.Fatalf("list by job/version failed: %v", err)
	}
	if len(versionTags) != 3 {
		t.Fatalf("expected 3 version tags, got %d", len(versionTags))
	}

	missingTags, err := repo.ListByArtifactIDs(ctx, []string{"missing"})
	if err != nil {
		t.Fatalf("list missing artifact ids failed: %v", err)
	}
	if len(missingTags) != 0 {
		t.Fatalf("expected no missing artifact tags, got %d", len(missingTags))
	}
}

func TestVersionTagRepository_CreateForTargets_RejectsMissingTargetsAndManagedImageDuplicates(t *testing.T) {
	ctx := context.Background()
	jobID := "job-1"

	tests := []struct {
		name    string
		prepare func(*VersionTagRepository)
		params  repository.CreateVersionTagsParams
		wantErr error
	}{
		{
			name: "missing artifact",
			params: repository.CreateVersionTagsParams{
				JobID:       jobID,
				Version:     "v1",
				ArtifactIDs: []string{"missing"},
			},
			wantErr: repository.ErrVersionTagTargetNotFound,
		},
		{
			name: "artifact build missing",
			prepare: func(repo *VersionTagRepository) {
				repo.SeedArtifacts(domain.BuildArtifact{ID: "artifact-1", BuildID: "missing-build"})
			},
			params: repository.CreateVersionTagsParams{
				JobID:       jobID,
				Version:     "v1",
				ArtifactIDs: []string{"artifact-1"},
			},
			wantErr: repository.ErrVersionTagTargetNotFound,
		},
		{
			name: "missing managed image version",
			params: repository.CreateVersionTagsParams{
				JobID:                  jobID,
				Version:                "v1",
				ManagedImageVersionIDs: []string{"missing-version"},
			},
			wantErr: repository.ErrVersionTagTargetNotFound,
		},
		{
			name: "missing managed image",
			prepare: func(repo *VersionTagRepository) {
				repo.SeedManagedImageVersions(domain.ManagedImageVersion{ID: "version-1", ManagedImageID: "missing-image"})
			},
			params: repository.CreateVersionTagsParams{
				JobID:                  jobID,
				Version:                "v1",
				ManagedImageVersionIDs: []string{"version-1"},
			},
			wantErr: repository.ErrVersionTagTargetNotFound,
		},
		{
			name: "job project missing",
			prepare: func(repo *VersionTagRepository) {
				repo.SeedBuilds(domain.Build{ID: "build-1", JobID: &jobID})
				repo.SeedManagedImages(domain.ManagedImage{ID: "image-1", ProjectID: "project-1"})
				repo.SeedManagedImageVersions(domain.ManagedImageVersion{ID: "version-1", ManagedImageID: "image-1"})
			},
			params: repository.CreateVersionTagsParams{
				JobID:                  jobID,
				Version:                "v1",
				ManagedImageVersionIDs: []string{"version-1"},
			},
			wantErr: repository.ErrVersionTagTargetNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := NewVersionTagRepository()
			if tt.prepare != nil {
				tt.prepare(repo)
			}
			_, err := repo.CreateForTargets(ctx, tt.params)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected %v, got %v", tt.wantErr, err)
			}
		})
	}

	repo := NewVersionTagRepository()
	repo.SeedBuilds(domain.Build{ID: "build-1", ProjectID: "project-1", JobID: &jobID})
	repo.SeedManagedImages(domain.ManagedImage{ID: "image-1", ProjectID: "project-1"})
	repo.SeedManagedImageVersions(domain.ManagedImageVersion{ID: "version-1", ManagedImageID: "image-1"})
	_, err := repo.CreateForTargets(ctx, repository.CreateVersionTagsParams{
		JobID:                  jobID,
		Version:                "v1",
		ManagedImageVersionIDs: []string{"version-1"},
	})
	if err != nil {
		t.Fatalf("initial managed image tag create failed: %v", err)
	}
	_, err = repo.CreateForTargets(ctx, repository.CreateVersionTagsParams{
		JobID:                  jobID,
		Version:                "v1",
		ManagedImageVersionIDs: []string{"version-1"},
	})
	if !errors.Is(err, repository.ErrVersionTagConflict) {
		t.Fatalf("expected ErrVersionTagConflict for duplicate managed image tag, got %v", err)
	}
}

func assertVersionTagTargets(t *testing.T, tags []domain.VersionTag, want []string) {
	t.Helper()
	if len(tags) != len(want) {
		t.Fatalf("expected %d tags, got %d: %#v", len(want), len(tags), tags)
	}
	seen := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		switch tag.TargetType {
		case domain.VersionTagTargetArtifact:
			if tag.ArtifactID != nil {
				seen["artifact:"+*tag.ArtifactID] = struct{}{}
			}
		case domain.VersionTagTargetManagedImageVersion:
			if tag.ManagedImageVersionID != nil {
				seen["managed_image_version:"+*tag.ManagedImageVersionID] = struct{}{}
			}
		}
	}
	for _, target := range want {
		if _, ok := seen[target]; !ok {
			t.Fatalf("expected target %q in %#v", target, tags)
		}
	}
}

func TestVersionTagRepository_FilterSortsByCreatedAtThenID(t *testing.T) {
	repo := NewVersionTagRepository()
	artifactID := "artifact-1"
	repo.tags = []domain.VersionTag{
		{ID: "tag-b", JobID: "job-1", Version: "v1", TargetType: domain.VersionTagTargetArtifact, ArtifactID: &artifactID, CreatedAt: time.Date(2026, 5, 28, 14, 0, 0, 0, time.UTC)},
		{ID: "tag-a", JobID: "job-1", Version: "v1", TargetType: domain.VersionTagTargetArtifact, ArtifactID: &artifactID, CreatedAt: time.Date(2026, 5, 28, 14, 0, 0, 0, time.UTC)},
		{ID: "tag-old", JobID: "job-1", Version: "v1", TargetType: domain.VersionTagTargetArtifact, ArtifactID: &artifactID, CreatedAt: time.Date(2026, 5, 28, 13, 0, 0, 0, time.UTC)},
	}

	tags, err := repo.ListByArtifactID(context.Background(), artifactID)
	if err != nil {
		t.Fatalf("list by artifact failed: %v", err)
	}
	wantIDs := []string{"tag-old", "tag-a", "tag-b"}
	for i, tag := range tags {
		if tag.ID != wantIDs[i] {
			t.Fatalf("tag[%d]: expected %q, got %q", i, wantIDs[i], tag.ID)
		}
	}
}
