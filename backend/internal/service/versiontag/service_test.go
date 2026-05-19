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
	repo.SeedBuilds(domain.Build{ID: buildID, ProjectID: "project-1", JobID: &jobID})
	repo.SeedArtifacts(domain.BuildArtifact{ID: "artifact-1", BuildID: buildID})
	repo.SeedManagedImages(domain.ManagedImage{ID: "image-1", ProjectID: "project-1", Name: "go"})
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

func TestService_CreateVersionTags_RepositoryNotConfigured(t *testing.T) {
	svc := NewService(nil)

	_, err := svc.CreateVersionTags(context.Background(), "job-1", CreateVersionTagsInput{Version: "v1", ArtifactIDs: []string{"artifact-1"}})
	if !errors.Is(err, ErrVersionTagRepositoryNotConfigured) {
		t.Fatalf("expected ErrVersionTagRepositoryNotConfigured, got %v", err)
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

func TestService_CreateVersionTags_DefaultsUnknownOmittedKindToVersion(t *testing.T) {
	artifactRepo := repositorymemory.NewArtifactLabelRepository()
	jobID := "job-1"
	buildID := "build-1"
	artifactRepo.SeedBuilds(domain.Build{ID: buildID, JobID: &jobID})
	artifactRepo.SeedArtifacts(domain.BuildArtifact{ID: "artifact-1", BuildID: buildID, LogicalPath: "packages/pkg-a.tgz"})

	svc := NewService(nil).WithArtifactLabels(artifactRepo)
	tags, err := svc.CreateVersionTags(context.Background(), jobID, CreateVersionTagsInput{
		Version:     "release-42",
		ArtifactIDs: []string{"artifact-1"},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(tags) != 1 || tags[0].Kind != domain.VersionTagKindVersion {
		t.Fatalf("expected inferred version tag, got %#v", tags)
	}
}

func TestService_CreateVersionTags_InfersKnownChannels(t *testing.T) {
	artifactRepo := repositorymemory.NewArtifactLabelRepository()
	jobID := "job-1"
	buildID := "build-1"
	artifactRepo.SeedBuilds(domain.Build{ID: buildID, JobID: &jobID})
	artifactRepo.SeedArtifacts(domain.BuildArtifact{ID: "artifact-1", BuildID: buildID, LogicalPath: "packages/pkg-a.tgz"})

	svc := NewService(nil).WithArtifactLabels(artifactRepo)
	tags, err := svc.CreateVersionTags(context.Background(), jobID, CreateVersionTagsInput{
		Version:     "prod",
		ArtifactIDs: []string{"artifact-1"},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(tags) != 1 || tags[0].Kind != domain.VersionTagKindChannel {
		t.Fatalf("expected inferred channel tag, got %#v", tags)
	}
}

func TestService_CreateVersionTags_ArtifactChannelsRequireArtifactLabelRepository(t *testing.T) {
	repo := repositorymemory.NewVersionTagRepository()
	jobID := "job-1"
	buildID := "build-1"
	repo.SeedBuilds(domain.Build{ID: buildID, JobID: &jobID})
	repo.SeedArtifacts(domain.BuildArtifact{ID: "artifact-1", BuildID: buildID})

	svc := NewService(repo)
	_, err := svc.CreateVersionTags(context.Background(), jobID, CreateVersionTagsInput{
		Kind:        "channel",
		Version:     "prod",
		ArtifactIDs: []string{"artifact-1"},
	})
	if !errors.Is(err, ErrArtifactChannelsRequireArtifactLabelRepository) {
		t.Fatalf("expected ErrArtifactChannelsRequireArtifactLabelRepository, got %v", err)
	}
}

func TestService_ListArtifactAndManagedImageTags(t *testing.T) {
	legacyRepo := repositorymemory.NewVersionTagRepository()
	artifactRepo := repositorymemory.NewArtifactLabelRepository()
	jobID := "job-1"
	buildID := "build-1"
	artifactRepo.SeedBuilds(domain.Build{ID: buildID, JobID: &jobID})
	artifactRepo.SeedArtifacts(
		domain.BuildArtifact{ID: "artifact-1", BuildID: buildID, LogicalPath: "packages/pkg-a.tgz"},
		domain.BuildArtifact{ID: "artifact-2", BuildID: buildID, LogicalPath: "packages/pkg-b.tgz"},
	)

	legacyRepo.SeedBuilds(domain.Build{ID: buildID, ProjectID: "project-1", JobID: &jobID})
	legacyRepo.SeedArtifacts(domain.BuildArtifact{ID: "artifact-legacy", BuildID: buildID})
	legacyRepo.SeedManagedImages(domain.ManagedImage{ID: "image-1", ProjectID: "project-1", Name: "go"})
	legacyRepo.SeedManagedImageVersions(domain.ManagedImageVersion{ID: "image-version-1", ManagedImageID: "image-1"})

	_, err := artifactRepo.CreateForArtifacts(context.Background(), repository.CreateArtifactLabelsParams{JobID: jobID, Value: "1.2.3", Kind: domain.VersionTagKindVersion, ArtifactIDs: []string{"artifact-1"}})
	if err != nil {
		t.Fatalf("expected artifact version create to succeed, got %v", err)
	}
	_, err = artifactRepo.CreateForArtifacts(context.Background(), repository.CreateArtifactLabelsParams{JobID: jobID, Value: "prod", Kind: domain.VersionTagKindChannel, ArtifactIDs: []string{"artifact-1"}})
	if err != nil {
		t.Fatalf("expected artifact channel create to succeed, got %v", err)
	}
	_, err = artifactRepo.CreateForArtifacts(context.Background(), repository.CreateArtifactLabelsParams{JobID: jobID, Value: "2.0.0", Kind: domain.VersionTagKindVersion, ArtifactIDs: []string{"artifact-2"}})
	if err != nil {
		t.Fatalf("expected second artifact version create to succeed, got %v", err)
	}
	_, err = legacyRepo.CreateForTargets(context.Background(), repository.CreateVersionTagsParams{JobID: jobID, Version: "1.2.3", ManagedImageVersionIDs: []string{"image-version-1"}})
	if err != nil {
		t.Fatalf("expected managed image version create to succeed, got %v", err)
	}

	svc := NewService(legacyRepo).WithArtifactLabels(artifactRepo)
	artifactTags, err := svc.ListArtifactTags(context.Background(), "artifact-1")
	if err != nil {
		t.Fatalf("expected list artifact tags to succeed, got %v", err)
	}
	if len(artifactTags) != 2 {
		t.Fatalf("expected 2 artifact tags, got %#v", artifactTags)
	}

	artifactTagsByID, err := svc.ListArtifactTagsByIDs(context.Background(), []string{"artifact-1", "artifact-2"})
	if err != nil {
		t.Fatalf("expected list artifact tags by ids to succeed, got %v", err)
	}
	if len(artifactTagsByID["artifact-1"]) != 2 || len(artifactTagsByID["artifact-2"]) != 1 {
		t.Fatalf("expected grouped artifact tags, got %#v", artifactTagsByID)
	}

	managedImageTags, err := svc.ListManagedImageVersionTags(context.Background(), "image-version-1")
	if err != nil {
		t.Fatalf("expected list managed image version tags to succeed, got %v", err)
	}
	if len(managedImageTags) != 1 || managedImageTags[0].ManagedImageVersionID == nil || *managedImageTags[0].ManagedImageVersionID != "image-version-1" {
		t.Fatalf("expected one managed image version tag, got %#v", managedImageTags)
	}

	jobTags, err := svc.ListJobVersionTags(context.Background(), jobID, "1.2.3")
	if err != nil {
		t.Fatalf("expected list job version tags to succeed, got %v", err)
	}
	if len(jobTags) != 2 {
		t.Fatalf("expected artifact + managed image tags for job/version, got %#v", jobTags)
	}

	resolved, err := svc.ResolveReleaseVersion(context.Background(), domain.Build{ID: buildID, JobID: &jobID, BuildNumber: 8}, versioning.Config{Strategy: "semver-patch", Version: "1.2"})
	if err != nil {
		t.Fatalf("expected release version resolve to succeed, got %v", err)
	}
	if resolved != "1.2.4" {
		t.Fatalf("expected next artifact-backed patch version 1.2.4, got %q", resolved)
	}
}

func TestService_ListArtifactTags_UsesLegacyRepoWhenArtifactRepoMissing(t *testing.T) {
	repo := repositorymemory.NewVersionTagRepository()
	jobID := "job-1"
	buildID := "build-1"
	repo.SeedBuilds(domain.Build{ID: buildID, JobID: &jobID})
	repo.SeedArtifacts(domain.BuildArtifact{ID: "artifact-1", BuildID: buildID})
	_, err := repo.CreateForTargets(context.Background(), repository.CreateVersionTagsParams{JobID: jobID, Version: "1.2.3", ArtifactIDs: []string{"artifact-1"}})
	if err != nil {
		t.Fatalf("expected legacy artifact version create to succeed, got %v", err)
	}

	svc := NewService(repo)
	tags, err := svc.ListArtifactTags(context.Background(), "artifact-1")
	if err != nil {
		t.Fatalf("expected legacy artifact tag list to succeed, got %v", err)
	}
	if len(tags) != 1 || tags[0].Version != "1.2.3" {
		t.Fatalf("expected one legacy artifact version tag, got %#v", tags)
	}
}

func TestService_ListArtifactTagsByIDs_UsesLegacyRepoWhenArtifactRepoMissing(t *testing.T) {
	repo := repositorymemory.NewVersionTagRepository()
	jobID := "job-1"
	buildID := "build-1"
	repo.SeedBuilds(domain.Build{ID: buildID, JobID: &jobID})
	repo.SeedArtifacts(
		domain.BuildArtifact{ID: "artifact-1", BuildID: buildID},
		domain.BuildArtifact{ID: "artifact-2", BuildID: buildID},
	)
	_, err := repo.CreateForTargets(context.Background(), repository.CreateVersionTagsParams{JobID: jobID, Version: "1.2.3", ArtifactIDs: []string{"artifact-1", "artifact-2"}})
	if err != nil {
		t.Fatalf("expected legacy artifact version create to succeed, got %v", err)
	}

	svc := NewService(repo)
	tagsByID, err := svc.ListArtifactTagsByIDs(context.Background(), []string{"artifact-1", "artifact-2"})
	if err != nil {
		t.Fatalf("expected legacy artifact tag list by ids to succeed, got %v", err)
	}
	if len(tagsByID["artifact-1"]) != 1 || len(tagsByID["artifact-2"]) != 1 {
		t.Fatalf("expected grouped legacy artifact tags, got %#v", tagsByID)
	}
}

func TestService_ListJobVersionTags_UsesSingleRepositoryBranches(t *testing.T) {
	legacyRepo := repositorymemory.NewVersionTagRepository()
	jobID := "job-1"
	buildID := "build-1"
	legacyRepo.SeedBuilds(domain.Build{ID: buildID, JobID: &jobID})
	legacyRepo.SeedArtifacts(domain.BuildArtifact{ID: "artifact-1", BuildID: buildID})
	_, err := legacyRepo.CreateForTargets(context.Background(), repository.CreateVersionTagsParams{JobID: jobID, Version: "1.2.3", ArtifactIDs: []string{"artifact-1"}})
	if err != nil {
		t.Fatalf("expected legacy artifact version create to succeed, got %v", err)
	}

	legacySvc := NewService(legacyRepo)
	legacyTags, err := legacySvc.ListJobVersionTags(context.Background(), jobID, "1.2.3")
	if err != nil {
		t.Fatalf("expected legacy job version tag list to succeed, got %v", err)
	}
	if len(legacyTags) != 1 {
		t.Fatalf("expected 1 legacy job tag, got %#v", legacyTags)
	}

	artifactRepo := repositorymemory.NewArtifactLabelRepository()
	artifactRepo.SeedBuilds(domain.Build{ID: buildID, JobID: &jobID})
	artifactRepo.SeedArtifacts(domain.BuildArtifact{ID: "artifact-2", BuildID: buildID, LogicalPath: "packages/pkg-a.tgz"})
	_, err = artifactRepo.CreateForArtifacts(context.Background(), repository.CreateArtifactLabelsParams{JobID: jobID, Value: "2.0.0", Kind: domain.VersionTagKindVersion, ArtifactIDs: []string{"artifact-2"}})
	if err != nil {
		t.Fatalf("expected artifact label create to succeed, got %v", err)
	}

	artifactSvc := NewService(nil).WithArtifactLabels(artifactRepo)
	artifactTags, err := artifactSvc.ListJobVersionTags(context.Background(), jobID, "2.0.0")
	if err != nil {
		t.Fatalf("expected artifact-backed job version tag list to succeed, got %v", err)
	}
	if len(artifactTags) != 1 {
		t.Fatalf("expected 1 artifact-backed job tag, got %#v", artifactTags)
	}

	_, err = artifactSvc.ListJobVersionTags(context.Background(), "", "2.0.0")
	if !errors.Is(err, ErrJobIDRequired) {
		t.Fatalf("expected ErrJobIDRequired, got %v", err)
	}
	_, err = artifactSvc.ListJobVersionTags(context.Background(), jobID, " ")
	if !errors.Is(err, ErrVersionRequired) {
		t.Fatalf("expected ErrVersionRequired, got %v", err)
	}
}

func TestResolveKind(t *testing.T) {
	kind, err := resolveKind("", "prod")
	if err != nil {
		t.Fatalf("expected no error for inferred channel, got %v", err)
	}
	if kind != domain.VersionTagKindChannel {
		t.Fatalf("expected channel kind, got %q", kind)
	}

	kind, err = resolveKind("version", "prod")
	if err != nil {
		t.Fatalf("expected no error for explicit version kind, got %v", err)
	}
	if kind != domain.VersionTagKindVersion {
		t.Fatalf("expected version kind, got %q", kind)
	}

	_, err = resolveKind("invalid", "prod")
	if !errors.Is(err, ErrVersionTagKindInvalid) {
		t.Fatalf("expected ErrVersionTagKindInvalid, got %v", err)
	}
}
