package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	repositorymemory "github.com/radiation/coyote-ci/backend/internal/repository/memory"
	"github.com/radiation/coyote-ci/backend/internal/service"
	buildsvc "github.com/radiation/coyote-ci/backend/internal/service/build"
)

func TestJobHandler_CreateListGetUpdateRunNow(t *testing.T) {
	buildRepo := repositorymemory.NewBuildRepository()
	jobRepo := repositorymemory.NewJobRepository()
	configRepo := repositorymemory.NewJobManagedImageConfigRepository()
	credentialRepo := repositorymemory.NewSourceCredentialRepository()
	_, err := credentialRepo.Create(context.Background(), serviceCredential("cred-1", "github-bot"))
	if err != nil {
		t.Fatalf("create credential failed: %v", err)
	}
	buildSvc := buildsvc.NewBuildService(buildRepo, nil, nil)
	jobSvc := service.NewJobService(jobRepo, buildSvc).WithManagedImageConfigRepository(configRepo, credentialRepo)
	h := NewJobHandler(jobSvc)

	createBody := `{"project_id":"project-1","name":"backend-ci","repository_url":"https://github.com/example/backend.git","default_ref":"main","push_enabled":true,"push_branch":"main","pipeline_yaml":"version: 1\nsteps:\n  - name: test\n    run: go test ./...\n","managed_image":{"enabled":true,"managed_image_name":"go","pipeline_path":".coyote/pipeline.yml","write_credential_id":"cred-1"},"enabled":true}`
	createReq := httptest.NewRequest(http.MethodPost, "/jobs", bytes.NewBufferString(createBody))
	createRes := httptest.NewRecorder()
	h.CreateJob(createRes, createReq)
	if createRes.Code != http.StatusCreated {
		t.Fatalf("expected create status %d, got %d", http.StatusCreated, createRes.Code)
	}

	createData := decodeDataMap(t, createRes)
	if createData["push_enabled"] != true {
		t.Fatalf("expected push_enabled true, got %v", createData["push_enabled"])
	}
	if createData["push_branch"] != "main" {
		t.Fatalf("expected push_branch main, got %v", createData["push_branch"])
	}
	managedImage, ok := createData["managed_image"].(map[string]any)
	if !ok || managedImage["managed_image_name"] != "go" {
		t.Fatalf("expected managed image payload, got %v", createData["managed_image"])
	}
	jobID, ok := createData["id"].(string)
	if !ok || jobID == "" {
		t.Fatalf("expected created job id, got %v", createData["id"])
	}

	initialBuilds, err := buildRepo.ListByJobID(context.Background(), jobID)
	if err != nil {
		t.Fatalf("list initial builds failed: %v", err)
	}
	if len(initialBuilds) != 1 {
		t.Fatalf("expected one initial build after create, got %d", len(initialBuilds))
	}
	if initialBuilds[0].Status != domain.BuildStatusQueued {
		t.Fatalf("expected queued initial build, got %q", initialBuilds[0].Status)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/jobs", nil)
	listRes := httptest.NewRecorder()
	h.ListJobs(listRes, listReq)
	if listRes.Code != http.StatusOK {
		t.Fatalf("expected list status %d, got %d", http.StatusOK, listRes.Code)
	}

	listData := decodeDataMap(t, listRes)
	jobs, ok := listData["jobs"].([]any)
	if !ok || len(jobs) != 1 {
		t.Fatalf("expected one job in list, got %v", listData["jobs"])
	}
	listedJob, ok := jobs[0].(map[string]any)
	if !ok {
		t.Fatalf("expected job list item object, got %T", jobs[0])
	}
	latestBuild, ok := listedJob["latest_build"].(map[string]any)
	if !ok {
		t.Fatalf("expected latest_build summary in list response, got %v", listedJob["latest_build"])
	}
	if latestBuild["status"] != "queued" {
		t.Fatalf("expected latest_build queued, got %v", latestBuild["status"])
	}

	getReq := addURLParam(httptest.NewRequest(http.MethodGet, "/jobs/"+jobID, nil), "jobID", jobID)
	getRes := httptest.NewRecorder()
	h.GetJob(getRes, getReq)
	if getRes.Code != http.StatusOK {
		t.Fatalf("expected get status %d, got %d", http.StatusOK, getRes.Code)
	}

	updateBody := `{"enabled":false,"push_enabled":false,"push_branch":"","managed_image":null}`
	updateReq := addURLParam(httptest.NewRequest(http.MethodPut, "/jobs/"+jobID, bytes.NewBufferString(updateBody)), "jobID", jobID)
	updateRes := httptest.NewRecorder()
	h.UpdateJob(updateRes, updateReq)
	if updateRes.Code != http.StatusOK {
		t.Fatalf("expected update status %d, got %d", http.StatusOK, updateRes.Code)
	}
	updateData := decodeDataMap(t, updateRes)
	if updateData["push_enabled"] != false {
		t.Fatalf("expected push_enabled false after update, got %v", updateData["push_enabled"])
	}
	if managedImage, exists := updateData["managed_image"]; exists && managedImage != nil {
		t.Fatalf("expected managed image config removed, got %v", managedImage)
	}

	runReq := addURLParam(httptest.NewRequest(http.MethodPost, "/jobs/"+jobID+"/run", nil), "jobID", jobID)
	runRes := httptest.NewRecorder()
	h.RunNow(runRes, runReq)
	if runRes.Code != http.StatusConflict {
		t.Fatalf("expected disabled run status %d, got %d", http.StatusConflict, runRes.Code)
	}

	enableBody := `{"enabled":true}`
	enableReq := addURLParam(httptest.NewRequest(http.MethodPut, "/jobs/"+jobID, bytes.NewBufferString(enableBody)), "jobID", jobID)
	enableRes := httptest.NewRecorder()
	h.UpdateJob(enableRes, enableReq)
	if enableRes.Code != http.StatusOK {
		t.Fatalf("expected re-enable status %d, got %d", http.StatusOK, enableRes.Code)
	}

	runReq = addURLParam(httptest.NewRequest(http.MethodPost, "/jobs/"+jobID+"/run", nil), "jobID", jobID)
	runRes = httptest.NewRecorder()
	h.RunNow(runRes, runReq)
	if runRes.Code != http.StatusCreated {
		t.Fatalf("expected run-now status %d, got %d", http.StatusCreated, runRes.Code)
	}

	runPayload := decodeDataMap(t, runRes)
	if runPayload["status"] != "queued" {
		t.Fatalf("expected queued build from run-now, got %v", runPayload["status"])
	}
	updatedBuilds, err := buildRepo.ListByJobID(context.Background(), jobID)
	if err != nil {
		t.Fatalf("list builds after run-now failed: %v", err)
	}
	if len(updatedBuilds) != 2 {
		t.Fatalf("expected initial build plus run-now build, got %d", len(updatedBuilds))
	}
	source, ok := runPayload["source"].(map[string]interface{})
	if !ok || source == nil {
		t.Fatal("expected source object in run-now response")
	}
	if source["repository_url"] != "https://github.com/example/backend.git" {
		t.Fatalf("expected build source.repository_url from job, got %v", source["repository_url"])
	}
	if source["ref"] != "main" {
		t.Fatalf("expected build source.ref from job, got %v", source["ref"])
	}

	overrideRunReq := addURLParam(httptest.NewRequest(http.MethodPost, "/jobs/"+jobID+"/run", bytes.NewBufferString(`{"ref":"release/2026.07"}`)), "jobID", jobID)
	overrideRunRes := httptest.NewRecorder()
	h.RunNow(overrideRunRes, overrideRunReq)
	if overrideRunRes.Code != http.StatusCreated {
		t.Fatalf("expected override run-now status %d, got %d body=%s", http.StatusCreated, overrideRunRes.Code, overrideRunRes.Body.String())
	}
	overridePayload := decodeDataMap(t, overrideRunRes)
	overrideSource, ok := overridePayload["source"].(map[string]interface{})
	if !ok || overrideSource == nil {
		t.Fatal("expected source object in override run-now response")
	}
	if overrideSource["ref"] != "release/2026.07" {
		t.Fatalf("expected override ref in response, got %v", overrideSource["ref"])
	}
}

func TestJobHandler_RunNowRejectsInvalidBodyAndBlankRef(t *testing.T) {
	buildRepo := repositorymemory.NewBuildRepository()
	jobRepo := repositorymemory.NewJobRepository()
	buildSvc := buildsvc.NewBuildService(buildRepo, nil, nil)
	jobSvc := service.NewJobService(jobRepo, buildSvc)
	h := NewJobHandler(jobSvc)

	job, err := jobSvc.CreateJob(context.Background(), service.CreateJobInput{
		ProjectID:     "project-1",
		Name:          "backend-ci",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PipelineYAML:  "version: 1\nsteps:\n  - name: test\n    run: go test ./...\n",
		Enabled:       boolPtr(true),
	})
	if err != nil {
		t.Fatalf("create job failed: %v", err)
	}

	invalidReq := addURLParam(httptest.NewRequest(http.MethodPost, "/jobs/"+job.ID+"/run", bytes.NewBufferString(`{"ref":`)), "jobID", job.ID)
	invalidRes := httptest.NewRecorder()
	h.RunNow(invalidRes, invalidReq)
	if invalidRes.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid body status %d, got %d", http.StatusBadRequest, invalidRes.Code)
	}

	blankReq := addURLParam(httptest.NewRequest(http.MethodPost, "/jobs/"+job.ID+"/run", bytes.NewBufferString(`{"ref":"   "}`)), "jobID", job.ID)
	blankRes := httptest.NewRecorder()
	h.RunNow(blankRes, blankReq)
	if blankRes.Code != http.StatusBadRequest {
		t.Fatalf("expected blank ref status %d, got %d body=%s", http.StatusBadRequest, blankRes.Code, blankRes.Body.String())
	}
	if !strings.Contains(blankRes.Body.String(), "job run ref is required") {
		t.Fatalf("unexpected blank ref body: %s", blankRes.Body.String())
	}
}

