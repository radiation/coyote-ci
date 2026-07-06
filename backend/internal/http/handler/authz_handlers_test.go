package handler

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/auth"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/logs"
	repositorymemory "github.com/radiation/coyote-ci/backend/internal/repository/memory"
	"github.com/radiation/coyote-ci/backend/internal/service"
	artifactsvc "github.com/radiation/coyote-ci/backend/internal/service/artifact"
	buildsvc "github.com/radiation/coyote-ci/backend/internal/service/build"
)

func TestSourceCredentialHandler_HeaderModeAuthorization(t *testing.T) {
	h := NewSourceCredentialHandler(service.NewSourceCredentialService(repositorymemory.NewSourceCredentialRepository()))
	h.SetAuthorization(auth.ModeHeader)
	admin := domain.User{ID: "admin-1", Email: "admin@example.com", GlobalRole: domain.GlobalRoleAdmin}

	forbiddenReq := httptest.NewRequest(http.MethodGet, "/source-credentials", nil)
	forbiddenReq = forbiddenReq.WithContext(auth.WithUser(forbiddenReq.Context(), domain.User{ID: "user-1", Email: "user@example.com", GlobalRole: domain.GlobalRoleUser}))
	forbiddenRes := httptest.NewRecorder()
	h.ListSourceCredentials(forbiddenRes, forbiddenReq)
	if forbiddenRes.Code != http.StatusForbidden {
		t.Fatalf("expected non-admin status %d, got %d", http.StatusForbidden, forbiddenRes.Code)
	}

	allowedReq := httptest.NewRequest(http.MethodGet, "/source-credentials", nil)
	allowedReq = allowedReq.WithContext(auth.WithUser(allowedReq.Context(), admin))
	allowedRes := httptest.NewRecorder()
	h.ListSourceCredentials(allowedRes, allowedReq)
	if allowedRes.Code != http.StatusOK {
		t.Fatalf("expected admin status %d, got %d body=%s", http.StatusOK, allowedRes.Code, allowedRes.Body.String())
	}

	createReq := httptest.NewRequest(http.MethodPost, "/source-credentials", bytes.NewBufferString(`{"name":"gh-token","kind":"https_token","username":"x-access-token","secret_ref":"COYOTE_TOKEN"}`))
	createReq = createReq.WithContext(auth.WithUser(createReq.Context(), admin))
	createRes := httptest.NewRecorder()
	h.CreateSourceCredential(createRes, createReq)
	if createRes.Code != http.StatusCreated {
		t.Fatalf("expected create status %d, got %d body=%s", http.StatusCreated, createRes.Code, createRes.Body.String())
	}
	created := decodeDataMap(t, createRes)
	credentialID, ok := created["id"].(string)
	if !ok || credentialID == "" {
		t.Fatalf("expected credential id, got %v", created["id"])
	}

	getReq := addURLParam(httptest.NewRequest(http.MethodGet, "/source-credentials/"+credentialID, nil), "credentialID", credentialID)
	getReq = getReq.WithContext(auth.WithUser(getReq.Context(), admin))
	getRes := httptest.NewRecorder()
	h.GetSourceCredential(getRes, getReq)
	if getRes.Code != http.StatusOK {
		t.Fatalf("expected get status %d, got %d body=%s", http.StatusOK, getRes.Code, getRes.Body.String())
	}

	missingReq := addURLParam(httptest.NewRequest(http.MethodGet, "/source-credentials/missing", nil), "credentialID", "missing")
	missingReq = missingReq.WithContext(auth.WithUser(missingReq.Context(), admin))
	missingRes := httptest.NewRecorder()
	h.GetSourceCredential(missingRes, missingReq)
	if missingRes.Code != http.StatusNotFound {
		t.Fatalf("expected missing credential status %d, got %d", http.StatusNotFound, missingRes.Code)
	}

	updateReq := addURLParam(httptest.NewRequest(http.MethodPut, "/source-credentials/"+credentialID, bytes.NewBufferString(`{"name":"github-token"}`)), "credentialID", credentialID)
	updateReq = updateReq.WithContext(auth.WithUser(updateReq.Context(), admin))
	updateRes := httptest.NewRecorder()
	h.UpdateSourceCredential(updateRes, updateReq)
	if updateRes.Code != http.StatusOK {
		t.Fatalf("expected update status %d, got %d body=%s", http.StatusOK, updateRes.Code, updateRes.Body.String())
	}

	deleteReq := addURLParam(httptest.NewRequest(http.MethodDelete, "/source-credentials/"+credentialID, nil), "credentialID", credentialID)
	deleteReq = deleteReq.WithContext(auth.WithUser(deleteReq.Context(), admin))
	deleteRes := httptest.NewRecorder()
	h.DeleteSourceCredential(deleteRes, deleteReq)
	if deleteRes.Code != http.StatusNoContent {
		t.Fatalf("expected delete status %d, got %d body=%s", http.StatusNoContent, deleteRes.Code, deleteRes.Body.String())
	}
}

