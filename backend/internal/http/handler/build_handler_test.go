package handler

// Core BuildHandler tests.

// Endpoint-focused pipeline/repo/artifact tests live in build_handler_endpoints_test.go.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/logs"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	repositorymemory "github.com/radiation/coyote-ci/backend/internal/repository/memory"
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

type fakeArtifactRepo struct {
	artifactsByBuild map[string][]domain.BuildArtifact
}

func (r *fakeArtifactRepo) Create(_ context.Context, artifact domain.BuildArtifact) (domain.BuildArtifact, error) {
	if r.artifactsByBuild == nil {
		r.artifactsByBuild = map[string][]domain.BuildArtifact{}
	}
	r.artifactsByBuild[artifact.BuildID] = append(r.artifactsByBuild[artifact.BuildID], artifact)
	return artifact, nil
}

func (r *fakeArtifactRepo) ListByBuildID(_ context.Context, buildID string) ([]domain.BuildArtifact, error) {
	items := r.artifactsByBuild[buildID]
	out := make([]domain.BuildArtifact, len(items))
	copy(out, items)
	return out, nil
}

func (r *fakeArtifactRepo) GetByID(_ context.Context, buildID string, artifactID string) (domain.BuildArtifact, error) {
	for _, item := range r.artifactsByBuild[buildID] {
		if item.ID == artifactID {
			return item, nil
		}
	}
	return domain.BuildArtifact{}, repository.ErrArtifactNotFound
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

func (r *fakeRepo) ListByJobID(_ context.Context, jobID string) ([]domain.Build, error) {
	builds := make([]domain.Build, 0)
	for _, build := range r.builds {
		if build.JobID != nil && *build.JobID == jobID {
			builds = append(builds, build)
		}
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

func (r *fakeRepo) UpdateSourceCommitSHA(_ context.Context, id string, commitSHA string) (domain.Build, error) {
	b, err := r.GetByID(context.Background(), id)
	if err != nil {
		return domain.Build{}, err
	}

	trimmed := strings.TrimSpace(commitSHA)
	if trimmed == "" {
		b.CommitSHA = nil
	} else {
		b.CommitSHA = &trimmed
	}
	b.Source = domain.NewSourceSpec(readOptionalString(b.RepoURL), readOptionalString(b.Ref), readOptionalString(b.CommitSHA))

	if r.builds == nil {
		r.build = b
	} else {
		r.builds[id] = b
	}
	return b, nil
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

func readOptionalString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
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

func (r *fakeRepo) ClaimPendingStep(_ context.Context, buildID string, stepIndex int, claim repository.StepClaim) (domain.BuildStep, bool, error) {
	steps := r.steps[buildID]
	for idx := range steps {
		if steps[idx].StepIndex != stepIndex {
			continue
		}
		if steps[idx].Status != domain.BuildStepStatusPending {
			return domain.BuildStep{}, false, nil
		}
		steps[idx].Status = domain.BuildStepStatusRunning
		steps[idx].WorkerID = &claim.WorkerID
		steps[idx].ClaimToken = &claim.ClaimToken
		steps[idx].ClaimedAt = &claim.ClaimedAt
		steps[idx].LeaseExpiresAt = &claim.LeaseExpiresAt
		steps[idx].StartedAt = &claim.ClaimedAt
		r.steps[buildID] = steps
		return steps[idx], true, nil
	}

	return domain.BuildStep{}, false, repository.ErrBuildNotFound
}

func (r *fakeRepo) ReclaimExpiredStep(_ context.Context, buildID string, stepIndex int, reclaimBefore time.Time, claim repository.StepClaim) (domain.BuildStep, bool, error) {
	steps := r.steps[buildID]
	for idx := range steps {
		if steps[idx].StepIndex != stepIndex {
			continue
		}
		if steps[idx].Status != domain.BuildStepStatusRunning {
			return domain.BuildStep{}, false, nil
		}
		if steps[idx].LeaseExpiresAt == nil || steps[idx].LeaseExpiresAt.After(reclaimBefore) {
			return domain.BuildStep{}, false, nil
		}
		steps[idx].WorkerID = &claim.WorkerID
		steps[idx].ClaimToken = &claim.ClaimToken
		steps[idx].ClaimedAt = &claim.ClaimedAt
		steps[idx].LeaseExpiresAt = &claim.LeaseExpiresAt
		r.steps[buildID] = steps
		return steps[idx], true, nil
	}

	return domain.BuildStep{}, false, repository.ErrBuildNotFound
}

func (r *fakeRepo) RenewStepLease(_ context.Context, buildID string, stepIndex int, claimToken string, leaseExpiresAt time.Time) (domain.BuildStep, repository.StepCompletionOutcome, error) {
	steps := r.steps[buildID]
	for idx := range steps {
		if steps[idx].StepIndex != stepIndex {
			continue
		}
		if steps[idx].Status == domain.BuildStepStatusSuccess || steps[idx].Status == domain.BuildStepStatusFailed {
			return steps[idx], repository.StepCompletionDuplicateTerminal, nil
		}
		if steps[idx].Status != domain.BuildStepStatusRunning {
			return domain.BuildStep{}, repository.StepCompletionInvalidTransition, nil
		}
		if steps[idx].ClaimToken == nil || *steps[idx].ClaimToken != claimToken {
			return steps[idx], repository.StepCompletionStaleClaim, nil
		}
		steps[idx].LeaseExpiresAt = &leaseExpiresAt
		r.steps[buildID] = steps
		return steps[idx], repository.StepCompletionCompleted, nil
	}

	return domain.BuildStep{}, repository.StepCompletionInvalidTransition, repository.ErrBuildNotFound
}

func (r *fakeRepo) UpdateStepByIndex(_ context.Context, buildID string, stepIndex int, update repository.StepUpdate) (domain.BuildStep, error) {
	if r.steps == nil {
		return domain.BuildStep{}, repository.ErrBuildNotFound
	}

	steps := r.steps[buildID]
	for i := range steps {
		if steps[i].StepIndex != stepIndex {
			continue
		}
		steps[i].Status = update.Status
		if update.StartedAt != nil {
			steps[i].StartedAt = update.StartedAt
		}
		if update.FinishedAt != nil {
			steps[i].FinishedAt = update.FinishedAt
		}
		r.steps[buildID] = steps
		return steps[i], nil
	}

	return domain.BuildStep{}, repository.ErrBuildNotFound
}

func (r *fakeRepo) CompleteStepIfRunning(_ context.Context, buildID string, stepIndex int, update repository.StepUpdate) (domain.BuildStep, bool, error) {
	if r.steps == nil {
		return domain.BuildStep{}, false, repository.ErrBuildNotFound
	}

	steps := r.steps[buildID]
	for i := range steps {
		if steps[i].StepIndex != stepIndex {
			continue
		}
		if steps[i].Status != domain.BuildStepStatusRunning {
			return steps[i], false, nil
		}
		steps[i].Status = update.Status
		if update.StartedAt != nil {
			steps[i].StartedAt = update.StartedAt
		}
		if update.FinishedAt != nil {
			steps[i].FinishedAt = update.FinishedAt
		}
		if update.ExitCode != nil {
			steps[i].ExitCode = update.ExitCode
		}
		if update.Stdout != nil {
			steps[i].Stdout = update.Stdout
		}
		if update.Stderr != nil {
			steps[i].Stderr = update.Stderr
		}
		if update.ErrorMessage != nil {
			steps[i].ErrorMessage = update.ErrorMessage
		}
		r.steps[buildID] = steps
		return steps[i], true, nil
	}

	return domain.BuildStep{}, false, repository.ErrBuildNotFound
}

func TestBuildHandler_GetBuildSteps_IncludesLinkedJobMetadata(t *testing.T) {
	repo := &fakeRepo{
		builds: map[string]domain.Build{
			"build-1": {
				ID:        "build-1",
				ProjectID: "project-1",
				Status:    domain.BuildStatusRunning,
				CreatedAt: time.Now().UTC(),
			},
		},
		steps: map[string][]domain.BuildStep{
			"build-1": {
				{ID: "step-1", BuildID: "build-1", StepIndex: 0, Name: "test", Command: "sh", Args: []string{"-c", "go test ./..."}, Status: domain.BuildStepStatusRunning},
			},
		},
	}

	serviceUnderTest := service.NewBuildService(repo, nil, logs.NewNoopSink())
	execRepo := repositorymemory.NewExecutionJobRepository()
	outputRepo := repositorymemory.NewExecutionJobOutputRepository()
	serviceUnderTest.SetExecutionJobRepository(execRepo)
	serviceUnderTest.SetExecutionJobOutputRepository(outputRepo)

	timeout := 120
	now := time.Now().UTC()
	_, _ = execRepo.CreateJobsForBuild(context.Background(), []domain.ExecutionJob{{
		ID:               "job-1",
		BuildID:          "build-1",
		StepID:           "step-1",
		Name:             "test",
		StepIndex:        0,
		Status:           domain.ExecutionJobStatusRunning,
		Image:            "golang:1.24",
		WorkingDir:       "backend",
		Command:          []string{"sh", "-c", "go test ./..."},
		Environment:      map[string]string{},
		TimeoutSeconds:   &timeout,
		ResolvedSpecJSON: `{"version":1}`,
		SpecVersion:      1,
		CreatedAt:        now,
		Source:           domain.SourceSnapshotRef{RepositoryURL: "https://github.com/acme/repo.git", CommitSHA: "abc123"},
	}})
	_, _ = outputRepo.CreateMany(context.Background(), []domain.ExecutionJobOutput{{
		ID:           "output-1",
		JobID:        "job-1",
		BuildID:      "build-1",
		Name:         "dist",
		Kind:         "artifact",
		DeclaredPath: "dist/**",
		Status:       domain.ExecutionJobOutputStatusDeclared,
		CreatedAt:    now,
	}})

	h := NewBuildHandler(serviceUnderTest)
	req := httptest.NewRequest(http.MethodGet, "/builds/build-1/steps", nil)
	ctx := chi.NewRouteContext()
	ctx.URLParams.Add("buildID", "build-1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, ctx))

	rr := httptest.NewRecorder()
	h.GetBuildSteps(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	body := rr.Body.String()
	if !strings.Contains(body, `"job"`) {
		t.Fatalf("expected linked job metadata in response, got %s", body)
	}
	if !strings.Contains(body, `"outputs"`) {
		t.Fatalf("expected output metadata in response, got %s", body)
	}
	if !strings.Contains(body, url.QueryEscape("dist/**")) && !strings.Contains(body, "dist/**") {
		t.Fatalf("expected declared output path in response, got %s", body)
	}
}

func (r *fakeRepo) CompleteStep(_ context.Context, request repository.CompleteStepRequest) (repository.CompleteStepResult, error) {
	buildID := request.BuildID
	stepIndex := request.StepIndex
	update := request.Update

	if request.RequireClaim {
		steps := r.steps[buildID]
		for idx := range steps {
			if steps[idx].StepIndex != stepIndex {
				continue
			}
			if steps[idx].Status == domain.BuildStepStatusSuccess || steps[idx].Status == domain.BuildStepStatusFailed {
				return repository.CompleteStepResult{Step: steps[idx], Outcome: repository.StepCompletionDuplicateTerminal}, nil
			}
			if steps[idx].Status != domain.BuildStepStatusRunning {
				return repository.CompleteStepResult{Outcome: repository.StepCompletionInvalidTransition}, nil
			}
			if steps[idx].ClaimToken == nil || *steps[idx].ClaimToken != request.ClaimToken {
				return repository.CompleteStepResult{Step: steps[idx], Outcome: repository.StepCompletionStaleClaim}, nil
			}
			break
		}
	}

	step, completed, err := r.CompleteStepIfRunning(context.Background(), buildID, stepIndex, update)
	if err != nil {
		return repository.CompleteStepResult{Outcome: repository.StepCompletionInvalidTransition}, err
	}
	if !completed {
		if step.Status == domain.BuildStepStatusSuccess || step.Status == domain.BuildStepStatusFailed {
			return repository.CompleteStepResult{Step: step, Outcome: repository.StepCompletionDuplicateTerminal}, nil
		}
		if step.ID == "" && step.Name == "" {
			return repository.CompleteStepResult{Outcome: repository.StepCompletionInvalidTransition}, repository.ErrBuildNotFound
		}
		return repository.CompleteStepResult{Outcome: repository.StepCompletionInvalidTransition}, nil
	}

	b, getErr := r.GetByID(context.Background(), buildID)
	if getErr != nil {
		return repository.CompleteStepResult{Outcome: repository.StepCompletionInvalidTransition}, getErr
	}

	if update.Status == domain.BuildStepStatusFailed {
		b.Status = domain.BuildStatusFailed
		b.ErrorMessage = step.ErrorMessage
	} else {
		nextIndex := stepIndex + 1
		if nextIndex > b.CurrentStepIndex {
			b.CurrentStepIndex = nextIndex
		}

		hasNext := false
		for idx := range r.steps[buildID] {
			if r.steps[buildID][idx].StepIndex > stepIndex {
				hasNext = true
				break
			}
		}

		if !hasNext {
			b.Status = domain.BuildStatusSuccess
			b.ErrorMessage = nil
		}
	}

	if r.builds == nil {
		r.build = b
	} else {
		r.builds[buildID] = b
	}

	return repository.CompleteStepResult{Step: step, Outcome: repository.StepCompletionCompleted}, nil
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

func addStepIndexParam(req *http.Request, stepIndex string) *http.Request {
	rctx := chi.RouteContext(req.Context())
	if rctx == nil {
		rctx = chi.NewRouteContext()
	}
	rctx.URLParams.Add("stepIndex", stepIndex)
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
		{name: "success with source", body: `{"project_id":"project-1","source":{"repository_url":"https://github.com/org/repo.git","ref":"main"}}`, repo: &fakeRepo{}, statusCode: http.StatusCreated},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := NewBuildHandler(service.NewBuildService(tc.repo, nil, nil))
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
			if tc.name == "success with source" {
				sourceData, ok := data["source"].(map[string]any)
				if !ok {
					t.Fatalf("expected source object, got %T", data["source"])
				}
				if sourceData["repository_url"] != "https://github.com/org/repo.git" {
					t.Fatalf("expected source repository_url, got %v", sourceData["repository_url"])
				}
				if sourceData["ref"] != "main" {
					t.Fatalf("expected source ref main, got %v", sourceData["ref"])
				}
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

	h := NewBuildHandler(service.NewBuildService(repo, nil, nil))
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
			h := NewBuildHandler(service.NewBuildService(tc.repo, nil, nil))
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
	stdout := "lint ok\n"
	stderr := ""
	repo := &fakeRepo{
		build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: now},
		steps: map[string][]domain.BuildStep{
			"build-1": {
				{ID: "step-2", BuildID: "build-1", StepIndex: 1, Name: "test", Command: "go", Args: []string{"test", "./..."}, Status: domain.BuildStepStatusPending},
				{ID: "step-1", BuildID: "build-1", StepIndex: 0, Name: "checkout", Command: "sh", Args: []string{"-c", "echo hello"}, Status: domain.BuildStepStatusSuccess, WorkerID: &workerID, StartedAt: &now, FinishedAt: &now, ExitCode: &exitCode, Stdout: &stdout, Stderr: &stderr},
			},
		},
	}
	h := NewBuildHandler(service.NewBuildService(repo, nil, nil))

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
	for _, field := range []string{"id", "build_id", "step_index", "name", "command", "status", "worker_id", "started_at", "finished_at", "exit_code", "stdout", "stderr", "error_message"} {
		if _, ok := first[field]; !ok {
			t.Fatalf("expected step field %q, got %v", field, first)
		}
	}
	if first["command"] != "echo hello" {
		t.Fatalf("expected command %q, got %v", "echo hello", first["command"])
	}
	if second["command"] != "go test ./..." {
		t.Fatalf("expected command %q, got %v", "go test ./...", second["command"])
	}
	if first["stdout"] != stdout {
		t.Fatalf("expected stdout %q, got %v", stdout, first["stdout"])
	}
	if first["stderr"] != stderr {
		t.Fatalf("expected stderr %q, got %v", stderr, first["stderr"])
	}
}

func TestBuildHandler_GetBuildSteps_NotFound(t *testing.T) {
	h := NewBuildHandler(service.NewBuildService(&fakeRepo{getErr: repository.ErrBuildNotFound}, nil, nil))
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
	h := NewBuildHandler(service.NewBuildService(repo, nil, nil))

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
	h := NewBuildHandler(service.NewBuildService(repo, nil, nil))

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

func TestBuildHandler_GetBuildStepLogs(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	repo := &fakeRepo{build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: now}}
	logSink := logs.NewMemorySink()

	_, _ = logSink.AppendStepLogChunk(context.Background(), logs.StepLogChunk{BuildID: "build-1", StepID: "step-1", StepIndex: 0, StepName: "setup", Stream: logs.StepLogStreamStdout, ChunkText: "line-1", CreatedAt: now})
	_, _ = logSink.AppendStepLogChunk(context.Background(), logs.StepLogChunk{BuildID: "build-1", StepID: "step-1", StepIndex: 0, StepName: "setup", Stream: logs.StepLogStreamStderr, ChunkText: "line-2", CreatedAt: now.Add(time.Second)})

	h := NewBuildHandler(service.NewBuildService(repo, nil, logSink))
	req := addStepIndexParam(addBuildIDParam(httptest.NewRequest(http.MethodGet, "/builds/build-1/steps/0/logs?after=0&limit=10", nil), "build-1"), "0")
	res := httptest.NewRecorder()

	h.GetBuildStepLogs(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, res.Code)
	}
	data := decodeDataMap(t, res)
	if data["build_id"] != "build-1" {
		t.Fatalf("expected build_id build-1, got %v", data["build_id"])
	}
	if data["step_index"] != float64(0) {
		t.Fatalf("expected step_index 0, got %v", data["step_index"])
	}
	chunks, ok := data["chunks"].([]any)
	if !ok {
		t.Fatalf("expected chunks array, got %T", data["chunks"])
	}
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
}

func TestBuildHandler_QueueBuild_WithTemplate(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	repo := &fakeRepo{build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusPending, CreatedAt: now}}
	h := NewBuildHandler(service.NewBuildService(repo, nil, nil))

	queueReq := addBuildIDParam(
		httptest.NewRequest(http.MethodPost, "/builds/build-1/queue", bytes.NewBufferString(`{"template":"test"}`)),
		"build-1",
	)
	queueRes := httptest.NewRecorder()
	h.QueueBuild(queueRes, queueReq)

	if queueRes.Code != http.StatusOK {
		t.Fatalf("expected queue status %d, got %d", http.StatusOK, queueRes.Code)
	}

	stepsReq := addBuildIDParam(httptest.NewRequest(http.MethodGet, "/builds/build-1/steps", nil), "build-1")
	stepsRes := httptest.NewRecorder()
	h.GetBuildSteps(stepsRes, stepsReq)
	if stepsRes.Code != http.StatusOK {
		t.Fatalf("expected steps status %d, got %d", http.StatusOK, stepsRes.Code)
	}

	stepsData := decodeDataMap(t, stepsRes)
	steps, ok := stepsData["steps"].([]any)
	if !ok {
		t.Fatalf("expected steps array, got %T", stepsData["steps"])
	}
	if len(steps) != 3 {
		t.Fatalf("expected 3 steps for test template, got %d", len(steps))
	}

	expectedNames := []string{"setup", "test", "teardown"}
	for idx, expectedName := range expectedNames {
		stepMap, ok := steps[idx].(map[string]any)
		if !ok {
			t.Fatalf("expected step object, got %T", steps[idx])
		}
		if stepMap["step_index"] != float64(idx) {
			t.Fatalf("expected step_index %d, got %v", idx, stepMap["step_index"])
		}
		if stepMap["name"] != expectedName {
			t.Fatalf("expected step name %q, got %v", expectedName, stepMap["name"])
		}
	}
}

func TestBuildHandler_QueueBuild_EmptyBodyUsesDefaultTemplate(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	repo := &fakeRepo{build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusPending, CreatedAt: now}}
	h := NewBuildHandler(service.NewBuildService(repo, nil, nil))

	queueReq := addBuildIDParam(httptest.NewRequest(http.MethodPost, "/builds/build-1/queue", nil), "build-1")
	queueRes := httptest.NewRecorder()
	h.QueueBuild(queueRes, queueReq)

	if queueRes.Code != http.StatusOK {
		t.Fatalf("expected queue status %d, got %d", http.StatusOK, queueRes.Code)
	}

	stepsReq := addBuildIDParam(httptest.NewRequest(http.MethodGet, "/builds/build-1/steps", nil), "build-1")
	stepsRes := httptest.NewRecorder()
	h.GetBuildSteps(stepsRes, stepsReq)
	if stepsRes.Code != http.StatusOK {
		t.Fatalf("expected steps status %d, got %d", http.StatusOK, stepsRes.Code)
	}

	stepsData := decodeDataMap(t, stepsRes)
	steps, ok := stepsData["steps"].([]any)
	if !ok {
		t.Fatalf("expected steps array, got %T", stepsData["steps"])
	}
	if len(steps) != 1 {
		t.Fatalf("expected 1 default step, got %d", len(steps))
	}

	stepMap, ok := steps[0].(map[string]any)
	if !ok {
		t.Fatalf("expected step object, got %T", steps[0])
	}
	if stepMap["name"] != "default" {
		t.Fatalf("expected default step name, got %v", stepMap["name"])
	}
}

func TestBuildHandler_QueueBuild_CustomTemplateWithCommands(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	repo := &fakeRepo{build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusPending, CreatedAt: now}}
	h := NewBuildHandler(service.NewBuildService(repo, nil, nil))

	body := `{"template":"custom","steps":[{"command":"echo ok && exit 0"},{"name":"fail","command":"echo fail && exit 1"}]}`
	queueReq := addBuildIDParam(httptest.NewRequest(http.MethodPost, "/builds/build-1/queue", bytes.NewBufferString(body)), "build-1")
	queueRes := httptest.NewRecorder()
	h.QueueBuild(queueRes, queueReq)

	if queueRes.Code != http.StatusOK {
		t.Fatalf("expected queue status %d, got %d", http.StatusOK, queueRes.Code)
	}

	stepsReq := addBuildIDParam(httptest.NewRequest(http.MethodGet, "/builds/build-1/steps", nil), "build-1")
	stepsRes := httptest.NewRecorder()
	h.GetBuildSteps(stepsRes, stepsReq)

	if stepsRes.Code != http.StatusOK {
		t.Fatalf("expected steps status %d, got %d", http.StatusOK, stepsRes.Code)
	}

	stepsData := decodeDataMap(t, stepsRes)
	steps, ok := stepsData["steps"].([]any)
	if !ok {
		t.Fatalf("expected steps array, got %T", stepsData["steps"])
	}
	if len(steps) != 2 {
		t.Fatalf("expected 2 custom steps, got %d", len(steps))
	}

	first, ok := steps[0].(map[string]any)
	if !ok {
		t.Fatalf("expected first step object, got %T", steps[0])
	}
	second, ok := steps[1].(map[string]any)
	if !ok {
		t.Fatalf("expected second step object, got %T", steps[1])
	}
	if first["name"] != "step-1" {
		t.Fatalf("expected generated first step name step-1, got %v", first["name"])
	}
	if second["name"] != "fail" {
		t.Fatalf("expected explicit second step name fail, got %v", second["name"])
	}
}

func TestBuildHandler_QueueBuild_CustomTemplateValidationErrors(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	repo := &fakeRepo{build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusPending, CreatedAt: now}}
	h := NewBuildHandler(service.NewBuildService(repo, nil, nil))

	missingStepsReq := addBuildIDParam(
		httptest.NewRequest(http.MethodPost, "/builds/build-1/queue", bytes.NewBufferString(`{"template":"custom","steps":[]}`)),
		"build-1",
	)
	missingStepsRes := httptest.NewRecorder()
	h.QueueBuild(missingStepsRes, missingStepsReq)
	if missingStepsRes.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d for missing custom steps, got %d", http.StatusBadRequest, missingStepsRes.Code)
	}
	if got := decodeErrorMessage(t, missingStepsRes); got != service.ErrCustomTemplateStepsRequired.Error() {
		t.Fatalf("expected error %q, got %q", service.ErrCustomTemplateStepsRequired.Error(), got)
	}

	emptyCommandReq := addBuildIDParam(
		httptest.NewRequest(http.MethodPost, "/builds/build-1/queue", bytes.NewBufferString(`{"template":"custom","steps":[{"command":"  "}]}`)),
		"build-1",
	)
	emptyCommandRes := httptest.NewRecorder()
	h.QueueBuild(emptyCommandRes, emptyCommandReq)
	if emptyCommandRes.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d for empty custom command, got %d", http.StatusBadRequest, emptyCommandRes.Code)
	}
	if got := decodeErrorMessage(t, emptyCommandRes); got != service.ErrCustomTemplateStepCommandRequired.Error() {
		t.Fatalf("expected error %q, got %q", service.ErrCustomTemplateStepCommandRequired.Error(), got)
	}
}
