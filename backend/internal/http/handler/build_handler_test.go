package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/service"
)

type fakeRepo struct {
	build     domain.Build
	builds    map[string]domain.Build
	createErr error
	getErr    error
	updateErr error
}

func (r *fakeRepo) Create(_ context.Context, build domain.Build) (domain.Build, error) {
	if r.createErr != nil {
		return domain.Build{}, r.createErr
	}
	if r.builds == nil {
		r.builds = map[string]domain.Build{}
	}
	r.builds[build.ID] = build
	r.build = build
	return build, nil
}

func (r *fakeRepo) GetByID(_ context.Context, id string) (domain.Build, error) {
	if r.getErr != nil {
		return domain.Build{}, r.getErr
	}
	if r.builds != nil {
		b, ok := r.builds[id]
		if !ok {
			return domain.Build{}, repository.ErrBuildNotFound
		}
		return b, nil
	}
	if r.build.ID == "" || r.build.ID != id {
		return domain.Build{}, repository.ErrBuildNotFound
	}
	return r.build, nil
}

func (r *fakeRepo) UpdateStatus(_ context.Context, id string, status domain.BuildStatus) (domain.Build, error) {
	if r.updateErr != nil {
		return domain.Build{}, r.updateErr
	}
	b, err := r.GetByID(context.Background(), id)
	if err != nil {
		return domain.Build{}, err
	}
	b.Status = status
	if r.builds == nil {
		r.build = b
	} else {
		r.builds[id] = b
	}
	return b, nil
}

func addBuildIDParam(req *http.Request, buildID string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("buildID", buildID)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	return req.WithContext(ctx)
}

func decodeBody(t *testing.T, rr *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	return body
}

func TestNewBuildHandler(t *testing.T) {
	h := NewBuildHandler(service.NewBuildService(&fakeRepo{}))
	if h == nil {
		t.Fatal("expected handler, got nil")
	}
}

func TestBuildHandler_CreateBuild(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		repo       *fakeRepo
		statusCode int
		errMsg     string
	}{
		{name: "invalid json", body: "{", repo: &fakeRepo{}, statusCode: http.StatusBadRequest, errMsg: "invalid request body"},
		{name: "missing project id", body: `{"project_id":""}`, repo: &fakeRepo{}, statusCode: http.StatusBadRequest, errMsg: service.ErrProjectIDRequired.Error()},
		{name: "repository error", body: `{"project_id":"project-1"}`, repo: &fakeRepo{createErr: errors.New("create failed")}, statusCode: http.StatusInternalServerError, errMsg: "internal server error"},
		{name: "success", body: `{"project_id":"project-1"}`, repo: &fakeRepo{}, statusCode: http.StatusCreated},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := NewBuildHandler(service.NewBuildService(tc.repo))
			req := httptest.NewRequest(http.MethodPost, "/builds", bytes.NewBufferString(tc.body))
			rr := httptest.NewRecorder()
			h.CreateBuild(rr, req)
			if rr.Code != tc.statusCode {
				t.Fatalf("expected status %d, got %d", tc.statusCode, rr.Code)
			}
			body := decodeBody(t, rr)
			if tc.errMsg != "" {
				if body["error"] != tc.errMsg {
					t.Fatalf("expected error %q, got %v", tc.errMsg, body["error"])
				}
				return
			}
			if body["id"] == "" {
				t.Fatal("expected id in response")
			}
			if body["project_id"] != "project-1" {
				t.Fatalf("expected project_id project-1, got %v", body["project_id"])
			}
			if body["status"] != string(domain.BuildStatusPending) {
				t.Fatalf("expected status pending, got %v", body["status"])
			}
			createdAt, ok := body["created_at"].(string)
			if !ok {
				t.Fatalf("expected created_at string, got %T", body["created_at"])
			}
			if _, err := time.Parse(time.RFC3339, createdAt); err != nil {
				t.Fatalf("expected RFC3339 timestamp, got %v", body["created_at"])
			}
		})
	}
}

