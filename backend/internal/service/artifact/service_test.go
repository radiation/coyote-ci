package artifact

import (
	"context"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type fakeBrowseRepo struct {
	records []domain.ArtifactBrowseRecord
	err     error
}

func (r *fakeBrowseRepo) Browse(_ context.Context, _ repository.BrowseArtifactsParams) ([]domain.ArtifactBrowseRecord, error) {
	return r.records, r.err
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
		{
			Artifact: domain.BuildArtifact{ID: "artifact-unknown", BuildID: "build-3", LogicalPath: "backend/dist/coyote-server", CreatedAt: now.Add(-2 * time.Minute)},
			Build:    domain.Build{ID: "build-3", JobID: &jobID, ProjectID: "project-1", Status: domain.BuildStatusSuccess, CreatedAt: now.Add(-2 * time.Minute)},
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

	items, err = svc.ListArtifacts(context.Background(), ListArtifactsInput{Type: string(domain.ArtifactTypeUnknown)})
	if err != nil {
		t.Fatalf("ListArtifacts returned error for unknown filter: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 unknown artifact group, got %d", len(items))
	}
	if items[0].ArtifactType != domain.ArtifactTypeUnknown {
		t.Fatalf("expected unknown type, got %q", items[0].ArtifactType)
	}
	if items[0].Path != "backend/dist/coyote-server" {
		t.Fatalf("expected backend/dist/coyote-server, got %q", items[0].Path)
	}

	if _, err := svc.ListArtifacts(context.Background(), ListArtifactsInput{Type: "bad-type"}); err != ErrInvalidArtifactTypeFilter {
		t.Fatalf("expected ErrInvalidArtifactTypeFilter, got %v", err)
	}
}
