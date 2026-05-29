package handler

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/logs"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	repositorymemory "github.com/radiation/coyote-ci/backend/internal/repository/memory"
	"github.com/radiation/coyote-ci/backend/internal/service"
	buildsvc "github.com/radiation/coyote-ci/backend/internal/service/build"
)

type nonFlushingResponseWriter struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (w *nonFlushingResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = http.Header{}
	}
	return w.header
}

func (w *nonFlushingResponseWriter) Write(body []byte) (int, error) {
	return w.body.Write(body)
}

func (w *nonFlushingResponseWriter) WriteHeader(status int) {
	w.status = status
}

func withURLParam(req *http.Request, key string, value string) *http.Request {
	rctx := chi.RouteContext(req.Context())
	if rctx == nil {
		rctx = chi.NewRouteContext()
	}
	rctx.URLParams.Add(key, value)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestBuildHandlerWriteServiceErrorMappings(t *testing.T) {
	h := NewBuildHandler(buildsvc.NewBuildService(&fakeRepo{}, nil, nil))

	tests := []struct {
		name       string
		inputErr   error
		wantStatus int
		wantCode   string
	}{
		{name: "build not found", inputErr: buildsvc.ErrBuildNotFound, wantStatus: http.StatusNotFound, wantCode: "build_not_found"},
		{name: "execution job not found", inputErr: buildsvc.ErrExecutionJobNotFound, wantStatus: http.StatusNotFound, wantCode: "execution_job_not_found"},
		{name: "artifact not found", inputErr: buildsvc.ErrArtifactNotFound, wantStatus: http.StatusNotFound, wantCode: "artifact_not_found"},
		{name: "invalid transition", inputErr: buildsvc.ErrInvalidBuildStatusTransition, wantStatus: http.StatusConflict, wantCode: "invalid_transition"},
		{name: "job not retryable", inputErr: buildsvc.ErrExecutionJobNotRetryable, wantStatus: http.StatusConflict, wantCode: "job_not_retryable"},
		{name: "invalid rerun step", inputErr: buildsvc.ErrInvalidRerunStepIndex, wantStatus: http.StatusBadRequest, wantCode: "invalid_step_index"},
		{name: "rerun unavailable", inputErr: buildsvc.ErrBuildRerunUnavailable, wantStatus: http.StatusBadRequest, wantCode: "rerun_unavailable"},
		{name: "repo not configured", inputErr: buildsvc.ErrExecutionJobRepoNotConfigured, wantStatus: http.StatusInternalServerError, wantCode: "internal_error"},
		{name: "custom steps required", inputErr: buildsvc.ErrCustomTemplateStepsRequired, wantStatus: http.StatusBadRequest, wantCode: "invalid_request"},
		{name: "custom command required", inputErr: buildsvc.ErrCustomTemplateStepCommandRequired, wantStatus: http.StatusBadRequest, wantCode: "invalid_request"},
		{name: "generic", inputErr: errors.New("boom"), wantStatus: http.StatusInternalServerError, wantCode: "internal_error"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := httptest.NewRecorder()
			h.writeServiceError(res, tc.inputErr)

			if res.Code != tc.wantStatus {
				t.Fatalf("expected status %d, got %d", tc.wantStatus, res.Code)
			}
			body := decodeBody(t, res)
			errorPayload, ok := body["error"].(map[string]any)
			if !ok {
				t.Fatalf("expected error payload, got %v", body)
			}
			if errorPayload["code"] != tc.wantCode {
				t.Fatalf("expected code %q, got %v", tc.wantCode, errorPayload["code"])
			}
		})
	}
}

