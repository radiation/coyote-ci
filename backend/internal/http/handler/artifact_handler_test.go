package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/auth"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	repositorymemory "github.com/radiation/coyote-ci/backend/internal/repository/memory"
	"github.com/radiation/coyote-ci/backend/internal/service"
	artifactsvc "github.com/radiation/coyote-ci/backend/internal/service/artifact"
	versiontagsvc "github.com/radiation/coyote-ci/backend/internal/service/versiontag"
)

type trackingJobRepo struct {
	repositorymemory.JobRepository
	listCalls     int
	getByIDsCalls int
	lastIDs       []string
}

func newTrackingJobRepo() *trackingJobRepo {
	return &trackingJobRepo{JobRepository: *repositorymemory.NewJobRepository()}
}

func (r *trackingJobRepo) List(ctx context.Context) ([]domain.Job, error) {
	r.listCalls++
	return r.JobRepository.List(ctx)
}

func (r *trackingJobRepo) GetByIDs(ctx context.Context, ids []string) ([]domain.Job, error) {
	r.getByIDsCalls++
	r.lastIDs = append([]string(nil), ids...)
	return r.JobRepository.GetByIDs(ctx, ids)
}

type fakeArtifactBrowseRepo struct {
	records []domain.ArtifactBrowseRecord
	err     error
	params  []repository.BrowseArtifactsParams
}

func (r *fakeArtifactBrowseRepo) Browse(_ context.Context, params repository.BrowseArtifactsParams) ([]domain.ArtifactBrowseRecord, error) {
	r.params = append(r.params, params)
	return r.records, r.err
}

type fakeArtifactCatalogRepo struct {
	records []domain.ArtifactRecord
	record  domain.ArtifactRecord
	err     error
	params  []repository.ArtifactCatalogParams
	ids     []string
}

func (r *fakeArtifactCatalogRepo) Browse(_ context.Context, _ repository.BrowseArtifactsParams) ([]domain.ArtifactBrowseRecord, error) {
	return nil, nil
}

func (r *fakeArtifactCatalogRepo) ListCatalog(_ context.Context, params repository.ArtifactCatalogParams) ([]domain.ArtifactRecord, error) {
	r.params = append(r.params, params)
	return r.records, r.err
}

func (r *fakeArtifactCatalogRepo) GetCatalogByID(_ context.Context, artifactID string) (domain.ArtifactRecord, error) {
	r.ids = append(r.ids, artifactID)
	if r.err != nil {
		return domain.ArtifactRecord{}, r.err
	}
	return r.record, nil
}