func TestArtifactHandler_HeaderModeAuthorizationAndFiltering(t *testing.T) {
	fixture := newHandlerAuthzFixture(t)
	repo := &fakeArtifactBrowseRepo{records: []domain.ArtifactBrowseRecord{
		{
			Artifact: domain.BuildArtifact{ID: "artifact-1", BuildID: "build-1", LogicalPath: "dist/app-1.tgz", CreatedAt: fixture.now, StorageProvider: domain.StorageProviderFilesystem},
			Build:    domain.Build{ID: "build-1", ProjectID: fixture.projectViewer.ID, Status: domain.BuildStatusSuccess, CreatedAt: fixture.now},
		},
		{
			Artifact: domain.BuildArtifact{ID: "artifact-2", BuildID: "build-2", LogicalPath: "dist/app-2.tgz", CreatedAt: fixture.now, StorageProvider: domain.StorageProviderFilesystem},
			Build:    domain.Build{ID: "build-2", ProjectID: fixture.projectOther.ID, Status: domain.BuildStatusSuccess, CreatedAt: fixture.now},
		},
	}}
	h := NewArtifactHandler(artifactsvc.NewService(repo))
	h.SetProjectService(fixture.projectService)
	h.SetAuthorization(auth.ModeHeader, fixture.membershipService)

	listReq := httptest.NewRequest(http.MethodGet, "/artifacts", nil)
	listReq = listReq.WithContext(auth.WithUser(listReq.Context(), fixture.viewer))
	listRes := httptest.NewRecorder()
	h.ListArtifacts(listRes, listReq)
	if listRes.Code != http.StatusOK {
		t.Fatalf("expected list status %d, got %d body=%s", http.StatusOK, listRes.Code, listRes.Body.String())
	}
	listData := decodeDataMap(t, listRes)
	items, ok := listData["artifacts"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("expected one authorized artifact item, got %v", listData["artifacts"])
	}

	forbiddenReq := httptest.NewRequest(http.MethodGet, "/artifacts?project_id="+fixture.projectOther.ID, nil)
	forbiddenReq = forbiddenReq.WithContext(auth.WithUser(forbiddenReq.Context(), fixture.viewer))
	forbiddenRes := httptest.NewRecorder()
	h.ListArtifacts(forbiddenRes, forbiddenReq)
	if forbiddenRes.Code != http.StatusForbidden {
		t.Fatalf("expected filtered project status %d, got %d", http.StatusForbidden, forbiddenRes.Code)
	}
}

