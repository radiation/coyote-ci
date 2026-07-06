package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/repository/memory"
	"github.com/radiation/coyote-ci/backend/internal/service"
	buildsvc "github.com/radiation/coyote-ci/backend/internal/service/build"
)

func TestProjectHandler_CreateListGetUpdateDeleteAndJobs(t *testing.T) {
	jobRepo := memory.NewJobRepository()
	projectRepo := memory.NewProjectRepository(jobRepo)
	buildRepo := memory.NewBuildRepository()
	jobService := service.NewJobService(jobRepo, buildsvc.NewBuildService(buildRepo, nil, nil)).WithProjectRepository(projectRepo)
	projectService := service.NewProjectService(projectRepo)
	h := NewProjectHandler(projectService, jobService)

	createReq := httptest.NewRequest(http.MethodPost, "/projects", bytes.NewBufferString(`{"name":"Platform","slug":"platform","description":"Platform pipelines"}`))
	createRes := httptest.NewRecorder()
	h.CreateProject(createRes, createReq)
	if createRes.Code != http.StatusCreated {
		t.Fatalf("expected create status %d, got %d", http.StatusCreated, createRes.Code)
	}
	created := decodeDataMap(t, createRes)
	projectID, _ := created["id"].(string)

	listReq := httptest.NewRequest(http.MethodGet, "/projects", nil)
	listRes := httptest.NewRecorder()
	h.ListProjects(listRes, listReq)
	if listRes.Code != http.StatusOK {
		t.Fatalf("expected list status %d, got %d", http.StatusOK, listRes.Code)
	}

	getReq := addURLParam(httptest.NewRequest(http.MethodGet, "/projects/"+projectID, nil), "id", projectID)
	getRes := httptest.NewRecorder()
	h.GetProject(getRes, getReq)
	if getRes.Code != http.StatusOK {
		t.Fatalf("expected get status %d, got %d", http.StatusOK, getRes.Code)
	}

	_, err := jobService.CreateJob(context.Background(), service.CreateJobInput{
		ProjectID:     projectID,
		Name:          "backend-ci",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PipelineYAML:  "version: 1\nsteps:\n  - name: test\n    run: go test ./...\n",
	})
	if err != nil {
		t.Fatalf("create job failed: %v", err)
	}

	jobsReq := addURLParam(httptest.NewRequest(http.MethodGet, "/projects/"+projectID+"/jobs", nil), "id", projectID)
	jobsRes := httptest.NewRecorder()
	h.ListProjectJobs(jobsRes, jobsReq)
	if jobsRes.Code != http.StatusOK {
		t.Fatalf("expected project jobs status %d, got %d", http.StatusOK, jobsRes.Code)
	}
	var jobsPayload map[string]any
	if err := json.Unmarshal(jobsRes.Body.Bytes(), &jobsPayload); err != nil {
		t.Fatalf("decode project jobs response failed: %v", err)
	}
	data, ok := jobsPayload["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected jobs response data object, got %T", jobsPayload["data"])
	}
	jobs, ok := data["jobs"].([]any)
	if !ok {
		t.Fatalf("expected jobs response list, got %T", data["jobs"])
	}
	if len(jobs) != 1 {
		t.Fatalf("expected 1 project job, got %d", len(jobs))
	}

	deleteReq := addURLParam(httptest.NewRequest(http.MethodDelete, "/projects/"+projectID, nil), "id", projectID)
	deleteRes := httptest.NewRecorder()
	h.DeleteProject(deleteRes, deleteReq)
	if deleteRes.Code != http.StatusConflict {
		t.Fatalf("expected delete conflict status %d, got %d", http.StatusConflict, deleteRes.Code)
	}

	updateReq := addURLParam(httptest.NewRequest(http.MethodPatch, "/projects/"+projectID, bytes.NewBufferString(`{"name":"Platform CI","slug":"platform-ci"}`)), "id", projectID)
	updateRes := httptest.NewRecorder()
	h.UpdateProject(updateRes, updateReq)
	if updateRes.Code != http.StatusOK {
		t.Fatalf("expected update status %d, got %d", http.StatusOK, updateRes.Code)
	}
}

