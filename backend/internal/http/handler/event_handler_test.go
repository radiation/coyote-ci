package handler

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	repositorymemory "github.com/radiation/coyote-ci/backend/internal/repository/memory"
	"github.com/radiation/coyote-ci/backend/internal/service"
)

func TestEventHandler_IngestPushEvent(t *testing.T) {
	buildRepo := repositorymemory.NewBuildRepository()
	jobRepo := repositorymemory.NewJobRepository()
	buildSvc := service.NewBuildService(buildRepo, nil, nil)
	jobSvc := service.NewJobService(jobRepo, buildSvc)
	h := NewEventHandler(jobSvc)

	_, err := jobSvc.CreateJob(context.Background(), service.CreateJobInput{
		ProjectID:     "project-1",
		Name:          "backend-ci",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PushEnabled:   boolPtr(true),
		PushBranch:    strPtr("main"),
		PipelineYAML:  "version: 1\nsteps:\n  - name: test\n    run: go test ./...\n",
		Enabled:       boolPtr(true),
	})
	if err != nil {
		t.Fatalf("create job failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/events/push", bytes.NewBufferString(`{"repository_url":"https://github.com/example/backend.git","ref":"refs/heads/main","commit_sha":"abc123"}`))
	res := httptest.NewRecorder()
	h.IngestPushEvent(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusOK, res.Code, res.Body.String())
	}

	data := decodeDataMap(t, res)
	if data["matched_jobs"] != float64(1) {
		t.Fatalf("expected matched_jobs=1, got %v", data["matched_jobs"])
	}
	if data["created_builds"] != float64(1) {
		t.Fatalf("expected created_builds=1, got %v", data["created_builds"])
	}
	if data["ref"] != "main" {
		t.Fatalf("expected normalized ref main, got %v", data["ref"])
	}
}

func TestEventHandler_IngestPushEvent_BadRequest(t *testing.T) {
	buildRepo := repositorymemory.NewBuildRepository()
	jobRepo := repositorymemory.NewJobRepository()
	h := NewEventHandler(service.NewJobService(jobRepo, service.NewBuildService(buildRepo, nil, nil)))

	req := httptest.NewRequest(http.MethodPost, "/events/push", bytes.NewBufferString(`{"repository_url":"","ref":"","commit_sha":""}`))
	res := httptest.NewRecorder()
	h.IngestPushEvent(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, res.Code)
	}
}

func boolPtr(v bool) *bool    { return &v }
func strPtr(v string) *string { return &v }