func TestBuildHandlerStreamBuildStepLogs_ReplaysFromLastEventID(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	repo := &fakeRepo{build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: now}}
	logSink := logs.NewMemorySink()
	if _, appendErr := logSink.AppendStepLogChunk(context.Background(), logs.StepLogChunk{BuildID: "build-1", StepID: "step-1", StepIndex: 0, StepName: "test", Stream: logs.StepLogStreamStdout, ChunkText: "line-1", CreatedAt: now}); appendErr != nil {
		t.Fatalf("append first chunk: %v", appendErr)
	}
	if _, appendErr := logSink.AppendStepLogChunk(context.Background(), logs.StepLogChunk{BuildID: "build-1", StepID: "step-1", StepIndex: 0, StepName: "test", Stream: logs.StepLogStreamStdout, ChunkText: "line-2", CreatedAt: now.Add(time.Second)}); appendErr != nil {
		t.Fatalf("append second chunk: %v", appendErr)
	}

	h := NewBuildHandler(buildsvc.NewBuildService(repo, nil, logSink))
	req := addStepIndexParam(addBuildIDParam(httptest.NewRequest(http.MethodGet, "/builds/build-1/steps/0/logs/stream?after=0", nil), "build-1"), "0")
	req.Header.Set("Last-Event-ID", "1")
	canceledCtx, cancel := context.WithCancel(req.Context())
	cancel()
	req = req.WithContext(canceledCtx)
	res := httptest.NewRecorder()

	h.StreamBuildStepLogs(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, res.Code)
	}
	if got := res.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("expected event-stream content type, got %q", got)
	}
	body := res.Body.String()
	if !strings.Contains(body, "line-2") {
		t.Fatalf("expected replayed second chunk, got %q", body)
	}
	if strings.Contains(body, "line-1") {
		t.Fatalf("expected Last-Event-ID cursor to skip first chunk, got %q", body)
	}
}

func TestBuildHandlerStreamBuildStepLogs_RequiresFlusher(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	repo := &fakeRepo{build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: now}}
	h := NewBuildHandler(buildsvc.NewBuildService(repo, nil, logs.NewMemorySink()))
	req := addStepIndexParam(addBuildIDParam(httptest.NewRequest(http.MethodGet, "/builds/build-1/steps/0/logs/stream", nil), "build-1"), "0")
	res := &nonFlushingResponseWriter{}

	h.StreamBuildStepLogs(res, req)

	if res.status != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d", http.StatusInternalServerError, res.status)
	}
	if !strings.Contains(res.body.String(), "streaming not supported") {
		t.Fatalf("expected streaming unsupported error, got %q", res.body.String())
	}
}

func TestBuildHandlerExecutionEndpointValidationAndFailTransition(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	repo := &fakeRepo{build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: now}}
	h := NewBuildHandler(buildsvc.NewBuildService(repo, nil, logs.NewNoopSink()))

	failReq := addBuildIDParam(httptest.NewRequest(http.MethodPost, "/builds/build-1/fail", nil), "build-1")
	failRes := httptest.NewRecorder()
	h.FailBuild(failRes, failReq)
	if failRes.Code != http.StatusOK {
		t.Fatalf("expected fail status %d, got %d", http.StatusOK, failRes.Code)
	}
	if data := decodeDataMap(t, failRes); data["status"] != string(domain.BuildStatusFailed) {
		t.Fatalf("expected failed build response, got %v", data["status"])
	}

	tests := []struct {
		name     string
		handle   func(http.ResponseWriter, *http.Request)
		request  *http.Request
		wantText string
	}{
		{name: "get logs missing build", handle: h.GetBuildLogs, request: httptest.NewRequest(http.MethodGet, "/builds//logs", nil), wantText: "build id is required"},
		{name: "retry missing job", handle: h.RetryJob, request: httptest.NewRequest(http.MethodPost, "/builds/jobs//retry", nil), wantText: "job id is required"},
		{name: "rerun missing build", handle: h.RerunBuild, request: httptest.NewRequest(http.MethodPost, "/builds//rerun", nil), wantText: "build id is required"},
		{name: "artifacts missing build", handle: h.GetBuildArtifacts, request: httptest.NewRequest(http.MethodGet, "/builds//artifacts", nil), wantText: "build id is required"},
		{name: "download missing build", handle: h.DownloadBuildArtifact, request: httptest.NewRequest(http.MethodGet, "/builds//artifacts/artifact-1/download", nil), wantText: "build id is required"},
		{name: "download missing artifact", handle: h.DownloadBuildArtifact, request: addBuildIDParam(httptest.NewRequest(http.MethodGet, "/builds/build-1/artifacts//download", nil), "build-1"), wantText: "artifact id is required"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := httptest.NewRecorder()
			tc.handle(res, tc.request)
			if res.Code != http.StatusBadRequest {
				t.Fatalf("expected status %d, got %d", http.StatusBadRequest, res.Code)
			}
			if got := decodeErrorMessage(t, res); got != tc.wantText {
				t.Fatalf("expected message %q, got %q", tc.wantText, got)
			}
		})
	}
}

