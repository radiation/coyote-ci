package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

func TestArtifactHandlerListArtifactsRejectsInvalidType(t *testing.T) {
	handler := NewArtifactHandler(artifactsvc.NewService(&fakeArtifactBrowseRepo{}))
	req := httptest.NewRequest(http.MethodGet, "/artifacts?type=not-real", nil)
	w := httptest.NewRecorder()

	handler.ListArtifacts(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestArtifactHandlerListArtifactsForwardsPaginationParams(t *testing.T) {
	repo := &fakeArtifactBrowseRepo{}
	handler := NewArtifactHandler(artifactsvc.NewService(repo))
	req := httptest.NewRequest(http.MethodGet, "/artifacts?q=pkg&type=npm_package&limit=5&offset=10", nil)
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
	if repo.params[0].Limit != 5 {
		t.Fatalf("expected limit 5, got %d", repo.params[0].Limit)
	}
	if repo.params[0].Offset != 10 {
		t.Fatalf("expected offset 10, got %d", repo.params[0].Offset)
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
	if _, err := jobRepo.Create(context.Background(), domain.Job{
		ID:            jobID,
		ProjectID:     project.ID,
		Name:          "backend-ci",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PipelineYAML:  "version: 1",
		Enabled:       true,
		CreatedAt:     now,
		UpdatedAt:     now,
	}); err != nil {
		t.Fatalf("create job failed: %v", err)
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
	if _, err := jobRepo.Create(context.Background(), domain.Job{
		ID:            jobID,
		ProjectID:     project.ID,
		Name:          "backend-ci",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PipelineYAML:  "version: 1",
		Enabled:       true,
		CreatedAt:     now,
		UpdatedAt:     now,
	}); err != nil {
		t.Fatalf("create job failed: %v", err)
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
	if _, err := jobRepo.Create(context.Background(), domain.Job{
		ID:            jobID,
		ProjectID:     project.ID,
		Name:          "backend-ci",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PipelineYAML:  "version: 1",
		Enabled:       true,
		CreatedAt:     now,
		UpdatedAt:     now,
	}); err != nil {
		t.Fatalf("create job failed: %v", err)
	}
	handler.SetProjectService(projectService)
	handler.SetJobService(service.NewJobService(jobRepo, nil))

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
	if len(repo.ids) != 1 || repo.ids[0] != "artifact-1" {
		t.Fatalf("expected artifact lookup for artifact-1, got %v", repo.ids)
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
}

func ptrString(value string) *string {
	return &value
}
