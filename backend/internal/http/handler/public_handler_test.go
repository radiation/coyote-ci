package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository/memory"
	"github.com/radiation/coyote-ci/backend/internal/service"
	buildsvc "github.com/radiation/coyote-ci/backend/internal/service/build"
)

func TestPublicHandler_OnlyExposesPublicProjectBuildsAndRedactedDTOs(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	jobRepo := memory.NewJobRepository()
	projectRepo := memory.NewProjectRepository(jobRepo)
	buildRepo := memory.NewBuildRepository()
	projectService := service.NewProjectService(projectRepo)
	buildService := buildsvc.NewBuildService(buildRepo, nil, nil)
	jobService := service.NewJobService(jobRepo, buildService).WithProjectRepository(projectRepo)
	h := NewPublicHandler(projectService, buildService, jobService)

	publicProject := createPublicHandlerProject(t, ctx, projectRepo, "project-public", "Public", "public", true, now)
	privateProject := createPublicHandlerProject(t, ctx, projectRepo, "project-private", "Private", "private", false, now)
	publicJobID := "job-public"
	privateJobID := "job-private"
	createPublicHandlerJob(t, ctx, jobRepo, publicJobID, publicProject.ID, "Public build", now)
	createPublicHandlerJob(t, ctx, jobRepo, privateJobID, privateProject.ID, "Private build", now)
	startedAt := now.Add(time.Minute)
	completedAt := now.Add(2 * time.Minute)
	createPublicHandlerBuild(t, ctx, buildRepo, domain.Build{
		ID:                 "public-build",
		ProjectID:          publicProject.ID,
		JobID:              &publicJobID,
		BuildNumber:        7,
		Status:             domain.BuildStatusFailed,
		CreatedAt:          now,
		StartedAt:          &startedAt,
		FinishedAt:         &completedAt,
		ErrorMessage:       publicString("TOKEN=super-secret"),
		PipelineConfigYAML: publicString("secret pipeline"),
		RepoURL:            publicString("https://example.test/private"),
	}, []domain.BuildStep{{
		StepIndex:  0,
		Name:       "test",
		Command:    "secret command",
		Env:        map[string]string{"TOKEN": "secret"},
		Status:     domain.BuildStepStatusFailed,
		StartedAt:  &startedAt,
		FinishedAt: &completedAt,
		Stdout:     publicString("secret output"),
	}})
	createPublicHandlerBuild(t, ctx, buildRepo, domain.Build{ID: "private-build", ProjectID: privateProject.ID, JobID: &privateJobID, BuildNumber: 9, Status: domain.BuildStatusSuccess, CreatedAt: now}, nil)

	listRes := httptest.NewRecorder()
	h.ListProjects(listRes, httptest.NewRequest(http.MethodGet, "/api/public/projects", nil))
	if listRes.Code != http.StatusOK {
		t.Fatalf("expected public project list status %d, got %d body=%s", http.StatusOK, listRes.Code, listRes.Body.String())
	}
	listData := decodePublicData(t, listRes)
	projects, ok := listData["projects"].([]any)
	if !ok {
		t.Fatalf("expected projects array, got %T", listData["projects"])
	}
	if len(projects) != 1 {
		t.Fatalf("expected only public project, got %v", projects)
	}
	project, ok := projects[0].(map[string]any)
	if !ok || project["slug"] != publicProject.Slug {
		t.Fatalf("expected only public project, got %v", projects)
	}

	buildListRes := httptest.NewRecorder()
	h.ListProjectBuilds(buildListRes, publicRequest("/api/public/projects/public/builds", "slug", publicProject.Slug))
	if buildListRes.Code != http.StatusOK {
		t.Fatalf("expected public build list status %d, got %d body=%s", http.StatusOK, buildListRes.Code, buildListRes.Body.String())
	}
	buildListData := decodePublicData(t, buildListRes)
	builds, ok := buildListData["builds"].([]any)
	if !ok {
		t.Fatalf("expected builds array, got %T", buildListData["builds"])
	}
	if len(builds) != 1 {
		t.Fatalf("expected only public project build, got %v", builds)
	}
	build, ok := builds[0].(map[string]any)
	if !ok || build["id"] != "public-build" {
		t.Fatalf("expected only public project build, got %v", builds)
	}

	detailRes := httptest.NewRecorder()
	h.GetProjectBuild(detailRes, publicRequest("/api/public/projects/public/builds/public-build", "slug", publicProject.Slug, "buildID", "public-build"))
	if detailRes.Code != http.StatusOK {
		t.Fatalf("expected public build detail status %d, got %d body=%s", http.StatusOK, detailRes.Code, detailRes.Body.String())
	}
	if strings.Contains(detailRes.Body.String(), "super-secret") {
		t.Fatalf("public build detail exposed an error-message secret: %s", detailRes.Body.String())
	}
	detailData := decodePublicData(t, detailRes)
	for _, field := range []string{"pipeline_config_yaml", "repository_url", "project_id", "command", "env", "stdout", "artifacts"} {
		if _, exists := detailData[field]; exists {
			t.Fatalf("public build detail unexpectedly exposed %q: %v", field, detailData)
		}
	}
	steps, ok := detailData["steps"].([]any)
	if !ok || len(steps) != 1 {
		t.Fatalf("expected one redacted step, got %v", steps)
	}
	step, ok := steps[0].(map[string]any)
	if !ok {
		t.Fatalf("expected redacted step object, got %T", steps[0])
	}
	if _, exists := step["command"]; exists {
		t.Fatalf("public step unexpectedly exposed command: %v", step)
	}

	for _, testCase := range []struct {
		name string
		call func(http.ResponseWriter, *http.Request)
		req  *http.Request
	}{
		{name: "private project", call: h.GetProject, req: publicRequest("/api/public/projects/private", "slug", privateProject.Slug)},
		{name: "private build", call: h.GetProjectBuild, req: publicRequest("/api/public/projects/private/builds/private-build", "slug", privateProject.Slug, "buildID", "private-build")},
		{name: "cross project build", call: h.GetProjectBuild, req: publicRequest("/api/public/projects/public/builds/private-build", "slug", publicProject.Slug, "buildID", "private-build")},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			res := httptest.NewRecorder()
			testCase.call(res, testCase.req)
			if res.Code != http.StatusNotFound {
				t.Fatalf("expected status %d, got %d body=%s", http.StatusNotFound, res.Code, res.Body.String())
			}
		})
	}
}