func serviceCredential(id string, name string) domain.SourceCredential {
	return domain.SourceCredential{
		ID:        id,
		Name:      name,
		Kind:      domain.SourceCredentialKindHTTPSToken,
		SecretRef: "COYOTE_TOKEN",
	}
}

func addURLParam(req *http.Request, key string, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	return req.WithContext(ctx)
}

func TestJobHandler_CreateRejectsInvalidPipeline(t *testing.T) {
	buildRepo := repositorymemory.NewBuildRepository()
	jobRepo := repositorymemory.NewJobRepository()
	h := NewJobHandler(service.NewJobService(jobRepo, buildsvc.NewBuildService(buildRepo, nil, nil)))

	body := `{"project_id":"project-1","name":"bad","repository_url":"https://github.com/example/backend.git","default_ref":"main","pipeline_yaml":"version: 2\nsteps:\n  - name: test\n    run: go test ./...\n"}`
	req := httptest.NewRequest(http.MethodPost, "/jobs", bytes.NewBufferString(body))
	res := httptest.NewRecorder()
	h.CreateJob(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, res.Code)
	}

	var payload map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if payload["error"] == nil {
		t.Fatalf("expected error response, got %v", payload)
	}

	builds, err := buildRepo.List(context.Background())
	if err != nil {
		t.Fatalf("list builds failed: %v", err)
	}
	if len(builds) != 0 {
		t.Fatalf("expected no builds after failed create, got %d", len(builds))
	}
}

