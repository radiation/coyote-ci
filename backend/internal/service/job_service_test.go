package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/repository/memory"
	buildsvc "github.com/radiation/coyote-ci/backend/internal/service/build"
	webhooksvc "github.com/radiation/coyote-ci/backend/internal/service/webhook"
)

type selectorAwareProjectRepo struct {
	repository.ProjectRepository
	getByIDCalls   []string
	getBySlugCalls []string
}

type fakeServiceRepoFetcher struct {
	localPath string
	commitSHA string
	err       error
	calls     int
}

type fakeJobServiceArtifactRepo struct {
	artifactsByBuild map[string][]domain.BuildArtifact
	listByBuildErr   error
}

func (r *fakeJobServiceArtifactRepo) Create(_ context.Context, artifact domain.BuildArtifact) (domain.BuildArtifact, error) {
	if r.artifactsByBuild == nil {
		r.artifactsByBuild = map[string][]domain.BuildArtifact{}
	}
	r.artifactsByBuild[artifact.BuildID] = append(r.artifactsByBuild[artifact.BuildID], artifact)
	return artifact, nil
}

func (r *fakeJobServiceArtifactRepo) ListByBuildID(_ context.Context, buildID string) ([]domain.BuildArtifact, error) {
	if r.listByBuildErr != nil {
		return nil, r.listByBuildErr
	}
	items := append([]domain.BuildArtifact(nil), r.artifactsByBuild[buildID]...)
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	return items, nil
}

func (r *fakeJobServiceArtifactRepo) Browse(_ context.Context, _ repository.BrowseArtifactsParams) ([]domain.ArtifactBrowseRecord, error) {
	return nil, nil
}

func (r *fakeJobServiceArtifactRepo) ListCatalog(_ context.Context, _ repository.ArtifactCatalogParams) ([]domain.ArtifactRecord, error) {
	return nil, nil
}

func (r *fakeJobServiceArtifactRepo) GetCatalogByID(_ context.Context, artifactID string) (domain.ArtifactRecord, error) {
	for buildID, items := range r.artifactsByBuild {
		for _, item := range items {
			if item.ID == artifactID {
				return domain.ArtifactRecord{Artifact: item, Build: domain.Build{ID: buildID}}, nil
			}
		}
	}
	return domain.ArtifactRecord{}, repository.ErrArtifactNotFound
}

func (r *fakeJobServiceArtifactRepo) GetByID(_ context.Context, buildID string, artifactID string) (domain.BuildArtifact, error) {
	for _, item := range r.artifactsByBuild[buildID] {
		if item.ID == artifactID {
			return item, nil
		}
	}
	return domain.BuildArtifact{}, repository.ErrArtifactNotFound
}

func (r *fakeJobServiceArtifactRepo) ListByStepID(_ context.Context, stepID string) ([]domain.BuildArtifact, error) {
	var out []domain.BuildArtifact
	for _, items := range r.artifactsByBuild {
		for _, item := range items {
			if item.StepID != nil && *item.StepID == stepID {
				out = append(out, item)
			}
		}
	}
	return out, nil
}

func (f *fakeServiceRepoFetcher) Fetch(_ context.Context, _ string, _ string) (string, string, error) {
	f.calls++
	if f.err != nil {
		return "", "", f.err
	}
	return f.localPath, f.commitSHA, nil
}

type fakeServiceManagedImageRefresher struct {
	calls   int
	lastReq buildsvc.ManagedImageRefreshInput
	result  buildsvc.ManagedImageRefreshResult
	err     error
}

type erroringArtifactTriggerJobRepo struct {
	*memory.JobRepository
	getByIDsErr error
}

func (r *erroringArtifactTriggerJobRepo) GetByIDs(_ context.Context, _ []string) ([]domain.Job, error) {
	return nil, r.getByIDsErr
}

type erroringRegisteredRepositoryRepo struct {
	repository.SCMRepositoryRegistrationRepository
	getByIDsErr                     error
	getByConnectionAndProviderIDErr error
}

func (r *erroringRegisteredRepositoryRepo) GetByConnectionIDAndProviderRepositoryID(_ context.Context, _ string, _ string) (domain.SCMRepositoryRegistration, error) {
	return domain.SCMRepositoryRegistration{}, r.getByConnectionAndProviderIDErr
}

type erroringWebhookRoutingJobRepo struct {
	*memory.JobRepository
	listPushEnabledByRepositoryIDErr error
}

func (r *erroringWebhookRoutingJobRepo) ListPushEnabledByRepositoryID(_ context.Context, _ string) ([]domain.Job, error) {
	return nil, r.listPushEnabledByRepositoryIDErr
}

type urlLookupDetectingJobRepo struct {
	*memory.JobRepository
	legacyLookupCalls int
}

func (r *urlLookupDetectingJobRepo) ListPushEnabledByRepository(_ context.Context, _ string) ([]domain.Job, error) {
	r.legacyLookupCalls++
	return nil, errors.New("legacy repository URL lookup must not be used")
}

func (r *erroringRegisteredRepositoryRepo) GetByIDs(_ context.Context, _ []string) ([]domain.SCMRepositoryRegistration, error) {
	return nil, r.getByIDsErr
}

func (f *fakeServiceManagedImageRefresher) RefreshManagedPipelineImage(_ context.Context, req buildsvc.ManagedImageRefreshInput) (buildsvc.ManagedImageRefreshResult, error) {
	f.calls++
	f.lastReq = req
	return f.result, f.err
}

func (r *selectorAwareProjectRepo) GetByID(ctx context.Context, id string) (domain.Project, error) {
	r.getByIDCalls = append(r.getByIDCalls, id)
	if _, err := uuid.Parse(strings.TrimSpace(id)); err != nil {
		return domain.Project{}, errors.New("non-uuid selector reached GetByID")
	}
	return r.ProjectRepository.GetByID(ctx, id)
}

func (r *selectorAwareProjectRepo) GetBySlug(ctx context.Context, slug string) (domain.Project, error) {
	r.getBySlugCalls = append(r.getBySlugCalls, slug)
	return r.ProjectRepository.GetBySlug(ctx, slug)
}

func TestJobService_CreateListGetUpdate(t *testing.T) {
	jobRepo := memory.NewJobRepository()
	buildRepo := memory.NewBuildRepository()
	buildService := buildsvc.NewBuildService(buildRepo, nil, nil)
	jobService := NewJobService(jobRepo, buildService)

	job, err := jobService.CreateJob(context.Background(), CreateJobInput{
		ProjectID:     "project-1",
		Name:          "backend-ci",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PushEnabled:   boolPtr(true),
		PushBranch:    strPtr("main"),
		PipelineYAML:  "version: 1\nsteps:\n  - name: test\n    run: go test ./...\n",
		Enabled:       boolPtr(true),
	})
	if err != nil {
		t.Fatalf("create job failed: %v", err)
	}
	if !job.Enabled {
		t.Fatal("expected created job enabled=true")
	}
	if !job.PushEnabled {
		t.Fatal("expected created job push_enabled=true")
	}
	if job.PushBranch == nil || *job.PushBranch != "main" {
		t.Fatalf("expected created job push_branch=main, got %v", job.PushBranch)
	}

	builds, err := buildService.ListBuildsByJobID(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("list builds by job failed: %v", err)
	}
	if len(builds) != 1 {
		t.Fatalf("expected one initial build, got %d", len(builds))
	}
	if builds[0].Status != domain.BuildStatusQueued {
		t.Fatalf("expected initial build queued, got %q", builds[0].Status)
	}
	if builds[0].JobID == nil || *builds[0].JobID != job.ID {
		t.Fatalf("expected initial build job_id=%q, got %v", job.ID, builds[0].JobID)
	}

	list, err := jobService.ListJobs(context.Background())
	if err != nil {
		t.Fatalf("list jobs failed: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 job, got %d", len(list))
	}
	if list[0].LatestBuild == nil {
		t.Fatal("expected latest build summary on listed job")
	}
	if list[0].LatestBuild.Status != domain.BuildStatusQueued {
		t.Fatalf("expected listed latest build queued, got %q", list[0].LatestBuild.Status)
	}
	if list[0].LatestBuild.BuildNumber != builds[0].BuildNumber {
		t.Fatalf("expected listed latest build number %d, got %d", builds[0].BuildNumber, list[0].LatestBuild.BuildNumber)
	}

	got, err := jobService.GetJob(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("get job failed: %v", err)
	}
	if got.Name != "backend-ci" {
		t.Fatalf("expected backend-ci, got %q", got.Name)
	}

	updated, err := jobService.UpdateJob(context.Background(), job.ID, UpdateJobInput{
		Name:        strPtr("backend-ci-updated"),
		Enabled:     boolPtr(false),
		PushEnabled: boolPtr(false),
		PushBranch:  strPtr(""),
	})
	if err != nil {
		t.Fatalf("update job failed: %v", err)
	}
	if updated.Enabled {
		t.Fatal("expected updated enabled=false")
	}
	if updated.Name != "backend-ci-updated" {
		t.Fatalf("expected updated name, got %q", updated.Name)
	}
	if updated.PushEnabled {
		t.Fatal("expected updated push_enabled=false")
	}
	if updated.PushBranch != nil {
		t.Fatalf("expected updated push_branch=nil, got %v", updated.PushBranch)
	}
}

func TestJobService_CreateJobDisabledSkipsInitialBuild(t *testing.T) {
	jobRepo := memory.NewJobRepository()
	buildRepo := memory.NewBuildRepository()
	buildService := buildsvc.NewBuildService(buildRepo, nil, nil)
	jobService := NewJobService(jobRepo, buildService)

	job, err := jobService.CreateJob(context.Background(), CreateJobInput{
		ProjectID:     "project-1",
		Name:          "backend-disabled",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PipelineYAML:  "version: 1\nsteps:\n  - name: test\n    run: go test ./...\n",
		Enabled:       boolPtr(false),
	})
	if err != nil {
		t.Fatalf("create disabled job failed: %v", err)
	}

	builds, err := buildService.ListBuildsByJobID(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("list builds by job failed: %v", err)
	}
	if len(builds) != 0 {
		t.Fatalf("expected no initial build for disabled job, got %d", len(builds))
	}
}

func TestJobService_CreateAndUpdateWithRegisteredRepository(t *testing.T) {
	ctx := context.Background()
	jobRepo := memory.NewJobRepository()
	buildRepo := memory.NewBuildRepository()
	registeredRepo := memory.NewSCMRepositoryRegistrationRepository()
	buildService := buildsvc.NewBuildService(buildRepo, nil, nil)
	jobService := NewJobService(jobRepo, buildService).WithSCMRepositoryRegistrationRepository(registeredRepo)

	active, createErr := registeredRepo.Create(ctx, domain.SCMRepositoryRegistration{
		ID:                   "repo-1",
		ConnectionID:         "connection-1",
		ProviderRepositoryID: "1001",
		Owner:                "octo",
		Name:                 "widgets",
		FullName:             "octo/widgets",
		CloneURL:             "https://github.com/octo/widgets.git",
		WebURL:               "https://github.com/octo/widgets",
		MetadataRefreshedAt:  time.Now().UTC(),
		CreatedAt:            time.Now().UTC(),
		UpdatedAt:            time.Now().UTC(),
	})
	if createErr != nil {
		t.Fatalf("create registered repository failed: %v", createErr)
	}
	_, createErr = registeredRepo.Create(ctx, domain.SCMRepositoryRegistration{
		ID:                   "repo-disabled",
		ConnectionID:         "connection-1",
		ProviderRepositoryID: "1002",
		Owner:                "octo",
		Name:                 "disabled",
		FullName:             "octo/disabled",
		CloneURL:             "https://github.com/octo/disabled.git",
		WebURL:               "https://github.com/octo/disabled",
		Disabled:             true,
		MetadataRefreshedAt:  time.Now().UTC(),
		CreatedAt:            time.Now().UTC(),
		UpdatedAt:            time.Now().UTC(),
	})
	if createErr != nil {
		t.Fatalf("create disabled registered repository failed: %v", createErr)
	}

	mappedJob, err := jobService.CreateJob(ctx, CreateJobInput{
		ProjectID:    "project-1",
		Name:         "backend-ci",
		RepositoryID: active.ID,
		DefaultRef:   "main",
		PipelineYAML: "version: 1\nsteps:\n  - name: test\n    run: go test ./...\n",
		Enabled:      boolPtr(false),
	})
	if err != nil {
		t.Fatalf("create job with registered repository failed: %v", err)
	}
	if mappedJob.RepositoryID == nil || *mappedJob.RepositoryID != active.ID {
		t.Fatalf("expected repository_id %q, got %#v", active.ID, mappedJob.RepositoryID)
	}
	if mappedJob.RepositoryURL != active.CloneURL {
		t.Fatalf("expected repository_url %q, got %q", active.CloneURL, mappedJob.RepositoryURL)
	}

	blankRepositoryID := "  "
	_, updateErr := jobService.UpdateJob(ctx, mappedJob.ID, UpdateJobInput{RepositoryIDSet: true, RepositoryID: &blankRepositoryID})
	if !errors.Is(updateErr, ErrJobRepositoryIDEmpty) {
		t.Fatalf("expected empty repository id error, got %v", updateErr)
	}

	manualURL := "https://github.com/example/manual.git"
	_, updateErr = jobService.UpdateJob(ctx, mappedJob.ID, UpdateJobInput{RepositoryURL: &manualURL})
	if !errors.Is(updateErr, ErrJobRepositoryAssignmentConflict) {
		t.Fatalf("expected mapped job URL-only update conflict, got %v", updateErr)
	}

	_, updateErr = jobService.UpdateJob(ctx, mappedJob.ID, UpdateJobInput{RepositoryIDSet: true, RepositoryID: nil})
	if !errors.Is(updateErr, ErrJobRepositorySourceRequired) {
		t.Fatalf("expected clear without replacement URL error, got %v", updateErr)
	}

	blankURL := "  "
	_, updateErr = jobService.UpdateJob(ctx, mappedJob.ID, UpdateJobInput{RepositoryIDSet: true, RepositoryID: nil, RepositoryURL: &blankURL})
	if !errors.Is(updateErr, ErrJobRepositorySourceRequired) {
		t.Fatalf("expected blank replacement URL error, got %v", updateErr)
	}

	updated, updateErr := jobService.UpdateJob(ctx, mappedJob.ID, UpdateJobInput{RepositoryIDSet: true, RepositoryID: nil, RepositoryURL: &manualURL})
	if updateErr != nil {
		t.Fatalf("clear repository assignment with replacement URL failed: %v", updateErr)
	}
	if updated.RepositoryID != nil {
		t.Fatalf("expected repository_id cleared, got %#v", updated.RepositoryID)
	}
	if updated.RepositoryURL != manualURL {
		t.Fatalf("expected replacement repository_url %q, got %q", manualURL, updated.RepositoryURL)
	}

	updated, updateErr = jobService.UpdateJob(ctx, mappedJob.ID, UpdateJobInput{})
	if updateErr != nil {
		t.Fatalf("omit repository fields update failed: %v", updateErr)
	}
	if updated.RepositoryID != nil || updated.RepositoryURL != manualURL {
		t.Fatalf("expected omitted repository fields to preserve state, got id=%#v url=%q", updated.RepositoryID, updated.RepositoryURL)
	}

	unmappedJob, err := jobService.CreateJob(ctx, CreateJobInput{
		ProjectID:     "project-1",
		Name:          "manual-job",
		RepositoryURL: "https://github.com/example/original.git",
		DefaultRef:    "main",
		PipelineYAML:  "version: 1\nsteps:\n  - name: test\n    run: go test ./...\n",
		Enabled:       boolPtr(false),
	})
	if err != nil {
		t.Fatalf("create unmapped job failed: %v", err)
	}
	replacementURL := "https://github.com/example/updated.git"
	updated, updateErr = jobService.UpdateJob(ctx, unmappedJob.ID, UpdateJobInput{RepositoryURL: &replacementURL})
	if updateErr != nil {
		t.Fatalf("unmapped URL-only update failed: %v", updateErr)
	}
	if updated.RepositoryID != nil || updated.RepositoryURL != replacementURL {
		t.Fatalf("expected unmapped URL update to persist replacement URL, got id=%#v url=%q", updated.RepositoryID, updated.RepositoryURL)
	}

	jobForSet, err := jobService.CreateJob(ctx, CreateJobInput{
		ProjectID:     "project-1",
		Name:          "set-job",
		RepositoryURL: "https://github.com/example/before-set.git",
		DefaultRef:    "main",
		PipelineYAML:  "version: 1\nsteps:\n  - name: test\n    run: go test ./...\n",
		Enabled:       boolPtr(false),
	})
	if err != nil {
		t.Fatalf("create job for repository set failed: %v", err)
	}
	activeRepositoryID := active.ID
	updated, updateErr = jobService.UpdateJob(ctx, jobForSet.ID, UpdateJobInput{RepositoryIDSet: true, RepositoryID: &activeRepositoryID})
	if updateErr != nil {
		t.Fatalf("set repository assignment failed: %v", updateErr)
	}
	if updated.RepositoryID == nil || *updated.RepositoryID != active.ID {
		t.Fatalf("expected repository_id %q after set, got %#v", active.ID, updated.RepositoryID)
	}
	if updated.RepositoryURL != active.CloneURL {
		t.Fatalf("expected derived repository_url %q after set, got %q", active.CloneURL, updated.RepositoryURL)
	}

	repositoryURL := "https://github.com/example/manual.git"
	_, updateErr = jobService.UpdateJob(ctx, jobForSet.ID, UpdateJobInput{RepositoryIDSet: true, RepositoryID: &activeRepositoryID, RepositoryURL: &repositoryURL})
	if !errors.Is(updateErr, ErrJobRepositoryAssignmentConflict) {
		t.Fatalf("expected repository assignment conflict, got %v", updateErr)
	}

	preserved, updateErr := jobService.UpdateJob(ctx, jobForSet.ID, UpdateJobInput{Name: strPtr("set-job-preserved")})
	if updateErr != nil {
		t.Fatalf("preserve mapped repository fields update failed: %v", updateErr)
	}
	if preserved.RepositoryID == nil || *preserved.RepositoryID != active.ID || preserved.RepositoryURL != active.CloneURL {
		t.Fatalf("expected omitted repository fields to preserve mapping, got id=%#v url=%q", preserved.RepositoryID, preserved.RepositoryURL)
	}

	_, createJobErr := jobService.CreateJob(ctx, CreateJobInput{
		ProjectID:    "project-1",
		Name:         "disabled-repo-job",
		RepositoryID: "repo-disabled",
		DefaultRef:   "main",
		PipelineYAML: "version: 1\nsteps:\n  - name: test\n    run: go test ./...\n",
		Enabled:      boolPtr(false),
	})
	if !errors.Is(createJobErr, ErrJobRegisteredRepositoryDisabled) {
		t.Fatalf("expected disabled repository error, got %v", createJobErr)
	}

	_, createJobErr = jobService.CreateJob(ctx, CreateJobInput{
		ProjectID:    "project-1",
		Name:         "missing-repo-job",
		RepositoryID: "missing",
		DefaultRef:   "main",
		PipelineYAML: "version: 1\nsteps:\n  - name: test\n    run: go test ./...\n",
		Enabled:      boolPtr(false),
	})
	if !errors.Is(createJobErr, repository.ErrSCMRepositoryRegistrationNotFound) {
		t.Fatalf("expected missing repository error, got %v", createJobErr)
	}

	withoutRepositoryStore := NewJobService(memory.NewJobRepository(), buildsvc.NewBuildService(memory.NewBuildRepository(), nil, nil))
	_, createJobErr = withoutRepositoryStore.CreateJob(ctx, CreateJobInput{
		ProjectID:    "project-1",
		Name:         "unconfigured-repo-store-job",
		RepositoryID: active.ID,
		DefaultRef:   "main",
		PipelineYAML: "version: 1\nsteps:\n  - name: test\n    run: go test ./...\n",
		Enabled:      boolPtr(false),
	})
	if !errors.Is(createJobErr, repository.ErrSCMRepositoryRegistrationNotFound) {
		t.Fatalf("expected missing repository store error, got %v", createJobErr)
	}
}

