package http

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/http/handler"
	"github.com/radiation/coyote-ci/backend/internal/service"
	"github.com/radiation/coyote-ci/backend/internal/store/memory"
)

func TestNewRouter_HealthzAndNotFound(t *testing.T) {
	h := handler.NewBuildHandler(service.NewBuildService(memory.NewBuildStore()))
	r := NewRouter(h)

	tests := []struct {
		name       string
		method     string
		path       string
		statusCode int
		body       string
	}{
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
	h := handler.NewBuildHandler(service.NewBuildService(memory.NewBuildStore()))
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
	id, ok := createBody["id"].(string)
	if !ok || id == "" {
		t.Fatalf("expected create response id, got %v", createBody["id"])
	}

	tests := []struct {
		name       string
		method     string
		path       string
		statusCode int
	}{
		{name: "get build", method: http.MethodGet, path: "/builds/" + id, statusCode: http.StatusOK},
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