func TestArtifactHandlerListArtifacts(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	jobID := "job-1"
	repo := &fakeArtifactBrowseRepo{records: []domain.ArtifactBrowseRecord{
		{
			Artifact: domain.BuildArtifact{ID: "artifact-2", BuildID: "build-2", LogicalPath: "packages/pkg-a-1.2.3.tgz", CreatedAt: now.Add(2 * time.Minute), StorageProvider: domain.StorageProviderFilesystem, SizeBytes: 256},
			Build:    domain.Build{ID: "build-2", BuildNumber: 22, JobID: &jobID, ProjectID: "project-1", Status: domain.BuildStatusSuccess, CreatedAt: now.Add(2 * time.Minute)},
			Step:     &domain.BuildStep{ID: "step-2", BuildID: "build-2", StepIndex: 1, Name: "Publish package"},
		},
		{
			Artifact: domain.BuildArtifact{ID: "artifact-1", BuildID: "build-1", LogicalPath: "packages/pkg-a-1.2.2.tgz", CreatedAt: now, StorageProvider: domain.StorageProviderFilesystem, SizeBytes: 128},
			Build:    domain.Build{ID: "build-1", BuildNumber: 21, JobID: &jobID, ProjectID: "project-1", Status: domain.BuildStatusSuccess, CreatedAt: now},
			Step:     &domain.BuildStep{ID: "step-1", BuildID: "build-1", StepIndex: 1, Name: "Publish package"},
		},
	}}
	handler := NewArtifactHandler(artifactsvc.NewService(repo))
	jobRepo := repositorymemory.NewJobRepository()
	projectRepo := repositorymemory.NewProjectRepository(jobRepo)
	projectService := service.NewProjectService(projectRepo)
	project, err := projectService.CreateProject(context.Background(), service.CreateProjectInput{Name: "Platform", Slug: "platform"})
	if err != nil {
		t.Fatalf("create project failed: %v", err)
	}
	repo.records[0].Build.ProjectID = project.ID
	repo.records[1].Build.ProjectID = project.ID
	if _, createErr := jobRepo.Create(context.Background(), domain.Job{
		ID:            jobID,
		ProjectID:     project.ID,
		Name:          "backend-ci",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PipelineYAML:  "version: 1",
		Enabled:       true,
		CreatedAt:     now,
		UpdatedAt:     now,
	}); createErr != nil {
		t.Fatalf("create job failed: %v", createErr)
	}
	handler.SetProjectService(projectService)
	handler.SetJobService(service.NewJobService(jobRepo, nil))

	versionTagRepo := repositorymemory.NewVersionTagRepository()
	versionTagRepo.SeedBuilds(repo.records[0].Build, repo.records[1].Build)
	versionTagRepo.SeedArtifacts(repo.records[0].Artifact, repo.records[1].Artifact)
	_, err = versionTagRepo.CreateForTargets(context.Background(), repository.CreateVersionTagsParams{
		JobID:       jobID,
		Version:     "v1.2.3",
		ArtifactIDs: []string{"artifact-2"},
	})
	if err != nil {
		t.Fatalf("CreateForTargets returned error: %v", err)
	}
	handler.SetVersionTagService(versiontagsvc.NewService(versionTagRepo))

	req := httptest.NewRequest(http.MethodGet, "/artifacts?type=npm_package", nil)
	w := httptest.NewRecorder()
	handler.ListArtifacts(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var response struct {
		Data struct {
			Artifacts []map[string]any `json:"artifacts"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(response.Data.Artifacts) != 2 {
		t.Fatalf("expected 2 artifact groups, got %d", len(response.Data.Artifacts))
	}
	first := response.Data.Artifacts[0]
	if first["artifact_type"] != "npm_package" {
		t.Fatalf("expected npm_package type, got %v", first["artifact_type"])
	}
	if first["project_name"] != "Platform" {
		t.Fatalf("expected project_name Platform, got %v", first["project_name"])
	}
	if first["project_slug"] != "platform" {
		t.Fatalf("expected project_slug platform, got %v", first["project_slug"])
	}
	if first["job_name"] != "backend-ci" {
		t.Fatalf("expected job_name backend-ci, got %v", first["job_name"])
	}
	versions, ok := first["versions"].([]any)
	if !ok {
		t.Fatalf("expected versions to be []any, got %T", first["versions"])
	}
	if len(versions) != 1 {
		t.Fatalf("expected 1 version on first artifact, got %d", len(versions))
	}
	version, ok := versions[0].(map[string]any)
	if !ok {
		t.Fatalf("expected version to be map[string]any, got %T", versions[0])
	}
	tags, ok := version["version_tags"].([]any)
	if !ok {
		t.Fatalf("expected version_tags to be []any, got %T", version["version_tags"])
	}
	if len(tags) != 1 {
		t.Fatalf("expected 1 version tag, got %d", len(tags))
	}
	if version["job_name"] != "backend-ci" {
		t.Fatalf("expected version job_name backend-ci, got %v", version["job_name"])
	}
	if version["step_name"] != "Publish package" {
		t.Fatalf("expected step name, got %v", version["step_name"])
	}
	if version["download_url_path"] != "/builds/build-2/artifacts/artifact-2/download" {
		t.Fatalf("expected route-relative download path, got %v", version["download_url_path"])
	}
}

type artifactStubProjectRoleLookup struct {
	membership domain.ProjectMembership
	err        error
}

func (s artifactStubProjectRoleLookup) GetProjectMembership(_ context.Context, projectID string, userID string) (domain.ProjectMembership, error) {
	if s.err != nil {
		return domain.ProjectMembership{}, s.err
	}
	if s.membership.ProjectID != projectID || s.membership.UserID != userID {
		return domain.ProjectMembership{}, repository.ErrProjectMembershipNotFound
	}
	return s.membership, nil
}

func TestArtifactHandlerListArtifactsRejectsInvalidType(t *testing.T) {
	handler := NewArtifactHandler(artifactsvc.NewService(&fakeArtifactBrowseRepo{}))
	req := httptest.NewRequest(http.MethodGet, "/artifacts?type=not-real", nil)
	w := httptest.NewRecorder()

	handler.ListArtifacts(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestArtifactHandlerListArtifactsRequiresService(t *testing.T) {
	handler := &ArtifactHandler{}
	req := httptest.NewRequest(http.MethodGet, "/artifacts", nil)
	w := httptest.NewRecorder()

	handler.ListArtifacts(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestArtifactHandlerListArtifactCatalogRequiresCatalogRepository(t *testing.T) {
	handler := NewArtifactHandler(artifactsvc.NewService(&fakeArtifactBrowseRepo{}))
	req := httptest.NewRequest(http.MethodGet, "/artifacts/catalog", nil)
	w := httptest.NewRecorder()

	handler.ListArtifactCatalog(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
	if got := decodeErrorMessage(t, w); got != "artifact repository not configured" {
		t.Fatalf("expected repository not configured error, got %q", got)
	}
}

func TestArtifactHandlerGetArtifactRequiresConfiguredService(t *testing.T) {
	handler := &ArtifactHandler{}
	req := addURLParams(httptest.NewRequest(http.MethodGet, "/artifacts/artifact-1", nil), map[string]string{"artifactID": "artifact-1"})
	w := httptest.NewRecorder()

	handler.GetArtifact(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestArtifactHandlerGetArtifactRequiresCatalogRepository(t *testing.T) {
	handler := NewArtifactHandler(artifactsvc.NewService(&fakeArtifactBrowseRepo{}))
	req := addURLParams(httptest.NewRequest(http.MethodGet, "/artifacts/artifact-1", nil), map[string]string{"artifactID": "artifact-1"})
	w := httptest.NewRecorder()

	handler.GetArtifact(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
	if got := decodeErrorMessage(t, w); got != "artifact repository not configured" {
		t.Fatalf("expected repository not configured error, got %q", got)
	}
}

func TestArtifactHandlerListArtifactCatalogRequiresAuthorizedProjectUser(t *testing.T) {
	handler := NewArtifactHandler(artifactsvc.NewService(&fakeArtifactCatalogRepo{}))
	handler.SetAuthorization(auth.ModeHeader, artifactStubProjectRoleLookup{})
	req := httptest.NewRequest(http.MethodGet, "/artifacts/catalog?project_id=project-1", nil)
	w := httptest.NewRecorder()

	handler.ListArtifactCatalog(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestArtifactHandlerListArtifactsForwardsPaginationParams(t *testing.T) {
	repo := &fakeArtifactBrowseRepo{}
	handler := NewArtifactHandler(artifactsvc.NewService(repo))
	req := httptest.NewRequest(http.MethodGet, "/artifacts?q=pkg&type=npm_package&job_id=job-1&limit=5&offset=10", nil)
	w := httptest.NewRecorder()

	handler.ListArtifacts(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(repo.params) != 1 {
		t.Fatalf("expected one browse call, got %d", len(repo.params))
	}
	if repo.params[0].Query != "pkg" {
		t.Fatalf("expected query pkg, got %q", repo.params[0].Query)
	}
	if repo.params[0].Type != domain.ArtifactTypeNPMPackage {
		t.Fatalf("expected npm_package type, got %q", repo.params[0].Type)
	}
	if repo.params[0].JobID != "job-1" {
		t.Fatalf("expected job id job-1, got %q", repo.params[0].JobID)
	}
	if repo.params[0].Limit != 5 {
		t.Fatalf("expected limit 5, got %d", repo.params[0].Limit)
	}
	if repo.params[0].Offset != 10 {
		t.Fatalf("expected offset 10, got %d", repo.params[0].Offset)
	}
}

func TestArtifactHandlerListArtifactsRejectsNegativePaginationParams(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		message string
	}{
		{name: "negative limit", query: "/artifacts?limit=-1", message: "limit must be a non-negative integer"},
		{name: "negative offset", query: "/artifacts?offset=-1", message: "offset must be a non-negative integer"},
		{name: "invalid limit", query: "/artifacts?limit=abc", message: "limit must be a non-negative integer"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			handler := NewArtifactHandler(artifactsvc.NewService(&fakeArtifactBrowseRepo{}))
			w := httptest.NewRecorder()

			handler.ListArtifacts(w, httptest.NewRequest(http.MethodGet, tc.query, nil))

			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
			if got := decodeErrorMessage(t, w); got != tc.message {
				t.Fatalf("expected %q, got %q", tc.message, got)
			}
		})
	}
}

func TestArtifactHandlerListArtifactsCapsLimit(t *testing.T) {
	repo := &fakeArtifactBrowseRepo{}
	handler := NewArtifactHandler(artifactsvc.NewService(repo))
	req := httptest.NewRequest(http.MethodGet, "/artifacts?limit=999&offset=10", nil)
	w := httptest.NewRecorder()

	handler.ListArtifacts(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(repo.params) != 1 {
		t.Fatalf("expected one browse call, got %d", len(repo.params))
	}
	if repo.params[0].Limit != repository.MaxPageLimit {
		t.Fatalf("expected limit capped to %d, got %d", repository.MaxPageLimit, repo.params[0].Limit)
	}
}

func TestArtifactHandlerListArtifactsUsesBatchJobLookup(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	jobID := "job-1"
	repo := &fakeArtifactBrowseRepo{records: []domain.ArtifactBrowseRecord{
		{
			Artifact: domain.BuildArtifact{ID: "artifact-1", BuildID: "build-1", LogicalPath: "packages/pkg-a-1.2.2.tgz", CreatedAt: now, StorageProvider: domain.StorageProviderFilesystem, SizeBytes: 128},
			Build:    domain.Build{ID: "build-1", BuildNumber: 21, JobID: &jobID, ProjectID: "project-1", Status: domain.BuildStatusSuccess, CreatedAt: now},
		},
	}}
	handler := NewArtifactHandler(artifactsvc.NewService(repo))
	jobRepo := newTrackingJobRepo()
	projectRepo := repositorymemory.NewProjectRepository(jobRepo)
	projectService := service.NewProjectService(projectRepo)
	project, err := projectService.CreateProject(context.Background(), service.CreateProjectInput{Name: "Platform", Slug: "platform"})
	if err != nil {
		t.Fatalf("create project failed: %v", err)
	}
	repo.records[0].Build.ProjectID = project.ID
	if _, createErr := jobRepo.Create(context.Background(), domain.Job{
		ID:            jobID,
		ProjectID:     project.ID,
		Name:          "backend-ci",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PipelineYAML:  "version: 1",
		Enabled:       true,
		CreatedAt:     now,
		UpdatedAt:     now,
	}); createErr != nil {
		t.Fatalf("create job failed: %v", createErr)
	}
	handler.SetProjectService(projectService)
	handler.SetJobService(service.NewJobService(jobRepo, nil))

	req := httptest.NewRequest(http.MethodGet, "/artifacts", nil)
	w := httptest.NewRecorder()
	handler.ListArtifacts(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if jobRepo.listCalls != 0 {
		t.Fatalf("expected List not to be called, got %d", jobRepo.listCalls)
	}
	if jobRepo.getByIDsCalls != 1 {
		t.Fatalf("expected GetByIDs to be called once, got %d", jobRepo.getByIDsCalls)
	}
	if len(jobRepo.lastIDs) != 1 || jobRepo.lastIDs[0] != jobID {
		t.Fatalf("expected GetByIDs to be called with %q, got %v", jobID, jobRepo.lastIDs)
	}
}

func TestArtifactHandlerListArtifacts_ForwardsProjectFilterFromSlug(t *testing.T) {
	repo := &fakeArtifactBrowseRepo{}
	handler := NewArtifactHandler(artifactsvc.NewService(repo))
	jobRepo := repositorymemory.NewJobRepository()
	projectRepo := repositorymemory.NewProjectRepository(jobRepo)
	projectService := service.NewProjectService(projectRepo)
	project, err := projectService.CreateProject(context.Background(), service.CreateProjectInput{Name: "Platform", Slug: "platform"})
	if err != nil {
		t.Fatalf("create project failed: %v", err)
	}
	handler.SetProjectService(projectService)
	req := httptest.NewRequest(http.MethodGet, "/artifacts?project_slug=platform", nil)
	w := httptest.NewRecorder()

	handler.ListArtifacts(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(repo.params) != 1 {
		t.Fatalf("expected one browse call, got %d", len(repo.params))
	}
	if repo.params[0].ProjectID != project.ID {
		t.Fatalf("expected project id %q, got %q", project.ID, repo.params[0].ProjectID)
	}
}

func TestArtifactHandlerListArtifactCatalog(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	jobID := "job-1"
	repo := &fakeArtifactCatalogRepo{records: []domain.ArtifactRecord{{
		Artifact: domain.BuildArtifact{
			ID:              "artifact-1",
			BuildID:         "build-1",
			StepID:          ptrString("step-1"),
			Name:            "coyote-ci/package-a",
			LogicalPath:     "packages/pkg-a.tgz",
			ArtifactType:    domain.ArtifactTypeNPMPackage,
			StorageKey:      "build-1/packages/pkg-a.tgz",
			StorageProvider: domain.StorageProviderFilesystem,
			SizeBytes:       128,
			CreatedAt:       now,
		},
		Build: domain.Build{ID: "build-1", BuildNumber: 41, JobID: &jobID, ProjectID: "project-1", Status: domain.BuildStatusSuccess, CreatedAt: now},
		Step:  &domain.BuildStep{ID: "step-1", BuildID: "build-1", StepIndex: 1, Name: "Publish package"},
	}}}
	handler := NewArtifactHandler(artifactsvc.NewService(repo))
	jobRepo := repositorymemory.NewJobRepository()
	projectRepo := repositorymemory.NewProjectRepository(jobRepo)
	projectService := service.NewProjectService(projectRepo)
	project, err := projectService.CreateProject(context.Background(), service.CreateProjectInput{Name: "Platform", Slug: "platform"})
	if err != nil {
		t.Fatalf("create project failed: %v", err)
	}
	repo.records[0].Build.ProjectID = project.ID
	if _, createErr := jobRepo.Create(context.Background(), domain.Job{
		ID:            jobID,
		ProjectID:     project.ID,
		Name:          "backend-ci",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PipelineYAML:  "version: 1",
		Enabled:       true,
		CreatedAt:     now,
		UpdatedAt:     now,
	}); createErr != nil {
		t.Fatalf("create job failed: %v", createErr)
	}
	handler.SetProjectService(projectService)
	handler.SetJobService(service.NewJobService(jobRepo, nil))

	req := httptest.NewRequest(http.MethodGet, "/artifacts/catalog?q=pkg&job_id=job-1&build_id=build-1&limit=5&offset=10", nil)
	w := httptest.NewRecorder()
	handler.ListArtifactCatalog(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(repo.params) != 1 {
		t.Fatalf("expected one catalog call, got %d", len(repo.params))
	}
	if repo.params[0].Query != "pkg" || repo.params[0].JobID != "job-1" || repo.params[0].BuildID != "build-1" {
		t.Fatalf("unexpected params: %#v", repo.params[0])
	}

	var response struct {
		Data struct {
			Artifacts []map[string]any `json:"artifacts"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(response.Data.Artifacts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(response.Data.Artifacts))
	}
	first := response.Data.Artifacts[0]
	if first["job_name"] != "backend-ci" {
		t.Fatalf("expected job_name backend-ci, got %v", first["job_name"])
	}
	if first["project_name"] != "Platform" {
		t.Fatalf("expected project_name Platform, got %v", first["project_name"])
	}
	if first["step_name"] != "Publish package" {
		t.Fatalf("expected step_name Publish package, got %v", first["step_name"])
	}
	if first["download_url_path"] != "/builds/build-1/artifacts/artifact-1/download" {
		t.Fatalf("unexpected download path: %v", first["download_url_path"])
	}
	if _, ok := first["storage_key"]; ok {
		t.Fatalf("expected storage_key to be omitted, got %v", first["storage_key"])
	}
}

func TestArtifactHandlerListArtifactCatalogRejectsNegativePaginationParams(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		message string
	}{
		{name: "negative limit", query: "/artifacts/catalog?limit=-1", message: "limit must be a non-negative integer"},
		{name: "negative offset", query: "/artifacts/catalog?offset=-1", message: "offset must be a non-negative integer"},
		{name: "invalid offset", query: "/artifacts/catalog?offset=abc", message: "offset must be a non-negative integer"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			handler := NewArtifactHandler(artifactsvc.NewService(&fakeArtifactCatalogRepo{}))
			w := httptest.NewRecorder()

			handler.ListArtifactCatalog(w, httptest.NewRequest(http.MethodGet, tc.query, nil))

			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
			if got := decodeErrorMessage(t, w); got != tc.message {
				t.Fatalf("expected %q, got %q", tc.message, got)
			}
		})
	}
}

