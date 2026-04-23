package handler

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
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
	repo.SeedBuilds(domain.Build{ID: buildID, JobID: &jobID})
	repo.SeedArtifacts(domain.BuildArtifact{ID: "artifact-1", BuildID: buildID})
	repo.SeedManagedImageVersions(domain.ManagedImageVersion{ID: "image-version-1", ManagedImageID: "image-1"})
	h := NewVersionTagHandler(versiontagsvc.NewService(repo))

	createReq := httptest.NewRequest(http.MethodPost, "/jobs/job-1/version-tags", bytes.NewBufferString(`{"version":"v1","artifact_ids":["artifact-1"],"managed_image_version_ids":["image-version-1"]}`))
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
	_, _ = service.CreateVersionTags(context.Background(), jobID, versiontagsvc.CreateVersionTagsInput{Version: "v1", ArtifactIDs: []string{"artifact-1"}})
	h := NewVersionTagHandler(service)

	req := httptest.NewRequest(http.MethodPost, "/jobs/job-1/version-tags", bytes.NewBufferString(`{"version":"v1","artifact_ids":["artifact-1"]}`))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("jobID", jobID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	res := httptest.NewRecorder()
	h.CreateJobVersionTags(res, req)
	if res.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", res.Code)
	}
}