func TestPublicHandler_PublicReadEdgeCases(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	jobRepo := memory.NewJobRepository()
	projectRepo := memory.NewProjectRepository(jobRepo)
	buildRepo := memory.NewBuildRepository()
	projectService := service.NewProjectService(projectRepo)
	buildService := buildsvc.NewBuildService(buildRepo, nil, nil)
	h := NewPublicHandler(projectService, buildService, nil)
	publicProject := createPublicHandlerProject(t, ctx, projectRepo, "project-public", "Public", "public", true, now)
	createPublicHandlerBuild(t, ctx, buildRepo, domain.Build{
		ID:          "public-build",
		ProjectID:   publicProject.ID,
		BuildNumber: 1,
		Status:      domain.BuildStatusSuccess,
		CreatedAt:   now,
	}, nil)
	createPublicHandlerBuild(t, ctx, buildRepo, domain.Build{
		ID:          "newer-public-build",
		ProjectID:   publicProject.ID,
		BuildNumber: 2,
		Status:      domain.BuildStatusSuccess,
		CreatedAt:   now.Add(time.Second),
	}, nil)

	for _, testCase := range []struct {
		name string
		call func(http.ResponseWriter, *http.Request)
		req  *http.Request
	}{
		{name: "unknown project", call: h.GetProject, req: publicRequest("/api/public/projects/missing", "slug", "missing")},
		{name: "empty build id", call: h.GetProjectBuild, req: publicRequest("/api/public/projects/public/builds/", "slug", publicProject.Slug, "buildID", " ")},
		{name: "missing build", call: h.GetProjectBuild, req: publicRequest("/api/public/projects/public/builds/missing", "slug", publicProject.Slug, "buildID", "missing")},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			testCase.call(response, testCase.req)
			if response.Code != http.StatusNotFound {
				t.Fatalf("expected status %d, got %d body=%s", http.StatusNotFound, response.Code, response.Body.String())
			}
		})
	}

	buildListResponse := httptest.NewRecorder()
	h.ListProjectBuilds(buildListResponse, publicRequest("/api/public/projects/public/builds?limit=1", "slug", publicProject.Slug))
	if buildListResponse.Code != http.StatusOK {
		t.Fatalf("expected public build list status %d, got %d body=%s", http.StatusOK, buildListResponse.Code, buildListResponse.Body.String())
	}
	buildListData := decodePublicData(t, buildListResponse)
	builds, ok := buildListData["builds"].([]any)
	if !ok || len(builds) != 1 {
		t.Fatalf("expected one public build, got %v", buildListData["builds"])
	}
	build, ok := builds[0].(map[string]any)
	if !ok {
		t.Fatalf("expected public build object, got %T", builds[0])
	}
	if _, exists := build["job_name"]; exists {
		t.Fatalf("expected build without job to omit job_name, got %v", build)
	}
	if _, exists := build["started_at"]; exists {
		t.Fatalf("expected build without start time to omit started_at, got %v", build)
	}
	if _, exists := build["completed_at"]; exists {
		t.Fatalf("expected build without completion time to omit completed_at, got %v", build)
	}

	paginatedResponse := httptest.NewRecorder()
	h.ListProjectBuilds(paginatedResponse, publicRequest("/api/public/projects/public/builds?limit=1&offset=1", "slug", publicProject.Slug))
	if paginatedResponse.Code != http.StatusOK {
		t.Fatalf("expected paginated public build list status %d, got %d body=%s", http.StatusOK, paginatedResponse.Code, paginatedResponse.Body.String())
	}
	paginatedData := decodePublicData(t, paginatedResponse)
	paginatedBuilds, ok := paginatedData["builds"].([]any)
	if !ok || len(paginatedBuilds) != 1 {
		t.Fatalf("expected one paginated public build, got %v", paginatedData["builds"])
	}
	paginatedBuild, ok := paginatedBuilds[0].(map[string]any)
	if !ok || paginatedBuild["id"] != "public-build" {
		t.Fatalf("expected offset build public-build, got %v", paginatedBuilds)
	}
}

