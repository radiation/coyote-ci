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
	steps     map[string][]domain.BuildStep
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

func (r *fakeRepo) List(_ context.Context) ([]domain.Build, error) {
	builds := make([]domain.Build, 0, len(r.builds))
	for _, build := range r.builds {
		builds = append(builds, build)
	}
	if len(builds) == 0 && r.build.ID != "" {
		builds = append(builds, r.build)
	}
	return builds, nil
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

func (r *fakeRepo) UpdateStatus(_ context.Context, id string, status domain.BuildStatus, errorMessage *string) (domain.Build, error) {
	if r.updateErr != nil {
		return domain.Build{}, r.updateErr
	}
	b, err := r.GetByID(context.Background(), id)
	if err != nil {
		return domain.Build{}, err
	}
	b.Status = status
	b.ErrorMessage = errorMessage
	if r.builds == nil {
		r.build = b
	} else {
		r.builds[id] = b
	}
	return b, nil
}

func (r *fakeRepo) QueueBuild(ctx context.Context, id string, steps []domain.BuildStep) (domain.Build, error) {
	if r.steps == nil {
		r.steps = map[string][]domain.BuildStep{}
	}
	r.steps[id] = append([]domain.BuildStep(nil), steps...)
	return r.UpdateStatus(ctx, id, domain.BuildStatusQueued, nil)
}

func (r *fakeRepo) GetStepsByBuildID(_ context.Context, buildID string) ([]domain.BuildStep, error) {
	if _, err := r.GetByID(context.Background(), buildID); err != nil {
		return nil, err
	}

	steps := r.steps[buildID]
	out := make([]domain.BuildStep, len(steps))
	copy(out, steps)
	return out, nil
}

func (r *fakeRepo) ClaimStepIfPending(_ context.Context, buildID string, stepIndex int, _ *string, startedAt time.Time) (domain.BuildStep, bool, error) {
	steps := r.steps[buildID]
	for idx := range steps {
		if steps[idx].StepIndex != stepIndex {
			continue
		}
		if steps[idx].Status != domain.BuildStepStatusPending {
			return domain.BuildStep{}, false, nil
		}
		steps[idx].Status = domain.BuildStepStatusRunning
		steps[idx].StartedAt = &startedAt
		r.steps[buildID] = steps
		return steps[idx], true, nil
	}

	return domain.BuildStep{}, false, repository.ErrBuildNotFound
}

func (r *fakeRepo) UpdateStepByIndex(_ context.Context, buildID string, stepIndex int, status domain.BuildStepStatus, _ *string, _ *int, _ *string, startedAt *time.Time, finishedAt *time.Time) (domain.BuildStep, error) {
	if r.steps == nil {
		return domain.BuildStep{}, repository.ErrBuildNotFound
	}

	steps := r.steps[buildID]
	for i := range steps {
		if steps[i].StepIndex != stepIndex {
			continue
		}
		steps[i].Status = status
		if startedAt != nil {
			steps[i].StartedAt = startedAt
		}
		if finishedAt != nil {
			steps[i].FinishedAt = finishedAt
		}
		r.steps[buildID] = steps
		return steps[i], nil
	}

	return domain.BuildStep{}, repository.ErrBuildNotFound
}

func (r *fakeRepo) UpdateCurrentStepIndex(_ context.Context, id string, currentStepIndex int) (domain.Build, error) {
	b, err := r.GetByID(context.Background(), id)
	if err != nil {
		return domain.Build{}, err
	}
	b.CurrentStepIndex = currentStepIndex
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

func decodeDataMap(t *testing.T, rr *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	body := decodeBody(t, rr)
	data, ok := body["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data envelope, got %v", body)
	}
	return data
}

func decodeErrorMessage(t *testing.T, rr *httptest.ResponseRecorder) string {
	t.Helper()
	body := decodeBody(t, rr)
	errObj, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error envelope, got %v", body)
	}
	message, ok := errObj["message"].(string)
	if !ok {
		t.Fatalf("expected error.message string, got %v", errObj)
	}
	return message
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

			if tc.errMsg != "" {
				if got := decodeErrorMessage(t, rr); got != tc.errMsg {
					t.Fatalf("expected error %q, got %q", tc.errMsg, got)
				}
				return
			}

			data := decodeDataMap(t, rr)
			if data["id"] == "" {
				t.Fatal("expected id in response")
			}
			if data["project_id"] != "project-1" {
				t.Fatalf("expected project_id project-1, got %v", data["project_id"])
			}
			if data["status"] != string(domain.BuildStatusPending) {
				t.Fatalf("expected status pending, got %v", data["status"])
			}
			createdAt, ok := data["created_at"].(string)
			if !ok {
				t.Fatalf("expected created_at string, got %T", data["created_at"])
			}
			if _, err := time.Parse(time.RFC3339, createdAt); err != nil {
				t.Fatalf("expected RFC3339 timestamp, got %v", data["created_at"])
			}
		})
	}
}

func TestBuildHandler_ListBuilds(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	repo := &fakeRepo{builds: map[string]domain.Build{
		"build-1": {ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusPending, CreatedAt: now},
		"build-2": {ID: "build-2", ProjectID: "project-2", Status: domain.BuildStatusQueued, CreatedAt: now},
	}}

	h := NewBuildHandler(service.NewBuildService(repo))
	req := httptest.NewRequest(http.MethodGet, "/builds", nil)
	rr := httptest.NewRecorder()

	h.ListBuilds(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}

	data := decodeDataMap(t, rr)
	listPayload, ok := data["builds"].([]any)
	if !ok {
		t.Fatalf("expected builds array, got %v", data)
	}
	if len(listPayload) != 2 {
		t.Fatalf("expected two builds, got %d", len(listPayload))
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

			if tc.errMsg != "" {
				if got := decodeErrorMessage(t, rr); got != tc.errMsg {
					t.Fatalf("expected error %q, got %q", tc.errMsg, got)
				}
				return
			}

			data := decodeDataMap(t, rr)
			if data["id"] != "build-1" {
				t.Fatalf("expected id build-1, got %v", data["id"])
			}
		})
	}
}

func TestBuildHandler_GetBuildStepsAndLogs(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	repo := &fakeRepo{build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: now}}
	h := NewBuildHandler(service.NewBuildService(repo))

	stepsReq := addBuildIDParam(httptest.NewRequest(http.MethodGet, "/builds/build-1/steps", nil), "build-1")
	stepsRes := httptest.NewRecorder()
	h.GetBuildSteps(stepsRes, stepsReq)
	if stepsRes.Code != http.StatusOK {
		t.Fatalf("expected steps status %d, got %d", http.StatusOK, stepsRes.Code)
	}
	stepsData := decodeDataMap(t, stepsRes)
	if stepsData["build_id"] != "build-1" {
		t.Fatalf("expected build_id build-1, got %v", stepsData["build_id"])
	}

	logsReq := addBuildIDParam(httptest.NewRequest(http.MethodGet, "/builds/build-1/logs", nil), "build-1")
	logsRes := httptest.NewRecorder()
	h.GetBuildLogs(logsRes, logsReq)
	if logsRes.Code != http.StatusOK {
		t.Fatalf("expected logs status %d, got %d", http.StatusOK, logsRes.Code)
	}
	logsData := decodeDataMap(t, logsRes)
	if logsData["build_id"] != "build-1" {
		t.Fatalf("expected build_id build-1, got %v", logsData["build_id"])
	}
}