func TestProjectHandler_CreateDuplicateSlugReturnsConflict(t *testing.T) {
	jobRepo := memory.NewJobRepository()
	projectRepo := memory.NewProjectRepository(jobRepo)
	projectService := service.NewProjectService(projectRepo)
	h := NewProjectHandler(projectService, service.NewJobService(jobRepo, buildsvc.NewBuildService(memory.NewBuildRepository(), nil, nil)).WithProjectRepository(projectRepo))

	body := `{"name":"Platform","slug":"platform"}`
	firstReq := httptest.NewRequest(http.MethodPost, "/projects", bytes.NewBufferString(body))
	firstRes := httptest.NewRecorder()
	h.CreateProject(firstRes, firstReq)
	if firstRes.Code != http.StatusCreated {
		t.Fatalf("expected first create status %d, got %d", http.StatusCreated, firstRes.Code)
	}

	secondReq := httptest.NewRequest(http.MethodPost, "/projects", bytes.NewBufferString(body))
	secondRes := httptest.NewRecorder()
	h.CreateProject(secondRes, secondReq)
	if secondRes.Code != http.StatusConflict {
		t.Fatalf("expected duplicate slug status %d, got %d", http.StatusConflict, secondRes.Code)
	}
}

func TestProjectHandler_DeleteDefaultProjectReturnsConflict(t *testing.T) {
	jobRepo := memory.NewJobRepository()
	projectRepo := memory.NewProjectRepository(jobRepo)
	projectService := service.NewProjectService(projectRepo)
	h := NewProjectHandler(projectService, service.NewJobService(jobRepo, buildsvc.NewBuildService(memory.NewBuildRepository(), nil, nil)).WithProjectRepository(projectRepo))

	defaultProject, err := projectRepo.Create(context.Background(), serviceProject("00000000-0000-0000-0000-000000000001", "Default Project", domain.DefaultProjectSlug))
	if err != nil {
		t.Fatalf("create default project failed: %v", err)
	}

	deleteReq := addURLParam(httptest.NewRequest(http.MethodDelete, "/projects/"+defaultProject.ID, nil), "id", defaultProject.ID)
	deleteRes := httptest.NewRecorder()
	h.DeleteProject(deleteRes, deleteReq)
	if deleteRes.Code != http.StatusConflict {
		t.Fatalf("expected default project delete status %d, got %d", http.StatusConflict, deleteRes.Code)
	}
}

func TestProjectHandler_GetProjectWithMissingIDReturnsBadRequest(t *testing.T) {
	jobRepo := memory.NewJobRepository()
	projectRepo := memory.NewProjectRepository(jobRepo)
	projectService := service.NewProjectService(projectRepo)
	h := NewProjectHandler(projectService, service.NewJobService(jobRepo, buildsvc.NewBuildService(memory.NewBuildRepository(), nil, nil)).WithProjectRepository(projectRepo))

	getReq := addURLParam(httptest.NewRequest(http.MethodGet, "/projects", nil), "id", "")
	getRes := httptest.NewRecorder()
	h.GetProject(getRes, getReq)
	if getRes.Code != http.StatusBadRequest {
		t.Fatalf("expected missing id status %d, got %d", http.StatusBadRequest, getRes.Code)
	}
}

