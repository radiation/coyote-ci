package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func TestArtifactLabelRepository_AllowsSameVersionAcrossPackages(t *testing.T) {
	repo := NewArtifactLabelRepository()
	jobID := "job-1"
	now := time.Now().UTC()
	repo.SeedBuilds(
		domain.Build{ID: "build-1", JobID: &jobID},
		domain.Build{ID: "build-2", JobID: &jobID},
	)
	repo.SeedArtifacts(
		domain.BuildArtifact{ID: "artifact-1", BuildID: "build-1", LogicalPath: "packages/pkg-a.tgz", CreatedAt: now},
		domain.BuildArtifact{ID: "artifact-2", BuildID: "build-2", LogicalPath: "packages/pkg-b.tgz", CreatedAt: now.Add(time.Minute)},
	)

	tags, err := repo.CreateForArtifacts(context.Background(), repository.CreateArtifactLabelsParams{
		JobID:       jobID,
		Value:       "1.2.3",
		Kind:        domain.VersionTagKindVersion,
		ArtifactIDs: []string{"artifact-1", "artifact-2"},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(tags) != 2 {
		t.Fatalf("expected 2 created tags, got %d", len(tags))
	}
}

func TestArtifactLabelRepository_RejectsSameVersionWithinPackage(t *testing.T) {
	repo := NewArtifactLabelRepository()
	jobID := "job-1"
	now := time.Now().UTC()
	repo.SeedBuilds(
		domain.Build{ID: "build-1", JobID: &jobID},
		domain.Build{ID: "build-2", JobID: &jobID},
	)
	repo.SeedArtifacts(
		domain.BuildArtifact{ID: "artifact-1", BuildID: "build-1", LogicalPath: "packages/pkg-a.tgz", CreatedAt: now},
		domain.BuildArtifact{ID: "artifact-2", BuildID: "build-2", LogicalPath: "packages/pkg-a.tgz", CreatedAt: now.Add(time.Minute)},
	)

	_, err := repo.CreateForArtifacts(context.Background(), repository.CreateArtifactLabelsParams{
		JobID:       jobID,
		Value:       "1.2.3",
		Kind:        domain.VersionTagKindVersion,
		ArtifactIDs: []string{"artifact-1"},
	})
	if err != nil {
		t.Fatalf("expected initial create to succeed, got %v", err)
	}

	repeated, err := repo.CreateForArtifacts(context.Background(), repository.CreateArtifactLabelsParams{
		JobID:       jobID,
		Value:       "1.2.3",
		Kind:        domain.VersionTagKindVersion,
		ArtifactIDs: []string{"artifact-1"},
	})
	if err != nil {
		t.Fatalf("expected same-artifact reassign to be idempotent, got %v", err)
	}
	if len(repeated) != 1 || repeated[0].ArtifactID == nil || *repeated[0].ArtifactID != "artifact-1" {
		t.Fatalf("expected existing artifact version tag on idempotent create, got %#v", repeated)
	}

	_, err = repo.CreateForArtifacts(context.Background(), repository.CreateArtifactLabelsParams{
		JobID:       jobID,
		Value:       "1.2.3",
		Kind:        domain.VersionTagKindVersion,
		ArtifactIDs: []string{"artifact-2"},
	})
	if !errors.Is(err, repository.ErrVersionTagConflict) {
		t.Fatalf("expected ErrVersionTagConflict, got %v", err)
	}
}

func TestArtifactLabelRepository_ChannelMoveUpdatesCurrentArtifactAndRecordsEvent(t *testing.T) {
	repo := NewArtifactLabelRepository()
	jobID := "job-1"
	now := time.Now().UTC()
	repo.SeedBuilds(
		domain.Build{ID: "build-1", JobID: &jobID},
		domain.Build{ID: "build-2", JobID: &jobID},
	)
	repo.SeedArtifacts(
		domain.BuildArtifact{ID: "artifact-1", BuildID: "build-1", LogicalPath: "packages/pkg-a.tgz", CreatedAt: now},
		domain.BuildArtifact{ID: "artifact-2", BuildID: "build-2", LogicalPath: "packages/pkg-a.tgz", CreatedAt: now.Add(time.Minute)},
	)

	_, err := repo.CreateForArtifacts(context.Background(), repository.CreateArtifactLabelsParams{
		JobID:       jobID,
		Value:       "prod",
		Kind:        domain.VersionTagKindChannel,
		ArtifactIDs: []string{"artifact-1"},
	})
	if err != nil {
		t.Fatalf("expected initial channel create to succeed, got %v", err)
	}
	_, err = repo.CreateForArtifacts(context.Background(), repository.CreateArtifactLabelsParams{
		JobID:       jobID,
		Value:       "prod",
		Kind:        domain.VersionTagKindChannel,
		ArtifactIDs: []string{"artifact-2"},
	})
	if err != nil {
		t.Fatalf("expected channel move to succeed, got %v", err)
	}

	oldArtifactTags, err := repo.ListByArtifactID(context.Background(), "artifact-1")
	if err != nil {
		t.Fatalf("expected no error listing old artifact tags, got %v", err)
	}
	if len(oldArtifactTags) != 0 {
		t.Fatalf("expected moved channel to disappear from old artifact, got %d tags", len(oldArtifactTags))
	}

	newArtifactTags, err := repo.ListByArtifactID(context.Background(), "artifact-2")
	if err != nil {
		t.Fatalf("expected no error listing new artifact tags, got %v", err)
	}
	if len(newArtifactTags) != 1 || newArtifactTags[0].Version != "prod" || newArtifactTags[0].Kind != domain.VersionTagKindChannel {
		t.Fatalf("expected current prod channel on new artifact, got %#v", newArtifactTags)
	}
	if len(repo.events) != 2 {
		t.Fatalf("expected create and move events, got %d", len(repo.events))
	}
}

func TestArtifactLabelRepository_ListByArtifactIDReturnsCurrentVersionsAndChannels(t *testing.T) {
	repo := NewArtifactLabelRepository()
	jobID := "job-1"
	now := time.Now().UTC()
	repo.SeedBuilds(domain.Build{ID: "build-1", JobID: &jobID})
	repo.SeedArtifacts(domain.BuildArtifact{ID: "artifact-1", BuildID: "build-1", LogicalPath: "packages/pkg-a.tgz", CreatedAt: now})

	_, err := repo.CreateForArtifacts(context.Background(), repository.CreateArtifactLabelsParams{
		JobID:       jobID,
		Value:       "1.2.3",
		Kind:        domain.VersionTagKindVersion,
		ArtifactIDs: []string{"artifact-1"},
	})
	if err != nil {
		t.Fatalf("expected version create to succeed, got %v", err)
	}
	_, err = repo.CreateForArtifacts(context.Background(), repository.CreateArtifactLabelsParams{
		JobID:       jobID,
		Value:       "prod",
		Kind:        domain.VersionTagKindChannel,
		ArtifactIDs: []string{"artifact-1"},
	})
	if err != nil {
		t.Fatalf("expected channel create to succeed, got %v", err)
	}

	tags, err := repo.ListByArtifactID(context.Background(), "artifact-1")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(tags))
	}
	kinds := map[domain.VersionTagKind]bool{}
	for _, tag := range tags {
		kinds[tag.Kind] = true
	}
	if !kinds[domain.VersionTagKindVersion] || !kinds[domain.VersionTagKindChannel] {
		t.Fatalf("expected both version and channel tags, got %#v", tags)
	}
}