func TestBuildHandlerRetryJobSuccess(t *testing.T) {
	buildRepo := repositorymemory.NewBuildRepository()
	execRepo := repositorymemory.NewExecutionJobRepository()
	outputRepo := repositorymemory.NewExecutionJobOutputRepository()
	svc := buildsvc.NewBuildService(buildRepo, nil, logs.NewNoopSink())
	svc.SetExecutionJobRepository(execRepo)
	svc.SetExecutionJobOutputRepository(outputRepo)
	h := NewBuildHandler(svc)

	now := time.Now().UTC().Truncate(time.Second)
	sourceBuild := domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusFailed, AttemptNumber: 1, CreatedAt: now}
	sourceStep := domain.BuildStep{ID: "step-1", BuildID: sourceBuild.ID, StepIndex: 0, Name: "test", Command: "sh", Args: []string{"-c", "go test ./..."}, Env: map[string]string{}, WorkingDir: ".", Status: domain.BuildStepStatusFailed}
	if _, createErr := buildRepo.CreateQueuedBuild(context.Background(), sourceBuild, []domain.BuildStep{sourceStep}); createErr != nil {
		t.Fatalf("seed source build: %v", createErr)
	}

	lineageRoot := "job-1"
	timeout := 120
	failedJob := domain.ExecutionJob{
		ID:               "job-1",
		BuildID:          sourceBuild.ID,
		StepID:           sourceStep.ID,
		Name:             "test",
		StepIndex:        0,
		AttemptNumber:    1,
		LineageRootJobID: &lineageRoot,
		Status:           domain.ExecutionJobStatusFailed,
		Image:            "golang:1.24",
		WorkingDir:       ".",
		Command:          []string{"sh", "-c", "go test ./..."},
		Environment:      map[string]string{},
		TimeoutSeconds:   &timeout,
		SpecVersion:      1,
		ResolvedSpecJSON: `{"version":1}`,
		CreatedAt:        now,
	}
	if _, createErr := execRepo.CreateJobsForBuild(context.Background(), []domain.ExecutionJob{failedJob}); createErr != nil {
		t.Fatalf("seed failed job: %v", createErr)
	}

	req := httptest.NewRequest(http.MethodPost, "/builds/jobs/job-1/retry", nil)
	req = withURLParam(req, "jobID", "job-1")
	res := httptest.NewRecorder()

	h.RetryJob(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusOK, res.Code, res.Body.String())
	}
	data := decodeDataMap(t, res)
	buildData, ok := data["build"].(map[string]any)
	if !ok {
		t.Fatalf("expected build response, got %T", data["build"])
	}
	jobData, ok := data["job"].(map[string]any)
	if !ok {
		t.Fatalf("expected job response, got %T", data["job"])
	}
	if buildData["id"] == sourceBuild.ID {
		t.Fatalf("expected retry to create a new build, got %v", buildData["id"])
	}
	if jobData["retry_of_job_id"] != failedJob.ID {
		t.Fatalf("expected retry_of_job_id %q, got %v", failedJob.ID, jobData["retry_of_job_id"])
	}
}

