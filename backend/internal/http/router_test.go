package http

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/http/handler"
	"github.com/radiation/coyote-ci/backend/internal/observability"
	repositorymemory "github.com/radiation/coyote-ci/backend/internal/repository/memory"
	"github.com/radiation/coyote-ci/backend/internal/service"
	buildsvc "github.com/radiation/coyote-ci/backend/internal/service/build"
	webhooksvc "github.com/radiation/coyote-ci/backend/internal/service/webhook"
)

func TestNewRouter_HealthAndNotFound(t *testing.T) {
	buildRepo := repositorymemory.NewBuildRepository()
	jobRepo := repositorymemory.NewJobRepository()
	buildSvc := buildsvc.NewBuildService(buildRepo, nil, nil)
	h := handler.NewBuildHandler(buildSvc)
	jobSvc := service.NewJobService(jobRepo, buildSvc)
	jh := handler.NewJobHandler(jobSvc)
	eh := handler.NewEventHandler(jobSvc, webhooksvc.NewDeliveryIngressService(repositorymemory.NewWebhookDeliveryRepository(), jobSvc), observability.NewNoopWebhookIngressMetrics(), "")
	r := NewRouter(h, jh, nil, eh, "")

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
	buildRepo := repositorymemory.NewBuildRepository()
	jobRepo := repositorymemory.NewJobRepository()
	buildSvc := buildsvc.NewBuildService(buildRepo, nil, nil)
	h := handler.NewBuildHandler(buildSvc)
	jobSvc := service.NewJobService(jobRepo, buildSvc)
	jh := handler.NewJobHandler(jobSvc)
	eh := handler.NewEventHandler(jobSvc, webhooksvc.NewDeliveryIngressService(repositorymemory.NewWebhookDeliveryRepository(), jobSvc), observability.NewNoopWebhookIngressMetrics(), "")
	r := NewRouter(h, jh, nil, eh, "")

	createReq := httptest.NewRequest(http.MethodPost, "/api/builds/", bytes.NewBufferString(`{"project_id":"project-1"}`))
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
		{name: "list builds", method: http.MethodGet, path: "/api/builds/", statusCode: http.StatusOK},
		{name: "get build", method: http.MethodGet, path: "/api/builds/" + id, statusCode: http.StatusOK},
		{name: "build steps", method: http.MethodGet, path: "/api/builds/" + id + "/steps", statusCode: http.StatusOK},
		{name: "build step logs", method: http.MethodGet, path: "/api/builds/" + id + "/steps/0/logs", statusCode: http.StatusOK},
		{name: "build logs", method: http.MethodGet, path: "/api/builds/" + id + "/logs", statusCode: http.StatusOK},
		{name: "build artifacts", method: http.MethodGet, path: "/api/builds/" + id + "/artifacts", statusCode: http.StatusOK},
		{name: "build artifact download missing", method: http.MethodGet, path: "/api/builds/" + id + "/artifacts/missing/download", statusCode: http.StatusNotFound},
		{name: "queue build", method: http.MethodPost, path: "/api/builds/" + id + "/queue", statusCode: http.StatusOK},
		{name: "start build", method: http.MethodPost, path: "/api/builds/" + id + "/start", statusCode: http.StatusOK},
		{name: "complete build", method: http.MethodPost, path: "/api/builds/" + id + "/complete", statusCode: http.StatusOK},
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

	failReq := httptest.NewRequest(http.MethodPost, "/api/builds/"+id+"/fail", nil)
	failRes := httptest.NewRecorder()
	r.ServeHTTP(failRes, failReq)
	if failRes.Code != http.StatusConflict {
		t.Fatalf("expected fail status %d after completion, got %d", http.StatusConflict, failRes.Code)
	}

	cancelReq := httptest.NewRequest(http.MethodPost, "/api/builds/"+id+"/cancel", nil)
	cancelRes := httptest.NewRecorder()
	r.ServeHTTP(cancelRes, cancelReq)
	if cancelRes.Code != http.StatusOK {
		t.Fatalf("expected cancel status %d, got %d", http.StatusOK, cancelRes.Code)
	}
}