func TestJobHandler_CreateAcceptsLegacyProjectSlugInProjectID(t *testing.T) {
	buildRepo := repositorymemory.NewBuildRepository()
	jobRepo := repositorymemory.NewJobRepository()
	projectRepo := repositorymemory.NewProjectRepository(jobRepo)
	buildSvc := buildsvc.NewBuildService(buildRepo, nil, nil)
	jobSvc := service.NewJobService(jobRepo, buildSvc).WithProjectRepository(projectRepo)
	h := NewJobHandler(jobSvc)

	project, err := projectRepo.Create(context.Background(), domain.Project{
		ID:        "00000000-0000-0000-0000-000000000123",
		Name:      "Fixtures",
		Slug:      "fixtures",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("create project failed: %v", err)
	}

	body := `{"project_id":"fixtures","name":"fixture-job","repository_url":"https://github.com/example/backend.git","default_ref":"main","pipeline_yaml":"version: 1\nsteps:\n  - name: test\n    run: go test ./...\n"}`
	req := httptest.NewRequest(http.MethodPost, "/jobs", bytes.NewBufferString(body))
	res := httptest.NewRecorder()
	h.CreateJob(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusCreated, res.Code, res.Body.String())
	}
	data := decodeDataMap(t, res)
	if data["project_id"] != project.ID {
		t.Fatalf("expected project_id %q, got %v", project.ID, data["project_id"])
	}
}

