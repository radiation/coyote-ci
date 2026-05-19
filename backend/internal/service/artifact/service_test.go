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

type fakeCatalogRepo struct {
	records []domain.ArtifactRecord
	record  domain.ArtifactRecord
	err     error
	params  []repository.ArtifactCatalogParams
	ids     []string
}

type fakeBrowseCatalogRepo struct {
	fakeBrowseRepo
	fakeCatalogRepo
}

func (r *fakeBrowseRepo) Browse(_ context.Context, params repository.BrowseArtifactsParams) ([]domain.ArtifactBrowseRecord, error) {
	if r.err != nil {
		return nil, r.err
	}
	if params.Type == "" {
		return r.records, nil
	}
	filtered := make([]domain.ArtifactBrowseRecord, 0, len(r.records))
	for _, record := range r.records {
		if domain.ResolveArtifactType(record.Artifact) == params.Type {
			filtered = append(filtered, record)
		}
	}
	return filtered, nil
}

func (r *fakeCatalogRepo) ListCatalog(_ context.Context, params repository.ArtifactCatalogParams) ([]domain.ArtifactRecord, error) {
	r.params = append(r.params, params)
	if r.err != nil {
		return nil, r.err
	}
	return r.records, nil
}

func (r *fakeCatalogRepo) GetCatalogByID(_ context.Context, artifactID string) (domain.ArtifactRecord, error) {
	r.ids = append(r.ids, artifactID)
	if r.err != nil {
		return domain.ArtifactRecord{}, r.err
	}
	return r.record, nil
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

func TestNewServiceAssignsCatalogRepositoryWhenAvailable(t *testing.T) {
	repo := &fakeBrowseCatalogRepo{}
	svc := NewService(repo)

	if svc.repo != repo {
		t.Fatalf("expected browse repo to be wired")
	}
	if svc.catalogRepo != repo {
		t.Fatalf("expected catalog repo to be wired from browse repo")
	}
}

func TestServiceListCatalogDelegatesAndTrimsParams(t *testing.T) {
	repo := &fakeCatalogRepo{records: []domain.ArtifactRecord{{Artifact: domain.BuildArtifact{ID: "artifact-1"}}}}
	svc := &Service{catalogRepo: repo}

	records, err := svc.ListCatalog(context.Background(), ListCatalogInput{
		Query:     "  pkg  ",
		ProjectID: "  project-1  ",
		JobID:     "  job-1  ",
		BuildID:   "  build-1  ",
		Limit:     5,
		Offset:    10,
	})
	if err != nil {
		t.Fatalf("ListCatalog returned error: %v", err)
	}
	if len(records) != 1 || records[0].Artifact.ID != "artifact-1" {
		t.Fatalf("unexpected records: %#v", records)
	}
	if len(repo.params) != 1 {
		t.Fatalf("expected one catalog call, got %d", len(repo.params))
	}
	if repo.params[0].Query != "pkg" || repo.params[0].ProjectID != "project-1" || repo.params[0].JobID != "job-1" || repo.params[0].BuildID != "build-1" || repo.params[0].Limit != 5 || repo.params[0].Offset != 10 {
		t.Fatalf("unexpected params: %#v", repo.params[0])
	}
}

func TestServiceListArtifactsRequiresRepository(t *testing.T) {
	svc := &Service{}

	_, err := svc.ListArtifacts(context.Background(), ListArtifactsInput{})
	if err != ErrArtifactRepositoryNotConfigured {
		t.Fatalf("expected ErrArtifactRepositoryNotConfigured, got %v", err)
	}
}

func TestServiceListArtifactsReturnsRepositoryError(t *testing.T) {
	wantErr := repository.ErrArtifactNotFound
	svc := NewService(&fakeBrowseRepo{err: wantErr})

	_, err := svc.ListArtifacts(context.Background(), ListArtifactsInput{})
	if err != wantErr {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}

func TestServiceListCatalogReturnsRepositoryError(t *testing.T) {
	wantErr := repository.ErrArtifactNotFound
	svc := &Service{catalogRepo: &fakeCatalogRepo{err: wantErr}}

	_, err := svc.ListCatalog(context.Background(), ListCatalogInput{})
	if err != wantErr {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}

func TestServiceGetArtifactDelegatesAndTrimsID(t *testing.T) {
	repo := &fakeCatalogRepo{record: domain.ArtifactRecord{Artifact: domain.BuildArtifact{ID: "artifact-1"}}}
	svc := &Service{catalogRepo: repo}

	record, err := svc.GetArtifact(context.Background(), "  artifact-1  ")
	if err != nil {
		t.Fatalf("GetArtifact returned error: %v", err)
	}
	if record.Artifact.ID != "artifact-1" {
		t.Fatalf("expected artifact-1, got %#v", record)
	}
	if len(repo.ids) != 1 || repo.ids[0] != "artifact-1" {
		t.Fatalf("expected trimmed artifact id lookup, got %v", repo.ids)
	}
}

func TestServiceGetArtifactReturnsRepositoryError(t *testing.T) {
	wantErr := repository.ErrArtifactNotFound
	svc := &Service{catalogRepo: &fakeCatalogRepo{err: wantErr}}

	_, err := svc.GetArtifact(context.Background(), "artifact-1")
	if err != wantErr {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}

func TestServiceCatalogMethodsRequireCatalogRepository(t *testing.T) {
	svc := NewService(&fakeBrowseRepo{})

	if _, err := svc.ListCatalog(context.Background(), ListCatalogInput{}); err != ErrArtifactRepositoryNotConfigured {
		t.Fatalf("expected ErrArtifactRepositoryNotConfigured from ListCatalog, got %v", err)
	}
	if _, err := svc.GetArtifact(context.Background(), "artifact-1"); err != ErrArtifactRepositoryNotConfigured {
		t.Fatalf("expected ErrArtifactRepositoryNotConfigured from GetArtifact, got %v", err)
	}
}
