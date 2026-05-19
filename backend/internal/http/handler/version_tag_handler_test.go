package handler

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	repositorymemory "github.com/radiation/coyote-ci/backend/internal/repository/memory"
	versiontagsvc "github.com/radiation/coyote-ci/backend/internal/service/versiontag"
)

func TestVersionTagHandler_CreateAndList(t *testing.T) {
	repo := repositorymemory.NewVersionTagRepository()
	jobID := "job-1"
	buildID := "build-1"
	repo.SeedBuilds(domain.Build{ID: buildID, ProjectID: "project-1", JobID: &jobID})
	repo.SeedArtifacts(domain.BuildArtifact{ID: "artifact-1", BuildID: buildID})
	repo.SeedManagedImages(domain.ManagedImage{ID: "image-1", ProjectID: "project-1", Name: "go"})
	repo.SeedManagedImageVersions(domain.ManagedImageVersion{ID: "image-version-1", ManagedImageID: "image-1"})
	h := NewVersionTagHandler(versiontagsvc.NewService(repo))

	createReq := httptest.NewRequest(http.MethodPost, "/jobs/job-1/version-tags", bytes.NewBufferString(`{"kind":"version","version":"1.2.3","artifact_ids":["artifact-1"],"managed_image_version_ids":["image-version-1"]}`))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("jobID", jobID)
	createReq = createReq.WithContext(context.WithValue(createReq.Context(), chi.RouteCtxKey, rctx))
	createRes := httptest.NewRecorder()
	h.CreateJobVersionTags(createRes, createReq)
	if createRes.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", createRes.Code, createRes.Body.String())
	}
	createData := decodeDataMap(t, createRes)
	tags, ok := createData["tags"].([]any)
	if !ok || len(tags) != 2 {
		t.Fatalf("expected 2 created tags, got %v", createData["tags"])
	}
	firstTag, ok := tags[0].(map[string]any)
	if !ok || firstTag["kind"] != "version" {
		t.Fatalf("expected created tag kind version, got %v", tags[0])
	}

	listReq := httptest.NewRequest(http.MethodGet, "/artifacts/artifact-1/version-tags", nil)
	rctx = chi.NewRouteContext()
	rctx.URLParams.Add("artifactID", "artifact-1")
	listReq = listReq.WithContext(context.WithValue(listReq.Context(), chi.RouteCtxKey, rctx))
	listRes := httptest.NewRecorder()
	h.ListArtifactVersionTags(listRes, listReq)
	if listRes.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", listRes.Code)
	}
	listData := decodeDataMap(t, listRes)
	artifactTags, ok := listData["tags"].([]any)
	if !ok || len(artifactTags) != 1 {
		t.Fatalf("expected 1 artifact tag, got %v", listData["tags"])
	}
}

func TestVersionTagHandler_CreateConflict(t *testing.T) {
	repo := repositorymemory.NewVersionTagRepository()
	jobID := "job-1"
	buildID := "build-1"
	repo.SeedBuilds(domain.Build{ID: buildID, JobID: &jobID})
	repo.SeedArtifacts(domain.BuildArtifact{ID: "artifact-1", BuildID: buildID})
	service := versiontagsvc.NewService(repo)
	_, _ = service.CreateVersionTags(context.Background(), jobID, versiontagsvc.CreateVersionTagsInput{Kind: "version", Version: "1.2.3", ArtifactIDs: []string{"artifact-1"}})
	h := NewVersionTagHandler(service)

	req := httptest.NewRequest(http.MethodPost, "/jobs/job-1/version-tags", bytes.NewBufferString(`{"kind":"version","version":"1.2.3","artifact_ids":["artifact-1"]}`))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("jobID", jobID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	res := httptest.NewRecorder()
	h.CreateJobVersionTags(res, req)
	if res.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", res.Code)
	}
}

func TestVersionTagHandler_CreateNotFound(t *testing.T) {
	repo := repositorymemory.NewVersionTagRepository()
	h := NewVersionTagHandler(versiontagsvc.NewService(repo))

	req := httptest.NewRequest(http.MethodPost, "/jobs/job-1/version-tags", bytes.NewBufferString(`{"kind":"version","version":"1.2.3","artifact_ids":["missing-artifact"]}`))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("jobID", "job-1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	res := httptest.NewRecorder()
	h.CreateJobVersionTags(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", res.Code, res.Body.String())
	}
}

func TestVersionTagHandler_Create_DefaultsUnknownOmittedKindToVersion(t *testing.T) {
	artifactRepo := repositorymemory.NewArtifactLabelRepository()
	jobID := "job-1"
	buildID := "build-1"
	artifactRepo.SeedBuilds(domain.Build{ID: buildID, JobID: &jobID})
	artifactRepo.SeedArtifacts(domain.BuildArtifact{ID: "artifact-1", BuildID: buildID, LogicalPath: "packages/pkg-a.tgz"})
	h := NewVersionTagHandler(versiontagsvc.NewService(nil).WithArtifactLabels(artifactRepo))

	req := httptest.NewRequest(http.MethodPost, "/jobs/job-1/version-tags", bytes.NewBufferString(`{"version":"release-42","artifact_ids":["artifact-1"]}`))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("jobID", jobID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	res := httptest.NewRecorder()
	h.CreateJobVersionTags(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", res.Code, res.Body.String())
	}
	data := decodeDataMap(t, res)
	tags, ok := data["tags"].([]any)
	if !ok || len(tags) != 1 {
		t.Fatalf("expected 1 created tag, got %v", data["tags"])
	}
	firstTag, ok := tags[0].(map[string]any)
	if !ok || firstTag["kind"] != "version" {
		t.Fatalf("expected inferred version kind, got %v", tags[0])
	}
}

func TestVersionTagHandler_Create_ArtifactChannelsRequireArtifactLabelRepository(t *testing.T) {
	repo := repositorymemory.NewVersionTagRepository()
	jobID := "job-1"
	buildID := "build-1"
	repo.SeedBuilds(domain.Build{ID: buildID, JobID: &jobID})
	repo.SeedArtifacts(domain.BuildArtifact{ID: "artifact-1", BuildID: buildID})
	h := NewVersionTagHandler(versiontagsvc.NewService(repo))

	req := httptest.NewRequest(http.MethodPost, "/jobs/job-1/version-tags", bytes.NewBufferString(`{"kind":"channel","version":"prod","artifact_ids":["artifact-1"]}`))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("jobID", jobID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	res := httptest.NewRecorder()
	h.CreateJobVersionTags(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", res.Code, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), "artifact channel labels require artifact label repository") {
		t.Fatalf("expected specific artifact channel error, got %s", res.Body.String())
	}
}