func TestNewRouter_QueueBuild_WithTemplate_PersistsTemplateSteps(t *testing.T) {
	buildRepo := repositorymemory.NewBuildRepository()
	jobRepo := repositorymemory.NewJobRepository()
	buildSvc := buildsvc.NewBuildService(buildRepo, nil, nil)
	h := handler.NewBuildHandler(buildSvc)
	jobSvc := service.NewJobService(jobRepo, buildSvc)
	jh := handler.NewJobHandler(jobSvc)
	eh := handler.NewEventHandler(jobSvc, webhooksvc.NewDeliveryIngressService(repositorymemory.NewWebhookDeliveryRepository(), jobSvc), observability.NewNoopWebhookIngressMetrics(), "")
	r := NewRouter(h, jh, nil, eh, "")

	createReq := httptest.NewRequest(http.MethodPost, "/api/builds/", bytes.NewBufferString(`{"project_id":"project-1"}`))
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

	queueReq := httptest.NewRequest(http.MethodPost, "/api/builds/"+buildID+"/queue", bytes.NewBufferString(`{"template":"test"}`))
	queueRes := httptest.NewRecorder()
	r.ServeHTTP(queueRes, queueReq)
	if queueRes.Code != http.StatusOK {
		t.Fatalf("expected queue status %d, got %d", http.StatusOK, queueRes.Code)
	}

	stepsReq := httptest.NewRequest(http.MethodGet, "/api/builds/"+buildID+"/steps", nil)
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
	buildRepo := repositorymemory.NewBuildRepository()
	jobRepo := repositorymemory.NewJobRepository()
	buildSvc := buildsvc.NewBuildService(buildRepo, nil, nil)
	h := handler.NewBuildHandler(buildSvc)
	jobSvc := service.NewJobService(jobRepo, buildSvc)
	jh := handler.NewJobHandler(jobSvc)
	eh := handler.NewEventHandler(jobSvc, webhooksvc.NewDeliveryIngressService(repositorymemory.NewWebhookDeliveryRepository(), jobSvc), observability.NewNoopWebhookIngressMetrics(), "")
	r := NewRouter(h, jh, nil, eh, "")

	createReq := httptest.NewRequest(http.MethodPost, "/api/builds/", bytes.NewBufferString(`{"project_id":"project-1"}`))
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

	queueReq := httptest.NewRequest(http.MethodPost, "/api/builds/"+buildID+"/queue", bytes.NewBufferString(`{"template":"not-a-template"}`))
	queueRes := httptest.NewRecorder()
	r.ServeHTTP(queueRes, queueReq)
	if queueRes.Code != http.StatusOK {
		t.Fatalf("expected queue status %d, got %d", http.StatusOK, queueRes.Code)
	}

	stepsReq := httptest.NewRequest(http.MethodGet, "/api/builds/"+buildID+"/steps", nil)
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

func TestNewRouter_JobRoutes(t *testing.T) {
	buildRepo := repositorymemory.NewBuildRepository()
	jobRepo := repositorymemory.NewJobRepository()
	buildSvc := buildsvc.NewBuildService(buildRepo, nil, nil)
	h := handler.NewBuildHandler(buildSvc)
	jobSvc := service.NewJobService(jobRepo, buildSvc)
	jh := handler.NewJobHandler(jobSvc)
	eh := handler.NewEventHandler(jobSvc, webhooksvc.NewDeliveryIngressService(repositorymemory.NewWebhookDeliveryRepository(), jobSvc), observability.NewNoopWebhookIngressMetrics(), "")
	r := NewRouter(h, jh, nil, eh, "")

	createBody := `{"project_id":"project-1","name":"backend-ci","repository_url":"https://github.com/example/backend.git","default_ref":"main","pipeline_yaml":"version: 1\nsteps:\n  - name: test\n    run: go test ./...\n","enabled":true}`
	createReq := httptest.NewRequest(http.MethodPost, "/api/jobs/", bytes.NewBufferString(createBody))
	createRes := httptest.NewRecorder()
	r.ServeHTTP(createRes, createReq)
	if createRes.Code != http.StatusCreated {
		t.Fatalf("expected create job status %d, got %d", http.StatusCreated, createRes.Code)
	}

	var createPayload map[string]any
	if err := json.Unmarshal(createRes.Body.Bytes(), &createPayload); err != nil {
		t.Fatalf("parse create job response failed: %v", err)
	}
	data, ok := createPayload["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected create response data object, got %T", createPayload["data"])
	}
	jobID, ok := data["id"].(string)
	if !ok || jobID == "" {
		t.Fatalf("expected create response job id string, got %v", data["id"])
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/jobs/", nil)
	listRes := httptest.NewRecorder()
	r.ServeHTTP(listRes, listReq)
	if listRes.Code != http.StatusOK {
		t.Fatalf("expected list jobs status %d, got %d", http.StatusOK, listRes.Code)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/jobs/"+jobID, nil)
	getRes := httptest.NewRecorder()
	r.ServeHTTP(getRes, getReq)
	if getRes.Code != http.StatusOK {
		t.Fatalf("expected get job status %d, got %d", http.StatusOK, getRes.Code)
	}

	runReq := httptest.NewRequest(http.MethodPost, "/api/jobs/"+jobID+"/run", nil)
	runRes := httptest.NewRecorder()
	r.ServeHTTP(runRes, runReq)
	if runRes.Code != http.StatusCreated {
		t.Fatalf("expected run-now status %d, got %d", http.StatusCreated, runRes.Code)
	}
}

func TestNewRouter_PushEventRoute(t *testing.T) {
	buildRepo := repositorymemory.NewBuildRepository()
	jobRepo := repositorymemory.NewJobRepository()
	buildSvc := buildsvc.NewBuildService(buildRepo, nil, nil)
	h := handler.NewBuildHandler(buildSvc)
	jobSvc := service.NewJobService(jobRepo, buildSvc)
	jh := handler.NewJobHandler(jobSvc)
	eh := handler.NewEventHandler(jobSvc, webhooksvc.NewDeliveryIngressService(repositorymemory.NewWebhookDeliveryRepository(), jobSvc), observability.NewNoopWebhookIngressMetrics(), "")
	r := NewRouter(h, jh, nil, eh, "")

	createBody := `{"project_id":"project-1","name":"backend-ci","repository_url":"https://github.com/example/backend.git","default_ref":"main","push_enabled":true,"push_branch":"main","pipeline_yaml":"version: 1\nsteps:\n  - name: test\n    run: go test ./...\n","enabled":true}`
	createReq := httptest.NewRequest(http.MethodPost, "/api/jobs/", bytes.NewBufferString(createBody))
	createRes := httptest.NewRecorder()
	r.ServeHTTP(createRes, createReq)
	if createRes.Code != http.StatusCreated {
		t.Fatalf("expected create job status %d, got %d", http.StatusCreated, createRes.Code)
	}

	eventBody := `{"repository_url":"https://github.com/example/backend.git","ref":"main","commit_sha":"abc123"}`
	eventReq := httptest.NewRequest(http.MethodPost, "/api/events/push", bytes.NewBufferString(eventBody))
	eventRes := httptest.NewRecorder()
	r.ServeHTTP(eventRes, eventReq)
	if eventRes.Code != http.StatusOK {
		t.Fatalf("expected push event status %d, got %d body=%s", http.StatusOK, eventRes.Code, eventRes.Body.String())
	}
}