func TestJobService_GetRegisteredRepositoriesByIDs(t *testing.T) {
	ctx := context.Background()
	jobService := NewJobService(memory.NewJobRepository(), buildsvc.NewBuildService(memory.NewBuildRepository(), nil, nil))

	items, err := jobService.GetRegisteredRepositoriesByIDs(ctx, []string{"repo-1"})
	if err != nil {
		t.Fatalf("get registered repositories without repository store: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected no repositories without configured store, got %+v", items)
	}

	registeredRepo := memory.NewSCMRepositoryRegistrationRepository()
	registered, createErr := registeredRepo.Create(ctx, domain.SCMRepositoryRegistration{
		ID:                   "repo-1",
		ConnectionID:         "connection-1",
		ProviderRepositoryID: "1001",
		Owner:                "octo",
		Name:                 "widgets",
		FullName:             "octo/widgets",
		CloneURL:             "https://github.com/octo/widgets.git",
		WebURL:               "https://github.com/octo/widgets",
		MetadataRefreshedAt:  time.Now().UTC(),
		CreatedAt:            time.Now().UTC(),
		UpdatedAt:            time.Now().UTC(),
	})
	if createErr != nil {
		t.Fatalf("create registered repository: %v", createErr)
	}
	jobService.WithSCMRepositoryRegistrationRepository(registeredRepo)
	items, err = jobService.GetRegisteredRepositoriesByIDs(ctx, []string{registered.ID, "missing"})
	if err != nil {
		t.Fatalf("get registered repositories: %v", err)
	}
	if len(items) != 1 || items[registered.ID].FullName != registered.FullName {
		t.Fatalf("expected indexed registered repository, got %+v", items)
	}

	lookupErr := errors.New("registered repository lookup failed")
	jobService.WithSCMRepositoryRegistrationRepository(&erroringRegisteredRepositoryRepo{getByIDsErr: lookupErr})
	_, err = jobService.GetRegisteredRepositoriesByIDs(ctx, []string{"repo-1"})
	if !errors.Is(err, lookupErr) {
		t.Fatalf("expected registered repository lookup error, got %v", err)
	}
}