func TestBuildHandlerProjectFilterAndLookupErrors(t *testing.T) {
	h := NewBuildHandler(buildsvc.NewBuildService(&fakeRepo{}, nil, nil))
	missingServiceReq := httptest.NewRequest(http.MethodGet, "/builds?project_slug=platform", nil)
	missingServiceRes := httptest.NewRecorder()
	if _, ok := h.resolveProjectFilter(missingServiceRes, missingServiceReq); ok {
		t.Fatal("expected missing project service to fail")
	}
	if missingServiceRes.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d", http.StatusInternalServerError, missingServiceRes.Code)
	}

	jobRepo := repositorymemory.NewJobRepository()
	projectRepo := repositorymemory.NewProjectRepository(jobRepo)
	projectService := service.NewProjectService(projectRepo)
	h.SetProjectService(projectService)

	missingProjectReq := httptest.NewRequest(http.MethodGet, "/builds?project_slug=missing", nil)
	missingProjectRes := httptest.NewRecorder()
	if _, ok := h.resolveProjectFilter(missingProjectRes, missingProjectReq); ok {
		t.Fatal("expected missing project lookup to fail")
	}
	if missingProjectRes.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, missingProjectRes.Code)
	}

	project, createErr := projectService.CreateProject(context.Background(), service.CreateProjectInput{Name: "Platform", Slug: "platform"})
	if createErr != nil {
		t.Fatalf("create project: %v", createErr)
	}
	mismatchReq := httptest.NewRequest(http.MethodGet, "/builds?project_id=other&project_slug=platform", nil)
	mismatchRes := httptest.NewRecorder()
	if _, ok := h.resolveProjectFilter(mismatchRes, mismatchReq); ok {
		t.Fatal("expected mismatched project filters to fail")
	}
	if mismatchRes.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, mismatchRes.Code)
	}

	matchReq := httptest.NewRequest(http.MethodGet, "/builds?project_id="+project.ID+"&project_slug=platform", nil)
	matchRes := httptest.NewRecorder()
	projectID, ok := h.resolveProjectFilter(matchRes, matchReq)
	if !ok || projectID != project.ID {
		t.Fatalf("expected project id %q, got %q ok=%v", project.ID, projectID, ok)
	}

	genericRes := httptest.NewRecorder()
	h.writeProjectLookupError(genericRes, errors.New("lookup failed"))
	if genericRes.Code != http.StatusInternalServerError {
		t.Fatalf("expected generic lookup status %d, got %d", http.StatusInternalServerError, genericRes.Code)
	}
}

func TestBuildHandlerProjectLookupSkipsBlankProjectIDs(t *testing.T) {
	jobRepo := repositorymemory.NewJobRepository()
	projectRepo := newTrackingProjectRepo(jobRepo)
	projectService := service.NewProjectService(projectRepo)
	h := NewBuildHandler(buildsvc.NewBuildService(&fakeRepo{}, nil, nil))
	h.SetProjectService(projectService)

	lookup, lookupErr := h.projectLookup(context.Background(), []domain.Build{{ID: "build-1"}})
	if lookupErr != nil {
		t.Fatalf("project lookup failed: %v", lookupErr)
	}
	if len(lookup) != 0 {
		t.Fatalf("expected empty lookup, got %+v", lookup)
	}
	if projectRepo.getByIDsCalls != 0 {
		t.Fatalf("expected no project batch fetch for blank project IDs, got %d", projectRepo.getByIDsCalls)
	}
}

func TestBuildHandlerAuthorizeBuildHelpersMapServiceErrors(t *testing.T) {
	h := NewBuildHandler(buildsvc.NewBuildService(&fakeRepo{getErr: repository.ErrBuildNotFound}, nil, nil))
	req := httptest.NewRequest(http.MethodGet, "/builds/missing", nil)
	res := httptest.NewRecorder()

	if _, ok := h.authorizeBuildDownload(res, req, "missing"); ok {
		t.Fatal("expected download authorization to fail")
	}
	if res.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, res.Code)
	}
}
