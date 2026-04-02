package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	repositorymemory "github.com/radiation/coyote-ci/backend/internal/repository/memory"
	"github.com/radiation/coyote-ci/backend/internal/service"
)

func TestJobHandler_CreateListGetUpdateRunNow(t *testing.T) {
	buildRepo := repositorymemory.NewBuildRepository()
	jobRepo := repositorymemory.NewJobRepository()
	buildSvc := service.NewBuildService(buildRepo, nil, nil)
	jobSvc := service.NewJobService(jobRepo, buildSvc)
	h := NewJobHandler(jobSvc)

	createBody := `{"project_id":"project-1","name":"backend-ci","repository_url":"https://github.com/example/backend.git","default_ref":"main","push_enabled":true,"push_branch":"main","pipeline_yaml":"version: 1\nsteps:\n  - name: test\n    run: go test ./...\n","enabled":true}`
	createReq := httptest.NewRequest(http.MethodPost, "/jobs", bytes.NewBufferString(createBody))
	createRes := httptest.NewRecorder()
	h.CreateJob(createRes, createReq)
	if createRes.Code != http.StatusCreated {
		t.Fatalf("expected create status %d, got %d", http.StatusCreated, createRes.Code)
	}

	createData := decodeDataMap(t, createRes)
	if createData["push_enabled"] != true {
		t.Fatalf("expected push_enabled true, got %v", createData["push_enabled"])
	}
	if createData["push_branch"] != "main" {
		t.Fatalf("expected push_branch main, got %v", createData["push_branch"])
	}
	jobID, ok := createData["id"].(string)
	if !ok || jobID == "" {
		t.Fatalf("expected created job id, got %v", createData["id"])
	}

	listReq := httptest.NewRequest(http.MethodGet, "/jobs", nil)
	listRes := httptest.NewRecorder()
	h.ListJobs(listRes, listReq)
	if listRes.Code != http.StatusOK {
		t.Fatalf("expected list status %d, got %d", http.StatusOK, listRes.Code)
	}

	listData := decodeDataMap(t, listRes)
	jobs, ok := listData["jobs"].([]any)
	if !ok || len(jobs) != 1 {
		t.Fatalf("expected one job in list, got %v", listData["jobs"])
	}

	getReq := addURLParam(httptest.NewRequest(http.MethodGet, "/jobs/"+jobID, nil), "jobID", jobID)
	getRes := httptest.NewRecorder()
	h.GetJob(getRes, getReq)
	if getRes.Code != http.StatusOK {
		t.Fatalf("expected get status %d, got %d", http.StatusOK, getRes.Code)
	}

	updateBody := `{"enabled":false,"push_enabled":false,"push_branch":""}`
	updateReq := addURLParam(httptest.NewRequest(http.MethodPut, "/jobs/"+jobID, bytes.NewBufferString(updateBody)), "jobID", jobID)
	updateRes := httptest.NewRecorder()
	h.UpdateJob(updateRes, updateReq)
	if updateRes.Code != http.StatusOK {
		t.Fatalf("expected update status %d, got %d", http.StatusOK, updateRes.Code)
	}
	updateData := decodeDataMap(t, updateRes)
	if updateData["push_enabled"] != false {
		t.Fatalf("expected push_enabled false after update, got %v", updateData["push_enabled"])
	}

	runReq := addURLParam(httptest.NewRequest(http.MethodPost, "/jobs/"+jobID+"/run", nil), "jobID", jobID)
	runRes := httptest.NewRecorder()
	h.RunNow(runRes, runReq)
	if runRes.Code != http.StatusConflict {
		t.Fatalf("expected disabled run status %d, got %d", http.StatusConflict, runRes.Code)
	}

	enableBody := `{"enabled":true}`
	enableReq := addURLParam(httptest.NewRequest(http.MethodPut, "/jobs/"+jobID, bytes.NewBufferString(enableBody)), "jobID", jobID)
	enableRes := httptest.NewRecorder()
	h.UpdateJob(enableRes, enableReq)
	if enableRes.Code != http.StatusOK {
		t.Fatalf("expected re-enable status %d, got %d", http.StatusOK, enableRes.Code)
	}

	runReq = addURLParam(httptest.NewRequest(http.MethodPost, "/jobs/"+jobID+"/run", nil), "jobID", jobID)
	runRes = httptest.NewRecorder()
	h.RunNow(runRes, runReq)
	if runRes.Code != http.StatusCreated {
		t.Fatalf("expected run-now status %d, got %d", http.StatusCreated, runRes.Code)
	}

	runPayload := decodeDataMap(t, runRes)
	if runPayload["status"] != "queued" {
		t.Fatalf("expected queued build from run-now, got %v", runPayload["status"])
	}
	if runPayload["repo_url"] != "https://github.com/example/backend.git" {
		t.Fatalf("expected build repo_url from job, got %v", runPayload["repo_url"])
	}
	if runPayload["ref"] != "main" {
		t.Fatalf("expected build ref from job, got %v", runPayload["ref"])
	}
}

func addURLParam(req *http.Request, key string, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	return req.WithContext(ctx)
}

func TestJobHandler_CreateRejectsInvalidPipeline(t *testing.T) {
	buildRepo := repositorymemory.NewBuildRepository()
	jobRepo := repositorymemory.NewJobRepository()
	h := NewJobHandler(service.NewJobService(jobRepo, service.NewBuildService(buildRepo, nil, nil)))

	body := `{"project_id":"project-1","name":"bad","repository_url":"https://github.com/example/backend.git","default_ref":"main","pipeline_yaml":"version: 2\nsteps:\n  - name: test\n    run: go test ./...\n"}`
	req := httptest.NewRequest(http.MethodPost, "/jobs", bytes.NewBufferString(body))
	res := httptest.NewRecorder()
	h.CreateJob(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, res.Code)
	}

	var payload map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if payload["error"] == nil {
		t.Fatalf("expected error response, got %v", payload)
	}
}