func TestJobService_DispatchArtifactTriggers_QueuesOncePerArtifactAndConsumer(t *testing.T) {
	ctx := context.Background()
	jobRepo := memory.NewJobRepository()
	buildRepo := memory.NewBuildRepository()
	deliveryRepo := memory.NewArtifactTriggerDeliveryRepository()
	buildService := buildsvc.NewBuildService(buildRepo, nil, nil)
	jobService := NewJobService(jobRepo, buildService).WithArtifactTriggerDeliveryRepository(deliveryRepo)
	buildService.SetArtifactTriggerDispatcher(jobService)

	now := time.Now().UTC()
	producerJob, err := jobRepo.Create(ctx, domain.Job{
		ID:            "job-upstream",
		ProjectID:     "project-1",
		Name:          "upstream",
		Priority:      5,
		RepositoryURL: "https://github.com/example/repo.git",
		DefaultRef:    "main",
		PipelineYAML:  "version: 1\nsteps:\n  - name: build\n    run: make build\n",
		Enabled:       true,
		CreatedAt:     now,
		UpdatedAt:     now,
	})
	if err != nil {
		t.Fatalf("create producer job failed: %v", err)
	}
	consumerJob, err := jobRepo.Create(ctx, domain.Job{
		ID:            "job-downstream",
		ProjectID:     "project-1",
		Name:          "downstream",
		Priority:      5,
		RepositoryURL: "https://github.com/example/repo.git",
		DefaultRef:    "main",
		PipelineYAML:  "version: 1\nsteps:\n  - name: consume\n    run: make consume\n",
		ArtifactTriggers: []domain.JobArtifactTrigger{{
			ProducerJobID: producerJob.ID,
			Path:          "dist/app.tgz",
		}},
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("create consumer job failed: %v", err)
	}

	producerBuild, err := buildRepo.CreateQueuedBuild(ctx, domain.Build{
		ID:        "build-upstream",
		ProjectID: "project-1",
		JobID:     &producerJob.ID,
		Status:    domain.BuildStatusQueued,
		CreatedAt: now,
		Trigger:   domain.BuildTrigger{Kind: domain.BuildTriggerKindManual},
	}, nil)
	if err != nil {
		t.Fatalf("create producer build failed: %v", err)
	}

	artifact := domain.BuildArtifact{
		ID:          "artifact-1",
		BuildID:     producerBuild.ID,
		Name:        "app",
		LogicalPath: "dist/app.tgz",
		SizeBytes:   42,
		CreatedAt:   now,
	}
	if dispatchErr := jobService.DispatchArtifactTriggers(ctx, producerBuild, artifact); dispatchErr != nil {
		t.Fatalf("dispatch artifact triggers failed: %v", dispatchErr)
	}
	if secondDispatchErr := jobService.DispatchArtifactTriggers(ctx, producerBuild, artifact); secondDispatchErr != nil {
		t.Fatalf("dispatch artifact triggers second call failed: %v", secondDispatchErr)
	}

	builds, err := buildService.ListBuildsByJobID(ctx, consumerJob.ID)
	if err != nil {
		t.Fatalf("list builds by consumer job failed: %v", err)
	}
	if len(builds) != 1 {
		t.Fatalf("expected one downstream build after dedupe, got %d", len(builds))
	}
	if builds[0].Trigger.Kind != domain.BuildTriggerKindArtifact {
		t.Fatalf("expected artifact trigger kind, got %q", builds[0].Trigger.Kind)
	}
	if builds[0].Trigger.ArtifactPath == nil || *builds[0].Trigger.ArtifactPath != "dist/app.tgz" {
		t.Fatalf("expected artifact path provenance, got %#v", builds[0].Trigger.ArtifactPath)
	}
	delivery, err := deliveryRepo.GetByArtifactIDAndConsumerJobID(ctx, artifact.ID, consumerJob.ID)
	if err != nil {
		t.Fatalf("get delivery failed: %v", err)
	}
	if delivery.Status != domain.ArtifactTriggerDeliveryStatusQueued {
		t.Fatalf("expected queued delivery status, got %q", delivery.Status)
	}
	if delivery.QueuedBuildID == nil || *delivery.QueuedBuildID != builds[0].ID {
		t.Fatalf("expected queued build id %q, got %#v", builds[0].ID, delivery.QueuedBuildID)
	}
}

func TestJobService_UpdateJob_InvalidArtifactTriggersAreNotSilentlyDropped(t *testing.T) {
	jobRepo := memory.NewJobRepository()
	buildRepo := memory.NewBuildRepository()
	buildService := buildsvc.NewBuildService(buildRepo, nil, nil)
	jobService := NewJobService(jobRepo, buildService)

	job, err := jobService.CreateJob(context.Background(), CreateJobInput{
		ProjectID:     "project-1",
		Name:          "backend-ci",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PipelineYAML:  "version: 1\nsteps:\n  - name: test\n    run: go test ./...\n",
		Enabled:       boolPtr(false),
	})
	if err != nil {
		t.Fatalf("create job failed: %v", err)
	}

	missingProducer := []domain.JobArtifactTrigger{{ProducerJobID: " ", Path: "dist/app.tgz"}}
	_, err = jobService.UpdateJob(context.Background(), job.ID, UpdateJobInput{ArtifactTriggers: &missingProducer})
	if !errors.Is(err, ErrJobArtifactTriggerProducerJobIDRequired) {
		t.Fatalf("expected missing producer error, got %v", err)
	}

	missingPath := []domain.JobArtifactTrigger{{ProducerJobID: "job-upstream", Path: " "}}
	_, err = jobService.UpdateJob(context.Background(), job.ID, UpdateJobInput{ArtifactTriggers: &missingPath})
	if !errors.Is(err, ErrJobArtifactTriggerPathRequired) {
		t.Fatalf("expected missing path error, got %v", err)
	}
}

func TestJobService_DispatchArtifactTriggers_QueuesManagedImageConfiguredRepoJob(t *testing.T) {
	ctx := context.Background()
	jobRepo := memory.NewJobRepository()
	buildRepo := memory.NewBuildRepository()
	deliveryRepo := memory.NewArtifactTriggerDeliveryRepository()
	configRepo := memory.NewJobManagedImageConfigRepository()
	credentialRepo := memory.NewSourceCredentialRepository()
	refresher := &fakeServiceManagedImageRefresher{}
	repoDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repoDir, ".coyote"), 0o755); err != nil {
		t.Fatalf("mkdir repo dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, ".coyote", "pipeline.yml"), []byte("version: 1\nsteps:\n  - name: consume\n    run: echo consume\n"), 0o644); err != nil {
		t.Fatalf("write pipeline: %v", err)
	}
	buildService := buildsvc.NewBuildServiceFromConfig(buildRepo, nil, nil, buildsvc.BuildServiceConfig{
		RepoFetcher:           &fakeServiceRepoFetcher{localPath: repoDir, commitSHA: "abc123"},
		ManagedImageRefresher: refresher,
	})
	jobService := NewJobService(jobRepo, buildService).
		WithArtifactTriggerDeliveryRepository(deliveryRepo).
		WithManagedImageConfigRepository(configRepo, credentialRepo)
	buildService.SetArtifactTriggerDispatcher(jobService)

	_, err := credentialRepo.Create(ctx, domain.SourceCredential{
		ID:        "cred-1",
		Name:      "bot",
		Kind:      domain.SourceCredentialKindHTTPSToken,
		SecretRef: "COYOTE_TOKEN",
	})
	if err != nil {
		t.Fatalf("create credential: %v", err)
	}

	now := time.Now().UTC()
	pipelinePath := ".coyote/pipeline.yml"
	producerJob, err := jobRepo.Create(ctx, domain.Job{
		ID:            "job-upstream",
		ProjectID:     "project-1",
		Name:          "upstream",
		Priority:      5,
		RepositoryURL: "https://github.com/example/repo.git",
		DefaultRef:    "main",
		PipelineYAML:  "version: 1\nsteps:\n  - name: build\n    run: make build\n",
		Enabled:       true,
		CreatedAt:     now,
		UpdatedAt:     now,
	})
	if err != nil {
		t.Fatalf("create producer job failed: %v", err)
	}
	consumerJob, err := jobRepo.Create(ctx, domain.Job{
		ID:            "job-downstream",
		ProjectID:     "project-1",
		Name:          "downstream-managed",
		Priority:      5,
		RepositoryURL: "https://github.com/example/repo.git",
		DefaultRef:    "main",
		PipelinePath:  &pipelinePath,
		ArtifactTriggers: []domain.JobArtifactTrigger{{
			ProducerJobID: producerJob.ID,
			Path:          "dist/app.tgz",
		}},
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("create managed-image downstream job failed: %v", err)
	}
	_, err = configRepo.UpsertByJobID(ctx, domain.JobManagedImageConfig{
		ID:                "cfg-1",
		JobID:             consumerJob.ID,
		ManagedImageName:  "go",
		PipelinePath:      ".coyote/pipeline.yml",
		WriteCredentialID: "cred-1",
		Enabled:           true,
		CreatedAt:         now,
		UpdatedAt:         now,
	})
	if err != nil {
		t.Fatalf("upsert managed image config failed: %v", err)
	}

	producerBuild, err := buildRepo.CreateQueuedBuild(ctx, domain.Build{
		ID:        "build-upstream",
		ProjectID: "project-1",
		JobID:     &producerJob.ID,
		Status:    domain.BuildStatusQueued,
		CreatedAt: now,
		Trigger:   domain.BuildTrigger{Kind: domain.BuildTriggerKindManual},
	}, nil)
	if err != nil {
		t.Fatalf("create producer build failed: %v", err)
	}

	err = jobService.DispatchArtifactTriggers(ctx, producerBuild, domain.BuildArtifact{
		ID:          "artifact-1",
		BuildID:     producerBuild.ID,
		LogicalPath: "dist/app.tgz",
		CreatedAt:   now,
	})
	if err != nil {
		t.Fatalf("dispatch artifact triggers failed: %v", err)
	}
	if refresher.calls != 1 {
		t.Fatalf("expected managed image refresher to run once, got %d", refresher.calls)
	}
	if refresher.lastReq.JobID != consumerJob.ID {
		t.Fatalf("expected refresher for consumer job %q, got %q", consumerJob.ID, refresher.lastReq.JobID)
	}
}

func TestJobService_DispatchArtifactTriggers_QueueFailureMarksDeliveryFailed(t *testing.T) {
	ctx := context.Background()
	jobRepo := memory.NewJobRepository()
	buildRepo := memory.NewBuildRepository()
	deliveryRepo := memory.NewArtifactTriggerDeliveryRepository()
	buildService := buildsvc.NewBuildService(buildRepo, nil, nil)
	jobService := NewJobService(jobRepo, buildService).WithArtifactTriggerDeliveryRepository(deliveryRepo)

	now := time.Now().UTC()
	pipelinePath := ".coyote/pipeline.yml"
	producerJob, err := jobRepo.Create(ctx, domain.Job{
		ID:            "job-upstream",
		ProjectID:     "project-1",
		Name:          "upstream",
		Priority:      5,
		RepositoryURL: "https://github.com/example/repo.git",
		DefaultRef:    "main",
		PipelineYAML:  "version: 1\nsteps:\n  - name: build\n    run: make build\n",
		Enabled:       true,
		CreatedAt:     now,
		UpdatedAt:     now,
	})
	if err != nil {
		t.Fatalf("create producer job failed: %v", err)
	}
	consumerJob, err := jobRepo.Create(ctx, domain.Job{
		ID:            "job-downstream",
		ProjectID:     "project-1",
		Name:          "downstream",
		Priority:      5,
		RepositoryURL: "https://github.com/example/repo.git",
		DefaultRef:    "main",
		PipelinePath:  &pipelinePath,
		ArtifactTriggers: []domain.JobArtifactTrigger{{
			ProducerJobID: producerJob.ID,
			Path:          "dist/app.tgz",
		}},
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("create consumer job failed: %v", err)
	}

	producerBuild, err := buildRepo.CreateQueuedBuild(ctx, domain.Build{
		ID:        "build-upstream",
		ProjectID: "project-1",
		JobID:     &producerJob.ID,
		Status:    domain.BuildStatusQueued,
		CreatedAt: now,
		Trigger:   domain.BuildTrigger{Kind: domain.BuildTriggerKindManual},
	}, nil)
	if err != nil {
		t.Fatalf("create producer build failed: %v", err)
	}

	artifact := domain.BuildArtifact{ID: "artifact-1", BuildID: producerBuild.ID, LogicalPath: "dist/app.tgz", CreatedAt: now}
	dispatchErr := jobService.DispatchArtifactTriggers(ctx, producerBuild, artifact)
	if !errors.Is(dispatchErr, buildsvc.ErrRepoFetcherNotConfigured) {
		t.Fatalf("expected repo fetcher error, got %v", dispatchErr)
	}

	delivery, err := deliveryRepo.GetByArtifactIDAndConsumerJobID(ctx, artifact.ID, consumerJob.ID)
	if err != nil {
		t.Fatalf("get delivery failed: %v", err)
	}
	if delivery.Status != domain.ArtifactTriggerDeliveryStatusFailed {
		t.Fatalf("expected failed delivery status, got %q", delivery.Status)
	}
	if delivery.ErrorMessage == nil || !strings.Contains(*delivery.ErrorMessage, buildsvc.ErrRepoFetcherNotConfigured.Error()) {
		t.Fatalf("expected stored error message, got %#v", delivery.ErrorMessage)
	}
	if delivery.QueuedBuildID != nil {
		t.Fatalf("expected no queued build id on failure, got %#v", delivery.QueuedBuildID)
	}
}

func TestJobService_DispatchArtifactTriggers_RetriesFailedDelivery(t *testing.T) {
	ctx := context.Background()
	jobRepo := memory.NewJobRepository()
	buildRepo := memory.NewBuildRepository()
	deliveryRepo := memory.NewArtifactTriggerDeliveryRepository()
	repoDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repoDir, ".coyote"), 0o755); err != nil {
		t.Fatalf("mkdir repo dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, ".coyote", "pipeline.yml"), []byte("version: 1\nsteps:\n  - name: consume\n    run: echo consume\n"), 0o644); err != nil {
		t.Fatalf("write pipeline: %v", err)
	}
	buildService := buildsvc.NewBuildService(buildRepo, nil, nil)
	jobService := NewJobService(jobRepo, buildService).WithArtifactTriggerDeliveryRepository(deliveryRepo)

	now := time.Now().UTC()
	pipelinePath := ".coyote/pipeline.yml"
	producerJob, err := jobRepo.Create(ctx, domain.Job{
		ID:            "job-upstream",
		ProjectID:     "project-1",
		Name:          "upstream",
		Priority:      5,
		RepositoryURL: "https://github.com/example/repo.git",
		DefaultRef:    "main",
		PipelineYAML:  "version: 1\nsteps:\n  - name: build\n    run: make build\n",
		Enabled:       true,
		CreatedAt:     now,
		UpdatedAt:     now,
	})
	if err != nil {
		t.Fatalf("create producer job failed: %v", err)
	}
	consumerJob, err := jobRepo.Create(ctx, domain.Job{
		ID:            "job-downstream",
		ProjectID:     "project-1",
		Name:          "downstream",
		Priority:      5,
		RepositoryURL: "https://github.com/example/repo.git",
		DefaultRef:    "main",
		PipelinePath:  &pipelinePath,
		ArtifactTriggers: []domain.JobArtifactTrigger{{
			ProducerJobID: producerJob.ID,
			Path:          "dist/app.tgz",
		}},
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("create consumer job failed: %v", err)
	}

	producerBuild, err := buildRepo.CreateQueuedBuild(ctx, domain.Build{
		ID:        "build-upstream",
		ProjectID: "project-1",
		JobID:     &producerJob.ID,
		Status:    domain.BuildStatusQueued,
		CreatedAt: now,
		Trigger:   domain.BuildTrigger{Kind: domain.BuildTriggerKindManual},
	}, nil)
	if err != nil {
		t.Fatalf("create producer build failed: %v", err)
	}

	artifact := domain.BuildArtifact{ID: "artifact-1", BuildID: producerBuild.ID, LogicalPath: "dist/app.tgz", CreatedAt: now}
	firstDispatchErr := jobService.DispatchArtifactTriggers(ctx, producerBuild, artifact)
	if !errors.Is(firstDispatchErr, buildsvc.ErrRepoFetcherNotConfigured) {
		t.Fatalf("expected repo fetcher error on first dispatch, got %v", firstDispatchErr)
	}

	failedDelivery, err := deliveryRepo.GetByArtifactIDAndConsumerJobID(ctx, artifact.ID, consumerJob.ID)
	if err != nil {
		t.Fatalf("get failed delivery failed: %v", err)
	}
	if failedDelivery.Status != domain.ArtifactTriggerDeliveryStatusFailed {
		t.Fatalf("expected failed status after first dispatch, got %q", failedDelivery.Status)
	}

	buildService.SetRepoFetcher(&fakeServiceRepoFetcher{localPath: repoDir, commitSHA: "abc123"})
	secondDispatchErr := jobService.DispatchArtifactTriggers(ctx, producerBuild, artifact)
	if secondDispatchErr != nil {
		t.Fatalf("expected retry dispatch to succeed, got %v", secondDispatchErr)
	}

	builds, err := buildService.ListBuildsByJobID(ctx, consumerJob.ID)
	if err != nil {
		t.Fatalf("list builds by consumer job failed: %v", err)
	}
	if len(builds) != 1 {
		t.Fatalf("expected one downstream build after retry, got %d", len(builds))
	}

	retriedDelivery, err := deliveryRepo.GetByArtifactIDAndConsumerJobID(ctx, artifact.ID, consumerJob.ID)
	if err != nil {
		t.Fatalf("get retried delivery failed: %v", err)
	}
	if retriedDelivery.ID != failedDelivery.ID {
		t.Fatalf("expected retry to reuse delivery %q, got %q", failedDelivery.ID, retriedDelivery.ID)
	}
	if retriedDelivery.Status != domain.ArtifactTriggerDeliveryStatusQueued {
		t.Fatalf("expected queued status after retry, got %q", retriedDelivery.Status)
	}
	if retriedDelivery.QueuedBuildID == nil || *retriedDelivery.QueuedBuildID != builds[0].ID {
		t.Fatalf("expected queued build id %q, got %#v", builds[0].ID, retriedDelivery.QueuedBuildID)
	}
	if retriedDelivery.ErrorMessage != nil {
		t.Fatalf("expected error message cleared after retry, got %#v", retriedDelivery.ErrorMessage)
	}
}

func TestJobService_RetryArtifactTriggerDelivery(t *testing.T) {
	ctx := context.Background()
	jobRepo := memory.NewJobRepository()
	buildRepo := memory.NewBuildRepository()
	deliveryRepo := memory.NewArtifactTriggerDeliveryRepository()
	artifactRepo := &fakeJobServiceArtifactRepo{}
	buildService := buildsvc.NewBuildService(buildRepo, nil, nil)
	buildService.SetArtifactPersistence(artifactRepo, nil, "")
	jobService := NewJobService(jobRepo, buildService).WithArtifactTriggerDeliveryRepository(deliveryRepo)

	now := time.Now().UTC()
	producerJob, err := jobRepo.Create(ctx, domain.Job{ID: "job-upstream", ProjectID: "project-1", Name: "upstream", Priority: 5, RepositoryURL: "https://github.com/example/repo.git", DefaultRef: "main", PipelineYAML: "version: 1\nsteps:\n  - name: build\n    run: make build\n", Enabled: true, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatalf("create producer job failed: %v", err)
	}
	consumerJob, err := jobRepo.Create(ctx, domain.Job{ID: "job-downstream", ProjectID: "project-1", Name: "downstream", Priority: 5, RepositoryURL: "https://github.com/example/repo.git", DefaultRef: "main", PipelineYAML: "version: 1\nsteps:\n  - name: consume\n    run: echo consume\n", Enabled: true, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatalf("create consumer job failed: %v", err)
	}
	producerBuild, err := buildRepo.CreateQueuedBuild(ctx, domain.Build{ID: "build-upstream", ProjectID: "project-1", JobID: &producerJob.ID, Status: domain.BuildStatusQueued, CreatedAt: now, Trigger: domain.BuildTrigger{Kind: domain.BuildTriggerKindManual}}, nil)
	if err != nil {
		t.Fatalf("create producer build failed: %v", err)
	}
	if _, createArtifactErr := artifactRepo.Create(ctx, domain.BuildArtifact{ID: "artifact-1", BuildID: producerBuild.ID, Name: "app.tgz", LogicalPath: "dist/app.tgz", SizeBytes: 42, CreatedAt: now}); createArtifactErr != nil {
		t.Fatalf("create artifact failed: %v", createArtifactErr)
	}
	errorMessage := "queue failed"
	if _, createFailedDeliveryErr := deliveryRepo.Create(ctx, domain.ArtifactTriggerDelivery{ID: "delivery-1", ArtifactID: "artifact-1", ConsumerJobID: consumerJob.ID, ProducerBuildID: producerBuild.ID, ProducerProjectID: producerBuild.ProjectID, ProducerJobID: producerJob.ID, ArtifactPath: "dist/app.tgz", ErrorMessage: &errorMessage, Status: domain.ArtifactTriggerDeliveryStatusFailed, CreatedAt: now, UpdatedAt: now}); createFailedDeliveryErr != nil {
		t.Fatalf("create failed delivery failed: %v", createFailedDeliveryErr)
	}

	result, err := jobService.RetryArtifactTriggerDelivery(ctx, "delivery-1")
	if err != nil {
		t.Fatalf("retry artifact trigger delivery failed: %v", err)
	}
	if result.Result != "retried" || result.View.Delivery.Status != domain.ArtifactTriggerDeliveryStatusQueued {
		t.Fatalf("unexpected retry result: %+v", result)
	}
	if result.View.Delivery.QueuedBuildID == nil {
		t.Fatalf("expected queued build id after retry, got %+v", result.View.Delivery)
	}
	builds, err := buildService.ListBuildsByJobID(ctx, consumerJob.ID)
	if err != nil {
		t.Fatalf("list builds by consumer job failed: %v", err)
	}
	if len(builds) != 1 {
		t.Fatalf("expected one downstream build after retry, got %d", len(builds))
	}

	if _, createPendingDeliveryErr := deliveryRepo.Create(ctx, domain.ArtifactTriggerDelivery{ID: "delivery-pending", ArtifactID: "artifact-pending", ConsumerJobID: consumerJob.ID, ProducerBuildID: producerBuild.ID, ProducerProjectID: producerBuild.ProjectID, ProducerJobID: producerJob.ID, ArtifactPath: "dist/app.tgz", Status: domain.ArtifactTriggerDeliveryStatusPending, CreatedAt: now, UpdatedAt: now}); createPendingDeliveryErr != nil {
		t.Fatalf("create pending delivery failed: %v", createPendingDeliveryErr)
	}
	_, err = jobService.RetryArtifactTriggerDelivery(ctx, "delivery-pending")
	if !errors.Is(err, ErrArtifactTriggerDeliveryPendingRetryDeferred) {
		t.Fatalf("expected pending retry deferred error, got %v", err)
	}

	queuedBuildID := "build-downstream-existing"
	if _, createQueuedDeliveryErr := deliveryRepo.Create(ctx, domain.ArtifactTriggerDelivery{ID: "delivery-queued", ArtifactID: "artifact-queued", ConsumerJobID: consumerJob.ID, ProducerBuildID: producerBuild.ID, ProducerProjectID: producerBuild.ProjectID, ProducerJobID: producerJob.ID, ArtifactPath: "dist/app.tgz", QueuedBuildID: &queuedBuildID, Status: domain.ArtifactTriggerDeliveryStatusQueued, CreatedAt: now, UpdatedAt: now}); createQueuedDeliveryErr != nil {
		t.Fatalf("create queued delivery failed: %v", createQueuedDeliveryErr)
	}
	alreadySatisfied, err := jobService.RetryArtifactTriggerDelivery(ctx, "delivery-queued")
	if err != nil {
		t.Fatalf("expected already satisfied retry to succeed, got %v", err)
	}
	if alreadySatisfied.Result != "already_satisfied" || alreadySatisfied.View.Delivery.QueuedBuildID == nil || *alreadySatisfied.View.Delivery.QueuedBuildID != queuedBuildID {
		t.Fatalf("unexpected already satisfied result: %+v", alreadySatisfied)
	}

	strandedMessage := "old failure"
	if _, createMissingJobDeliveryErr := deliveryRepo.Create(ctx, domain.ArtifactTriggerDelivery{ID: "delivery-missing-job", ArtifactID: "artifact-1", ConsumerJobID: "job-missing", ProducerBuildID: producerBuild.ID, ProducerProjectID: producerBuild.ProjectID, ProducerJobID: producerJob.ID, ArtifactPath: "dist/app.tgz", ErrorMessage: &strandedMessage, Status: domain.ArtifactTriggerDeliveryStatusFailed, CreatedAt: now, UpdatedAt: now}); createMissingJobDeliveryErr != nil {
		t.Fatalf("create missing-job delivery failed: %v", createMissingJobDeliveryErr)
	}
	_, err = jobService.RetryArtifactTriggerDelivery(ctx, "delivery-missing-job")
	if !errors.Is(err, repository.ErrJobNotFound) {
		t.Fatalf("expected missing consumer job error, got %v", err)
	}
	restoredDelivery, err := deliveryRepo.GetByID(ctx, "delivery-missing-job")
	if err != nil {
		t.Fatalf("get restored delivery failed: %v", err)
	}
	if restoredDelivery.Status != domain.ArtifactTriggerDeliveryStatusFailed {
		t.Fatalf("expected failed status after pre-enqueue retry error, got %q", restoredDelivery.Status)
	}
	if restoredDelivery.QueuedBuildID != nil {
		t.Fatalf("expected nil queued build id after pre-enqueue retry error, got %#v", restoredDelivery.QueuedBuildID)
	}
	if restoredDelivery.ErrorMessage == nil || !strings.Contains(*restoredDelivery.ErrorMessage, repository.ErrJobNotFound.Error()) {
		t.Fatalf("expected useful persisted error message, got %#v", restoredDelivery.ErrorMessage)
	}
}

func TestJobService_RetryArtifactTriggerDelivery_ErrorPaths(t *testing.T) {
	newFixture := func(t *testing.T) (*JobService, *memory.ArtifactTriggerDeliveryRepository, *fakeJobServiceArtifactRepo, domain.Job, domain.Job, domain.Build) {
		t.Helper()
		ctx := context.Background()
		jobRepo := memory.NewJobRepository()
		buildRepo := memory.NewBuildRepository()
		deliveryRepo := memory.NewArtifactTriggerDeliveryRepository()
		artifactRepo := &fakeJobServiceArtifactRepo{}
		buildService := buildsvc.NewBuildService(buildRepo, nil, nil)
		buildService.SetArtifactPersistence(artifactRepo, nil, "")
		jobService := NewJobService(jobRepo, buildService).WithArtifactTriggerDeliveryRepository(deliveryRepo)

		now := time.Now().UTC()
		producerJob, err := jobRepo.Create(ctx, domain.Job{ID: "job-upstream", ProjectID: "project-1", Name: "upstream", Priority: 5, RepositoryURL: "https://github.com/example/repo.git", DefaultRef: "main", PipelineYAML: "version: 1\nsteps:\n  - name: build\n    run: make build\n", Enabled: true, CreatedAt: now, UpdatedAt: now})
		if err != nil {
			t.Fatalf("create producer job failed: %v", err)
		}
		consumerJob, err := jobRepo.Create(ctx, domain.Job{ID: "job-downstream", ProjectID: "project-1", Name: "downstream", Priority: 5, RepositoryURL: "https://github.com/example/repo.git", DefaultRef: "main", PipelineYAML: "version: 1\nsteps:\n  - name: consume\n    run: echo consume\n", Enabled: true, CreatedAt: now, UpdatedAt: now})
		if err != nil {
			t.Fatalf("create consumer job failed: %v", err)
		}
		producerBuild, err := buildRepo.CreateQueuedBuild(ctx, domain.Build{ID: "build-upstream", ProjectID: "project-1", JobID: &producerJob.ID, Status: domain.BuildStatusQueued, CreatedAt: now, Trigger: domain.BuildTrigger{Kind: domain.BuildTriggerKindManual}}, nil)
		if err != nil {
			t.Fatalf("create producer build failed: %v", err)
		}
		if _, createArtifactErr := artifactRepo.Create(ctx, domain.BuildArtifact{ID: "artifact-1", BuildID: producerBuild.ID, Name: "app.tgz", LogicalPath: "dist/app.tgz", SizeBytes: 42, CreatedAt: now}); createArtifactErr != nil {
			t.Fatalf("create artifact failed: %v", createArtifactErr)
		}
		return jobService, deliveryRepo, artifactRepo, producerJob, consumerJob, producerBuild
	}

	t.Run("requires delivery repository", func(t *testing.T) {
		jobService := NewJobService(memory.NewJobRepository(), buildsvc.NewBuildService(memory.NewBuildRepository(), nil, nil))
		_, err := jobService.RetryArtifactTriggerDelivery(context.Background(), "delivery-1")
		if !errors.Is(err, ErrJobArtifactTriggerDeliveryRepositoryNotConfigured) {
			t.Fatalf("expected missing delivery repository error, got %v", err)
		}
	})

	t.Run("requires build service", func(t *testing.T) {
		jobService := NewJobService(memory.NewJobRepository(), nil).WithArtifactTriggerDeliveryRepository(memory.NewArtifactTriggerDeliveryRepository())
		_, err := jobService.RetryArtifactTriggerDelivery(context.Background(), "delivery-1")
		if !errors.Is(err, ErrJobBuildServiceNotConfigured) {
			t.Fatalf("expected missing build service error, got %v", err)
		}
	})

	t.Run("requires job repository", func(t *testing.T) {
		jobService := NewJobService(nil, buildsvc.NewBuildService(memory.NewBuildRepository(), nil, nil)).WithArtifactTriggerDeliveryRepository(memory.NewArtifactTriggerDeliveryRepository())
		_, err := jobService.RetryArtifactTriggerDelivery(context.Background(), "delivery-1")
		if !errors.Is(err, ErrJobNotFound) {
			t.Fatalf("expected missing job repository error, got %v", err)
		}
	})

	t.Run("queued build conflict when delivery already references downstream build", func(t *testing.T) {
		ctx := context.Background()
		jobService, deliveryRepo, _, producerJob, consumerJob, producerBuild := newFixture(t)
		queuedBuildID := "build-existing"
		if _, createDeliveryErr := deliveryRepo.Create(ctx, domain.ArtifactTriggerDelivery{ID: "delivery-conflict", ArtifactID: "artifact-1", ConsumerJobID: consumerJob.ID, ProducerBuildID: producerBuild.ID, ProducerProjectID: producerBuild.ProjectID, ProducerJobID: producerJob.ID, ArtifactPath: "dist/app.tgz", QueuedBuildID: &queuedBuildID, Status: domain.ArtifactTriggerDeliveryStatusFailed, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}); createDeliveryErr != nil {
			t.Fatalf("create conflict delivery failed: %v", createDeliveryErr)
		}
		_, err := jobService.RetryArtifactTriggerDelivery(ctx, "delivery-conflict")
		if !errors.Is(err, ErrArtifactTriggerDeliveryQueuedBuildConflict) {
			t.Fatalf("expected queued build conflict, got %v", err)
		}
	})

	t.Run("retry not supported for unexpected status", func(t *testing.T) {
		ctx := context.Background()
		jobService, deliveryRepo, _, producerJob, consumerJob, producerBuild := newFixture(t)
		if _, createDeliveryErr := deliveryRepo.Create(ctx, domain.ArtifactTriggerDelivery{ID: "delivery-unsupported", ArtifactID: "artifact-1", ConsumerJobID: consumerJob.ID, ProducerBuildID: producerBuild.ID, ProducerProjectID: producerBuild.ProjectID, ProducerJobID: producerJob.ID, ArtifactPath: "dist/app.tgz", Status: domain.ArtifactTriggerDeliveryStatus("mystery"), CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}); createDeliveryErr != nil {
			t.Fatalf("create unsupported delivery failed: %v", createDeliveryErr)
		}
		_, err := jobService.RetryArtifactTriggerDelivery(ctx, "delivery-unsupported")
		if !errors.Is(err, ErrArtifactTriggerDeliveryRetryNotSupported) {
			t.Fatalf("expected retry not supported, got %v", err)
		}
	})

	t.Run("artifact list failure restores failed status", func(t *testing.T) {
		ctx := context.Background()
		jobService, deliveryRepo, artifactRepo, producerJob, consumerJob, producerBuild := newFixture(t)
		artifactRepo.listByBuildErr = errors.New("artifact lookup failed")
		if _, createDeliveryErr := deliveryRepo.Create(ctx, domain.ArtifactTriggerDelivery{ID: "delivery-artifact-error", ArtifactID: "artifact-1", ConsumerJobID: consumerJob.ID, ProducerBuildID: producerBuild.ID, ProducerProjectID: producerBuild.ProjectID, ProducerJobID: producerJob.ID, ArtifactPath: "dist/app.tgz", Status: domain.ArtifactTriggerDeliveryStatusFailed, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}); createDeliveryErr != nil {
			t.Fatalf("create artifact error delivery failed: %v", createDeliveryErr)
		}
		_, err := jobService.RetryArtifactTriggerDelivery(ctx, "delivery-artifact-error")
		if err == nil || err.Error() != "artifact lookup failed" {
			t.Fatalf("expected artifact lookup failure, got %v", err)
		}
		restored, getErr := deliveryRepo.GetByID(ctx, "delivery-artifact-error")
		if getErr != nil {
			t.Fatalf("get restored artifact error delivery failed: %v", getErr)
		}
		if restored.Status != domain.ArtifactTriggerDeliveryStatusFailed || restored.QueuedBuildID != nil || restored.ErrorMessage == nil || *restored.ErrorMessage != "artifact lookup failed" {
			t.Fatalf("unexpected restored artifact error delivery: %+v", restored)
		}
	})

	t.Run("missing artifact restores failed status", func(t *testing.T) {
		ctx := context.Background()
		jobService, deliveryRepo, _, producerJob, consumerJob, producerBuild := newFixture(t)
		if _, createDeliveryErr := deliveryRepo.Create(ctx, domain.ArtifactTriggerDelivery{ID: "delivery-missing-artifact", ArtifactID: "artifact-missing", ConsumerJobID: consumerJob.ID, ProducerBuildID: producerBuild.ID, ProducerProjectID: producerBuild.ProjectID, ProducerJobID: producerJob.ID, ArtifactPath: "dist/app.tgz", Status: domain.ArtifactTriggerDeliveryStatusFailed, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}); createDeliveryErr != nil {
			t.Fatalf("create missing artifact delivery failed: %v", createDeliveryErr)
		}
		_, err := jobService.RetryArtifactTriggerDelivery(ctx, "delivery-missing-artifact")
		if !errors.Is(err, buildsvc.ErrArtifactNotFound) {
			t.Fatalf("expected missing artifact error, got %v", err)
		}
		restored, getErr := deliveryRepo.GetByID(ctx, "delivery-missing-artifact")
		if getErr != nil {
			t.Fatalf("get restored missing artifact delivery failed: %v", getErr)
		}
		if restored.Status != domain.ArtifactTriggerDeliveryStatusFailed || restored.QueuedBuildID != nil || restored.ErrorMessage == nil || !strings.Contains(*restored.ErrorMessage, buildsvc.ErrArtifactNotFound.Error()) {
			t.Fatalf("unexpected restored missing artifact delivery: %+v", restored)
		}
	})

	t.Run("empty retry error clears persisted error message", func(t *testing.T) {
		ctx := context.Background()
		jobService, deliveryRepo, _, producerJob, consumerJob, producerBuild := newFixture(t)
		oldMessage := "old failure"
		created, createErr := deliveryRepo.Create(ctx, domain.ArtifactTriggerDelivery{ID: "delivery-empty-error", ArtifactID: "artifact-1", ConsumerJobID: consumerJob.ID, ProducerBuildID: producerBuild.ID, ProducerProjectID: producerBuild.ProjectID, ProducerJobID: producerJob.ID, ArtifactPath: "dist/app.tgz", ErrorMessage: &oldMessage, Status: domain.ArtifactTriggerDeliveryStatusPending, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()})
		if createErr != nil {
			t.Fatalf("create empty error delivery failed: %v", createErr)
		}
		jobService.failArtifactTriggerDeliveryRetry(ctx, created, emptyRetryError{})
		updated, getErr := deliveryRepo.GetByID(ctx, created.ID)
		if getErr != nil {
			t.Fatalf("get empty error delivery failed: %v", getErr)
		}
		if updated.Status != domain.ArtifactTriggerDeliveryStatusFailed || updated.ErrorMessage != nil {
			t.Fatalf("expected failed delivery with cleared error message, got %+v", updated)
		}
	})
}

type emptyRetryError struct{}

func (emptyRetryError) Error() string { return "" }

func TestJobService_ListArtifactTriggerDeliveriesByProducerBuildID(t *testing.T) {
	ctx := context.Background()
	jobRepo := memory.NewJobRepository()
	deliveryRepo := memory.NewArtifactTriggerDeliveryRepository()
	jobService := NewJobService(jobRepo, nil).WithArtifactTriggerDeliveryRepository(deliveryRepo)

	now := time.Now().UTC()
	if _, err := jobRepo.Create(ctx, domain.Job{ID: "job-downstream", ProjectID: "project-1", Name: "downstream", RepositoryURL: "https://github.com/example/repo.git", DefaultRef: "main", PipelineYAML: "version: 1", Enabled: true, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create downstream job failed: %v", err)
	}
	if _, err := deliveryRepo.Create(ctx, domain.ArtifactTriggerDelivery{
		ID:                "delivery-1",
		ArtifactID:        "artifact-1",
		ConsumerJobID:     "job-downstream",
		ProducerBuildID:   "build-upstream",
		ProducerProjectID: "project-1",
		ProducerJobID:     "job-upstream",
		ArtifactPath:      "dist/app.tgz",
		Status:            domain.ArtifactTriggerDeliveryStatusQueued,
		CreatedAt:         now,
		UpdatedAt:         now,
	}); err != nil {
		t.Fatalf("create delivery failed: %v", err)
	}
	if _, err := deliveryRepo.Create(ctx, domain.ArtifactTriggerDelivery{
		ID:                "delivery-2",
		ArtifactID:        "artifact-2",
		ConsumerJobID:     "job-missing",
		ProducerBuildID:   "build-other",
		ProducerProjectID: "project-1",
		ProducerJobID:     "job-upstream",
		ArtifactPath:      "dist/other.tgz",
		Status:            domain.ArtifactTriggerDeliveryStatusFailed,
		CreatedAt:         now,
		UpdatedAt:         now,
	}); err != nil {
		t.Fatalf("create second delivery failed: %v", err)
	}

	views, err := jobService.ListArtifactTriggerDeliveriesByProducerBuildID(ctx, " build-upstream ")
	if err != nil {
		t.Fatalf("list artifact trigger deliveries failed: %v", err)
	}
	if len(views) != 1 {
		t.Fatalf("expected one delivery view, got %d", len(views))
	}
	if views[0].Delivery.ID != "delivery-1" {
		t.Fatalf("expected delivery-1, got %+v", views[0])
	}
	if views[0].ConsumerJobName == nil || *views[0].ConsumerJobName != "downstream" {
		t.Fatalf("expected downstream consumer job name, got %+v", views[0].ConsumerJobName)
	}

	emptyViews, err := jobService.ListArtifactTriggerDeliveriesByProducerBuildID(ctx, "missing-build")
	if err != nil {
		t.Fatalf("list empty artifact trigger deliveries failed: %v", err)
	}
	if len(emptyViews) != 0 {
		t.Fatalf("expected no delivery views, got %+v", emptyViews)
	}
}

func TestJobService_ListArtifactTriggerDeliveriesByProducerBuildID_Branches(t *testing.T) {
	ctx := context.Background()

	t.Run("missing delivery repository", func(t *testing.T) {
		jobService := NewJobService(memory.NewJobRepository(), nil)
		_, err := jobService.ListArtifactTriggerDeliveriesByProducerBuildID(ctx, "build-1")
		if !errors.Is(err, ErrJobArtifactTriggerDeliveryRepositoryNotConfigured) {
			t.Fatalf("expected missing delivery repo error, got %v", err)
		}
	})

	t.Run("nil job repository leaves consumer name empty", func(t *testing.T) {
		deliveryRepo := memory.NewArtifactTriggerDeliveryRepository()
		jobService := NewJobService(nil, nil).WithArtifactTriggerDeliveryRepository(deliveryRepo)
		now := time.Now().UTC()
		if _, err := deliveryRepo.Create(ctx, domain.ArtifactTriggerDelivery{ID: "delivery-1", ArtifactID: "artifact-1", ConsumerJobID: "job-downstream", ProducerBuildID: "build-1", ProducerProjectID: "project-1", ProducerJobID: "job-upstream", ArtifactPath: "dist/app.tgz", Status: domain.ArtifactTriggerDeliveryStatusQueued, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatalf("create delivery failed: %v", err)
		}

		views, err := jobService.ListArtifactTriggerDeliveriesByProducerBuildID(ctx, "build-1")
		if err != nil {
			t.Fatalf("list artifact trigger deliveries failed: %v", err)
		}
		if len(views) != 1 || views[0].ConsumerJobName != nil {
			t.Fatalf("expected nil consumer job name without job repo, got %+v", views)
		}
	})

	t.Run("blank job name is omitted", func(t *testing.T) {
		jobRepo := memory.NewJobRepository()
		deliveryRepo := memory.NewArtifactTriggerDeliveryRepository()
		jobService := NewJobService(jobRepo, nil).WithArtifactTriggerDeliveryRepository(deliveryRepo)
		now := time.Now().UTC()
		if _, err := jobRepo.Create(ctx, domain.Job{ID: "job-blank", ProjectID: "project-1", Name: "   ", RepositoryURL: "https://github.com/example/repo.git", DefaultRef: "main", PipelineYAML: "version: 1", Enabled: true, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatalf("create blank-name job failed: %v", err)
		}
		if _, err := deliveryRepo.Create(ctx, domain.ArtifactTriggerDelivery{ID: "delivery-1", ArtifactID: "artifact-1", ConsumerJobID: "job-blank", ProducerBuildID: "build-1", ProducerProjectID: "project-1", ProducerJobID: "job-upstream", ArtifactPath: "dist/app.tgz", Status: domain.ArtifactTriggerDeliveryStatusQueued, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatalf("create delivery failed: %v", err)
		}

		views, err := jobService.ListArtifactTriggerDeliveriesByProducerBuildID(ctx, "build-1")
		if err != nil {
			t.Fatalf("list artifact trigger deliveries failed: %v", err)
		}
		if len(views) != 1 || views[0].ConsumerJobName != nil {
			t.Fatalf("expected blank consumer job name to be omitted, got %+v", views)
		}
	})

	t.Run("job repository lookup failure is returned", func(t *testing.T) {
		jobRepo := &erroringArtifactTriggerJobRepo{JobRepository: memory.NewJobRepository(), getByIDsErr: errors.New("lookup failed")}
		deliveryRepo := memory.NewArtifactTriggerDeliveryRepository()
		jobService := NewJobService(jobRepo, nil).WithArtifactTriggerDeliveryRepository(deliveryRepo)
		now := time.Now().UTC()
		if _, err := deliveryRepo.Create(ctx, domain.ArtifactTriggerDelivery{ID: "delivery-1", ArtifactID: "artifact-1", ConsumerJobID: "job-downstream", ProducerBuildID: "build-1", ProducerProjectID: "project-1", ProducerJobID: "job-upstream", ArtifactPath: "dist/app.tgz", Status: domain.ArtifactTriggerDeliveryStatusQueued, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatalf("create delivery failed: %v", err)
		}

		_, err := jobService.ListArtifactTriggerDeliveriesByProducerBuildID(ctx, "build-1")
		if err == nil || err.Error() != "lookup failed" {
			t.Fatalf("expected lookup error, got %v", err)
		}
	})
}

func TestJobService_CreateJobAcceptsLegacyProjectSlugInProjectID(t *testing.T) {
	jobRepo := memory.NewJobRepository()
	buildRepo := memory.NewBuildRepository()
	projectRepo := memory.NewProjectRepository(jobRepo)
	buildService := buildsvc.NewBuildService(buildRepo, nil, nil)
	jobService := NewJobService(jobRepo, buildService).WithProjectRepository(projectRepo)

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

	job, err := jobService.CreateJob(context.Background(), CreateJobInput{
		ProjectID:     "fixtures",
		Name:          "fixture-job",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PipelineYAML:  "version: 1\nsteps:\n  - name: test\n    run: go test ./...\n",
	})
	if err != nil {
		t.Fatalf("create job failed: %v", err)
	}
	if job.ProjectID != project.ID {
		t.Fatalf("expected resolved project id %q, got %q", project.ID, job.ProjectID)
	}
}

func TestJobService_ResolveProjectIDUsesWrapper(t *testing.T) {
	jobRepo := memory.NewJobRepository()
	projectRepo := memory.NewProjectRepository(jobRepo)
	jobService := NewJobService(jobRepo, buildsvc.NewBuildService(memory.NewBuildRepository(), nil, nil)).WithProjectRepository(projectRepo)
	project, err := projectRepo.Create(context.Background(), domain.Project{
		ID:        "00000000-0000-0000-0000-000000000999",
		Name:      "Fixtures",
		Slug:      "fixtures",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("create project failed: %v", err)
	}

	resolved, err := jobService.ResolveProjectID(context.Background(), "fixtures", "")
	if err != nil {
		t.Fatalf("resolve project id failed: %v", err)
	}
	if resolved != project.ID {
		t.Fatalf("expected resolved project id %q, got %q", project.ID, resolved)
	}
}

func TestJobService_ListJobsByProjectAcceptsSlugSelector(t *testing.T) {
	jobRepo := memory.NewJobRepository()
	buildRepo := memory.NewBuildRepository()
	baseProjectRepo := memory.NewProjectRepository(jobRepo)
	projectRepo := &selectorAwareProjectRepo{ProjectRepository: baseProjectRepo}
	buildService := buildsvc.NewBuildService(buildRepo, nil, nil)
	jobService := NewJobService(jobRepo, buildService).WithProjectRepository(projectRepo)

	project, err := baseProjectRepo.Create(context.Background(), domain.Project{
		ID:        "00000000-0000-0000-0000-000000000124",
		Name:      "Default",
		Slug:      "default",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("create project failed: %v", err)
	}
	job, err := jobService.CreateJob(context.Background(), CreateJobInput{
		ProjectID:     project.ID,
		Name:          "coyote-ci",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PipelineYAML:  "version: 1\nsteps:\n  - name: test\n    run: go test ./...\n",
	})
	if err != nil {
		t.Fatalf("create job failed: %v", err)
	}

	jobs, err := jobService.ListJobsByProject(context.Background(), "default")
	if err != nil {
		t.Fatalf("list jobs by slug selector failed: %v", err)
	}
	if len(jobs) != 1 || jobs[0].ID != job.ID {
		t.Fatalf("unexpected jobs payload: %+v", jobs)
	}
	for _, call := range projectRepo.getByIDCalls {
		if call == "default" {
			t.Fatalf("non-uuid selector unexpectedly hit GetByID")
		}
	}
	if len(projectRepo.getBySlugCalls) == 0 || projectRepo.getBySlugCalls[0] != "default" {
		t.Fatalf("expected slug lookup for default selector, got %+v", projectRepo.getBySlugCalls)
	}

	_, err = jobService.ListJobsByProject(context.Background(), "definitely-not-a-project")
	if !errors.Is(err, repository.ErrProjectNotFound) {
		t.Fatalf("expected ErrProjectNotFound for unknown slug selector, got %v", err)
	}
	for _, call := range projectRepo.getByIDCalls {
		if call == "definitely-not-a-project" {
			t.Fatalf("unknown non-uuid selector unexpectedly hit GetByID")
		}
	}
}

func TestJobService_ResolveJobByProjectAndName(t *testing.T) {
	jobRepo := memory.NewJobRepository()
	buildRepo := memory.NewBuildRepository()
	projectRepo := memory.NewProjectRepository(jobRepo)
	buildService := buildsvc.NewBuildService(buildRepo, nil, nil)
	jobService := NewJobService(jobRepo, buildService).WithProjectRepository(projectRepo)

	project, err := projectRepo.Create(context.Background(), domain.Project{
		ID:        "00000000-0000-0000-0000-000000000321",
		Name:      "Platform",
		Slug:      "platform",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("create project failed: %v", err)
	}
	job, err := jobService.CreateJob(context.Background(), CreateJobInput{
		ProjectID:     project.ID,
		Name:          "backend-ci",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PipelineYAML:  "version: 1\nsteps:\n  - name: test\n    run: go test ./...\n",
		Enabled:       boolPtr(true),
	})
	if err != nil {
		t.Fatalf("create job failed: %v", err)
	}

	resolved, err := jobService.ResolveJobByProjectAndName(context.Background(), project.ID, " backend-ci ")
	if err != nil {
		t.Fatalf("resolve job by name failed: %v", err)
	}
	if resolved.ID != job.ID {
		t.Fatalf("expected resolved id %q, got %q", job.ID, resolved.ID)
	}
	if resolved.LatestBuild == nil {
		t.Fatal("expected latest build summary on resolved job")
	}
	if resolved.LatestBuild.Status != domain.BuildStatusQueued {
		t.Fatalf("expected queued latest build, got %q", resolved.LatestBuild.Status)
	}

	_, missingErr := jobService.ResolveJobByProjectAndName(context.Background(), project.ID, "missing")
	if !errors.Is(missingErr, ErrJobNotFound) {
		t.Fatalf("expected ErrJobNotFound, got %v", missingErr)
	}

	_, err = jobRepo.Create(context.Background(), domain.Job{
		ID:            "job-dup-a",
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
		t.Fatalf("create duplicate job a failed: %v", err)
	}
	_, err = jobRepo.Create(context.Background(), domain.Job{
		ID:            "job-dup-b",
		ProjectID:     project.ID,
		Name:          "duplicate",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PipelineYAML:  "version: 1\nsteps:\n  - name: test\n    run: go test ./...\n",
		Enabled:       true,
		CreatedAt:     time.Now().UTC().Add(time.Second),
		UpdatedAt:     time.Now().UTC().Add(time.Second),
	})
	if err != nil {
		t.Fatalf("create duplicate job b failed: %v", err)
	}

	_, ambiguousErr := jobService.ResolveJobByProjectAndName(context.Background(), project.ID, "duplicate")
	if !errors.Is(ambiguousErr, ErrJobNameAmbiguous) {
		t.Fatalf("expected ErrJobNameAmbiguous, got %v", ambiguousErr)
	}
}

func TestJobService_ResolveJobByProjectAndNameValidationAndErrors(t *testing.T) {
	jobRepo := memory.NewJobRepository()
	projectRepo := memory.NewProjectRepository(jobRepo)
	jobService := NewJobService(jobRepo, nil).WithProjectRepository(projectRepo)

	if _, err := jobService.ResolveJobByProjectAndName(context.Background(), " ", "backend-ci"); !errors.Is(err, ErrProjectIDRequired) {
		t.Fatalf("expected ErrProjectIDRequired, got %v", err)
	}
	if _, err := jobService.ResolveJobByProjectAndName(context.Background(), "00000000-0000-0000-0000-000000000111", " "); !errors.Is(err, ErrJobNameRequired) {
		t.Fatalf("expected ErrJobNameRequired, got %v", err)
	}
	if _, err := jobService.ResolveJobByProjectAndName(context.Background(), "00000000-0000-0000-0000-000000000111", "backend-ci"); !errors.Is(err, repository.ErrProjectNotFound) {
		t.Fatalf("expected ErrProjectNotFound, got %v", err)
	}

	project, err := projectRepo.Create(context.Background(), domain.Project{
		ID:        "00000000-0000-0000-0000-000000000555",
		Name:      "Platform",
		Slug:      "platform",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("create project failed: %v", err)
	}
	job, err := jobRepo.Create(context.Background(), domain.Job{
		ID:            "job-1",
		ProjectID:     project.ID,
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

	resolved, err := jobService.ResolveJobByProjectAndName(context.Background(), project.ID, job.Name)
	if err != nil {
		t.Fatalf("resolve job without build service failed: %v", err)
	}
	if resolved.ID != job.ID {
		t.Fatalf("expected resolved id %q, got %q", job.ID, resolved.ID)
	}
	if resolved.LatestBuild != nil {
		t.Fatalf("expected nil latest build without build service, got %+v", resolved.LatestBuild)
	}
}

func TestJobService_CreateJobFailureQueuesNoInitialBuild(t *testing.T) {
	jobRepo := memory.NewJobRepository()
	buildRepo := memory.NewBuildRepository()
	buildService := buildsvc.NewBuildService(buildRepo, nil, nil)
	jobService := NewJobService(jobRepo, buildService)

	_, err := jobService.CreateJob(context.Background(), CreateJobInput{
		ProjectID:     "project-1",
		Name:          "backend-bad",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PipelineYAML:  "version: 2\nsteps:\n  - name: bad\n    run: echo bad\n",
	})
	if err == nil {
		t.Fatal("expected invalid pipeline error")
	}

	builds, listErr := buildService.ListBuilds(context.Background())
	if listErr != nil {
		t.Fatalf("list builds failed: %v", listErr)
	}
	if len(builds) != 0 {
		t.Fatalf("expected no builds after failed create, got %d", len(builds))
	}
}

func TestJobService_CreateJobRepositoryErrorQueuesNoInitialBuild(t *testing.T) {
	jobRepo := &failingCreateJobRepository{}
	buildRepo := memory.NewBuildRepository()
	buildService := buildsvc.NewBuildService(buildRepo, nil, nil)
	jobService := NewJobService(jobRepo, buildService)

	_, err := jobService.CreateJob(context.Background(), CreateJobInput{
		ProjectID:     "project-1",
		Name:          "backend-ci",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PipelineYAML:  "version: 1\nsteps:\n  - name: test\n    run: go test ./...\n",
	})
	if !errors.Is(err, errJobCreateRejected) {
		t.Fatalf("expected create rejection, got %v", err)
	}

	builds, listErr := buildService.ListBuilds(context.Background())
	if listErr != nil {
		t.Fatalf("list builds failed: %v", listErr)
	}
	if len(builds) != 0 {
		t.Fatalf("expected no builds after rejected create, got %d", len(builds))
	}
}

func TestJobService_CreateJobInitialBuildFailureRollsBackJobAndManagedImageConfig(t *testing.T) {
	jobRepo := &recordingJobRepository{JobRepository: memory.NewJobRepository()}
	configRepo := memory.NewJobManagedImageConfigRepository()
	credentialRepo := memory.NewSourceCredentialRepository()
	jobService := NewJobService(jobRepo, nil).WithManagedImageConfigRepository(configRepo, credentialRepo)

	_, err := credentialRepo.Create(context.Background(), domain.SourceCredential{
		ID:        "cred-1",
		Name:      "github-bot",
		Kind:      domain.SourceCredentialKindHTTPSToken,
		SecretRef: "COYOTE_TOKEN",
	})
	if err != nil {
		t.Fatalf("create credential failed: %v", err)
	}

	_, err = jobService.CreateJob(context.Background(), CreateJobInput{
		ProjectID:     "project-1",
		Name:          "backend-ci",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PipelineYAML:  "version: 1\nsteps:\n  - name: test\n    run: go test ./...\n",
		ManagedImage: &ManagedImageConfigInput{
			Enabled:           true,
			ManagedImageName:  "go",
			PipelinePath:      ".coyote/pipeline.yml",
			WriteCredentialID: "cred-1",
		},
	})
	if !errors.Is(err, ErrJobBuildServiceNotConfigured) {
		t.Fatalf("expected build service not configured error, got %v", err)
	}

	jobs, listErr := jobService.ListJobs(context.Background())
	if listErr != nil {
		t.Fatalf("list jobs failed: %v", listErr)
	}
	if len(jobs) != 0 {
		t.Fatalf("expected rolled back create to leave no jobs, got %d", len(jobs))
	}
	if _, configErr := configRepo.GetByJobID(context.Background(), jobRepo.lastCreatedID); !errors.Is(configErr, repository.ErrJobManagedImageConfigNotFound) {
		t.Fatalf("expected managed image config rollback, got %v", configErr)
	}
}

func TestJobService_CreateAndUpdateManagedImageConfig(t *testing.T) {
	jobRepo := memory.NewJobRepository()
	buildRepo := memory.NewBuildRepository()
	configRepo := memory.NewJobManagedImageConfigRepository()
	credentialRepo := memory.NewSourceCredentialRepository()
	buildService := buildsvc.NewBuildService(buildRepo, nil, nil)
	jobService := NewJobService(jobRepo, buildService).WithManagedImageConfigRepository(configRepo, credentialRepo)

	_, err := credentialRepo.Create(context.Background(), domain.SourceCredential{
		ID:        "cred-1",
		Name:      "github-bot",
		Kind:      domain.SourceCredentialKindHTTPSToken,
		SecretRef: "COYOTE_GITHUB_TOKEN",
	})
	if err != nil {
		t.Fatalf("create credential failed: %v", err)
	}

	job, err := jobService.CreateJob(context.Background(), CreateJobInput{
		ProjectID:     "project-1",
		Name:          "backend-ci",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PipelineYAML:  "version: 1\nsteps:\n  - name: test\n    run: go test ./...\n",
		ManagedImage: &ManagedImageConfigInput{
			Enabled:           true,
			ManagedImageName:  "go",
			PipelinePath:      ".coyote/pipeline.yml",
			WriteCredentialID: "cred-1",
		},
	})
	if err != nil {
		t.Fatalf("create job failed: %v", err)
	}
	if job.ManagedImageConfig == nil {
		t.Fatal("expected managed image config on created job")
	}
	if job.ManagedImageConfig.WriteCredentialID != "cred-1" {
		t.Fatalf("expected write credential id cred-1, got %q", job.ManagedImageConfig.WriteCredentialID)
	}

	loaded, err := jobService.GetJob(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("get job failed: %v", err)
	}
	if loaded.ManagedImageConfig == nil || loaded.ManagedImageConfig.ManagedImageName != "go" {
		t.Fatalf("expected managed image config to load, got %+v", loaded.ManagedImageConfig)
	}

	updated, err := jobService.UpdateJob(context.Background(), job.ID, UpdateJobInput{
		ManagedImageSet: true,
		ManagedImage: &ManagedImageConfigPatch{
			Enabled:          boolPtr(true),
			ManagedImageName: strPtr("go-1-24"),
		},
	})
	if err != nil {
		t.Fatalf("update job failed: %v", err)
	}
	if updated.ManagedImageConfig == nil {
		t.Fatal("expected managed image config on updated job")
	}
	if !updated.ManagedImageConfig.Enabled {
		t.Fatal("expected managed image config to remain enabled")
	}
	if updated.ManagedImageConfig.ManagedImageName != "go-1-24" {
		t.Fatalf("expected updated managed image name, got %q", updated.ManagedImageConfig.ManagedImageName)
	}
}

func TestJobService_UpdateJobManagedImageDisabledDeletesConfig(t *testing.T) {
	jobRepo := memory.NewJobRepository()
	buildRepo := memory.NewBuildRepository()
	configRepo := memory.NewJobManagedImageConfigRepository()
	credentialRepo := memory.NewSourceCredentialRepository()
	buildService := buildsvc.NewBuildService(buildRepo, nil, nil)
	jobService := NewJobService(jobRepo, buildService).WithManagedImageConfigRepository(configRepo, credentialRepo)

	_, err := credentialRepo.Create(context.Background(), domain.SourceCredential{
		ID:        "cred-1",
		Name:      "github-bot",
		Kind:      domain.SourceCredentialKindHTTPSToken,
		SecretRef: "COYOTE_GITHUB_TOKEN",
	})
	if err != nil {
		t.Fatalf("create credential failed: %v", err)
	}

	job, err := jobService.CreateJob(context.Background(), CreateJobInput{
		ProjectID:     "project-1",
		Name:          "backend-ci",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PipelineYAML:  "version: 1\nsteps:\n  - name: test\n    run: go test ./...\n",
		ManagedImage: &ManagedImageConfigInput{
			Enabled:           true,
			ManagedImageName:  "go",
			PipelinePath:      ".coyote/pipeline.yml",
			WriteCredentialID: "cred-1",
		},
	})
	if err != nil {
		t.Fatalf("create job failed: %v", err)
	}

	updated, err := jobService.UpdateJob(context.Background(), job.ID, UpdateJobInput{
		ManagedImageSet: true,
		ManagedImage: &ManagedImageConfigPatch{
			Enabled: boolPtr(false),
		},
	})
	if err != nil {
		t.Fatalf("disable managed image failed: %v", err)
	}
	if updated.ManagedImageConfig != nil {
		t.Fatalf("expected managed image config removed, got %+v", updated.ManagedImageConfig)
	}
	if _, err := configRepo.GetByJobID(context.Background(), job.ID); !errors.Is(err, repository.ErrJobManagedImageConfigNotFound) {
		t.Fatalf("expected config repo row deleted, got %v", err)
	}
}

func TestJobService_UpdateJobManagedImageCreateDefaultsEnabledTrue(t *testing.T) {
	jobRepo := memory.NewJobRepository()
	buildRepo := memory.NewBuildRepository()
	configRepo := memory.NewJobManagedImageConfigRepository()
	credentialRepo := memory.NewSourceCredentialRepository()
	buildService := buildsvc.NewBuildService(buildRepo, nil, nil)
	jobService := NewJobService(jobRepo, buildService).WithManagedImageConfigRepository(configRepo, credentialRepo)

	_, err := credentialRepo.Create(context.Background(), domain.SourceCredential{
		ID:        "cred-1",
		Name:      "github-bot",
		Kind:      domain.SourceCredentialKindHTTPSToken,
		SecretRef: "COYOTE_GITHUB_TOKEN",
	})
	if err != nil {
		t.Fatalf("create credential failed: %v", err)
	}

	job, err := jobService.CreateJob(context.Background(), CreateJobInput{
		ProjectID:     "project-1",
		Name:          "backend-ci",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PipelineYAML:  "version: 1\nsteps:\n  - name: test\n    run: go test ./...\n",
	})
	if err != nil {
		t.Fatalf("create job failed: %v", err)
	}

	updated, err := jobService.UpdateJob(context.Background(), job.ID, UpdateJobInput{
		ManagedImageSet: true,
		ManagedImage: &ManagedImageConfigPatch{
			ManagedImageName:  strPtr("go"),
			PipelinePath:      strPtr(".coyote/pipeline.yml"),
			WriteCredentialID: strPtr("cred-1"),
		},
	})
	if err != nil {
		t.Fatalf("create managed image via update failed: %v", err)
	}
	if updated.ManagedImageConfig == nil {
		t.Fatal("expected managed image config on updated job")
	}
	if !updated.ManagedImageConfig.Enabled {
		t.Fatal("expected managed image config to default enabled=true when created via update")
	}
	stored, err := configRepo.GetByJobID(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("expected stored managed image config, got %v", err)
	}
	if !stored.Enabled {
		t.Fatal("expected persisted managed image config enabled=true")
	}
}

func TestJobService_UpdateJobManagedImageNullDeletesConfigWithoutValidation(t *testing.T) {
	jobRepo := memory.NewJobRepository()
	buildRepo := memory.NewBuildRepository()
	configRepo := memory.NewJobManagedImageConfigRepository()
	credentialRepo := memory.NewSourceCredentialRepository()
	buildService := buildsvc.NewBuildService(buildRepo, nil, nil)
	jobService := NewJobService(jobRepo, buildService).WithManagedImageConfigRepository(configRepo, credentialRepo)

	_, err := credentialRepo.Create(context.Background(), domain.SourceCredential{
		ID:        "cred-1",
		Name:      "github-bot",
		Kind:      domain.SourceCredentialKindHTTPSToken,
		SecretRef: "COYOTE_GITHUB_TOKEN",
	})
	if err != nil {
		t.Fatalf("create credential failed: %v", err)
	}

	job, err := jobService.CreateJob(context.Background(), CreateJobInput{
		ProjectID:     "project-1",
		Name:          "backend-ci",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PipelineYAML:  "version: 1\nsteps:\n  - name: test\n    run: go test ./...\n",
		ManagedImage: &ManagedImageConfigInput{
			Enabled:           true,
			ManagedImageName:  "go",
			PipelinePath:      ".coyote/pipeline.yml",
			WriteCredentialID: "cred-1",
		},
	})
	if err != nil {
		t.Fatalf("create job failed: %v", err)
	}

	updated, err := jobService.UpdateJob(context.Background(), job.ID, UpdateJobInput{
		ManagedImageSet: true,
		ManagedImage:    nil,
	})
	if err != nil {
		t.Fatalf("null managed image update failed: %v", err)
	}
	if updated.ManagedImageConfig != nil {
		t.Fatalf("expected managed image config removed, got %+v", updated.ManagedImageConfig)
	}
	if _, err := configRepo.GetByJobID(context.Background(), job.ID); !errors.Is(err, repository.ErrJobManagedImageConfigNotFound) {
		t.Fatalf("expected config repo row deleted, got %v", err)
	}
}

func TestJobService_CreateRejectsInvalidPipelineYAML(t *testing.T) {
	jobService := NewJobService(memory.NewJobRepository(), buildsvc.NewBuildService(memory.NewBuildRepository(), nil, nil))

	_, err := jobService.CreateJob(context.Background(), CreateJobInput{
		ProjectID:     "project-1",
		Name:          "backend-ci",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PipelineYAML:  "version: 2\nsteps:\n  - name: bad\n    run: echo bad\n",
	})
	if err == nil {
		t.Fatal("expected invalid pipeline error")
	}
}

func TestJobService_CreateAllowsRepoPipelinePathWithoutInlineYAML(t *testing.T) {
	buildService := buildsvc.NewBuildService(memory.NewBuildRepository(), nil, nil)
	buildService.SetRepoFetcher(&jobServiceRepoFetcher{localPath: writeTestPipelineRepo(t, "scenarios/success-basic/coyote.yml")})
	jobService := NewJobService(memory.NewJobRepository(), buildService)

	job, err := jobService.CreateJob(context.Background(), CreateJobInput{
		ProjectID:        "project-1",
		Name:             "backend-path",
		RepositoryURL:    "https://github.com/example/backend.git",
		DefaultRef:       "main",
		DefaultCommitSHA: "",
		PipelinePath:     "scenarios/success-basic/coyote.yml",
		PipelineYAML:     "",
	})
	if err != nil {
		t.Fatalf("expected path-based job create to succeed, got %v", err)
	}
	if job.PipelinePath == nil || *job.PipelinePath != "scenarios/success-basic/coyote.yml" {
		t.Fatalf("expected persisted pipeline_path, got %v", job.PipelinePath)
	}
	if strings.TrimSpace(job.PipelineYAML) != "" {
		t.Fatalf("expected empty inline pipeline yaml, got %q", job.PipelineYAML)
	}
}

func TestJobService_CreatePrefersInlinePipelineWhenPathAlsoPresent(t *testing.T) {
	jobRepo := memory.NewJobRepository()
	buildRepo := memory.NewBuildRepository()
	buildService := buildsvc.NewBuildService(buildRepo, nil, nil)
	jobService := NewJobService(jobRepo, buildService)

	job, err := jobService.CreateJob(context.Background(), CreateJobInput{
		ProjectID:     "project-1",
		Name:          "backend-inline-preferred",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PipelinePath:  "scenarios/success-basic/coyote.yml",
		PipelineYAML:  "version: 1\nsteps:\n  - name: run\n    run: ./scripts/run.sh\n",
	})
	if err != nil {
		t.Fatalf("expected create to succeed with inline pipeline, got %v", err)
	}

	builds, listErr := buildService.ListBuildsByJobID(context.Background(), job.ID)
	if listErr != nil {
		t.Fatalf("list builds failed: %v", listErr)
	}
	if len(builds) != 1 {
		t.Fatalf("expected 1 build, got %d", len(builds))
	}
	if builds[0].PipelineSource == nil || *builds[0].PipelineSource != "inline" {
		t.Fatalf("expected inline pipeline source, got %v", builds[0].PipelineSource)
	}
	if builds[0].PipelinePath == nil || *builds[0].PipelinePath != "scenarios/success-basic/coyote.yml" {
		t.Fatalf("expected persisted pipeline_path for inline build context, got %v", builds[0].PipelinePath)
	}
}

func TestJobService_RunNowCreatesNormalBuildAndSnapshotsPipeline(t *testing.T) {
	jobRepo := memory.NewJobRepository()
	buildRepo := memory.NewBuildRepository()
	buildService := buildsvc.NewBuildService(buildRepo, nil, nil)
	jobService := NewJobService(jobRepo, buildService)

	job, err := jobService.CreateJob(context.Background(), CreateJobInput{
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

	build, err := jobService.RunJobNow(context.Background(), job.ID, nil)
	if err != nil {
		t.Fatalf("run job now failed: %v", err)
	}
	if build.RepoURL == nil || *build.RepoURL != "https://github.com/example/backend.git" {
		t.Fatalf("expected build source repository_url, got %v", build.RepoURL)
	}
	if build.Ref == nil || *build.Ref != "main" {
		t.Fatalf("expected build source ref main, got %v", build.Ref)
	}
	if build.PipelineConfigYAML == nil || !strings.Contains(*build.PipelineConfigYAML, "go test ./...") {
		t.Fatalf("expected build pipeline snapshot, got %v", build.PipelineConfigYAML)
	}

	_, err = jobService.UpdateJob(context.Background(), job.ID, UpdateJobInput{
		PipelineYAML: strPtr("version: 1\nsteps:\n  - name: lint\n    run: golangci-lint run\n"),
	})
	if err != nil {
		t.Fatalf("update job failed: %v", err)
	}

	storedBuild, err := buildService.GetBuild(context.Background(), build.ID)
	if err != nil {
		t.Fatalf("get build failed: %v", err)
	}
	if storedBuild.PipelineConfigYAML == nil || !strings.Contains(*storedBuild.PipelineConfigYAML, "go test ./...") {
		t.Fatalf("expected old build snapshot unchanged, got %v", storedBuild.PipelineConfigYAML)
	}
}

func TestJobService_RunNowDisabledJobRejected(t *testing.T) {
	jobRepo := memory.NewJobRepository()
	buildRepo := memory.NewBuildRepository()
	jobService := NewJobService(jobRepo, buildsvc.NewBuildService(buildRepo, nil, nil))

	job, err := jobService.CreateJob(context.Background(), CreateJobInput{
		ProjectID:     "project-1",
		Name:          "backend-ci",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PipelineYAML:  "version: 1\nsteps:\n  - name: test\n    run: go test ./...\n",
		Enabled:       boolPtr(false),
	})
	if err != nil {
		t.Fatalf("create job failed: %v", err)
	}

	_, err = jobService.RunJobNow(context.Background(), job.ID, nil)
	if !errors.Is(err, ErrJobDisabled) {
		t.Fatalf("expected ErrJobDisabled, got %v", err)
	}
}

func TestJobService_RunNowOverrideRefReplacesDefaultSourceTarget(t *testing.T) {
	jobRepo := memory.NewJobRepository()
	buildRepo := memory.NewBuildRepository()
	buildService := buildsvc.NewBuildService(buildRepo, nil, nil)
	jobService := NewJobService(jobRepo, buildService)

	job, err := jobService.CreateJob(context.Background(), CreateJobInput{
		ProjectID:        "project-1",
		Name:             "backend-ci",
		RepositoryURL:    "https://github.com/example/backend.git",
		DefaultRef:       "main",
		DefaultCommitSHA: "abcdef1234567890",
		PipelineYAML:     "version: 1\nsteps:\n  - name: test\n    run: go test ./...\n",
		Enabled:          boolPtr(true),
	})
	if err != nil {
		t.Fatalf("create job failed: %v", err)
	}

	overrideRef := "release/2026.07"
	build, err := jobService.RunJobNow(context.Background(), job.ID, &overrideRef)
	if err != nil {
		t.Fatalf("run job now with override ref failed: %v", err)
	}
	if build.Ref == nil || *build.Ref != overrideRef {
		t.Fatalf("expected build ref %q, got %v", overrideRef, build.Ref)
	}
	if build.CommitSHA != nil {
		t.Fatalf("expected override ref to clear default commit SHA, got %v", build.CommitSHA)
	}
	if build.Source == nil || build.Source.Ref == nil || *build.Source.Ref != overrideRef {
		t.Fatalf("expected source ref %q, got %+v", overrideRef, build.Source)
	}
	if build.Source != nil && build.Source.CommitSHA != nil && strings.TrimSpace(*build.Source.CommitSHA) != "" {
		t.Fatalf("expected source commit SHA cleared, got %+v", build.Source)
	}

	emptyRef := "   "
	_, err = jobService.RunJobNow(context.Background(), job.ID, &emptyRef)
	if !errors.Is(err, ErrJobRunRefRequired) {
		t.Fatalf("expected ErrJobRunRefRequired, got %v", err)
	}
}

func TestJobService_TriggerPushEvent_MatchesAndCreatesBuilds(t *testing.T) {
	jobRepo := memory.NewJobRepository()
	buildRepo := memory.NewBuildRepository()
	buildService := buildsvc.NewBuildService(buildRepo, nil, nil)
	jobService := NewJobService(jobRepo, buildService)

	jobA, err := jobService.CreateJob(context.Background(), CreateJobInput{
		ProjectID:     "project-1",
		Name:          "backend-main",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PushEnabled:   boolPtr(true),
		PushBranch:    strPtr("main"),
		PipelineYAML:  "version: 1\nsteps:\n  - name: test\n    run: go test ./...\n",
		Enabled:       boolPtr(true),
	})
	if err != nil {
		t.Fatalf("create job A failed: %v", err)
	}

	_, err = jobService.CreateJob(context.Background(), CreateJobInput{
		ProjectID:     "project-1",
		Name:          "backend-any-branch",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PushEnabled:   boolPtr(true),
		PipelineYAML:  "version: 1\nsteps:\n  - name: lint\n    run: golangci-lint run\n",
		Enabled:       boolPtr(true),
	})
	if err != nil {
		t.Fatalf("create job B failed: %v", err)
	}

	_, err = jobService.CreateJob(context.Background(), CreateJobInput{
		ProjectID:     "project-1",
		Name:          "backend-disabled",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PushEnabled:   boolPtr(true),
		PushBranch:    strPtr("main"),
		PipelineYAML:  "version: 1\nsteps:\n  - name: skip\n    run: echo skip\n",
		Enabled:       boolPtr(false),
	})
	if err != nil {
		t.Fatalf("create disabled job failed: %v", err)
	}

	_, err = jobService.CreateJob(context.Background(), CreateJobInput{
		ProjectID:     "project-1",
		Name:          "backend-push-disabled",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PushEnabled:   boolPtr(false),
		PushBranch:    strPtr("main"),
		PipelineYAML:  "version: 1\nsteps:\n  - name: skip\n    run: echo skip\n",
		Enabled:       boolPtr(true),
	})
	if err != nil {
		t.Fatalf("create push-disabled job failed: %v", err)
	}

	result, err := jobService.TriggerPushEvent(context.Background(), PushEventInput{
		RepositoryURL: "https://github.com/example/backend.git",
		Ref:           "refs/heads/main",
		CommitSHA:     "abc123",
	})
	if err != nil {
		t.Fatalf("trigger push event failed: %v", err)
	}

	if result.MatchedJobs != 2 {
		t.Fatalf("expected 2 matched jobs, got %d", result.MatchedJobs)
	}
	if len(result.Builds) != 2 {
		t.Fatalf("expected 2 created builds, got %d", len(result.Builds))
	}

	for _, item := range result.Builds {
		if item.Build.RepoURL == nil || *item.Build.RepoURL != "https://github.com/example/backend.git" {
			t.Fatalf("expected build repo_url from job, got %v", item.Build.RepoURL)
		}
		if item.Build.Ref == nil || *item.Build.Ref != "main" {
			t.Fatalf("expected build ref=main, got %v", item.Build.Ref)
		}
		if item.Build.CommitSHA == nil || *item.Build.CommitSHA != "abc123" {
			t.Fatalf("expected build commit_sha=abc123, got %v", item.Build.CommitSHA)
		}
		if item.Build.PipelineConfigYAML == nil || *item.Build.PipelineConfigYAML == "" {
			t.Fatal("expected build pipeline snapshot")
		}
		if item.Build.Trigger.Kind != domain.BuildTriggerKindWebhook {
			t.Fatalf("expected webhook trigger kind, got %q", item.Build.Trigger.Kind)
		}
		if item.Build.Trigger.SCMProvider == nil || *item.Build.Trigger.SCMProvider != "github" {
			t.Fatalf("expected trigger scm_provider=github, got %v", item.Build.Trigger.SCMProvider)
		}
		if item.Build.Trigger.EventType == nil || *item.Build.Trigger.EventType != "push" {
			t.Fatalf("expected trigger event_type=push, got %v", item.Build.Trigger.EventType)
		}
	}

	_, err = jobService.UpdateJob(context.Background(), jobA.ID, UpdateJobInput{
		PipelineYAML: strPtr("version: 1\nsteps:\n  - name: changed\n    run: echo changed\n"),
	})
	if err != nil {
		t.Fatalf("update job after trigger failed: %v", err)
	}

	storedBuild, err := buildService.GetBuild(context.Background(), result.Builds[0].Build.ID)
	if err != nil {
		t.Fatalf("get triggered build failed: %v", err)
	}
	if storedBuild.PipelineConfigYAML == nil || strings.Contains(*storedBuild.PipelineConfigYAML, "changed") {
		t.Fatalf("expected triggered build snapshot unchanged, got %v", storedBuild.PipelineConfigYAML)
	}
}

func TestJobService_TriggerPushEvent_NoMatches(t *testing.T) {
	jobRepo := memory.NewJobRepository()
	buildRepo := memory.NewBuildRepository()
	buildService := buildsvc.NewBuildService(buildRepo, nil, nil)
	jobService := NewJobService(jobRepo, buildService)

	result, err := jobService.TriggerPushEvent(context.Background(), PushEventInput{
		RepositoryURL: "https://github.com/example/backend.git",
		Ref:           "main",
		CommitSHA:     "abc123",
	})
	if err != nil {
		t.Fatalf("trigger push event failed: %v", err)
	}
	if result.MatchedJobs != 0 {
		t.Fatalf("expected 0 matched jobs, got %d", result.MatchedJobs)
	}
	if len(result.Builds) != 0 {
		t.Fatalf("expected 0 builds, got %d", len(result.Builds))
	}
}

func TestJobService_TriggerPushEvent_Validation(t *testing.T) {
	jobService := NewJobService(memory.NewJobRepository(), buildsvc.NewBuildService(memory.NewBuildRepository(), nil, nil))

	_, err := jobService.TriggerPushEvent(context.Background(), PushEventInput{})
	if !errors.Is(err, ErrPushEventRepositoryURLRequired) {
		t.Fatalf("expected ErrPushEventRepositoryURLRequired, got %v", err)
	}

	_, err = jobService.TriggerPushEvent(context.Background(), PushEventInput{RepositoryURL: "https://github.com/example/backend.git"})
	if !errors.Is(err, ErrPushEventRefRequired) {
		t.Fatalf("expected ErrPushEventRefRequired, got %v", err)
	}

	_, err = jobService.TriggerPushEvent(context.Background(), PushEventInput{RepositoryURL: "https://github.com/example/backend.git", Ref: "main"})
	if !errors.Is(err, ErrPushEventCommitSHARequired) {
		t.Fatalf("expected ErrPushEventCommitSHARequired, got %v", err)
	}
}

func TestJobService_TriggerWebhookEvent_TagOnlyJob(t *testing.T) {
	jobRepo := memory.NewJobRepository()
	buildRepo := memory.NewBuildRepository()
	buildService := buildsvc.NewBuildService(buildRepo, nil, nil)
	jobService := NewJobService(jobRepo, buildService)

	triggerMode := "tags"
	_, err := jobService.CreateJob(context.Background(), CreateJobInput{
		ProjectID:     "project-1",
		Name:          "backend-tags",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PushEnabled:   boolPtr(true),
		TriggerMode:   &triggerMode,
		TagAllowlist:  []string{"v*"},
		PipelineYAML:  "version: 1\nsteps:\n  - name: release\n    run: echo release\n",
		Enabled:       boolPtr(true),
	})
	if err != nil {
		t.Fatalf("create job failed: %v", err)
	}

	branchResult, err := jobService.TriggerWebhookEvent(context.Background(), webhooksvc.WebhookTriggerInput{
		SCMProvider:   "github",
		EventType:     "push",
		RepositoryURL: "https://github.com/example/backend.git",
		RawRef:        "refs/heads/main",
		CommitSHA:     "abc123",
	})
	if err != nil {
		t.Fatalf("branch trigger failed: %v", err)
	}
	if branchResult.MatchedJobs != 0 {
		t.Fatalf("expected no branch matches for tag-only job, got %d", branchResult.MatchedJobs)
	}

	tagResult, err := jobService.TriggerWebhookEvent(context.Background(), webhooksvc.WebhookTriggerInput{
		SCMProvider:   "github",
		EventType:     "push",
		RepositoryURL: "https://github.com/example/backend.git",
		RawRef:        "refs/tags/v1.2.3",
		CommitSHA:     "def456",
	})
	if err != nil {
		t.Fatalf("tag trigger failed: %v", err)
	}
	if tagResult.MatchedJobs != 1 {
		t.Fatalf("expected one tag match, got %d", tagResult.MatchedJobs)
	}
}

func TestJobService_TriggerWebhookEvent_DeletePushIgnored(t *testing.T) {
	jobRepo := memory.NewJobRepository()
	buildRepo := memory.NewBuildRepository()
	buildService := buildsvc.NewBuildService(buildRepo, nil, nil)
	jobService := NewJobService(jobRepo, buildService)

	_, err := jobService.CreateJob(context.Background(), CreateJobInput{
		ProjectID:     "project-1",
		Name:          "backend-main",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PushEnabled:   boolPtr(true),
		PushBranch:    strPtr("main"),
		PipelineYAML:  "version: 1\nsteps:\n  - name: test\n    run: go test ./...\n",
		Enabled:       boolPtr(true),
	})
	if err != nil {
		t.Fatalf("create job failed: %v", err)
	}

	result, err := jobService.TriggerWebhookEvent(context.Background(), webhooksvc.WebhookTriggerInput{
		SCMProvider:   "github",
		EventType:     "push",
		RepositoryURL: "https://github.com/example/backend.git",
		RawRef:        "refs/heads/main",
		Deleted:       true,
	})
	if err != nil {
		t.Fatalf("delete push should be ignored cleanly, got error: %v", err)
	}
	if result.MatchedJobs != 0 {
		t.Fatalf("expected no matches for deleted ref, got %d", result.MatchedJobs)
	}
	if result.NoMatchReason == nil || *result.NoMatchReason != string(webhooksvc.WebhookFilterDecisionDeletedRef) {
		t.Fatalf("expected no_match_reason deleted_ref, got %v", result.NoMatchReason)
	}
}

func TestJobService_TriggerWebhookEvent_UsesRegisteredRepositoryIdentity(t *testing.T) {
	ctx := context.Background()
	jobRepo := &urlLookupDetectingJobRepo{JobRepository: memory.NewJobRepository()}
	registeredRepo := memory.NewSCMRepositoryRegistrationRepository()
	buildRepo := memory.NewBuildRepository()
	buildService := buildsvc.NewBuildService(buildRepo, nil, nil)
	jobService := NewJobService(jobRepo, buildService).WithSCMRepositoryRegistrationRepository(registeredRepo)
	now := time.Now().UTC()
	for _, registration := range []domain.SCMRepositoryRegistration{
		{ID: "repo-a", ConnectionID: "connection-a", ProviderRepositoryID: "1001", Owner: "same", Name: "repository", FullName: "same/repository", CloneURL: "https://github.com/same/repository.git", WebURL: "https://github.com/same/repository", MetadataRefreshedAt: now, CreatedAt: now, UpdatedAt: now},
		{ID: "repo-b", ConnectionID: "connection-b", ProviderRepositoryID: "1001", Owner: "same", Name: "repository", FullName: "same/repository", CloneURL: "https://github.com/same/repository.git", WebURL: "https://github.com/same/repository", MetadataRefreshedAt: now, CreatedAt: now, UpdatedAt: now},
	} {
		if _, err := registeredRepo.Create(ctx, registration); err != nil {
			t.Fatalf("create registration %s: %v", registration.ID, err)
		}
	}
	for _, job := range []domain.Job{
		{ID: "job-a-1", ProjectID: "project-1", Name: "a-1", RepositoryID: strPtr("repo-a"), RepositoryURL: "https://github.com/old/name.git", PushEnabled: true, Enabled: true, PipelineYAML: "version: 1\nsteps:\n  - name: test\n    run: echo a1\n", CreatedAt: now, UpdatedAt: now},
		{ID: "job-a-2", ProjectID: "project-1", Name: "a-2", RepositoryID: strPtr("repo-a"), RepositoryURL: "https://github.com/old/name.git", PushEnabled: true, Enabled: true, PipelineYAML: "version: 1\nsteps:\n  - name: test\n    run: echo a2\n", CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second)},
		{ID: "job-b", ProjectID: "project-1", Name: "b", RepositoryID: strPtr("repo-b"), RepositoryURL: "https://github.com/old/name.git", PushEnabled: true, Enabled: true, PipelineYAML: "version: 1\nsteps:\n  - name: test\n    run: echo b\n", CreatedAt: now.Add(2 * time.Second), UpdatedAt: now.Add(2 * time.Second)},
		{ID: "job-unmapped", ProjectID: "project-1", Name: "unmapped", RepositoryURL: "https://github.com/old/name.git", PushEnabled: true, Enabled: true, PipelineYAML: "version: 1\nsteps:\n  - name: test\n    run: echo unmapped\n", CreatedAt: now.Add(3 * time.Second), UpdatedAt: now.Add(3 * time.Second)},
	} {
		if _, err := jobRepo.Create(ctx, job); err != nil {
			t.Fatalf("create job %s: %v", job.ID, err)
		}
	}

	input := webhooksvc.WebhookTriggerInput{ConnectionID: "connection-a", SCMProvider: "github", EventType: "push", ProviderRepositoryID: "1001", RepositoryOwner: "renamed", RepositoryName: "transferred", RepositoryURL: "https://github.com/renamed/transferred.git", RawRef: "refs/heads/main", CommitSHA: "abc123"}
	result, triggerErr := jobService.TriggerWebhookEvent(ctx, input)
	if triggerErr != nil {
		t.Fatalf("trigger connection-a webhook: %v", triggerErr)
	}
	if result.MatchedJobs != 2 || len(result.Builds) != 2 || result.Builds[0].Job.ID != "job-a-2" || result.Builds[1].Job.ID != "job-a-1" {
		t.Fatalf("expected only connection-a mapped jobs, got %+v", result.Builds)
	}
	for _, triggered := range result.Builds {
		if triggered.Build.RegisteredRepositoryID == nil || *triggered.Build.RegisteredRepositoryID != "repo-a" || triggered.Build.SCMConnectionID == nil || *triggered.Build.SCMConnectionID != "connection-a" || triggered.Build.ProviderRepositoryID == nil || *triggered.Build.ProviderRepositoryID != "1001" {
			t.Fatalf("expected build identity snapshot for %s, got %+v", triggered.Job.ID, triggered.Build)
		}
	}

	input.ConnectionID = "connection-b"
	result, triggerErr = jobService.TriggerWebhookEvent(ctx, input)
	if triggerErr != nil {
		t.Fatalf("trigger connection-b webhook: %v", triggerErr)
	}
	if result.MatchedJobs != 1 || len(result.Builds) != 1 || result.Builds[0].Job.ID != "job-b" {
		t.Fatalf("expected only connection-b mapped job, got %+v", result.Builds)
	}
	if jobRepo.legacyLookupCalls != 0 {
		t.Fatalf("expected no URL-based lookup for connection-aware webhooks, got %d calls", jobRepo.legacyLookupCalls)
	}
}

func TestJobServiceBuildRepositoryIdentityValidation(t *testing.T) {
	jobService := NewJobService(memory.NewJobRepository(), buildsvc.NewBuildService(memory.NewBuildRepository(), nil, nil))
	identity, err := jobService.buildRepositoryIdentity(context.Background(), domain.Job{})
	if err != nil || identity != nil {
		t.Fatalf("expected unmapped job to have no identity, identity=%+v err=%v", identity, err)
	}

	repositoryID := "repository-1"
	_, err = jobService.buildRepositoryIdentity(context.Background(), domain.Job{RepositoryID: &repositoryID})
	if !errors.Is(err, ErrJobRegisteredRepositoryStoreNotConfigured) {
		t.Fatalf("expected missing registration repository error, got %v", err)
	}
}

func TestJobService_TriggerWebhookEvent_RegisteredRepositoryNoOpsAndFilters(t *testing.T) {
	ctx := context.Background()
	jobRepo := memory.NewJobRepository()
	registeredRepo := memory.NewSCMRepositoryRegistrationRepository()
	buildService := buildsvc.NewBuildService(memory.NewBuildRepository(), nil, nil)
	jobService := NewJobService(jobRepo, buildService).WithSCMRepositoryRegistrationRepository(registeredRepo)
	now := time.Now().UTC()
	for _, registration := range []domain.SCMRepositoryRegistration{
		{ID: "repo-disabled", ConnectionID: "connection-a", ProviderRepositoryID: "1001", Owner: "owner", Name: "disabled", FullName: "owner/disabled", CloneURL: "https://github.com/owner/disabled.git", WebURL: "https://github.com/owner/disabled", Disabled: true, MetadataRefreshedAt: now, CreatedAt: now, UpdatedAt: now},
		{ID: "repo-archived", ConnectionID: "connection-a", ProviderRepositoryID: "1002", Owner: "owner", Name: "archived", FullName: "owner/archived", CloneURL: "https://github.com/owner/archived.git", WebURL: "https://github.com/owner/archived", Archived: true, MetadataRefreshedAt: now, CreatedAt: now, UpdatedAt: now},
		{ID: "repo-tags", ConnectionID: "connection-a", ProviderRepositoryID: "1003", Owner: "owner", Name: "tags", FullName: "owner/tags", CloneURL: "https://github.com/owner/tags.git", WebURL: "https://github.com/owner/tags", MetadataRefreshedAt: now, CreatedAt: now, UpdatedAt: now},
	} {
		if _, err := registeredRepo.Create(ctx, registration); err != nil {
			t.Fatalf("create registration %s: %v", registration.ID, err)
		}
	}
	for _, job := range []domain.Job{
		{ID: "job-disabled-repository", ProjectID: "project-1", Name: "disabled", RepositoryID: strPtr("repo-disabled"), PushEnabled: true, Enabled: true, PipelineYAML: "version: 1\nsteps:\n  - name: test\n    run: echo disabled\n", CreatedAt: now, UpdatedAt: now},
		{ID: "job-archived-repository", ProjectID: "project-1", Name: "archived", RepositoryID: strPtr("repo-archived"), RepositoryURL: "https://github.com/owner/archived.git", PushEnabled: true, Enabled: true, TriggerMode: domain.JobTriggerModeBranches, BranchAllowlist: []string{"main"}, PipelineYAML: "version: 1\nsteps:\n  - name: test\n    run: echo archived\n", CreatedAt: now, UpdatedAt: now},
		{ID: "job-tags-repository", ProjectID: "project-1", Name: "tags", RepositoryID: strPtr("repo-tags"), RepositoryURL: "https://github.com/owner/tags.git", PushEnabled: true, Enabled: true, TriggerMode: domain.JobTriggerModeTags, TagAllowlist: []string{"v*"}, PipelineYAML: "version: 1\nsteps:\n  - name: test\n    run: echo tags\n", CreatedAt: now, UpdatedAt: now},
	} {
		if _, err := jobRepo.Create(ctx, job); err != nil {
			t.Fatalf("create job %s: %v", job.ID, err)
		}
	}

	for _, testCase := range []struct {
		name        string
		providerID  string
		ref         string
		deleted     bool
		wantMatches int
	}{
		{name: "unknown repository", providerID: "missing", ref: "refs/heads/main"},
		{name: "disabled repository", providerID: "1001", ref: "refs/heads/main"},
		{name: "archived repository routes", providerID: "1002", ref: "refs/heads/main", wantMatches: 1},
		{name: "branch filter remains active", providerID: "1002", ref: "refs/heads/release"},
		{name: "deleted ref remains ignored", providerID: "1002", ref: "refs/heads/main", deleted: true},
		{name: "tag filter routes matching tag", providerID: "1003", ref: "refs/tags/v1.2.3", wantMatches: 1},
		{name: "tag filter rejects nonmatching tag", providerID: "1003", ref: "refs/tags/release"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			result, triggerErr := jobService.TriggerWebhookEvent(ctx, webhooksvc.WebhookTriggerInput{ConnectionID: "connection-a", SCMProvider: "github", EventType: "push", ProviderRepositoryID: testCase.providerID, RawRef: testCase.ref, Deleted: testCase.deleted, CommitSHA: "abc123"})
			if triggerErr != nil {
				t.Fatalf("trigger webhook: %v", triggerErr)
			}
			if result.MatchedJobs != testCase.wantMatches {
				t.Fatalf("expected %d matches, got %+v", testCase.wantMatches, result)
			}
		})
	}
}

func TestJobService_TriggerWebhookEvent_RepositoryAwareInputAndPersistenceFailures(t *testing.T) {
	ctx := context.Background()
	buildService := buildsvc.NewBuildService(memory.NewBuildRepository(), nil, nil)
	input := webhooksvc.WebhookTriggerInput{ConnectionID: "connection-a", SCMProvider: "github", EventType: "push", ProviderRepositoryID: "1001", RawRef: "refs/heads/main", CommitSHA: "abc123"}

	t.Run("provider repository id is required", func(t *testing.T) {
		jobService := NewJobService(memory.NewJobRepository(), buildService).WithSCMRepositoryRegistrationRepository(memory.NewSCMRepositoryRegistrationRepository())
		inputWithoutRepositoryID := input
		inputWithoutRepositoryID.ProviderRepositoryID = ""
		_, triggerErr := jobService.TriggerWebhookEvent(ctx, inputWithoutRepositoryID)
		if !errors.Is(triggerErr, ErrWebhookProviderRepositoryIDRequired) {
			t.Fatalf("expected ErrWebhookProviderRepositoryIDRequired, got %v", triggerErr)
		}
	})

	t.Run("registered repository lookup failure propagates", func(t *testing.T) {
		lookupErr := errors.New("registered repository lookup failed")
		jobService := NewJobService(memory.NewJobRepository(), buildService).WithSCMRepositoryRegistrationRepository(&erroringRegisteredRepositoryRepo{SCMRepositoryRegistrationRepository: memory.NewSCMRepositoryRegistrationRepository(), getByConnectionAndProviderIDErr: lookupErr})
		_, triggerErr := jobService.TriggerWebhookEvent(ctx, input)
		if !errors.Is(triggerErr, lookupErr) {
			t.Fatalf("expected lookup error, got %v", triggerErr)
		}
	})

	t.Run("mapped job lookup failure propagates", func(t *testing.T) {
		registeredRepo := memory.NewSCMRepositoryRegistrationRepository()
		now := time.Now().UTC()
		if _, err := registeredRepo.Create(ctx, domain.SCMRepositoryRegistration{ID: "repo-1", ConnectionID: "connection-a", ProviderRepositoryID: "1001", Owner: "owner", Name: "repository", FullName: "owner/repository", CloneURL: "https://github.com/owner/repository.git", WebURL: "https://github.com/owner/repository", MetadataRefreshedAt: now, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatalf("create registration: %v", err)
		}
		jobLookupErr := errors.New("mapped job lookup failed")
		jobService := NewJobService(&erroringWebhookRoutingJobRepo{JobRepository: memory.NewJobRepository(), listPushEnabledByRepositoryIDErr: jobLookupErr}, buildService).WithSCMRepositoryRegistrationRepository(registeredRepo)
		_, triggerErr := jobService.TriggerWebhookEvent(ctx, input)
		if !errors.Is(triggerErr, jobLookupErr) {
			t.Fatalf("expected mapped job lookup error, got %v", triggerErr)
		}
	})
}

func strPtr(v string) *string {
	return &v
}

func boolPtr(v bool) *bool {
	return &v
}

var errJobCreateRejected = errors.New("job create rejected")

type failingCreateJobRepository struct{}

type recordingJobRepository struct {
	repository.JobRepository
	lastCreatedID string
}

type jobServiceRepoFetcher struct {
	localPath string
}

func (f *jobServiceRepoFetcher) Fetch(_ context.Context, _ string, _ string) (string, string, error) {
	return f.localPath, "commit-sha", nil
}

func (r *recordingJobRepository) Create(ctx context.Context, job domain.Job) (domain.Job, error) {
	created, err := r.JobRepository.Create(ctx, job)
	if err == nil {
		r.lastCreatedID = created.ID
	}
	return created, err
}

func writeTestPipelineRepo(t *testing.T, pipelinePath string) string {
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

func (r *failingCreateJobRepository) Create(_ context.Context, _ domain.Job) (domain.Job, error) {
	return domain.Job{}, errJobCreateRejected
}

func (r *failingCreateJobRepository) Delete(_ context.Context, _ string) error {
	return repository.ErrJobNotFound
}

func (r *failingCreateJobRepository) FindByProjectIDAndName(_ context.Context, _ string, _ string, _ int) ([]domain.Job, error) {
	return []domain.Job{}, nil
}

func (r *failingCreateJobRepository) GetByIDs(_ context.Context, _ []string) ([]domain.Job, error) {
	return []domain.Job{}, nil
}

func (r *failingCreateJobRepository) List(_ context.Context) ([]domain.Job, error) {
	return []domain.Job{}, nil
}

func (r *failingCreateJobRepository) ListPaged(_ context.Context, _ repository.ListParams) ([]domain.Job, error) {
	return []domain.Job{}, nil
}

func (r *failingCreateJobRepository) ListByProjectID(_ context.Context, _ string) ([]domain.Job, error) {
	return []domain.Job{}, nil
}

func (r *failingCreateJobRepository) ListPushEnabledByRepository(_ context.Context, _ string) ([]domain.Job, error) {
	return []domain.Job{}, nil
}

func (r *failingCreateJobRepository) ListPushEnabledByRepositoryID(_ context.Context, _ string) ([]domain.Job, error) {
	return []domain.Job{}, nil
}

func (r *failingCreateJobRepository) GetByID(_ context.Context, _ string) (domain.Job, error) {
	return domain.Job{}, repository.ErrJobNotFound
}

func (r *failingCreateJobRepository) Update(_ context.Context, _ domain.Job) (domain.Job, error) {
	return domain.Job{}, repository.ErrJobNotFound
}