func TestJobHandler_ResolveJobSupportsSlashInName(t *testing.T) {
	buildRepo := repositorymemory.NewBuildRepository()
	jobRepo := &selectorAwareJobRepository{JobRepository: repositorymemory.NewJobRepository()}
	projectRepo := repositorymemory.NewProjectRepository(jobRepo)
	buildSvc := buildsvc.NewBuildService(buildRepo, nil, nil)
	jobSvc := service.NewJobService(jobRepo, buildSvc).WithProjectRepository(projectRepo)
	h := NewJobHandler(jobSvc)

	project, err := projectRepo.Create(context.Background(), domain.Project{
		ID:        "project-1",
		Name:      "Platform",
		Slug:      "platform/team",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("create project failed: %v", err)
	}
	job, err := jobRepo.Create(context.Background(), domain.Job{
		ID:            "job-1",
		ProjectID:     project.ID,
		Name:          "folder/job",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PipelineYAML:  "version: 1\nsteps:\n  - name: test\n    run: go test ./...\n",
		Enabled:       true,
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("create job failed: %v", err)
	}
	jobRepo.getByIDCalls = nil
	jobRepo.findByProjectAndNameCalls = nil

	req := httptest.NewRequest(http.MethodGet, "/jobs/resolve?project=platform%2Fteam&name=folder%2Fjob", nil)
	res := httptest.NewRecorder()
	h.ResolveJob(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected resolve status %d, got %d body=%s", http.StatusOK, res.Code, res.Body.String())
	}

	data := decodeDataMap(t, res)
	if data["id"] != job.ID || data["name"] != job.Name {
		t.Fatalf("unexpected resolved job payload: %+v", data)
	}
	if len(jobRepo.getByIDCalls) != 0 {
		t.Fatalf("expected non-uuid name selector to avoid GetByID, got %+v", jobRepo.getByIDCalls)
	}
	if len(jobRepo.findByProjectAndNameCalls) != 1 {
		t.Fatalf("expected one name lookup, got %+v", jobRepo.findByProjectAndNameCalls)
	}
}

func TestJobHandler_ResolveJobValidationAndAmbiguity(t *testing.T) {
	buildRepo := repositorymemory.NewBuildRepository()
	jobRepo := repositorymemory.NewJobRepository()
	projectRepo := repositorymemory.NewProjectRepository(jobRepo)
	buildSvc := buildsvc.NewBuildService(buildRepo, nil, nil)
	jobSvc := service.NewJobService(jobRepo, buildSvc).WithProjectRepository(projectRepo)
	h := NewJobHandler(jobSvc)

	project, err := projectRepo.Create(context.Background(), domain.Project{
		ID:        "00000000-0000-0000-0000-000000000888",
		Name:      "Platform",
		Slug:      "platform",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("create project failed: %v", err)
	}
	for _, id := range []string{"job-dup-a", "job-dup-b"} {
		_, err = jobRepo.Create(context.Background(), domain.Job{
			ID:            id,
			ProjectID:     project.ID,
			Name:          "duplicate",
			RepositoryURL: "https://github.com/example/backend.git",
			DefaultRef:    "main",
			PipelineYAML:  "version: 1\nsteps:\n  - name: test\n    run: go test ./...\n",
			Enabled:       true,
			CreatedAt:     time.Now().UTC(),
			UpdatedAt:     time.Now().UTC(),
		})
		if err != nil {
			t.Fatalf("create duplicate job failed: %v", err)
		}
	}

	missingProjectReq := httptest.NewRequest(http.MethodGet, "/jobs/resolve?name=duplicate", nil)
	missingProjectRes := httptest.NewRecorder()
	h.ResolveJob(missingProjectRes, missingProjectReq)
	if missingProjectRes.Code != http.StatusBadRequest {
		t.Fatalf("expected missing project status %d, got %d body=%s", http.StatusBadRequest, missingProjectRes.Code, missingProjectRes.Body.String())
	}

	missingNameReq := httptest.NewRequest(http.MethodGet, "/jobs/resolve?project=platform", nil)
	missingNameRes := httptest.NewRecorder()
	h.ResolveJob(missingNameRes, missingNameReq)
	if missingNameRes.Code != http.StatusBadRequest {
		t.Fatalf("expected missing name status %d, got %d body=%s", http.StatusBadRequest, missingNameRes.Code, missingNameRes.Body.String())
	}

	unknownProjectReq := httptest.NewRequest(http.MethodGet, "/jobs/resolve?project=definitely-not-a-project&name=duplicate", nil)
	unknownProjectRes := httptest.NewRecorder()
	h.ResolveJob(unknownProjectRes, unknownProjectReq)
	if unknownProjectRes.Code != http.StatusNotFound {
		t.Fatalf("expected unknown project status %d, got %d body=%s", http.StatusNotFound, unknownProjectRes.Code, unknownProjectRes.Body.String())
	}

	missingJobReq := httptest.NewRequest(http.MethodGet, "/jobs/resolve?project=platform&name=missing-job", nil)
	missingJobRes := httptest.NewRecorder()
	h.ResolveJob(missingJobRes, missingJobReq)
	if missingJobRes.Code != http.StatusNotFound {
		t.Fatalf("expected missing job status %d, got %d body=%s", http.StatusNotFound, missingJobRes.Code, missingJobRes.Body.String())
	}

	ambiguousReq := httptest.NewRequest(http.MethodGet, "/jobs/resolve?project=platform&name=duplicate", nil)
	ambiguousRes := httptest.NewRecorder()
	h.ResolveJob(ambiguousRes, ambiguousReq)
	if ambiguousRes.Code != http.StatusConflict {
		t.Fatalf("expected ambiguous status %d, got %d body=%s", http.StatusConflict, ambiguousRes.Code, ambiguousRes.Body.String())
	}
}

func TestJobHandler_ResolveJobSelectorWithinProjectUsesDirectIDAndRejectsMismatchedProject(t *testing.T) {
	buildRepo := repositorymemory.NewBuildRepository()
	jobRepo := repositorymemory.NewJobRepository()
	projectRepo := repositorymemory.NewProjectRepository(jobRepo)
	buildSvc := buildsvc.NewBuildService(buildRepo, nil, nil)
	jobSvc := service.NewJobService(jobRepo, buildSvc).WithProjectRepository(projectRepo)
	h := NewJobHandler(jobSvc)

	projectA, err := projectRepo.Create(context.Background(), domain.Project{ID: "00000000-0000-0000-0000-000000000901", Name: "A", Slug: "a", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatalf("create project A failed: %v", err)
	}
	projectB, err := projectRepo.Create(context.Background(), domain.Project{ID: "00000000-0000-0000-0000-000000000902", Name: "B", Slug: "b", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatalf("create project B failed: %v", err)
	}
	job, err := jobRepo.Create(context.Background(), domain.Job{
		ID:            "00000000-0000-0000-0000-000000000911",
		ProjectID:     projectA.ID,
		Name:          "backend-ci",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PipelineYAML:  "version: 1\nsteps:\n  - name: test\n    run: go test ./...\n",
		Enabled:       true,
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("create job failed: %v", err)
	}

	resolved, resolveErr := h.resolveJobSelectorWithinProject(context.Background(), job.ID, projectA.ID)
	if resolveErr != nil {
		t.Fatalf("resolve direct id failed: %v", resolveErr)
	}
	if resolved.ID != job.ID {
		t.Fatalf("expected resolved job id %q, got %q", job.ID, resolved.ID)
	}

	_, mismatchErr := h.resolveJobSelectorWithinProject(context.Background(), job.ID, projectB.ID)
	if !errors.Is(mismatchErr, service.ErrJobNotFound) {
		t.Fatalf("expected mismatched project to return ErrJobNotFound, got %v", mismatchErr)
	}
}

type selectorAwareJobRepository struct {
	repository.JobRepository
	getByIDCalls              []string
	findByProjectAndNameCalls []string
}

func (r *selectorAwareJobRepository) GetByID(ctx context.Context, id string) (domain.Job, error) {
	r.getByIDCalls = append(r.getByIDCalls, id)
	if _, err := uuid.Parse(strings.TrimSpace(id)); err != nil {
		return domain.Job{}, errors.New("non-uuid selector reached GetByID")
	}
	return r.JobRepository.GetByID(ctx, id)
}

func (r *selectorAwareJobRepository) FindByProjectIDAndName(ctx context.Context, projectID string, name string, limit int) ([]domain.Job, error) {
	r.findByProjectAndNameCalls = append(r.findByProjectAndNameCalls, projectID+"/"+name)
	return r.JobRepository.FindByProjectIDAndName(ctx, projectID, name, limit)
}

func TestJobHandler_CreateAcceptsPipelinePathWithoutInlineYAML(t *testing.T) {
	buildRepo := repositorymemory.NewBuildRepository()
	jobRepo := repositorymemory.NewJobRepository()
	buildSvc := buildsvc.NewBuildService(buildRepo, nil, nil)
	buildSvc.SetRepoFetcher(&handlerTestRepoFetcher{localPath: writeHandlerTestPipelineRepo(t, "scenarios/success-basic/coyote.yml")})
	h := NewJobHandler(service.NewJobService(jobRepo, buildSvc))

	body := `{"project_id":"project-1","name":"path-job","repository_url":"https://github.com/example/backend.git","default_ref":"main","pipeline_path":"scenarios/success-basic/coyote.yml"}`
	req := httptest.NewRequest(http.MethodPost, "/jobs", bytes.NewBufferString(body))
	res := httptest.NewRecorder()
	h.CreateJob(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d", http.StatusCreated, res.Code)
	}

	data := decodeDataMap(t, res)
	if data["pipeline_path"] != "scenarios/success-basic/coyote.yml" {
		t.Fatalf("expected pipeline_path in response, got %v", data["pipeline_path"])
	}
}

func TestJobHandler_CreateRollsBackJobWhenInitialBuildFails(t *testing.T) {
	jobRepo := repositorymemory.NewJobRepository()
	h := NewJobHandler(service.NewJobService(jobRepo, nil))

	body := `{"project_id":"project-1","name":"backend-ci","repository_url":"https://github.com/example/backend.git","default_ref":"main","pipeline_yaml":"version: 1\nsteps:\n  - name: test\n    run: go test ./...\n"}`
	req := httptest.NewRequest(http.MethodPost, "/jobs", bytes.NewBufferString(body))
	res := httptest.NewRecorder()
	h.CreateJob(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d", http.StatusInternalServerError, res.Code)
	}

	var payload map[string]map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	errorBody, ok := payload["error"]
	if !ok {
		t.Fatalf("expected error object, got %v", payload)
	}
	if errorBody["message"] != "build service not configured" {
		t.Fatalf("expected build service not configured message, got %v", errorBody["message"])
	}

	jobs, err := jobRepo.List(context.Background())
	if err != nil {
		t.Fatalf("list jobs failed: %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("expected failed create to roll back job, got %d jobs", len(jobs))
	}
}

func TestJobHandler_CreateReturnsRepoFetcherMisconfiguration(t *testing.T) {
	jobRepo := repositorymemory.NewJobRepository()
	buildSvc := buildsvc.NewBuildService(repositorymemory.NewBuildRepository(), nil, nil)
	h := NewJobHandler(service.NewJobService(jobRepo, buildSvc))

	body := `{"project_id":"project-1","name":"path-job","repository_url":"https://github.com/example/backend.git","default_ref":"main","pipeline_path":"scenarios/success-basic/coyote.yml"}`
	req := httptest.NewRequest(http.MethodPost, "/jobs", bytes.NewBufferString(body))
	res := httptest.NewRecorder()
	h.CreateJob(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d", http.StatusInternalServerError, res.Code)
	}

	var payload map[string]map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	errorBody, ok := payload["error"]
	if !ok {
		t.Fatalf("expected error object, got %v", payload)
	}
	if errorBody["message"] != "repo fetcher not configured" {
		t.Fatalf("expected repo fetcher not configured message, got %v", errorBody["message"])
	}

	jobs, err := jobRepo.List(context.Background())
	if err != nil {
		t.Fatalf("list jobs failed: %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("expected failed create to roll back job, got %d jobs", len(jobs))
	}
}

func TestJobHandler_WriteJobServiceErrorReturnsRepoFetcherMisconfiguration(t *testing.T) {
	h := NewJobHandler(nil)
	res := httptest.NewRecorder()

	h.writeJobServiceError(res, buildsvc.ErrRepoFetcherNotConfigured)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d", http.StatusInternalServerError, res.Code)
	}
	var payload map[string]map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if payload["error"]["message"] != "repo fetcher not configured" {
		t.Fatalf("expected repo fetcher not configured message, got %v", payload["error"]["message"])
	}
}

type handlerTestRepoFetcher struct {
	localPath string
}

func (f *handlerTestRepoFetcher) Fetch(_ context.Context, _ string, _ string) (string, string, error) {
	return f.localPath, "commit-sha", nil
}

func writeHandlerTestPipelineRepo(t *testing.T, pipelinePath string) string {
	t.Helper()
	repoRoot := t.TempDir()
	fullPath := filepath.Join(repoRoot, filepath.FromSlash(pipelinePath))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatalf("create pipeline dir failed: %v", err)
	}
	if err := os.WriteFile(fullPath, []byte("version: 1\nsteps:\n  - name: test\n    run: go test ./...\n"), 0o644); err != nil {
		t.Fatalf("write pipeline file failed: %v", err)
	}
	return repoRoot
}