func TestArtifactHandlerListArtifactCatalogCapsLimit(t *testing.T) {
	repo := &fakeArtifactCatalogRepo{}
	handler := NewArtifactHandler(artifactsvc.NewService(repo))
	req := httptest.NewRequest(http.MethodGet, "/artifacts/catalog?limit=999&offset=10", nil)
	w := httptest.NewRecorder()

	handler.ListArtifactCatalog(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(repo.params) != 1 {
		t.Fatalf("expected one catalog call, got %d", len(repo.params))
	}
	if repo.params[0].Limit != repository.MaxPageLimit {
		t.Fatalf("expected limit capped to %d, got %d", repository.MaxPageLimit, repo.params[0].Limit)
	}
}

func TestArtifactHandlerListArtifactCatalog_ForwardsProjectFilterFromSlug(t *testing.T) {
	repo := &fakeArtifactCatalogRepo{}
	handler := NewArtifactHandler(artifactsvc.NewService(repo))
	jobRepo := repositorymemory.NewJobRepository()
	projectRepo := repositorymemory.NewProjectRepository(jobRepo)
	projectService := service.NewProjectService(projectRepo)
	project, err := projectService.CreateProject(context.Background(), service.CreateProjectInput{Name: "Platform", Slug: "platform"})
	if err != nil {
		t.Fatalf("create project failed: %v", err)
	}
	handler.SetProjectService(projectService)
	req := httptest.NewRequest(http.MethodGet, "/artifacts/catalog?project_slug=platform", nil)
	w := httptest.NewRecorder()

	handler.ListArtifactCatalog(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(repo.params) != 1 || repo.params[0].ProjectID != project.ID {
		t.Fatalf("expected project id %q, got %#v", project.ID, repo.params)
	}
}

func TestArtifactHandlerListArtifactCatalog_InternalError(t *testing.T) {
	handler := NewArtifactHandler(artifactsvc.NewService(&fakeArtifactCatalogRepo{err: errors.New("boom")}))
	req := httptest.NewRequest(http.MethodGet, "/artifacts/catalog", nil)
	w := httptest.NewRecorder()

	handler.ListArtifactCatalog(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
	if got := decodeErrorMessage(t, w); got != "internal server error" {
		t.Fatalf("expected internal server error, got %q", got)
	}
}

func TestArtifactHandlerGetArtifact(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	jobID := "job-1"
	repo := &fakeArtifactCatalogRepo{record: domain.ArtifactRecord{
		Artifact: domain.BuildArtifact{
			ID:              "artifact-1",
			BuildID:         "build-1",
			StepID:          ptrString("step-1"),
			Name:            "coyote-ci/package-a",
			LogicalPath:     "packages/pkg-a.tgz",
			ArtifactType:    domain.ArtifactTypeNPMPackage,
			StorageKey:      "build-1/packages/pkg-a.tgz",
			StorageProvider: domain.StorageProviderFilesystem,
			SizeBytes:       128,
			CreatedAt:       now,
		},
		Build: domain.Build{ID: "build-1", BuildNumber: 41, JobID: &jobID, ProjectID: "project-1", Status: domain.BuildStatusSuccess, CreatedAt: now},
		Step:  &domain.BuildStep{ID: "step-1", BuildID: "build-1", StepIndex: 1, Name: "Publish package"},
	}}
	handler := NewArtifactHandler(artifactsvc.NewService(repo))
	jobRepo := repositorymemory.NewJobRepository()
	projectRepo := repositorymemory.NewProjectRepository(jobRepo)
	projectService := service.NewProjectService(projectRepo)
	project, err := projectService.CreateProject(context.Background(), service.CreateProjectInput{Name: "Platform", Slug: "platform"})
	if err != nil {
		t.Fatalf("create project failed: %v", err)
	}
	repo.record.Build.ProjectID = project.ID
	if _, createErr := jobRepo.Create(context.Background(), domain.Job{
		ID:            jobID,
		ProjectID:     project.ID,
		Name:          "backend-ci",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PipelineYAML:  "version: 1",
		Enabled:       true,
		CreatedAt:     now,
		UpdatedAt:     now,
	}); createErr != nil {
		t.Fatalf("create job failed: %v", createErr)
	}
	handler.SetProjectService(projectService)
	handler.SetJobService(service.NewJobService(jobRepo, nil))
	versionTagRepo := repositorymemory.NewVersionTagRepository()
	versionTagRepo.SeedBuilds(repo.record.Build)
	versionTagRepo.SeedArtifacts(repo.record.Artifact)
	_, err = versionTagRepo.CreateForTargets(context.Background(), repository.CreateVersionTagsParams{
		JobID:       jobID,
		Version:     "release-2026.05.19",
		ArtifactIDs: []string{"artifact-1"},
	})
	if err != nil {
		t.Fatalf("seed version tags failed: %v", err)
	}
	handler.SetVersionTagService(versiontagsvc.NewService(versionTagRepo))

	req := addURLParams(httptest.NewRequest(http.MethodGet, "/artifacts/artifact-1", nil), map[string]string{"artifactID": "artifact-1"})
	w := httptest.NewRecorder()
	handler.GetArtifact(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var response struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if response.Data["job_name"] != "backend-ci" {
		t.Fatalf("expected job name backend-ci, got %v", response.Data["job_name"])
	}
	if response.Data["download_url_path"] != "/builds/build-1/artifacts/artifact-1/download" {
		t.Fatalf("expected route-relative download path, got %v", response.Data["download_url_path"])
	}
	versionTags, ok := response.Data["version_tags"].([]any)
	if !ok || len(versionTags) != 1 {
		t.Fatalf("expected one version tag, got %v", response.Data["version_tags"])
	}
	if _, ok := response.Data["storage_key"]; ok {
		t.Fatalf("expected storage_key to be omitted, got %v", response.Data["storage_key"])
	}
	if len(repo.ids) != 1 || repo.ids[0] != "artifact-1" {
		t.Fatalf("expected artifact lookup for artifact-1, got %v", repo.ids)
	}
}

func TestArtifactHandlerGetArtifact_VersionTagError(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	jobID := "job-1"
	repo := &fakeArtifactCatalogRepo{record: domain.ArtifactRecord{
		Artifact: domain.BuildArtifact{ID: "artifact-1", BuildID: "build-1", LogicalPath: "packages/pkg-a.tgz", CreatedAt: now},
		Build:    domain.Build{ID: "build-1", BuildNumber: 41, JobID: &jobID, ProjectID: "project-1", Status: domain.BuildStatusSuccess, CreatedAt: now},
	}}
	handler := NewArtifactHandler(artifactsvc.NewService(repo))
	handler.SetVersionTagService(versiontagsvc.NewService(errorVersionTagRepo{err: errors.New("boom")}))

	req := addURLParams(httptest.NewRequest(http.MethodGet, "/artifacts/artifact-1", nil), map[string]string{"artifactID": "artifact-1"})
	w := httptest.NewRecorder()
	handler.GetArtifact(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
	if got := decodeErrorMessage(t, w); got != "internal server error" {
		t.Fatalf("expected internal server error, got %q", got)
	}
}

func TestArtifactHandlerGetArtifact_InternalError(t *testing.T) {
	handler := NewArtifactHandler(artifactsvc.NewService(&fakeArtifactCatalogRepo{err: errors.New("boom")}))
	req := addURLParams(httptest.NewRequest(http.MethodGet, "/artifacts/artifact-1", nil), map[string]string{"artifactID": "artifact-1"})
	w := httptest.NewRecorder()

	handler.GetArtifact(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
	if got := decodeErrorMessage(t, w); got != "internal server error" {
		t.Fatalf("expected internal server error, got %q", got)
	}
}

func TestArtifactHandlerGetArtifact_NotFound(t *testing.T) {
	handler := NewArtifactHandler(artifactsvc.NewService(&fakeArtifactCatalogRepo{err: repository.ErrArtifactNotFound}))
	req := addURLParams(httptest.NewRequest(http.MethodGet, "/artifacts/missing", nil), map[string]string{"artifactID": "missing"})
	w := httptest.NewRecorder()

	handler.GetArtifact(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
	var payload map[string]map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode error payload: %v", err)
	}
	if payload["error"]["code"] != "artifact_not_found" {
		t.Fatalf("expected artifact_not_found code, got %q", payload["error"]["code"])
	}
}

func TestArtifactHandlerResolveProjectFilterBranches(t *testing.T) {
	handler := &ArtifactHandler{}

	projectID, ok := handler.resolveProjectFilter(
		httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/artifacts?project_id=%20project-1%20", nil),
	)
	if !ok || projectID != "project-1" {
		t.Fatalf("expected trimmed project id, got %q ok=%v", projectID, ok)
	}

	w := httptest.NewRecorder()
	_, ok = handler.resolveProjectFilter(w, httptest.NewRequest(http.MethodGet, "/artifacts?project_slug=platform", nil))
	if ok || w.Code != http.StatusInternalServerError {
		t.Fatalf("expected missing project service to fail with 500, got ok=%v code=%d", ok, w.Code)
	}

	jobRepo := repositorymemory.NewJobRepository()
	projectRepo := repositorymemory.NewProjectRepository(jobRepo)
	projectService := service.NewProjectService(projectRepo)
	project, err := projectService.CreateProject(context.Background(), service.CreateProjectInput{Name: "Platform", Slug: "platform"})
	if err != nil {
		t.Fatalf("create project failed: %v", err)
	}
	handler.SetProjectService(projectService)

	w = httptest.NewRecorder()
	_, ok = handler.resolveProjectFilter(w, httptest.NewRequest(http.MethodGet, "/artifacts?project_slug=missing", nil))
	if ok || w.Code != http.StatusNotFound {
		t.Fatalf("expected missing slug to fail with 404, got ok=%v code=%d", ok, w.Code)
	}

	w = httptest.NewRecorder()
	_, ok = handler.resolveProjectFilter(
		w,
		httptest.NewRequest(http.MethodGet, "/artifacts?project_id=other&project_slug=platform", nil),
	)
	if ok || w.Code != http.StatusBadRequest {
		t.Fatalf("expected mismatched id and slug to fail with 400, got ok=%v code=%d", ok, w.Code)
	}

	w = httptest.NewRecorder()
	resolvedProjectID, ok := handler.resolveProjectFilter(
		w,
		httptest.NewRequest(http.MethodGet, "/artifacts?project_slug=platform", nil),
	)
	if !ok || resolvedProjectID != project.ID {
		t.Fatalf("expected slug to resolve to %q, got %q ok=%v", project.ID, resolvedProjectID, ok)
	}
}

func TestArtifactHandlerFiltersArtifactsByProjectAccess(t *testing.T) {
	handler := &ArtifactHandler{
		authMode: auth.ModeHeader,
		projectRoles: artifactStubProjectRoleLookup{membership: domain.ProjectMembership{
			ProjectID: "project-1",
			UserID:    "user-1",
			Role:      domain.ProjectMemberRoleViewer,
		}},
	}
	ctx := auth.WithUser(context.Background(), domain.User{ID: "user-1", GlobalRole: domain.GlobalRoleUser})
	jobID := "job-1"
	items, err := handler.filterArtifactsForRead(ctx, []domain.ArtifactBrowseItem{
		{Key: "one", ProjectID: "project-1", Versions: []domain.ArtifactBrowseVersion{{Artifact: domain.BuildArtifact{ID: "artifact-1"}, Build: domain.Build{ID: "build-1", ProjectID: "project-1", JobID: &jobID}}}},
		{Key: "two", ProjectID: "project-2", Versions: []domain.ArtifactBrowseVersion{{Artifact: domain.BuildArtifact{ID: "artifact-2"}, Build: domain.Build{ID: "build-2", ProjectID: "project-2"}}}},
	})
	if err != nil {
		t.Fatalf("filterArtifactsForRead returned error: %v", err)
	}
	if len(items) != 1 || items[0].ProjectID != "project-1" {
		t.Fatalf("expected only allowed project artifacts, got %#v", items)
	}

	records, err := handler.filterArtifactRecordsForRead(ctx, []domain.ArtifactRecord{
		{Artifact: domain.BuildArtifact{ID: "artifact-1"}, Build: domain.Build{ID: "build-1", ProjectID: "project-1"}},
		{Artifact: domain.BuildArtifact{ID: "artifact-2"}, Build: domain.Build{ID: "build-2", ProjectID: "project-2"}},
	})
	if err != nil {
		t.Fatalf("filterArtifactRecordsForRead returned error: %v", err)
	}
	if len(records) != 1 || records[0].Build.ProjectID != "project-1" {
		t.Fatalf("expected only allowed project records, got %#v", records)
	}
}

func TestArtifactHandlerResponseHelpersUseFallbacks(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	jobID := "job-1"
	sourceRef := "refs/heads/main"
	sourceSHA := "abcdef1234567890"
	browseVersion := domain.ArtifactBrowseVersion{
		Artifact: domain.BuildArtifact{
			ID:          "artifact-1",
			BuildID:     "build-1",
			LogicalPath: "packages/pkg-a.tgz",
			VersionTags: []domain.VersionTag{{ID: "tag-version", Kind: domain.VersionTagKindVersion, Version: "1.2.3"}, {ID: "tag-channel", Kind: domain.VersionTagKindChannel, Version: "latest"}},
			CreatedAt:   now,
		},
		Build: domain.Build{
			ID:          "build-1",
			ProjectID:   "project-1",
			JobID:       &jobID,
			Status:      domain.BuildStatusSuccess,
			SourceRef:   &sourceRef,
			SourceSHA:   &sourceSHA,
			BuildNumber: 42,
			CreatedAt:   now,
		},
	}
	browseResponse := toArtifactBrowseVersionResponse(browseVersion, map[string]domain.Project{}, map[string]domain.Job{})
	if browseResponse.StorageProvider != string(domain.StorageProviderFilesystem) {
		t.Fatalf("expected default filesystem provider, got %q", browseResponse.StorageProvider)
	}
	if browseResponse.StepIndex != nil || browseResponse.StepName != nil {
		t.Fatalf("expected nil step metadata, got %#v %#v", browseResponse.StepIndex, browseResponse.StepName)
	}
	if browseResponse.ProjectName != nil || browseResponse.ProjectSlug != nil || browseResponse.JobName != nil {
		t.Fatalf("expected missing lookup metadata to remain nil, got %#v %#v %#v", browseResponse.ProjectName, browseResponse.ProjectSlug, browseResponse.JobName)
	}
	if browseResponse.Lineage == nil {
		t.Fatal("expected browse lineage response")
	}
	if browseResponse.Lineage.BuildNumber != 42 || browseResponse.Lineage.ArtifactPath != "packages/pkg-a.tgz" {
		t.Fatalf("unexpected browse lineage %#v", browseResponse.Lineage)
	}
	if len(browseResponse.Lineage.Versions) != 1 || browseResponse.Lineage.Versions[0] != "1.2.3" {
		t.Fatalf("expected browse lineage version labels, got %#v", browseResponse.Lineage)
	}
	if len(browseResponse.Lineage.Channels) != 1 || browseResponse.Lineage.Channels[0] != "latest" {
		t.Fatalf("expected browse lineage channel labels, got %#v", browseResponse.Lineage)
	}
	if browseResponse.Lineage.GitRef == nil || *browseResponse.Lineage.GitRef != sourceRef {
		t.Fatalf("expected browse lineage git ref %q, got %#v", sourceRef, browseResponse.Lineage)
	}
	if browseResponse.Lineage.GitSHA == nil || *browseResponse.Lineage.GitSHA != sourceSHA {
		t.Fatalf("expected browse lineage git sha %q, got %#v", sourceSHA, browseResponse.Lineage)
	}

	record := domain.ArtifactRecord{
		Artifact: domain.BuildArtifact{ID: "artifact-1", BuildID: "build-1", LogicalPath: "packages/pkg-a.tgz", VersionTags: browseVersion.Artifact.VersionTags, CreatedAt: now},
		Build:    domain.Build{ID: "build-1", ProjectID: "project-1", JobID: &jobID, Status: domain.BuildStatusSuccess, SourceRef: &sourceRef, SourceSHA: &sourceSHA, BuildNumber: 42, CreatedAt: now},
	}
	catalogResponse := toArtifactCatalogItemResponse(record, map[string]domain.Project{}, map[string]domain.Job{})
	detailResponse := toArtifactDetailResponse(record, map[string]domain.Project{}, map[string]domain.Job{})
	if catalogResponse.StorageProvider != string(domain.StorageProviderFilesystem) {
		t.Fatalf("expected catalog default filesystem provider, got %q", catalogResponse.StorageProvider)
	}
	if detailResponse.StorageProvider != string(domain.StorageProviderFilesystem) {
		t.Fatalf("expected detail default filesystem provider, got %q", detailResponse.StorageProvider)
	}
	if detailResponse.Lineage == nil || detailResponse.Lineage.BuildNumber != 42 {
		t.Fatalf("expected detail lineage response, got %#v", detailResponse.Lineage)
	}

	projectName, projectSlug := projectContext(map[string]domain.Project{}, "missing")
	if projectName != nil || projectSlug != nil {
		t.Fatalf("expected missing project context to return nils, got %#v %#v", projectName, projectSlug)
	}
	if jobName := jobContext(map[string]domain.Job{}, nil); jobName != nil {
		t.Fatalf("expected nil job context for nil id, got %#v", jobName)
	}
	if jobName := jobContext(map[string]domain.Job{}, &jobID); jobName != nil {
		t.Fatalf("expected nil job context for missing lookup, got %#v", jobName)
	}
}

func TestArtifactHandlerWriteProjectLookupErrorInternal(t *testing.T) {
	handler := &ArtifactHandler{}
	w := httptest.NewRecorder()

	handler.writeProjectLookupError(w, errors.New("boom"))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
	if got := decodeErrorMessage(t, w); got != "internal server error" {
		t.Fatalf("expected internal server error, got %q", got)
	}
}

func ptrString(value string) *string {
	return &value
}

type errorVersionTagRepo struct {
	err error
}

func (r errorVersionTagRepo) ListByArtifactID(_ context.Context, _ string) ([]domain.VersionTag, error) {
	return nil, r.err
}

func (r errorVersionTagRepo) ListByArtifactIDs(_ context.Context, _ []string) ([]domain.VersionTag, error) {
	return nil, r.err
}

func (r errorVersionTagRepo) ListByManagedImageVersionID(_ context.Context, _ string) ([]domain.VersionTag, error) {
	return nil, r.err
}

func (r errorVersionTagRepo) ListByJobID(_ context.Context, _ string) ([]domain.VersionTag, error) {
	return nil, r.err
}

func (r errorVersionTagRepo) CreateForTargets(_ context.Context, _ repository.CreateVersionTagsParams) ([]domain.VersionTag, error) {
	return nil, r.err
}

func (r errorVersionTagRepo) ListByJobIDAndVersion(_ context.Context, _, _ string) ([]domain.VersionTag, error) {
	return nil, r.err
}
