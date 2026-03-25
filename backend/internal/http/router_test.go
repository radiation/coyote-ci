package http

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/http/handler"
	repositorymemory "github.com/radiation/coyote-ci/backend/internal/repository/memory"
	"github.com/radiation/coyote-ci/backend/internal/service"
)

func TestNewRouter_HealthAndNotFound(t *testing.T) {
	h := handler.NewBuildHandler(service.NewBuildService(repositorymemory.NewBuildRepository(), nil, nil))
	r := NewRouter(h)

	tests := []struct {
		name       string
		method     string
		path       string
		statusCode int
		body       string
	}{
		{name: "health", method: http.MethodGet, path: "/health", statusCode: http.StatusOK, body: "ok"},
		{name: "healthz", method: http.MethodGet, path: "/healthz", statusCode: http.StatusOK, body: "ok"},
		{name: "not found", method: http.MethodGet, path: "/missing", statusCode: http.StatusNotFound},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rr := httptest.NewRecorder()
			r.ServeHTTP(rr, req)
			if rr.Code != tc.statusCode {
				t.Fatalf("expected status %d, got %d", tc.statusCode, rr.Code)
			}
			if tc.body != "" && rr.Body.String() != tc.body {
				t.Fatalf("expected body %q, got %q", tc.body, rr.Body.String())
			}
		})
	}
}

func TestNewRouter_BuildRoutes(t *testing.T) {
	h := handler.NewBuildHandler(service.NewBuildService(repositorymemory.NewBuildRepository(), nil, nil))
	r := NewRouter(h)

	createReq := httptest.NewRequest(http.MethodPost, "/builds/", bytes.NewBufferString(`{"project_id":"project-1"}`))
	createRes := httptest.NewRecorder()
	r.ServeHTTP(createRes, createReq)
	if createRes.Code != http.StatusCreated {
		t.Fatalf("expected create status %d, got %d", http.StatusCreated, createRes.Code)
	}

	var createBody map[string]any
	if err := json.Unmarshal(createRes.Body.Bytes(), &createBody); err != nil {
		t.Fatalf("failed to parse create response: %v", err)
	}
	createData, ok := createBody["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected create data envelope, got %v", createBody)
	}
	id, ok := createData["id"].(string)
	if !ok || id == "" {
		t.Fatalf("expected create response id, got %v", createData["id"])
	}

	tests := []struct {
		name       string
		method     string
		path       string
		statusCode int
	}{
		{name: "list builds", method: http.MethodGet, path: "/builds/", statusCode: http.StatusOK},
		{name: "get build", method: http.MethodGet, path: "/builds/" + id, statusCode: http.StatusOK},
		{name: "build steps", method: http.MethodGet, path: "/builds/" + id + "/steps", statusCode: http.StatusOK},
		{name: "build logs", method: http.MethodGet, path: "/builds/" + id + "/logs", statusCode: http.StatusOK},
		{name: "queue build", method: http.MethodPost, path: "/builds/" + id + "/queue", statusCode: http.StatusOK},
		{name: "start build", method: http.MethodPost, path: "/builds/" + id + "/start", statusCode: http.StatusOK},
		{name: "complete build", method: http.MethodPost, path: "/builds/" + id + "/complete", statusCode: http.StatusOK},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rr := httptest.NewRecorder()
			r.ServeHTTP(rr, req)
			if rr.Code != tc.statusCode {
				t.Fatalf("expected status %d, got %d", tc.statusCode, rr.Code)
			}
		})
	}

	failReq := httptest.NewRequest(http.MethodPost, "/builds/"+id+"/fail", nil)
	failRes := httptest.NewRecorder()
	r.ServeHTTP(failRes, failReq)
	if failRes.Code != http.StatusConflict {
		t.Fatalf("expected fail status %d after completion, got %d", http.StatusConflict, failRes.Code)
	}
}