func TestArtifactLabelRepository_ListMethodsAndReleaseVersions(t *testing.T) {
	repo := NewArtifactLabelRepository()
	jobID := "job-1"
	otherJobID := "job-2"
	now := time.Now().UTC()
	repo.SeedBuilds(
		domain.Build{ID: "build-1", JobID: &jobID},
		domain.Build{ID: "build-2", JobID: &jobID},
		domain.Build{ID: "build-3", JobID: &otherJobID},
	)
	repo.SeedArtifacts(
		domain.BuildArtifact{ID: "artifact-1", BuildID: "build-1", LogicalPath: "packages/pkg-a.tgz", CreatedAt: now},
		domain.BuildArtifact{ID: "artifact-2", BuildID: "build-2", LogicalPath: "packages/pkg-b.tgz", CreatedAt: now.Add(time.Minute)},
		domain.BuildArtifact{ID: "artifact-3", BuildID: "build-3", LogicalPath: "packages/pkg-c.tgz", CreatedAt: now.Add(2 * time.Minute)},
	)

	_, err := repo.CreateForArtifacts(context.Background(), repository.CreateArtifactLabelsParams{
		JobID:       jobID,
		Value:       "1.2.3",
		Kind:        domain.VersionTagKindVersion,
		ArtifactIDs: []string{"artifact-1"},
	})
	if err != nil {
		t.Fatalf("expected version create to succeed, got %v", err)
	}
	_, err = repo.CreateForArtifacts(context.Background(), repository.CreateArtifactLabelsParams{
		JobID:       jobID,
		Value:       "prod",
		Kind:        domain.VersionTagKindChannel,
		ArtifactIDs: []string{"artifact-1"},
	})
	if err != nil {
		t.Fatalf("expected channel create to succeed, got %v", err)
	}
	_, err = repo.CreateForArtifacts(context.Background(), repository.CreateArtifactLabelsParams{
		JobID:       jobID,
		Value:       "2.0.0",
		Kind:        domain.VersionTagKindVersion,
		ArtifactIDs: []string{"artifact-2"},
	})
	if err != nil {
		t.Fatalf("expected second version create to succeed, got %v", err)
	}

	artifactTags, err := repo.ListByArtifactIDs(context.Background(), []string{"artifact-1", "artifact-2"})
	if err != nil {
		t.Fatalf("expected list by artifact ids to succeed, got %v", err)
	}
	if len(artifactTags) != 3 {
		t.Fatalf("expected 3 artifact tags, got %#v", artifactTags)
	}

	jobTags, err := repo.ListByJobID(context.Background(), jobID)
	if err != nil {
		t.Fatalf("expected list by job id to succeed, got %v", err)
	}
	if len(jobTags) != 3 {
		t.Fatalf("expected 3 job tags, got %#v", jobTags)
	}

	valueTags, err := repo.ListByJobIDAndValue(context.Background(), jobID, "prod")
	if err != nil {
		t.Fatalf("expected list by job/value to succeed, got %v", err)
	}
	if len(valueTags) != 1 || valueTags[0].Kind != domain.VersionTagKindChannel {
		t.Fatalf("expected one prod channel tag, got %#v", valueTags)
	}

	versions, err := repo.ListReleaseVersionsByJobID(context.Background(), jobID)
	if err != nil {
		t.Fatalf("expected release versions list to succeed, got %v", err)
	}
	if len(versions) != 2 || versions[0] != "1.2.3" || versions[1] != "2.0.0" {
		t.Fatalf("expected sorted release versions, got %#v", versions)
	}
}

