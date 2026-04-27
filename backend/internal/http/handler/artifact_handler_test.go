package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	repositorymemory "github.com/radiation/coyote-ci/backend/internal/repository/memory"
	artifactsvc "github.com/radiation/coyote-ci/backend/internal/service/artifact"
	versiontagsvc "github.com/radiation/coyote-ci/backend/internal/service/versiontag"
)

type fakeArtifactBrowseRepo struct {
	records []domain.ArtifactBrowseRecord
	err     error
}

func (r *fakeArtifactBrowseRepo) Create(_ context.Context, artifact domain.BuildArtifact) (domain.BuildArtifact, error) {
	return artifact, nil
}

func (r *fakeArtifactBrowseRepo) ListByBuildID(_ context.Context, _ string) ([]domain.BuildArtifact, error) {
	return nil, nil
}

func (r *fakeArtifactBrowseRepo) ListForBrowse(_ context.Context, _ string) ([]domain.ArtifactBrowseRecord, error) {
	return r.records, r.err
}

func (r *fakeArtifactBrowseRepo) GetByID(_ context.Context, _ string, _ string) (domain.BuildArtifact, error) {
	return domain.BuildArtifact{}, repository.ErrArtifactNotFound
}

func (r *fakeArtifactBrowseRepo) ListByStepID(_ context.Context, _ string) ([]domain.BuildArtifact, error) {
	return nil, nil
}

func TestArtifactHandlerListArtifacts(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	jobID := "job-1"
	repo := &fakeArtifactBrowseRepo{records: []domain.ArtifactBrowseRecord{
		{
			Artifact: domain.BuildArtifact{ID: "artifact-2", BuildID: "build-2", LogicalPath: "packages/pkg-a-1.2.3.tgz", CreatedAt: now.Add(2 * time.Minute), StorageProvider: domain.StorageProviderFilesystem, SizeBytes: 256},
			Build:    domain.Build{ID: "build-2", BuildNumber: 22, JobID: &jobID, ProjectID: "project-1", Status: domain.BuildStatusSuccess, CreatedAt: now.Add(2 * time.Minute)},
			Step:     &domain.BuildStep{ID: "step-2", BuildID: "build-2", StepIndex: 1, Name: "Publish package"},
		},
		{
			Artifact: domain.BuildArtifact{ID: "artifact-1", BuildID: "build-1", LogicalPath: "packages/pkg-a-1.2.2.tgz", CreatedAt: now, StorageProvider: domain.StorageProviderFilesystem, SizeBytes: 128},
			Build:    domain.Build{ID: "build-1", BuildNumber: 21, JobID: &jobID, ProjectID: "project-1", Status: domain.BuildStatusSuccess, CreatedAt: now},
			Step:     &domain.BuildStep{ID: "step-1", BuildID: "build-1", StepIndex: 1, Name: "Publish package"},
		},
	}}
	handler := NewArtifactHandler(artifactsvc.NewService(repo))

	versionTagRepo := repositorymemory.NewVersionTagRepository()
	versionTagRepo.SeedBuilds(repo.records[0].Build, repo.records[1].Build)
	versionTagRepo.SeedArtifacts(repo.records[0].Artifact, repo.records[1].Artifact)
	_, err := versionTagRepo.CreateForTargets(context.Background(), repository.CreateVersionTagsParams{
		JobID:       jobID,
		Version:     "v1.2.3",
		ArtifactIDs: []string{"artifact-2"},
	})
	if err != nil {
		t.Fatalf("CreateForTargets returned error: %v", err)
	}
	handler.SetVersionTagService(versiontagsvc.NewService(versionTagRepo))

	req := httptest.NewRequest(http.MethodGet, "/artifacts?type=npm_package", nil)
	w := httptest.NewRecorder()
	handler.ListArtifacts(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var response struct {
		Data struct {
			Artifacts []map[string]any `json:"artifacts"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(response.Data.Artifacts) != 2 {
		t.Fatalf("expected 2 artifact groups, got %d", len(response.Data.Artifacts))
	}
	first := response.Data.Artifacts[0]
	if first["artifact_type"] != "npm_package" {
		t.Fatalf("expected npm_package type, got %v", first["artifact_type"])
	}
	versions, ok := first["versions"].([]any)
	if !ok {
		t.Fatalf("expected versions to be []any, got %T", first["versions"])
	}
	if len(versions) != 1 {
		t.Fatalf("expected 1 version on first artifact, got %d", len(versions))
	}
	version, ok := versions[0].(map[string]any)
	if !ok {
		t.Fatalf("expected version to be map[string]any, got %T", versions[0])
	}
	tags, ok := version["version_tags"].([]any)
	if !ok {
		t.Fatalf("expected version_tags to be []any, got %T", version["version_tags"])
	}
	if len(tags) != 1 {
		t.Fatalf("expected 1 version tag, got %d", len(tags))
	}
	if version["step_name"] != "Publish package" {
		t.Fatalf("expected step name, got %v", version["step_name"])
	}
}

func TestArtifactHandlerListArtifactsRejectsInvalidType(t *testing.T) {
	handler := NewArtifactHandler(artifactsvc.NewService(&fakeArtifactBrowseRepo{}))
	req := httptest.NewRequest(http.MethodGet, "/artifacts?type=not-real", nil)
	w := httptest.NewRecorder()

	handler.ListArtifacts(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}