func TestProjectHandler_GetAndListJobsResolveSlugSelectors(t *testing.T) {
	jobRepo := memory.NewJobRepository()
	projectRepo := &selectorAwareProjectRepository{ProjectRepository: memory.NewProjectRepository(jobRepo)}
	buildRepo := memory.NewBuildRepository()
	jobService := service.NewJobService(jobRepo, buildsvc.NewBuildService(buildRepo, nil, nil)).WithProjectRepository(projectRepo)
	projectService := service.NewProjectService(projectRepo)
	h := NewProjectHandler(projectService, jobService)

	project, err := projectRepo.Create(context.Background(), serviceProject("00000000-0000-0000-0000-000000000777", "Platform", "platform"))
	if err != nil {
		t.Fatalf("create project failed: %v", err)
	}
	if _, err := jobService.CreateJob(context.Background(), service.CreateJobInput{
		ProjectID:     project.ID,
		Name:          "backend-ci",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PipelineYAML:  "version: 1\nsteps:\n  - name: test\n    run: go test ./...\n",
	}); err != nil {
		t.Fatalf("create job failed: %v", err)
	}
	projectRepo.getByIDCalls = nil
	projectRepo.getBySlugCalls = nil

	getReq := addURLParam(httptest.NewRequest(http.MethodGet, "/projects/platform", nil), "id", project.Slug)
	getRes := httptest.NewRecorder()
	h.GetProject(getRes, getReq)
	if getRes.Code != http.StatusOK {
		t.Fatalf("expected slug get status %d, got %d body=%s", http.StatusOK, getRes.Code, getRes.Body.String())
	}
	if len(projectRepo.getByIDCalls) != 0 {
		t.Fatalf("expected slug get to avoid GetByID, got %+v", projectRepo.getByIDCalls)
	}

	jobsReq := addURLParam(httptest.NewRequest(http.MethodGet, "/projects/platform/jobs", nil), "id", project.Slug)
	jobsRes := httptest.NewRecorder()
	h.ListProjectJobs(jobsRes, jobsReq)
	if jobsRes.Code != http.StatusOK {
		t.Fatalf("expected slug jobs status %d, got %d body=%s", http.StatusOK, jobsRes.Code, jobsRes.Body.String())
	}
	if len(projectRepo.getBySlugCalls) == 0 || projectRepo.getBySlugCalls[0] != project.Slug {
		t.Fatalf("expected slug lookup for project selector, got %+v", projectRepo.getBySlugCalls)
	}
	for _, idCall := range projectRepo.getByIDCalls {
		if idCall == project.Slug {
			t.Fatalf("unexpected slug selector reached GetByID: %+v", projectRepo.getByIDCalls)
		}
	}
}

func TestProjectHandler_GetProjectUnknownSlugReturnsNotFound(t *testing.T) {
	jobRepo := memory.NewJobRepository()
	projectRepo := &selectorAwareProjectRepository{ProjectRepository: memory.NewProjectRepository(jobRepo)}
	projectService := service.NewProjectService(projectRepo)
	h := NewProjectHandler(projectService, service.NewJobService(jobRepo, buildsvc.NewBuildService(memory.NewBuildRepository(), nil, nil)).WithProjectRepository(projectRepo))

	getReq := addURLParam(httptest.NewRequest(http.MethodGet, "/projects/definitely-not-a-project", nil), "id", "definitely-not-a-project")
	getRes := httptest.NewRecorder()
	h.GetProject(getRes, getReq)
	if getRes.Code != http.StatusNotFound {
		t.Fatalf("expected missing slug project status %d, got %d body=%s", http.StatusNotFound, getRes.Code, getRes.Body.String())
	}
	if !bytes.Contains(getRes.Body.Bytes(), []byte("project not found")) {
		t.Fatalf("unexpected missing slug body: %s", getRes.Body.String())
	}
	if len(projectRepo.getByIDCalls) != 0 {
		t.Fatalf("expected missing slug project lookup to avoid GetByID, got %+v", projectRepo.getByIDCalls)
	}
}

