package artifact

import (
	"context"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

type fakeBrowseRepo struct {
	records []domain.ArtifactBrowseRecord
	err     error
}

func (r *fakeBrowseRepo) Create(_ context.Context, artifact domain.BuildArtifact) (domain.BuildArtifact, error) {
	return artifact, nil
}

func (r *fakeBrowseRepo) ListByBuildID(_ context.Context, _ string) ([]domain.BuildArtifact, error) {
	return nil, nil
}

func (r *fakeBrowseRepo) ListForBrowse(_ context.Context, _ string) ([]domain.ArtifactBrowseRecord, error) {
	return r.records, r.err
}

func (r *fakeBrowseRepo) GetByID(_ context.Context, _ string, _ string) (domain.BuildArtifact, error) {
	return domain.BuildArtifact{}, nil
}

func (r *fakeBrowseRepo) ListByStepID(_ context.Context, _ string) ([]domain.BuildArtifact, error) {
	return nil, nil
}

func TestServiceListArtifactsGroupsVersionsByJobAndPath(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	jobID := "job-1"
	svc := NewService(&fakeBrowseRepo{records: []domain.ArtifactBrowseRecord{
		{
			Artifact: domain.BuildArtifact{ID: "artifact-2", BuildID: "build-2", LogicalPath: "dist/app.tar", CreatedAt: now.Add(2 * time.Minute)},
			Build:    domain.Build{ID: "build-2", BuildNumber: 12, JobID: &jobID, ProjectID: "project-1", Status: domain.BuildStatusSuccess, CreatedAt: now.Add(2 * time.Minute)},
		},
		{
			Artifact: domain.BuildArtifact{ID: "artifact-1", BuildID: "build-1", LogicalPath: "dist/app.tar", CreatedAt: now},
			Build:    domain.Build{ID: "build-1", BuildNumber: 11, JobID: &jobID, ProjectID: "project-1", Status: domain.BuildStatusSuccess, CreatedAt: now},
		},
	}})

	items, err := svc.ListArtifacts(context.Background(), ListArtifactsInput{})
	if err != nil {
		t.Fatalf("ListArtifacts returned error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 artifact group, got %d", len(items))
	}
	if items[0].Path != "dist/app.tar" {
		t.Fatalf("expected grouped path dist/app.tar, got %q", items[0].Path)
	}
	if len(items[0].Versions) != 2 {
		t.Fatalf("expected 2 versions, got %d", len(items[0].Versions))
	}
	if items[0].Versions[0].Artifact.ID != "artifact-2" {
		t.Fatalf("expected newest artifact first, got %q", items[0].Versions[0].Artifact.ID)
	}
}

func TestServiceListArtifactsFiltersByType(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	jobID := "job-1"
	svc := NewService(&fakeBrowseRepo{records: []domain.ArtifactBrowseRecord{
		{
			Artifact: domain.BuildArtifact{ID: "artifact-image", BuildID: "build-1", LogicalPath: "images/backend-image.tar", CreatedAt: now},
			Build:    domain.Build{ID: "build-1", JobID: &jobID, ProjectID: "project-1", Status: domain.BuildStatusSuccess, CreatedAt: now},
		},
		{
			Artifact: domain.BuildArtifact{ID: "artifact-generic", BuildID: "build-2", LogicalPath: "dist/report.txt", CreatedAt: now.Add(-time.Minute)},
			Build:    domain.Build{ID: "build-2", JobID: &jobID, ProjectID: "project-1", Status: domain.BuildStatusSuccess, CreatedAt: now.Add(-time.Minute)},
		},
	}})

	items, err := svc.ListArtifacts(context.Background(), ListArtifactsInput{Type: string(domain.ArtifactTypeDockerImage)})
	if err != nil {
		t.Fatalf("ListArtifacts returned error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 filtered artifact group, got %d", len(items))
	}
	if items[0].ArtifactType != domain.ArtifactTypeDockerImage {
		t.Fatalf("expected docker image type, got %q", items[0].ArtifactType)
	}

	if _, err := svc.ListArtifacts(context.Background(), ListArtifactsInput{Type: "bad-type"}); err != ErrInvalidArtifactTypeFilter {
		t.Fatalf("expected ErrInvalidArtifactTypeFilter, got %v", err)
	}
}