func TestNewRouter_QueueBuild_WithTemplate_PersistsTemplateSteps(t *testing.T) {
	h := handler.NewBuildHandler(service.NewBuildService(repositorymemory.NewBuildRepository(), nil, nil))
	r := NewRouter(h)

	createReq := httptest.NewRequest(http.MethodPost, "/builds/", bytes.NewBufferString(`{"project_id":"project-1"}`))
	createRes := httptest.NewRecorder()
	r.ServeHTTP(createRes, createReq)
	if createRes.Code != http.StatusCreated {
		t.Fatalf("expected create status %d, got %d", http.StatusCreated, createRes.Code)
	}

	var createBody map[string]any
	if err := json.Unmarshal(createRes.Body.Bytes(), &createBody); err != nil {
		t.Fatalf("failed to parse create response: %v", err)
	}
	createData, ok := createBody["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected create data envelope, got %v", createBody)
	}
	buildID, ok := createData["id"].(string)
	if !ok || buildID == "" {
		t.Fatalf("expected create response id, got %v", createData["id"])
	}

	queueReq := httptest.NewRequest(http.MethodPost, "/builds/"+buildID+"/queue", bytes.NewBufferString(`{"template":"test"}`))
	queueRes := httptest.NewRecorder()
	r.ServeHTTP(queueRes, queueReq)
	if queueRes.Code != http.StatusOK {
		t.Fatalf("expected queue status %d, got %d", http.StatusOK, queueRes.Code)
	}

	stepsReq := httptest.NewRequest(http.MethodGet, "/builds/"+buildID+"/steps", nil)
	stepsRes := httptest.NewRecorder()
	r.ServeHTTP(stepsRes, stepsReq)
	if stepsRes.Code != http.StatusOK {
		t.Fatalf("expected steps status %d, got %d", http.StatusOK, stepsRes.Code)
	}

	var stepsBody map[string]any
	if err := json.Unmarshal(stepsRes.Body.Bytes(), &stepsBody); err != nil {
		t.Fatalf("failed to parse steps response: %v", err)
	}
	stepsData, ok := stepsBody["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected steps data envelope, got %v", stepsBody)
	}
	steps, ok := stepsData["steps"].([]any)
	if !ok {
		t.Fatalf("expected steps array, got %T", stepsData["steps"])
	}

	expectedNames := []string{"setup", "test", "teardown"}
	if len(steps) != len(expectedNames) {
		t.Fatalf("expected %d steps, got %d", len(expectedNames), len(steps))
	}

	for idx, expectedName := range expectedNames {
		step, ok := steps[idx].(map[string]any)
		if !ok {
			t.Fatalf("expected step object at index %d, got %T", idx, steps[idx])
		}
		if step["step_index"] != float64(idx) {
			t.Fatalf("expected step_index %d, got %v", idx, step["step_index"])
		}
		if step["name"] != expectedName {
			t.Fatalf("expected step name %q, got %v", expectedName, step["name"])
		}
	}
}

func TestNewRouter_QueueBuild_UnknownTemplate_FallsBackToDefaultStep(t *testing.T) {
	h := handler.NewBuildHandler(service.NewBuildService(repositorymemory.NewBuildRepository(), nil, nil))
	r := NewRouter(h)

	createReq := httptest.NewRequest(http.MethodPost, "/builds/", bytes.NewBufferString(`{"project_id":"project-1"}`))
	createRes := httptest.NewRecorder()
	r.ServeHTTP(createRes, createReq)
	if createRes.Code != http.StatusCreated {
		t.Fatalf("expected create status %d, got %d", http.StatusCreated, createRes.Code)
	}

	var createBody map[string]any
	if err := json.Unmarshal(createRes.Body.Bytes(), &createBody); err != nil {
		t.Fatalf("failed to parse create response: %v", err)
	}
	createData, ok := createBody["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected create data envelope, got %v", createBody)
	}
	buildID, ok := createData["id"].(string)
	if !ok || buildID == "" {
		t.Fatalf("expected create response id, got %v", createData["id"])
	}

	queueReq := httptest.NewRequest(http.MethodPost, "/builds/"+buildID+"/queue", bytes.NewBufferString(`{"template":"not-a-template"}`))
	queueRes := httptest.NewRecorder()
	r.ServeHTTP(queueRes, queueReq)
	if queueRes.Code != http.StatusOK {
		t.Fatalf("expected queue status %d, got %d", http.StatusOK, queueRes.Code)
	}

	stepsReq := httptest.NewRequest(http.MethodGet, "/builds/"+buildID+"/steps", nil)
	stepsRes := httptest.NewRecorder()
	r.ServeHTTP(stepsRes, stepsReq)
	if stepsRes.Code != http.StatusOK {
		t.Fatalf("expected steps status %d, got %d", http.StatusOK, stepsRes.Code)
	}

	var stepsBody map[string]any
	if err := json.Unmarshal(stepsRes.Body.Bytes(), &stepsBody); err != nil {
		t.Fatalf("failed to parse steps response: %v", err)
	}
	stepsData, ok := stepsBody["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected steps data envelope, got %v", stepsBody)
	}
	steps, ok := stepsData["steps"].([]any)
	if !ok {
		t.Fatalf("expected steps array, got %T", stepsData["steps"])
	}
	if len(steps) != 1 {
		t.Fatalf("expected 1 default step, got %d", len(steps))
	}

	step, ok := steps[0].(map[string]any)
	if !ok {
		t.Fatalf("expected step object, got %T", steps[0])
	}
	if step["step_index"] != float64(0) {
		t.Fatalf("expected step_index 0, got %v", step["step_index"])
	}
	if step["name"] != "default" {
		t.Fatalf("expected default step name, got %v", step["name"])
	}
}
