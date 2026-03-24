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

func (r *fakeRepo) CreateQueuedBuild(_ context.Context, build domain.Build, steps []domain.BuildStep) (domain.Build, error) {
	if r.createErr != nil {
		return domain.Build{}, r.createErr
	}
	if r.builds == nil {
		r.builds = map[string]domain.Build{}
	}
	if r.steps == nil {
		r.steps = map[string][]domain.BuildStep{}
	}

	build.Status = domain.BuildStatusQueued
	r.builds[build.ID] = build
	r.steps[build.ID] = append([]domain.BuildStep(nil), steps...)
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
		{name: "success with steps auto queues", body: `{"project_id":"project-1","steps":[{"name":"checkout"}]}`, repo: &fakeRepo{}, statusCode: http.StatusCreated},
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
			expectedStatus := string(domain.BuildStatusPending)
			if tc.name == "success with steps auto queues" {
				expectedStatus = string(domain.BuildStatusQueued)
			}
			if data["status"] != expectedStatus {
				t.Fatalf("expected status %s, got %v", expectedStatus, data["status"])
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
	queuedAt := now.Add(10 * time.Second)
	startedAt := now.Add(20 * time.Second)
	finishedAt := now.Add(30 * time.Second)
	errMsg := "build failed"
	repo := &fakeRepo{builds: map[string]domain.Build{
		"build-1": {
			ID:               "build-1",
			ProjectID:        "project-1",
			Status:           domain.BuildStatusPending,
			CreatedAt:        now,
			CurrentStepIndex: 1,
		},
		"build-2": {
			ID:               "build-2",
			ProjectID:        "project-2",
			Status:           domain.BuildStatusFailed,
			CreatedAt:        now,
			QueuedAt:         &queuedAt,
			StartedAt:        &startedAt,
			FinishedAt:       &finishedAt,
			CurrentStepIndex: 3,
			ErrorMessage:     &errMsg,
		},
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

	for _, buildAny := range listPayload {
		buildMap, ok := buildAny.(map[string]any)
		if !ok {
			t.Fatalf("expected build object, got %T", buildAny)
		}
		for _, field := range []string{"id", "status", "created_at", "queued_at", "started_at", "finished_at", "current_step_index", "error_message"} {
			if _, ok := buildMap[field]; !ok {
				t.Fatalf("expected build field %q, got %v", field, buildMap)
			}
		}
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
		{name: "success", buildID: "build-1", repo: &fakeRepo{build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusQueued, CreatedAt: now, CurrentStepIndex: 2}}, statusCode: http.StatusOK},
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
			for _, field := range []string{"status", "created_at", "queued_at", "started_at", "finished_at", "current_step_index", "error_message"} {
				if _, ok := data[field]; !ok {
					t.Fatalf("expected build detail field %q, got %v", field, data)
				}
			}
		})
	}
}

func TestBuildHandler_GetBuildSteps_HappyPathOrdered(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	workerID := "worker-1"
	exitCode := 0
	repo := &fakeRepo{
		build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: now},
		steps: map[string][]domain.BuildStep{
			"build-1": {
				{ID: "step-2", BuildID: "build-1", StepIndex: 1, Name: "test", Status: domain.BuildStepStatusPending},
				{ID: "step-1", BuildID: "build-1", StepIndex: 0, Name: "checkout", Status: domain.BuildStepStatusSuccess, WorkerID: &workerID, StartedAt: &now, FinishedAt: &now, ExitCode: &exitCode},
			},
		},
	}
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
	steps, ok := stepsData["steps"].([]any)
	if !ok {
		t.Fatalf("expected steps array, got %T", stepsData["steps"])
	}
	if len(steps) != 2 {
		t.Fatalf("expected two steps, got %d", len(steps))
	}
	first, ok := steps[0].(map[string]any)
	if !ok {
		t.Fatalf("expected first step object, got %T", steps[0])
	}
	second, ok := steps[1].(map[string]any)
	if !ok {
		t.Fatalf("expected second step object, got %T", steps[1])
	}
	if first["step_index"] != float64(0) || second["step_index"] != float64(1) {
		t.Fatalf("expected steps ordered by step_index asc, got first=%v second=%v", first["step_index"], second["step_index"])
	}
	for _, field := range []string{"id", "build_id", "step_index", "name", "status", "worker_id", "started_at", "finished_at", "exit_code", "error_message"} {
		if _, ok := first[field]; !ok {
			t.Fatalf("expected step field %q, got %v", field, first)
		}
	}
}

func TestBuildHandler_GetBuildSteps_NotFound(t *testing.T) {
	h := NewBuildHandler(service.NewBuildService(&fakeRepo{getErr: repository.ErrBuildNotFound}))
	stepsReq := addBuildIDParam(httptest.NewRequest(http.MethodGet, "/builds/missing/steps", nil), "missing")
	stepsRes := httptest.NewRecorder()

	h.GetBuildSteps(stepsRes, stepsReq)

	if stepsRes.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, stepsRes.Code)
	}
	if got := decodeErrorMessage(t, stepsRes); got != "build not found" {
		t.Fatalf("expected build not found error, got %q", got)
	}
}

func TestBuildHandler_GetBuildSteps_EmptyForExistingBuild(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	repo := &fakeRepo{build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusQueued, CreatedAt: now}}
	h := NewBuildHandler(service.NewBuildService(repo))

	stepsReq := addBuildIDParam(httptest.NewRequest(http.MethodGet, "/builds/build-1/steps", nil), "build-1")
	stepsRes := httptest.NewRecorder()

	h.GetBuildSteps(stepsRes, stepsReq)

	if stepsRes.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, stepsRes.Code)
	}
	stepsData := decodeDataMap(t, stepsRes)
	steps, ok := stepsData["steps"].([]any)
	if !ok {
		t.Fatalf("expected steps array, got %T", stepsData["steps"])
	}
	if len(steps) != 0 {
		t.Fatalf("expected empty steps array, got %d", len(steps))
	}
}

func TestBuildHandler_GetBuildLogs(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	repo := &fakeRepo{build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: now}}
	h := NewBuildHandler(service.NewBuildService(repo))

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
