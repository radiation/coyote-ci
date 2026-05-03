package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/repository/memory"
	"github.com/radiation/coyote-ci/backend/internal/service"
	buildsvc "github.com/radiation/coyote-ci/backend/internal/service/build"
)

func TestProjectHandler_CreateListGetUpdateDeleteAndJobs(t *testing.T) {
	jobRepo := memory.NewJobRepository()
	projectRepo := memory.NewProjectRepository(jobRepo)
	buildRepo := memory.NewBuildRepository()
	jobService := service.NewJobService(jobRepo, buildsvc.NewBuildService(buildRepo, nil, nil)).WithProjectRepository(projectRepo)
	projectService := service.NewProjectService(projectRepo)
	h := NewProjectHandler(projectService, jobService)

	createReq := httptest.NewRequest(http.MethodPost, "/projects", bytes.NewBufferString(`{"name":"Platform","slug":"platform","description":"Platform pipelines"}`))
	createRes := httptest.NewRecorder()
	h.CreateProject(createRes, createReq)
	if createRes.Code != http.StatusCreated {
		t.Fatalf("expected create status %d, got %d", http.StatusCreated, createRes.Code)
	}
	created := decodeDataMap(t, createRes)
	projectID, _ := created["id"].(string)

	listReq := httptest.NewRequest(http.MethodGet, "/projects", nil)
	listRes := httptest.NewRecorder()
	h.ListProjects(listRes, listReq)
	if listRes.Code != http.StatusOK {
		t.Fatalf("expected list status %d, got %d", http.StatusOK, listRes.Code)
	}

	getReq := addURLParam(httptest.NewRequest(http.MethodGet, "/projects/"+projectID, nil), "id", projectID)
	getRes := httptest.NewRecorder()
	h.GetProject(getRes, getReq)
	if getRes.Code != http.StatusOK {
		t.Fatalf("expected get status %d, got %d", http.StatusOK, getRes.Code)
	}

	_, err := jobService.CreateJob(context.Background(), service.CreateJobInput{
		ProjectID:     projectID,
		Name:          "backend-ci",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PipelineYAML:  "version: 1\nsteps:\n  - name: test\n    run: go test ./...\n",
	})
	if err != nil {
		t.Fatalf("create job failed: %v", err)
	}

	jobsReq := addURLParam(httptest.NewRequest(http.MethodGet, "/projects/"+projectID+"/jobs", nil), "id", projectID)
	jobsRes := httptest.NewRecorder()
	h.ListProjectJobs(jobsRes, jobsReq)
	if jobsRes.Code != http.StatusOK {
		t.Fatalf("expected project jobs status %d, got %d", http.StatusOK, jobsRes.Code)
	}
	var jobsPayload map[string]any
	if err := json.Unmarshal(jobsRes.Body.Bytes(), &jobsPayload); err != nil {
		t.Fatalf("decode project jobs response failed: %v", err)
	}
	data, ok := jobsPayload["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected jobs response data object, got %T", jobsPayload["data"])
	}
	jobs, ok := data["jobs"].([]any)
	if !ok {
		t.Fatalf("expected jobs response list, got %T", data["jobs"])
	}
	if len(jobs) != 1 {
		t.Fatalf("expected 1 project job, got %d", len(jobs))
	}

	deleteReq := addURLParam(httptest.NewRequest(http.MethodDelete, "/projects/"+projectID, nil), "id", projectID)
	deleteRes := httptest.NewRecorder()
	h.DeleteProject(deleteRes, deleteReq)
	if deleteRes.Code != http.StatusConflict {
		t.Fatalf("expected delete conflict status %d, got %d", http.StatusConflict, deleteRes.Code)
	}

	updateReq := addURLParam(httptest.NewRequest(http.MethodPatch, "/projects/"+projectID, bytes.NewBufferString(`{"name":"Platform CI","slug":"platform-ci"}`)), "id", projectID)
	updateRes := httptest.NewRecorder()
	h.UpdateProject(updateRes, updateReq)
	if updateRes.Code != http.StatusOK {
		t.Fatalf("expected update status %d, got %d", http.StatusOK, updateRes.Code)
	}
}