func TestProjectHandler_HeaderModeAuthorizationAndFiltering(t *testing.T) {
	fixture := newHandlerAuthzFixture(t)
	h := NewProjectHandler(fixture.projectService, fixture.jobService)
	h.SetAuthorization(auth.ModeHeader, fixture.membershipService)

	createReq := httptest.NewRequest(http.MethodPost, "/projects", bytes.NewBufferString(`{"name":"Blocked","slug":"blocked"}`))
	createReq = createReq.WithContext(auth.WithUser(createReq.Context(), fixture.viewer))
	createRes := httptest.NewRecorder()
	h.CreateProject(createRes, createReq)
	if createRes.Code != http.StatusForbidden {
		t.Fatalf("expected non-admin create status %d, got %d", http.StatusForbidden, createRes.Code)
	}

	adminCreateReq := httptest.NewRequest(http.MethodPost, "/projects", bytes.NewBufferString(`{"name":"Allowed","slug":"allowed"}`))
	adminCreateReq = adminCreateReq.WithContext(auth.WithUser(adminCreateReq.Context(), fixture.admin))
	adminCreateRes := httptest.NewRecorder()
	h.CreateProject(adminCreateRes, adminCreateReq)
	if adminCreateRes.Code != http.StatusCreated {
		t.Fatalf("expected admin create status %d, got %d body=%s", http.StatusCreated, adminCreateRes.Code, adminCreateRes.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/projects", nil)
	listReq = listReq.WithContext(auth.WithUser(listReq.Context(), fixture.viewer))
	listRes := httptest.NewRecorder()
	h.ListProjects(listRes, listReq)
	if listRes.Code != http.StatusOK {
		t.Fatalf("expected list status %d, got %d body=%s", http.StatusOK, listRes.Code, listRes.Body.String())
	}
	listData := decodeDataMap(t, listRes)
	projects, ok := listData["projects"].([]any)
	if !ok || len(projects) != 1 {
		t.Fatalf("expected one visible project, got %v", listData["projects"])
	}

	viewerUpdateReq := addURLParam(httptest.NewRequest(http.MethodPatch, "/projects/"+fixture.projectViewer.ID, bytes.NewBufferString(`{"name":"Viewer Blocked"}`)), "id", fixture.projectViewer.ID)
	viewerUpdateReq = viewerUpdateReq.WithContext(auth.WithUser(viewerUpdateReq.Context(), fixture.viewer))
	viewerUpdateRes := httptest.NewRecorder()
	h.UpdateProject(viewerUpdateRes, viewerUpdateReq)
	if viewerUpdateRes.Code != http.StatusForbidden {
		t.Fatalf("expected viewer update status %d, got %d", http.StatusForbidden, viewerUpdateRes.Code)
	}

	ownerUpdateReq := addURLParam(httptest.NewRequest(http.MethodPatch, "/projects/"+fixture.projectViewer.ID, bytes.NewBufferString(`{"name":"Owner Allowed"}`)), "id", fixture.projectViewer.ID)
	ownerUpdateReq = ownerUpdateReq.WithContext(auth.WithUser(ownerUpdateReq.Context(), fixture.owner))
	ownerUpdateRes := httptest.NewRecorder()
	h.UpdateProject(ownerUpdateRes, ownerUpdateReq)
	if ownerUpdateRes.Code != http.StatusOK {
		t.Fatalf("expected owner update status %d, got %d body=%s", http.StatusOK, ownerUpdateRes.Code, ownerUpdateRes.Body.String())
	}

	jobsReq := addURLParam(httptest.NewRequest(http.MethodGet, "/projects/"+fixture.projectViewer.ID+"/jobs", nil), "id", fixture.projectViewer.ID)
	jobsReq = jobsReq.WithContext(auth.WithUser(jobsReq.Context(), fixture.viewer))
	jobsRes := httptest.NewRecorder()
	h.ListProjectJobs(jobsRes, jobsReq)
	if jobsRes.Code != http.StatusOK {
		t.Fatalf("expected viewer jobs status %d, got %d body=%s", http.StatusOK, jobsRes.Code, jobsRes.Body.String())
	}
}

func TestJobHandler_HeaderModeAuthorizationAndFiltering(t *testing.T) {
	fixture := newHandlerAuthzFixture(t)
	job, err := fixture.jobService.CreateJob(context.Background(), service.CreateJobInput{
		ProjectID:     fixture.projectViewer.ID,
		Name:          "backend-ci",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PipelineYAML:  "version: 1\nsteps:\n  - name: test\n    run: go test ./...\n",
	})
	if err != nil {
		t.Fatalf("create fixture job failed: %v", err)
	}
	if _, err := fixture.jobService.CreateJob(context.Background(), service.CreateJobInput{
		ProjectID:     fixture.projectOther.ID,
		Name:          "backend-other",
		RepositoryURL: "https://github.com/example/other.git",
		DefaultRef:    "main",
		PipelineYAML:  "version: 1\nsteps:\n  - name: test\n    run: go test ./...\n",
	}); err != nil {
		t.Fatalf("create other job failed: %v", err)
	}

	h := NewJobHandler(fixture.jobService)
	h.SetAuthorization(auth.ModeHeader, fixture.membershipService)

	viewerCreateReq := httptest.NewRequest(http.MethodPost, "/jobs", bytes.NewBufferString(`{"project_id":"`+fixture.projectViewer.ID+`","name":"viewer-job","repository_url":"https://github.com/example/backend.git","default_ref":"main","pipeline_yaml":"version: 1\nsteps:\n  - name: test\n    run: go test ./...\n"}`))
	viewerCreateReq = viewerCreateReq.WithContext(auth.WithUser(viewerCreateReq.Context(), fixture.viewer))
	viewerCreateRes := httptest.NewRecorder()
	h.CreateJob(viewerCreateRes, viewerCreateReq)
	if viewerCreateRes.Code != http.StatusForbidden {
		t.Fatalf("expected viewer create status %d, got %d", http.StatusForbidden, viewerCreateRes.Code)
	}

	maintainerCreateReq := httptest.NewRequest(http.MethodPost, "/jobs", bytes.NewBufferString(`{"project_id":"`+fixture.projectViewer.ID+`","name":"maintainer-job","repository_url":"https://github.com/example/backend.git","default_ref":"main","pipeline_yaml":"version: 1\nsteps:\n  - name: test\n    run: go test ./...\n"}`))
	maintainerCreateReq = maintainerCreateReq.WithContext(auth.WithUser(maintainerCreateReq.Context(), fixture.maintainer))
	maintainerCreateRes := httptest.NewRecorder()
	h.CreateJob(maintainerCreateRes, maintainerCreateReq)
	if maintainerCreateRes.Code != http.StatusCreated {
		t.Fatalf("expected maintainer create status %d, got %d body=%s", http.StatusCreated, maintainerCreateRes.Code, maintainerCreateRes.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/jobs", nil)
	listReq = listReq.WithContext(auth.WithUser(listReq.Context(), fixture.viewer))
	listRes := httptest.NewRecorder()
	h.ListJobs(listRes, listReq)
	if listRes.Code != http.StatusOK {
		t.Fatalf("expected list status %d, got %d body=%s", http.StatusOK, listRes.Code, listRes.Body.String())
	}
	listData := decodeDataMap(t, listRes)
	jobs, ok := listData["jobs"].([]any)
	if !ok || len(jobs) != 2 {
		t.Fatalf("expected only project-1 jobs in list, got %v", listData["jobs"])
	}

	getReq := addURLParam(httptest.NewRequest(http.MethodGet, "/jobs/"+job.ID, nil), "jobID", job.ID)
	getReq = getReq.WithContext(auth.WithUser(getReq.Context(), fixture.viewer))
	getRes := httptest.NewRecorder()
	h.GetJob(getRes, getReq)
	if getRes.Code != http.StatusOK {
		t.Fatalf("expected get status %d, got %d body=%s", http.StatusOK, getRes.Code, getRes.Body.String())
	}

	updateReq := addURLParam(httptest.NewRequest(http.MethodPut, "/jobs/"+job.ID, bytes.NewBufferString(`{"name":"blocked"}`)), "jobID", job.ID)
	updateReq = updateReq.WithContext(auth.WithUser(updateReq.Context(), fixture.viewer))
	updateRes := httptest.NewRecorder()
	h.UpdateJob(updateRes, updateReq)
	if updateRes.Code != http.StatusForbidden {
		t.Fatalf("expected viewer update status %d, got %d", http.StatusForbidden, updateRes.Code)
	}

	runReq := addURLParam(httptest.NewRequest(http.MethodPost, "/jobs/"+job.ID+"/run", nil), "jobID", job.ID)
	runReq = runReq.WithContext(auth.WithUser(runReq.Context(), fixture.maintainer))
	runRes := httptest.NewRecorder()
	h.RunNow(runRes, runReq)
	if runRes.Code != http.StatusCreated {
		t.Fatalf("expected maintainer run status %d, got %d body=%s", http.StatusCreated, runRes.Code, runRes.Body.String())
	}

	buildsReq := addURLParam(httptest.NewRequest(http.MethodGet, "/jobs/"+job.ID+"/builds", nil), "jobID", job.ID)
	buildsReq = buildsReq.WithContext(auth.WithUser(buildsReq.Context(), fixture.viewer))
	buildsRes := httptest.NewRecorder()
	h.ListJobBuilds(buildsRes, buildsReq)
	if buildsRes.Code != http.StatusOK {
		t.Fatalf("expected list job builds status %d, got %d body=%s", http.StatusOK, buildsRes.Code, buildsRes.Body.String())
	}
}

func TestProjectAndJobDiscoveryHandlers_RequireBuildReadScopeForAPITokens(t *testing.T) {
	fixture := newHandlerAuthzFixture(t)
	job, err := fixture.jobService.CreateJob(context.Background(), service.CreateJobInput{
		ProjectID:     fixture.projectViewer.ID,
		Name:          "backend-ci",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PipelineYAML:  "version: 1\nsteps:\n  - name: test\n    run: go test ./...\n",
	})
	if err != nil {
		t.Fatalf("create fixture job failed: %v", err)
	}

	projectHandler := NewProjectHandler(fixture.projectService, fixture.jobService)
	projectHandler.SetAuthorization(auth.ModeHeader, fixture.membershipService)
	jobHandler := NewJobHandler(fixture.jobService)
	jobHandler.SetAuthorization(auth.ModeHeader, fixture.membershipService)

	listProjectsReq := withScopedAPIToken(httptest.NewRequest(http.MethodGet, "/projects", nil), fixture.viewer, domain.APITokenScopeBuildRead)
	listProjectsRes := httptest.NewRecorder()
	projectHandler.ListProjects(listProjectsRes, listProjectsReq)
	if listProjectsRes.Code != http.StatusOK {
		t.Fatalf("expected scoped project list status %d, got %d body=%s", http.StatusOK, listProjectsRes.Code, listProjectsRes.Body.String())
	}

	getProjectReq := addURLParam(httptest.NewRequest(http.MethodGet, "/projects/platform", nil), "id", fixture.projectViewer.Slug)
	getProjectReq = withScopedAPIToken(getProjectReq, fixture.viewer, domain.APITokenScopeBuildRead)
	getProjectRes := httptest.NewRecorder()
	projectHandler.GetProject(getProjectRes, getProjectReq)
	if getProjectRes.Code != http.StatusOK {
		t.Fatalf("expected scoped project get status %d, got %d body=%s", http.StatusOK, getProjectRes.Code, getProjectRes.Body.String())
	}

	listJobsReq := addURLParam(httptest.NewRequest(http.MethodGet, "/projects/platform/jobs", nil), "id", fixture.projectViewer.Slug)
	listJobsReq = withScopedAPIToken(listJobsReq, fixture.viewer, domain.APITokenScopeBuildRead)
	listJobsRes := httptest.NewRecorder()
	projectHandler.ListProjectJobs(listJobsRes, listJobsReq)
	if listJobsRes.Code != http.StatusOK {
		t.Fatalf("expected scoped project jobs status %d, got %d body=%s", http.StatusOK, listJobsRes.Code, listJobsRes.Body.String())
	}

	getJobReq := addURLParam(httptest.NewRequest(http.MethodGet, "/jobs/"+job.ID, nil), "jobID", job.ID)
	getJobReq = withScopedAPIToken(getJobReq, fixture.viewer, domain.APITokenScopeBuildRead)
	getJobRes := httptest.NewRecorder()
	jobHandler.GetJob(getJobRes, getJobReq)
	if getJobRes.Code != http.StatusOK {
		t.Fatalf("expected scoped job get status %d, got %d body=%s", http.StatusOK, getJobRes.Code, getJobRes.Body.String())
	}

	missingScopeReq := withScopedAPIToken(httptest.NewRequest(http.MethodGet, "/projects", nil), fixture.viewer, domain.APITokenScopeBuildLogs)
	missingScopeRes := httptest.NewRecorder()
	projectHandler.ListProjects(missingScopeRes, missingScopeReq)
	if missingScopeRes.Code != http.StatusForbidden {
		t.Fatalf("expected missing scope project list status %d, got %d body=%s", http.StatusForbidden, missingScopeRes.Code, missingScopeRes.Body.String())
	}

	missingJobScopeReq := addURLParam(httptest.NewRequest(http.MethodGet, "/jobs/"+job.ID, nil), "jobID", job.ID)
	missingJobScopeReq = withScopedAPIToken(missingJobScopeReq, fixture.viewer, domain.APITokenScopeBuildLogs)
	missingJobScopeRes := httptest.NewRecorder()
	jobHandler.GetJob(missingJobScopeRes, missingJobScopeReq)
	if missingJobScopeRes.Code != http.StatusForbidden {
		t.Fatalf("expected missing scope job get status %d, got %d body=%s", http.StatusForbidden, missingJobScopeRes.Code, missingJobScopeRes.Body.String())
	}

	outsiderReq := addURLParam(httptest.NewRequest(http.MethodGet, "/jobs/"+job.ID, nil), "jobID", job.ID)
	outsiderReq = withScopedAPIToken(outsiderReq, fixture.outsider, domain.APITokenScopeBuildRead)
	outsiderRes := httptest.NewRecorder()
	jobHandler.GetJob(outsiderRes, outsiderReq)
	if outsiderRes.Code != http.StatusForbidden {
		t.Fatalf("expected outsider job get status %d, got %d body=%s", http.StatusForbidden, outsiderRes.Code, outsiderRes.Body.String())
	}
}

func TestBuildHandler_HeaderModeAuthorizationAndFiltering(t *testing.T) {
	fixture := newHandlerAuthzFixture(t)
	buildViewer, err := fixture.buildService.CreateBuild(context.Background(), buildsvc.CreateBuildInput{
		ProjectID: fixture.projectViewer.ID,
		Steps:     []buildsvc.CreateBuildStepInput{{Name: "test", Command: "go", Args: []string{"test", "./..."}}},
	})
	if err != nil {
		t.Fatalf("create viewer build failed: %v", err)
	}
	if _, err := fixture.buildService.CreateBuild(context.Background(), buildsvc.CreateBuildInput{
		ProjectID: fixture.projectOther.ID,
		Steps:     []buildsvc.CreateBuildStepInput{{Name: "test", Command: "go", Args: []string{"test", "./..."}}},
	}); err != nil {
		t.Fatalf("create other build failed: %v", err)
	}

	h := NewBuildHandler(fixture.buildService)
	h.SetAuthorization(auth.ModeHeader, fixture.membershipService)

	createReq := httptest.NewRequest(http.MethodPost, "/builds", bytes.NewBufferString(`{"project_id":"`+fixture.projectViewer.ID+`","steps":[{"name":"build","command":"go","args":["test","./..."]}]}`))
	createReq = createReq.WithContext(auth.WithUser(createReq.Context(), fixture.viewer))
	createRes := httptest.NewRecorder()
	h.CreateBuild(createRes, createReq)
	if createRes.Code != http.StatusForbidden {
		t.Fatalf("expected viewer create build status %d, got %d", http.StatusForbidden, createRes.Code)
	}

	maintainerCreateReq := httptest.NewRequest(http.MethodPost, "/builds", bytes.NewBufferString(`{"project_id":"`+fixture.projectViewer.ID+`","steps":[{"name":"build","command":"go","args":["test","./..."]}]}`))
	maintainerCreateReq = maintainerCreateReq.WithContext(auth.WithUser(maintainerCreateReq.Context(), fixture.maintainer))
	maintainerCreateRes := httptest.NewRecorder()
	h.CreateBuild(maintainerCreateRes, maintainerCreateReq)
	if maintainerCreateRes.Code != http.StatusCreated {
		t.Fatalf("expected maintainer create build status %d, got %d body=%s", http.StatusCreated, maintainerCreateRes.Code, maintainerCreateRes.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/builds", nil)
	listReq = listReq.WithContext(auth.WithUser(listReq.Context(), fixture.viewer))
	listRes := httptest.NewRecorder()
	h.ListBuilds(listRes, listReq)
	if listRes.Code != http.StatusOK {
		t.Fatalf("expected list status %d, got %d body=%s", http.StatusOK, listRes.Code, listRes.Body.String())
	}
	listData := decodeDataMap(t, listRes)
	builds, ok := listData["builds"].([]any)
	if !ok || len(builds) != 2 {
		t.Fatalf("expected only viewer-project builds, got %v", listData["builds"])
	}

	getReq := addBuildIDParam(httptest.NewRequest(http.MethodGet, "/builds/"+buildViewer.ID, nil), buildViewer.ID)
	getReq = getReq.WithContext(auth.WithUser(getReq.Context(), fixture.viewer))
	getRes := httptest.NewRecorder()
	h.GetBuild(getRes, getReq)
	if getRes.Code != http.StatusOK {
		t.Fatalf("expected get status %d, got %d body=%s", http.StatusOK, getRes.Code, getRes.Body.String())
	}

	artifactsReq := addBuildIDParam(httptest.NewRequest(http.MethodGet, "/builds/"+buildViewer.ID+"/artifacts", nil), buildViewer.ID)
	artifactsReq = artifactsReq.WithContext(auth.WithUser(artifactsReq.Context(), fixture.viewer))
	artifactsRes := httptest.NewRecorder()
	h.GetBuildArtifacts(artifactsRes, artifactsReq)
	if artifactsRes.Code != http.StatusOK {
		t.Fatalf("expected get artifacts status %d, got %d body=%s", http.StatusOK, artifactsRes.Code, artifactsRes.Body.String())
	}

	queueReq := addBuildIDParam(httptest.NewRequest(http.MethodPost, "/builds/"+buildViewer.ID+"/queue", nil), buildViewer.ID)
	queueReq = queueReq.WithContext(auth.WithUser(queueReq.Context(), fixture.viewer))
	queueRes := httptest.NewRecorder()
	h.QueueBuild(queueRes, queueReq)
	if queueRes.Code != http.StatusForbidden {
		t.Fatalf("expected viewer queue status %d, got %d", http.StatusForbidden, queueRes.Code)
	}

	maintainerQueueReq := addBuildIDParam(httptest.NewRequest(http.MethodPost, "/builds/"+buildViewer.ID+"/queue", nil), buildViewer.ID)
	maintainerQueueReq = maintainerQueueReq.WithContext(auth.WithUser(maintainerQueueReq.Context(), fixture.maintainer))
	maintainerQueueRes := httptest.NewRecorder()
	h.QueueBuild(maintainerQueueRes, maintainerQueueReq)
	if maintainerQueueRes.Code != http.StatusOK && maintainerQueueRes.Code != http.StatusConflict {
		t.Fatalf("expected maintainer queue status %d or %d, got %d body=%s", http.StatusOK, http.StatusConflict, maintainerQueueRes.Code, maintainerQueueRes.Body.String())
	}

	startReq := addBuildIDParam(httptest.NewRequest(http.MethodPost, "/builds/"+buildViewer.ID+"/start", nil), buildViewer.ID)
	startReq = startReq.WithContext(auth.WithUser(startReq.Context(), fixture.maintainer))
	startRes := httptest.NewRecorder()
	h.StartBuild(startRes, startReq)
	if startRes.Code != http.StatusOK {
		t.Fatalf("expected start status %d, got %d body=%s", http.StatusOK, startRes.Code, startRes.Body.String())
	}

	completeReq := addBuildIDParam(httptest.NewRequest(http.MethodPost, "/builds/"+buildViewer.ID+"/complete", nil), buildViewer.ID)
	completeReq = completeReq.WithContext(auth.WithUser(completeReq.Context(), fixture.maintainer))
	completeRes := httptest.NewRecorder()
	h.CompleteBuild(completeRes, completeReq)
	if completeRes.Code != http.StatusOK {
		t.Fatalf("expected complete status %d, got %d body=%s", http.StatusOK, completeRes.Code, completeRes.Body.String())
	}

	viewerDownloadReq := addURLParams(httptest.NewRequest(http.MethodGet, "/builds/"+buildViewer.ID+"/artifacts/missing/download", nil), map[string]string{"buildID": buildViewer.ID, "artifactID": "missing"})
	viewerDownloadReq = viewerDownloadReq.WithContext(auth.WithUser(viewerDownloadReq.Context(), fixture.viewer))
	viewerDownloadRes := httptest.NewRecorder()
	h.DownloadBuildArtifact(viewerDownloadRes, viewerDownloadReq)
	if viewerDownloadRes.Code != http.StatusNotFound {
		t.Fatalf("expected viewer download status %d, got %d", http.StatusNotFound, viewerDownloadRes.Code)
	}

	cancelReq := addBuildIDParam(httptest.NewRequest(http.MethodPost, "/builds/"+buildViewer.ID+"/cancel", nil), buildViewer.ID)
	cancelReq = cancelReq.WithContext(auth.WithUser(cancelReq.Context(), fixture.viewer))
	cancelRes := httptest.NewRecorder()
	h.CancelBuild(cancelRes, cancelReq)
	if cancelRes.Code != http.StatusForbidden {
		t.Fatalf("expected viewer cancel status %d, got %d", http.StatusForbidden, cancelRes.Code)
	}
}

func TestBuildHandler_RetryJobAuthorizesSourceBuildBeforeMutation(t *testing.T) {
	fixture := newHandlerAuthzFixture(t)
	ctx := context.Background()
	sourceBuild, err := fixture.buildService.CreateBuild(ctx, buildsvc.CreateBuildInput{
		ProjectID: fixture.projectOther.ID,
		Steps:     []buildsvc.CreateBuildStepInput{{Name: "test", Command: "sh", Args: []string{"-c", "go test ./..."}}},
	})
	if err != nil {
		t.Fatalf("create source build failed: %v", err)
	}
	if _, startErr := fixture.buildService.StartBuild(ctx, sourceBuild.ID); startErr != nil {
		t.Fatalf("start source build failed: %v", startErr)
	}
	if _, failErr := fixture.buildService.FailBuild(ctx, sourceBuild.ID); failErr != nil {
		t.Fatalf("fail source build failed: %v", failErr)
	}
	sourceSteps, err := fixture.buildService.GetBuildSteps(ctx, sourceBuild.ID)
	if err != nil {
		t.Fatalf("load source steps failed: %v", err)
	}
	if len(sourceSteps) != 1 {
		t.Fatalf("expected one source step, got %d", len(sourceSteps))
	}

	execRepo := repositorymemory.NewExecutionJobRepository()
	fixture.buildService.SetExecutionJobRepository(execRepo)
	timeout := 120
	failedJob := domain.ExecutionJob{
		ID:               "job-retry-cross-project",
		BuildID:          sourceBuild.ID,
		StepID:           sourceSteps[0].ID,
		Name:             sourceSteps[0].Name,
		StepIndex:        sourceSteps[0].StepIndex,
		AttemptNumber:    1,
		Status:           domain.ExecutionJobStatusFailed,
		Image:            "golang:1.24",
		WorkingDir:       ".",
		Command:          []string{"sh", "-c", "go test ./..."},
		Environment:      map[string]string{},
		TimeoutSeconds:   &timeout,
		SpecVersion:      1,
		ResolvedSpecJSON: `{"version":1}`,
		CreatedAt:        fixture.now,
	}
	if _, createJobsErr := execRepo.CreateJobsForBuild(ctx, []domain.ExecutionJob{failedJob}); createJobsErr != nil {
		t.Fatalf("seed failed execution job failed: %v", createJobsErr)
	}

	beforeBuilds, err := fixture.buildService.ListBuilds(ctx)
	if err != nil {
		t.Fatalf("list builds before retry failed: %v", err)
	}
	h := NewBuildHandler(fixture.buildService)
	h.SetAuthorization(auth.ModeHeader, fixture.membershipService)

	retryReq := addURLParam(httptest.NewRequest(http.MethodPost, "/builds/jobs/"+failedJob.ID+"/retry", nil), "jobID", failedJob.ID)
	retryReq = retryReq.WithContext(auth.WithUser(retryReq.Context(), fixture.viewer))
	retryRes := httptest.NewRecorder()
	h.RetryJob(retryRes, retryReq)

	if retryRes.Code != http.StatusForbidden {
		t.Fatalf("expected forbidden retry status %d, got %d body=%s", http.StatusForbidden, retryRes.Code, retryRes.Body.String())
	}
	afterBuilds, err := fixture.buildService.ListBuilds(ctx)
	if err != nil {
		t.Fatalf("list builds after retry failed: %v", err)
	}
	if len(afterBuilds) != len(beforeBuilds) {
		t.Fatalf("expected unauthorized retry not to create builds, before=%d after=%d", len(beforeBuilds), len(afterBuilds))
	}
}

func TestScopedAPITokenRouteEnforcement(t *testing.T) {
	fixture := newHandlerAuthzFixture(t)
	ctx := context.Background()
	build, err := fixture.buildService.CreateBuild(ctx, buildsvc.CreateBuildInput{
		ProjectID: fixture.projectViewer.ID,
		Steps:     []buildsvc.CreateBuildStepInput{{Name: "test", Command: "sh", Args: []string{"-c", "echo ok"}}},
	})
	if err != nil {
		t.Fatalf("create fixture build failed: %v", err)
	}
	h := NewBuildHandler(fixture.buildService)
	h.SetAuthorization(auth.ModeHeader, fixture.membershipService)
	job, err := fixture.jobService.CreateJob(ctx, service.CreateJobInput{
		ProjectID:     fixture.projectViewer.ID,
		Name:          "backend-ci",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PipelineYAML:  "version: 1\nsteps:\n  - name: test\n    run: go test ./...\n",
	})
	if err != nil {
		t.Fatalf("create fixture job failed: %v", err)
	}

	readReq := addBuildIDParam(httptest.NewRequest(http.MethodGet, "/builds/"+build.ID, nil), build.ID)
	readReq = withScopedAPIToken(readReq, fixture.viewer, domain.APITokenScopeBuildLogs)
	readRes := httptest.NewRecorder()
	h.GetBuild(readRes, readReq)
	if readRes.Code != http.StatusForbidden {
		t.Fatalf("expected missing build:read scope status %d, got %d body=%s", http.StatusForbidden, readRes.Code, readRes.Body.String())
	}

	logsReq := addBuildIDParam(httptest.NewRequest(http.MethodGet, "/builds/"+build.ID+"/logs", nil), build.ID)
	logsReq = withScopedAPIToken(logsReq, fixture.viewer, domain.APITokenScopeBuildLogs)
	logsRes := httptest.NewRecorder()
	h.GetBuildLogs(logsRes, logsReq)
	if logsRes.Code != http.StatusOK {
		t.Fatalf("expected build:logs status %d, got %d body=%s", http.StatusOK, logsRes.Code, logsRes.Body.String())
	}

	logsFailedReq := addBuildIDParam(httptest.NewRequest(http.MethodGet, "/builds/"+build.ID+"/logs?failed=true", nil), build.ID)
	logsFailedReq = withScopedAPIToken(logsFailedReq, fixture.viewer, domain.APITokenScopeBuildLogs)
	logsFailedRes := httptest.NewRecorder()
	h.GetBuildLogs(logsFailedRes, logsFailedReq)
	if logsFailedRes.Code != http.StatusBadRequest {
		t.Fatalf("expected failed-step selection to reach handler logic under build:logs scope, got %d body=%s", logsFailedRes.Code, logsFailedRes.Body.String())
	}

	stepsReq := addBuildIDParam(httptest.NewRequest(http.MethodGet, "/builds/"+build.ID+"/steps", nil), build.ID)
	stepsReq = withScopedAPIToken(stepsReq, fixture.viewer, domain.APITokenScopeBuildLogs)
	stepsRes := httptest.NewRecorder()
	h.GetBuildSteps(stepsRes, stepsReq)
	if stepsRes.Code != http.StatusForbidden {
		t.Fatalf("expected missing build:read for steps status %d, got %d body=%s", http.StatusForbidden, stepsRes.Code, stepsRes.Body.String())
	}

	stepLogsReq := addStepIndexParam(addBuildIDParam(httptest.NewRequest(http.MethodGet, "/builds/"+build.ID+"/steps/0/logs", nil), build.ID), "0")
	stepLogsReq = withScopedAPIToken(stepLogsReq, fixture.viewer, domain.APITokenScopeBuildRead)
	stepLogsRes := httptest.NewRecorder()
	h.GetBuildStepLogs(stepLogsRes, stepLogsReq)
	if stepLogsRes.Code != http.StatusForbidden {
		t.Fatalf("expected missing build:logs for step logs status %d, got %d body=%s", http.StatusForbidden, stepLogsRes.Code, stepLogsRes.Body.String())
	}

	artifactReq := addBuildIDParam(httptest.NewRequest(http.MethodGet, "/builds/"+build.ID+"/artifacts", nil), build.ID)
	artifactReq = withScopedAPIToken(artifactReq, fixture.viewer, domain.APITokenScopeBuildRead)
	artifactRes := httptest.NewRecorder()
	h.GetBuildArtifacts(artifactRes, artifactReq)
	if artifactRes.Code != http.StatusForbidden {
		t.Fatalf("expected missing artifact:read scope status %d, got %d body=%s", http.StatusForbidden, artifactRes.Code, artifactRes.Body.String())
	}

	queueReq := addBuildIDParam(httptest.NewRequest(http.MethodPost, "/builds/"+build.ID+"/queue", nil), build.ID)
	queueReq = withScopedAPIToken(queueReq, fixture.maintainer, domain.APITokenScopeBuildRead)
	queueRes := httptest.NewRecorder()
	h.QueueBuild(queueRes, queueReq)
	if queueRes.Code != http.StatusForbidden {
		t.Fatalf("expected missing build:run for queue status %d, got %d body=%s", http.StatusForbidden, queueRes.Code, queueRes.Body.String())
	}

	createReq := httptest.NewRequest(http.MethodPost, "/builds", bytes.NewBufferString(`{"project_id":"`+fixture.projectViewer.ID+`","steps":[{"name":"build","command":"go","args":["test","./..."]}]}`))
	createReq = withScopedAPIToken(createReq, fixture.maintainer, domain.APITokenScopeBuildRead)
	createRes := httptest.NewRecorder()
	h.CreateBuild(createRes, createReq)
	if createRes.Code != http.StatusForbidden {
		t.Fatalf("expected missing build:run scope status %d, got %d body=%s", http.StatusForbidden, createRes.Code, createRes.Body.String())
	}

	pipelineReq := httptest.NewRequest(http.MethodPost, "/builds/pipeline", bytes.NewBufferString(`{"project_id":"`+fixture.projectViewer.ID+`","pipeline_yaml":"version: 1\nsteps:\n  - name: test\n    run: go test ./...\n"}`))
	pipelineReq = withScopedAPIToken(pipelineReq, fixture.maintainer, domain.APITokenScopeBuildRead)
	pipelineRes := httptest.NewRecorder()
	h.CreatePipelineBuild(pipelineRes, pipelineReq)
	if pipelineRes.Code != http.StatusForbidden {
		t.Fatalf("expected missing build:run for pipeline create status %d, got %d body=%s", http.StatusForbidden, pipelineRes.Code, pipelineRes.Body.String())
	}

	repoReq := httptest.NewRequest(http.MethodPost, "/builds/repo", bytes.NewBufferString(`{"project_id":"`+fixture.projectViewer.ID+`","repo_url":"https://github.com/example/backend.git","ref":"main"}`))
	repoReq = withScopedAPIToken(repoReq, fixture.maintainer, domain.APITokenScopeBuildRead)
	repoRes := httptest.NewRecorder()
	h.CreateRepoBuild(repoRes, repoReq)
	if repoRes.Code != http.StatusForbidden {
		t.Fatalf("expected missing build:run for repo create status %d, got %d body=%s", http.StatusForbidden, repoRes.Code, repoRes.Body.String())
	}

	jobHandler := NewJobHandler(fixture.jobService)
	jobHandler.SetAuthorization(auth.ModeHeader, fixture.membershipService)

	runReq := addURLParam(httptest.NewRequest(http.MethodPost, "/jobs/"+job.ID+"/run", nil), "jobID", job.ID)
	runReq = withScopedAPIToken(runReq, fixture.maintainer, domain.APITokenScopeBuildRead)
	runRes := httptest.NewRecorder()
	jobHandler.RunNow(runRes, runReq)
	if runRes.Code != http.StatusForbidden {
		t.Fatalf("expected missing build:run for run-now status %d, got %d body=%s", http.StatusForbidden, runRes.Code, runRes.Body.String())
	}

	jobBuildsReq := addURLParam(httptest.NewRequest(http.MethodGet, "/jobs/"+job.ID+"/builds", nil), "jobID", job.ID)
	jobBuildsReq = withScopedAPIToken(jobBuildsReq, fixture.viewer, domain.APITokenScopeBuildLogs)
	jobBuildsRes := httptest.NewRecorder()
	jobHandler.ListJobBuilds(jobBuildsRes, jobBuildsReq)
	if jobBuildsRes.Code != http.StatusForbidden {
		t.Fatalf("expected missing build:read for list job builds status %d, got %d body=%s", http.StatusForbidden, jobBuildsRes.Code, jobBuildsRes.Body.String())
	}

	artifactCatalogRepo := &fakeArtifactCatalogRepo{records: []domain.ArtifactRecord{{
		Artifact: domain.BuildArtifact{ID: "artifact-1", BuildID: build.ID, LogicalPath: "dist/app.tgz", CreatedAt: fixture.now, StorageProvider: domain.StorageProviderFilesystem},
		Build:    domain.Build{ID: build.ID, ProjectID: fixture.projectViewer.ID, Status: domain.BuildStatusSuccess, CreatedAt: fixture.now},
	}}, record: domain.ArtifactRecord{
		Artifact: domain.BuildArtifact{ID: "artifact-1", BuildID: build.ID, LogicalPath: "dist/app.tgz", CreatedAt: fixture.now, StorageProvider: domain.StorageProviderFilesystem},
		Build:    domain.Build{ID: build.ID, ProjectID: fixture.projectViewer.ID, Status: domain.BuildStatusSuccess, CreatedAt: fixture.now},
	}}
	artifactHandler := NewArtifactHandler(artifactsvc.NewService(artifactCatalogRepo))
	artifactHandler.SetProjectService(fixture.projectService)
	artifactHandler.SetJobService(fixture.jobService)
	artifactHandler.SetAuthorization(auth.ModeHeader, fixture.membershipService)

	catalogReq := httptest.NewRequest(http.MethodGet, "/artifacts/catalog?project_id="+fixture.projectViewer.ID, nil)
	catalogReq = withScopedAPIToken(catalogReq, fixture.viewer, domain.APITokenScopeBuildRead)
	catalogRes := httptest.NewRecorder()
	artifactHandler.ListArtifactCatalog(catalogRes, catalogReq)
	if catalogRes.Code != http.StatusForbidden {
		t.Fatalf("expected missing artifact:read for catalog status %d, got %d body=%s", http.StatusForbidden, catalogRes.Code, catalogRes.Body.String())
	}

	getArtifactReq := addURLParams(httptest.NewRequest(http.MethodGet, "/artifacts/artifact-1", nil), map[string]string{"artifactID": "artifact-1"})
	getArtifactReq = withScopedAPIToken(getArtifactReq, fixture.viewer, domain.APITokenScopeBuildRead)
	getArtifactRes := httptest.NewRecorder()
	artifactHandler.GetArtifact(getArtifactRes, getArtifactReq)
	if getArtifactRes.Code != http.StatusForbidden {
		t.Fatalf("expected missing artifact:read for get artifact status %d, got %d body=%s", http.StatusForbidden, getArtifactRes.Code, getArtifactRes.Body.String())
	}
}

func withScopedAPIToken(req *http.Request, user domain.User, scopes ...domain.APITokenScope) *http.Request {
	ctx := auth.WithUser(req.Context(), user)
	ctx = auth.WithAuthMethod(ctx, auth.MethodAPIToken)
	ctx = auth.WithAuthenticatedAPIToken(ctx, "token-1", scopes)
	return req.WithContext(ctx)
}

type handlerAuthzFixture struct {
	now               time.Time
	projectViewer     domain.Project
	projectOther      domain.Project
	viewer            domain.User
	maintainer        domain.User
	owner             domain.User
	outsider          domain.User
	admin             domain.User
	projectService    *service.ProjectService
	jobService        *service.JobService
	buildService      *buildsvc.BuildService
	membershipService *service.ProjectMembershipService
}

func newHandlerAuthzFixture(t *testing.T) handlerAuthzFixture {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	jobRepo := repositorymemory.NewJobRepository()
	buildRepo := repositorymemory.NewBuildRepository()
	projectRepo := repositorymemory.NewProjectRepository(jobRepo)
	userRepo := repositorymemory.NewUserRepository()
	membershipRepo := repositorymemory.NewProjectMembershipRepository(projectRepo, userRepo)
	buildService := buildsvc.NewBuildService(buildRepo, nil, logs.NewNoopSink())
	projectService := service.NewProjectService(projectRepo)
	jobService := service.NewJobService(jobRepo, buildService).WithProjectRepository(projectRepo)
	membershipService := service.NewProjectMembershipService(projectRepo, membershipRepo)

	projectViewer, err := projectService.CreateProject(ctx, service.CreateProjectInput{Name: "Platform", Slug: "platform"})
	if err != nil {
		t.Fatalf("create viewer project failed: %v", err)
	}
	projectOther, err := projectService.CreateProject(ctx, service.CreateProjectInput{Name: "Other", Slug: "other"})
	if err != nil {
		t.Fatalf("create other project failed: %v", err)
	}

	viewer, err := userRepo.Create(ctx, domain.User{ID: "viewer-1", Email: "viewer@example.com", GlobalRole: domain.GlobalRoleUser, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatalf("create viewer failed: %v", err)
	}
	maintainer, err := userRepo.Create(ctx, domain.User{ID: "maintainer-1", Email: "maintainer@example.com", GlobalRole: domain.GlobalRoleUser, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatalf("create maintainer failed: %v", err)
	}
	owner, err := userRepo.Create(ctx, domain.User{ID: "owner-1", Email: "owner@example.com", GlobalRole: domain.GlobalRoleUser, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatalf("create owner failed: %v", err)
	}
	outsider, err := userRepo.Create(ctx, domain.User{ID: "outsider-1", Email: "outsider@example.com", GlobalRole: domain.GlobalRoleUser, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatalf("create outsider failed: %v", err)
	}
	admin, err := userRepo.Create(ctx, domain.User{ID: "admin-1", Email: "admin@example.com", GlobalRole: domain.GlobalRoleAdmin, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatalf("create admin failed: %v", err)
	}

	for _, membership := range []struct {
		projectID string
		userID    string
		role      string
	}{
		{projectID: projectViewer.ID, userID: viewer.ID, role: "viewer"},
		{projectID: projectViewer.ID, userID: maintainer.ID, role: "maintainer"},
		{projectID: projectViewer.ID, userID: owner.ID, role: "owner"},
	} {
		if _, err := membershipService.UpsertProjectMembership(ctx, service.UpsertProjectMembershipInput{ProjectID: membership.projectID, UserID: membership.userID, Role: membership.role}); err != nil {
			t.Fatalf("create membership failed: %v", err)
		}
	}

	return handlerAuthzFixture{
		now:               now,
		projectViewer:     projectViewer,
		projectOther:      projectOther,
		viewer:            viewer,
		maintainer:        maintainer,
		owner:             owner,
		outsider:          outsider,
		admin:             admin,
		projectService:    projectService,
		jobService:        jobService,
		buildService:      buildService,
		membershipService: membershipService,
	}
}