func TestBuildHandler_GetBuild(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	tests := []struct {
		name       string
		buildID    string
		repo       *fakeRepo
		statusCode int
		errMsg     string
	}{
		{name: "missing build id", buildID: "", repo: &fakeRepo{}, statusCode: http.StatusBadRequest, errMsg: "build id is required"},
		{name: "build not found", buildID: "missing", repo: &fakeRepo{getErr: repository.ErrBuildNotFound}, statusCode: http.StatusNotFound, errMsg: "build not found"},
		{name: "success", buildID: "build-1", repo: &fakeRepo{build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusQueued, CreatedAt: now}}, statusCode: http.StatusOK},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := NewBuildHandler(service.NewBuildService(tc.repo))
			req := httptest.NewRequest(http.MethodGet, "/builds/"+tc.buildID, nil)
			req = addBuildIDParam(req, tc.buildID)
			rr := httptest.NewRecorder()
			h.GetBuild(rr, req)
			if rr.Code != tc.statusCode {
				t.Fatalf("expected status %d, got %d", tc.statusCode, rr.Code)
			}
			body := decodeBody(t, rr)
			if tc.errMsg != "" {
				if body["error"] != tc.errMsg {
					t.Fatalf("expected error %q, got %v", tc.errMsg, body["error"])
				}
				return
			}
			if body["id"] != "build-1" {
				t.Fatalf("expected id build-1, got %v", body["id"])
			}
		})
	}
}

func TestBuildHandler_TransitionEndpoints(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	tests := []struct {
		name         string
		call         func(*BuildHandler, http.ResponseWriter, *http.Request)
		buildID      string
		repo         *fakeRepo
		statusCode   int
		expectedBody string
		expectStatus domain.BuildStatus
	}{
		{name: "queue success", call: (*BuildHandler).QueueBuild, buildID: "build-1", repo: &fakeRepo{build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusPending, CreatedAt: now}}, statusCode: http.StatusOK, expectStatus: domain.BuildStatusQueued},
		{name: "start success", call: (*BuildHandler).StartBuild, buildID: "build-2", repo: &fakeRepo{build: domain.Build{ID: "build-2", ProjectID: "project-1", Status: domain.BuildStatusQueued, CreatedAt: now}}, statusCode: http.StatusOK, expectStatus: domain.BuildStatusRunning},
		{name: "complete success", call: (*BuildHandler).CompleteBuild, buildID: "build-3", repo: &fakeRepo{build: domain.Build{ID: "build-3", ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: now}}, statusCode: http.StatusOK, expectStatus: domain.BuildStatusSuccess},
		{name: "fail success", call: (*BuildHandler).FailBuild, buildID: "build-4", repo: &fakeRepo{build: domain.Build{ID: "build-4", ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: now}}, statusCode: http.StatusOK, expectStatus: domain.BuildStatusFailed},
		{name: "invalid transition", call: (*BuildHandler).StartBuild, buildID: "build-5", repo: &fakeRepo{build: domain.Build{ID: "build-5", ProjectID: "project-1", Status: domain.BuildStatusPending, CreatedAt: now}}, statusCode: http.StatusConflict, expectedBody: service.ErrInvalidBuildStatusTransition.Error()},
		{name: "missing build", call: (*BuildHandler).QueueBuild, buildID: "missing", repo: &fakeRepo{getErr: repository.ErrBuildNotFound}, statusCode: http.StatusNotFound, expectedBody: "build not found"},
		{name: "missing param", call: (*BuildHandler).QueueBuild, buildID: "", repo: &fakeRepo{}, statusCode: http.StatusBadRequest, expectedBody: "build id is required"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := NewBuildHandler(service.NewBuildService(tc.repo))
			req := httptest.NewRequest(http.MethodPost, "/builds/"+tc.buildID, nil)
			req = addBuildIDParam(req, tc.buildID)
			rr := httptest.NewRecorder()
			tc.call(h, rr, req)
			if rr.Code != tc.statusCode {
				t.Fatalf("expected status %d, got %d", tc.statusCode, rr.Code)
			}
			body := decodeBody(t, rr)
			if tc.expectedBody != "" {
				if body["error"] != tc.expectedBody {
					t.Fatalf("expected error %q, got %v", tc.expectedBody, body["error"])
				}
				return
			}
			if body["status"] != string(tc.expectStatus) {
				t.Fatalf("expected status %q, got %v", tc.expectStatus, body["status"])
			}
		})
	}
}