func createPublicHandlerProject(t *testing.T, ctx context.Context, repo *memory.ProjectRepository, id string, name string, slug string, isPublic bool, now time.Time) domain.Project {
	t.Helper()
	project, createErr := repo.Create(ctx, domain.Project{ID: id, Name: name, Slug: slug, IsPublic: isPublic, CreatedAt: now, UpdatedAt: now})
	if createErr != nil {
		t.Fatalf("create project: %v", createErr)
	}
	return project
}

func createPublicHandlerJob(t *testing.T, ctx context.Context, repo *memory.JobRepository, id string, projectID string, name string, now time.Time) {
	t.Helper()
	_, createErr := repo.Create(ctx, domain.Job{ID: id, ProjectID: projectID, Name: name, CreatedAt: now, UpdatedAt: now})
	if createErr != nil {
		t.Fatalf("create job: %v", createErr)
	}
}

func createPublicHandlerBuild(t *testing.T, ctx context.Context, repo *memory.BuildRepository, build domain.Build, steps []domain.BuildStep) {
	t.Helper()
	_, createErr := repo.CreateQueuedBuild(ctx, build, steps)
	if createErr != nil {
		t.Fatalf("create build: %v", createErr)
	}
}

func decodePublicData(t *testing.T, response *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var payload struct {
		Data map[string]any `json:"data"`
	}
	if decodeErr := json.Unmarshal(response.Body.Bytes(), &payload); decodeErr != nil {
		t.Fatalf("decode response: %v", decodeErr)
	}
	return payload.Data
}

func publicRequest(path string, params ...string) *http.Request {
	request := httptest.NewRequest(http.MethodGet, path, nil)
	routeContext := chi.NewRouteContext()
	for index := 0; index < len(params); index += 2 {
		routeContext.URLParams.Add(params[index], params[index+1])
	}
	return request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, routeContext))
}

func publicString(value string) *string {
	return &value
}