func TestProjectHandler_ListProjectJobsUnknownSlugReturnsNotFound(t *testing.T) {
	jobRepo := memory.NewJobRepository()
	projectRepo := memory.NewProjectRepository(jobRepo)
	buildRepo := memory.NewBuildRepository()
	jobService := service.NewJobService(jobRepo, buildsvc.NewBuildService(buildRepo, nil, nil)).WithProjectRepository(projectRepo)
	projectService := service.NewProjectService(projectRepo)
	h := NewProjectHandler(projectService, jobService)

	jobsReq := addURLParam(httptest.NewRequest(http.MethodGet, "/projects/definitely-not-a-project/jobs", nil), "id", "definitely-not-a-project")
	jobsRes := httptest.NewRecorder()
	h.ListProjectJobs(jobsRes, jobsReq)
	if jobsRes.Code != http.StatusNotFound {
		t.Fatalf("expected missing slug jobs status %d, got %d body=%s", http.StatusNotFound, jobsRes.Code, jobsRes.Body.String())
	}
	if !bytes.Contains(jobsRes.Body.Bytes(), []byte("project not found")) {
		t.Fatalf("unexpected missing slug body: %s", jobsRes.Body.String())
	}
}

func TestProjectHandler_GetProjectUUIDSelectorFallsBackToSlugWhenIDNotFound(t *testing.T) {
	jobRepo := memory.NewJobRepository()
	projectRepo := &selectorAwareProjectRepository{ProjectRepository: memory.NewProjectRepository(jobRepo)}
	projectService := service.NewProjectService(projectRepo)
	h := NewProjectHandler(projectService, service.NewJobService(jobRepo, buildsvc.NewBuildService(memory.NewBuildRepository(), nil, nil)).WithProjectRepository(projectRepo))

	uuidSlug := "00000000-0000-0000-0000-000000000999"
	project, err := projectRepo.Create(context.Background(), serviceProject("project-slug-uuid", "Platform", uuidSlug))
	if err != nil {
		t.Fatalf("create project failed: %v", err)
	}
	projectRepo.getByIDCalls = nil
	projectRepo.getBySlugCalls = nil

	getReq := addURLParam(httptest.NewRequest(http.MethodGet, "/projects/"+uuidSlug, nil), "id", uuidSlug)
	getRes := httptest.NewRecorder()
	h.GetProject(getRes, getReq)
	if getRes.Code != http.StatusOK {
		t.Fatalf("expected uuid-slug fallback status %d, got %d body=%s", http.StatusOK, getRes.Code, getRes.Body.String())
	}
	data := decodeDataMap(t, getRes)
	if data["id"] != project.ID || data["slug"] != uuidSlug {
		t.Fatalf("unexpected fallback project payload: %+v", data)
	}
	if len(projectRepo.getByIDCalls) == 0 || projectRepo.getByIDCalls[0] != uuidSlug {
		t.Fatalf("expected uuid lookup attempt before slug fallback, got %+v", projectRepo.getByIDCalls)
	}
	if len(projectRepo.getBySlugCalls) == 0 || projectRepo.getBySlugCalls[0] != uuidSlug {
		t.Fatalf("expected slug fallback lookup, got %+v", projectRepo.getBySlugCalls)
	}
}

func serviceProject(id string, name string, slug string) domain.Project {
	now := time.Now().UTC()
	return domain.Project{ID: id, Name: name, Slug: slug, CreatedAt: now, UpdatedAt: now}
}

type selectorAwareProjectRepository struct {
	repository.ProjectRepository
	getByIDCalls   []string
	getBySlugCalls []string
}

func (r *selectorAwareProjectRepository) GetByID(ctx context.Context, id string) (domain.Project, error) {
	r.getByIDCalls = append(r.getByIDCalls, id)
	if _, err := uuid.Parse(strings.TrimSpace(id)); err != nil {
		return domain.Project{}, errors.New("non-uuid selector reached GetByID")
	}
	return r.ProjectRepository.GetByID(ctx, id)
}

func (r *selectorAwareProjectRepository) GetBySlug(ctx context.Context, slug string) (domain.Project, error) {
	r.getBySlugCalls = append(r.getBySlugCalls, slug)
	return r.ProjectRepository.GetBySlug(ctx, slug)
}