func TestArtifactLabelRepository_ResolveTargetErrors(t *testing.T) {
	repo := NewArtifactLabelRepository()
	jobID := "job-1"
	otherJobID := "job-2"
	repo.SeedBuilds(
		domain.Build{ID: "build-1", JobID: &jobID},
		domain.Build{ID: "build-2", JobID: &otherJobID},
	)
	repo.SeedArtifacts(
		domain.BuildArtifact{ID: "artifact-1", BuildID: "build-1", LogicalPath: "packages/pkg-a.tgz"},
		domain.BuildArtifact{ID: "artifact-2", BuildID: "build-2", LogicalPath: "packages/pkg-b.tgz"},
		domain.BuildArtifact{ID: "artifact-3", BuildID: "missing-build", LogicalPath: "packages/pkg-c.tgz"},
	)

	_, err := repo.CreateForArtifacts(context.Background(), repository.CreateArtifactLabelsParams{
		JobID:       jobID,
		Value:       "1.2.3",
		Kind:        domain.VersionTagKindVersion,
		ArtifactIDs: []string{"missing-artifact"},
	})
	if !errors.Is(err, repository.ErrVersionTagTargetNotFound) {
		t.Fatalf("expected ErrVersionTagTargetNotFound for missing artifact, got %v", err)
	}

	_, err = repo.CreateForArtifacts(context.Background(), repository.CreateArtifactLabelsParams{
		JobID:       jobID,
		Value:       "1.2.3",
		Kind:        domain.VersionTagKindVersion,
		ArtifactIDs: []string{"artifact-2"},
	})
	if !errors.Is(err, repository.ErrVersionTagTargetJobMismatch) {
		t.Fatalf("expected ErrVersionTagTargetJobMismatch, got %v", err)
	}

	_, err = repo.CreateForArtifacts(context.Background(), repository.CreateArtifactLabelsParams{
		JobID:       jobID,
		Value:       "1.2.3",
		Kind:        domain.VersionTagKindVersion,
		ArtifactIDs: []string{"artifact-3"},
	})
	if !errors.Is(err, repository.ErrVersionTagTargetNotFound) {
		t.Fatalf("expected ErrVersionTagTargetNotFound for artifact without build, got %v", err)
	}
}
