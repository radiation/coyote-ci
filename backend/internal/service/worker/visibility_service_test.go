package worker

import (
	"context"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	memoryrepo "github.com/radiation/coyote-ci/backend/internal/repository/memory"
)

type fakeVisibilityBuildBoundary struct {
	builds        []domain.Build
	activeBuilds  []domain.Build
	steps         map[string][]domain.BuildStep
	jobs          map[string][]domain.ExecutionJob
	listBuildsErr error
	listActiveErr error
}

type fakeVisibilityProjectRepository struct {
	projects map[string]domain.Project
}

func (f fakeVisibilityProjectRepository) Create(context.Context, domain.Project) (domain.Project, error) {
	panic("unexpected call")
}

func (f fakeVisibilityProjectRepository) GetByID(_ context.Context, id string) (domain.Project, error) {
	project, ok := f.projects[id]
	if !ok {
		return domain.Project{}, repository.ErrProjectNotFound
	}
	return project, nil
}

func (f fakeVisibilityProjectRepository) GetByIDs(context.Context, []string) ([]domain.Project, error) {
	panic("unexpected call")
}

func (f fakeVisibilityProjectRepository) GetBySlug(context.Context, string) (domain.Project, error) {
	panic("unexpected call")
}

func (f fakeVisibilityProjectRepository) List(context.Context) ([]domain.Project, error) {
	panic("unexpected call")
}

func (f fakeVisibilityProjectRepository) Update(context.Context, domain.Project) (domain.Project, error) {
	panic("unexpected call")
}

func (f fakeVisibilityProjectRepository) Delete(context.Context, string) error {
	panic("unexpected call")
}

type fakeVisibilityJobRepository struct {
	jobs map[string]domain.Job
}

func (f fakeVisibilityJobRepository) Create(context.Context, domain.Job) (domain.Job, error) {
	panic("unexpected call")
}

func (f fakeVisibilityJobRepository) Delete(context.Context, string) error {
	panic("unexpected call")
}

func (f fakeVisibilityJobRepository) GetByIDs(context.Context, []string) ([]domain.Job, error) {
	panic("unexpected call")
}

func (f fakeVisibilityJobRepository) List(context.Context) ([]domain.Job, error) {
	panic("unexpected call")
}

func (f fakeVisibilityJobRepository) ListPaged(context.Context, repository.ListParams) ([]domain.Job, error) {
	panic("unexpected call")
}

func (f fakeVisibilityJobRepository) ListByProjectID(context.Context, string) ([]domain.Job, error) {
	panic("unexpected call")
}

func (f fakeVisibilityJobRepository) ListPushEnabledByRepository(context.Context, string) ([]domain.Job, error) {
	panic("unexpected call")
}

func (f fakeVisibilityJobRepository) GetByID(_ context.Context, id string) (domain.Job, error) {
	job, ok := f.jobs[id]
	if !ok {
		return domain.Job{}, repository.ErrJobNotFound
	}
	return job, nil
}

func (f fakeVisibilityJobRepository) Update(context.Context, domain.Job) (domain.Job, error) {
	panic("unexpected call")
}

func (f fakeVisibilityBuildBoundary) ListBuilds(_ context.Context) ([]domain.Build, error) {
	if f.listBuildsErr != nil {
		return nil, f.listBuildsErr
	}
	return append([]domain.Build(nil), f.builds...), nil
}

func (f fakeVisibilityBuildBoundary) ListActiveBuilds(_ context.Context) ([]domain.Build, error) {
	if f.listActiveErr != nil {
		return nil, f.listActiveErr
	}
	if f.activeBuilds != nil {
		return append([]domain.Build(nil), f.activeBuilds...), nil
	}
	return append([]domain.Build(nil), f.builds...), nil
}

func (f fakeVisibilityBuildBoundary) GetBuildSteps(_ context.Context, id string) ([]domain.BuildStep, error) {
	return append([]domain.BuildStep(nil), f.steps[id]...), nil
}

func (f fakeVisibilityBuildBoundary) GetJobsByBuildID(_ context.Context, buildID string) ([]domain.ExecutionJob, error) {
	return append([]domain.ExecutionJob(nil), f.jobs[buildID]...), nil
}

func TestVisibilityService_ListWorkers_Empty(t *testing.T) {
	svc := NewVisibilityService(memoryrepo.NewWorkerRepository(), fakeVisibilityBuildBoundary{})

	workers, err := svc.ListWorkers(context.Background())
	if err != nil {
		t.Fatalf("ListWorkers returned error: %v", err)
	}
	if len(workers) != 0 {
		t.Fatalf("expected no workers, got %d", len(workers))
	}
}

func TestVisibilityService_ListWorkers_UsesActiveBuildsBoundary(t *testing.T) {
	now := time.Date(2026, time.May, 25, 12, 0, 0, 0, time.UTC)
	svc := NewVisibilityService(memoryrepo.NewWorkerRepository(), fakeVisibilityBuildBoundary{
		activeBuilds: []domain.Build{{ID: "build-1", BuildNumber: 7, ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: now.Add(-time.Minute)}},
		jobs: map[string][]domain.ExecutionJob{
			"build-1": {
				{ID: "job-1", BuildID: "build-1", StepID: "step-1", Name: "compile", StepIndex: 0, Status: domain.ExecutionJobStatusRunning, ClaimedBy: stringPtr("orphan-worker"), ClaimExpiresAt: timePtr(now.Add(30 * time.Second)), CreatedAt: now.Add(-50 * time.Second), StartedAt: timePtr(now.Add(-40 * time.Second)), Image: "alpine", WorkingDir: ".", Command: []string{"sh"}, Environment: map[string]string{}, SpecVersion: 1, ResolvedSpecJSON: `{}`},
			},
		},
		listBuildsErr: context.DeadlineExceeded,
	})
	svc.clock = func() time.Time { return now }

	workers, err := svc.ListWorkers(context.Background())
	if err != nil {
		t.Fatalf("ListWorkers returned error: %v", err)
	}
	if len(workers) != 1 {
		t.Fatalf("expected 1 orphan worker from active builds, got %d", len(workers))
	}
	if workers[0].ID != "orphan-worker" {
		t.Fatalf("expected orphan worker id, got %q", workers[0].ID)
	}
}

func TestVisibilityService_ListWorkers_IgnoresCompletedClaims(t *testing.T) {
	now := time.Date(2026, time.May, 25, 12, 0, 0, 0, time.UTC)
	workerRepo := memoryrepo.NewWorkerRepository()
	_, _ = workerRepo.UpsertHeartbeat(context.Background(), domain.WorkerHeartbeat{ID: "worker-a", Name: "worker-a", HeartbeatAt: now.Add(-5 * time.Second)})

	svc := NewVisibilityService(workerRepo, fakeVisibilityBuildBoundary{
		builds: []domain.Build{{ID: "build-1", BuildNumber: 1, ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: now.Add(-time.Minute)}},
		jobs: map[string][]domain.ExecutionJob{
			"build-1": {
				{ID: "job-1", BuildID: "build-1", StepID: "step-1", Name: "compile", StepIndex: 0, Status: domain.ExecutionJobStatusSuccess, ClaimedBy: stringPtr("worker-a"), ClaimExpiresAt: timePtr(now.Add(30 * time.Second)), CreatedAt: now.Add(-time.Minute)},
			},
		},
		steps: map[string][]domain.BuildStep{
			"build-1": {
				{ID: "step-1", BuildID: "build-1", StepIndex: 0, Name: "compile", Status: domain.BuildStepStatusSuccess, WorkerID: stringPtr("worker-a"), LeaseExpiresAt: timePtr(now.Add(30 * time.Second))},
			},
		},
	})
	svc.clock = func() time.Time { return now }

	workers, err := svc.ListWorkers(context.Background())
	if err != nil {
		t.Fatalf("ListWorkers returned error: %v", err)
	}
	if len(workers) != 1 {
		t.Fatalf("expected 1 worker, got %d", len(workers))
	}
	if workers[0].Status != domain.WorkerStatusIdle {
		t.Fatalf("expected completed claims to be ignored and worker to be idle, got %q", workers[0].Status)
	}
	if workers[0].CurrentBuildID != nil {
		t.Fatalf("expected no current build for completed claims, got %#v", workers[0].CurrentBuildID)
	}
}

func TestVisibilityService_ListWorkers_ActiveRunningClaimWithoutLeaseIsStale(t *testing.T) {
	now := time.Date(2026, time.May, 25, 12, 0, 0, 0, time.UTC)
	workerRepo := memoryrepo.NewWorkerRepository()
	_, _ = workerRepo.UpsertHeartbeat(context.Background(), domain.WorkerHeartbeat{ID: "worker-a", Name: "worker-a", HeartbeatAt: now.Add(-5 * time.Second)})

	svc := NewVisibilityService(workerRepo, fakeVisibilityBuildBoundary{
		builds: []domain.Build{{ID: "build-1", BuildNumber: 1, ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: now.Add(-time.Minute)}},
		steps: map[string][]domain.BuildStep{
			"build-1": {
				{ID: "step-1", BuildID: "build-1", StepIndex: 0, Name: "compile", Status: domain.BuildStepStatusRunning, WorkerID: stringPtr("worker-a"), ClaimedAt: timePtr(now.Add(-20 * time.Second))},
			},
		},
	})
	svc.clock = func() time.Time { return now }

	workers, err := svc.ListWorkers(context.Background())
	if err != nil {
		t.Fatalf("ListWorkers returned error: %v", err)
	}
	if workers[0].Status != domain.WorkerStatusStale || !workers[0].StaleLease {
		t.Fatalf("expected running claim without lease to be stale, got %#v", workers[0])
	}
}

func TestVisibilityService_ListWorkers_ActiveRunningStepClaimWithFutureLeaseIsBusy(t *testing.T) {
	now := time.Date(2026, time.May, 25, 12, 0, 0, 0, time.UTC)
	workerRepo := memoryrepo.NewWorkerRepository()
	_, _ = workerRepo.UpsertHeartbeat(context.Background(), domain.WorkerHeartbeat{ID: "worker-a", Name: "worker-a", HeartbeatAt: now.Add(-5 * time.Second)})

	svc := NewVisibilityService(workerRepo, fakeVisibilityBuildBoundary{
		builds: []domain.Build{{ID: "build-1", BuildNumber: 11, ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: now.Add(-time.Minute)}},
		steps: map[string][]domain.BuildStep{
			"build-1": {
				{ID: "step-1", BuildID: "build-1", StepIndex: 0, Name: "compile", Status: domain.BuildStepStatusRunning, WorkerID: stringPtr("worker-a"), ClaimedAt: timePtr(now.Add(-20 * time.Second)), LeaseExpiresAt: timePtr(now.Add(30 * time.Second))},
			},
		},
	})
	svc.clock = func() time.Time { return now }

	workers, err := svc.ListWorkers(context.Background())
	if err != nil {
		t.Fatalf("ListWorkers returned error: %v", err)
	}
	if len(workers) != 1 {
		t.Fatalf("expected 1 worker, got %d", len(workers))
	}
	if workers[0].Status != domain.WorkerStatusBusy {
		t.Fatalf("expected running step claim with future lease to be busy, got %#v", workers[0])
	}
	if workers[0].CurrentBuildID == nil || *workers[0].CurrentBuildID != "build-1" {
		t.Fatalf("expected current build to be build-1, got %#v", workers[0].CurrentBuildID)
	}
	if workers[0].CurrentStepName == nil || *workers[0].CurrentStepName != "compile" {
		t.Fatalf("expected current step name compile, got %#v", workers[0].CurrentStepName)
	}
	if workers[0].LeaseExpiresAt == nil || !workers[0].LeaseExpiresAt.After(now) {
		t.Fatalf("expected future lease expiry, got %#v", workers[0].LeaseExpiresAt)
	}
}

func TestVisibilityService_ListWorkers_OrphanClaimStillVisible(t *testing.T) {
	now := time.Date(2026, time.May, 25, 12, 0, 0, 0, time.UTC)
	svc := NewVisibilityService(memoryrepo.NewWorkerRepository(), fakeVisibilityBuildBoundary{
		builds: []domain.Build{{ID: "build-1", BuildNumber: 7, ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: now.Add(-time.Minute)}},
		jobs: map[string][]domain.ExecutionJob{
			"build-1": {
				{ID: "job-1", BuildID: "build-1", StepID: "step-1", Name: "compile", StepIndex: 0, Status: domain.ExecutionJobStatusRunning, ClaimedBy: stringPtr("orphan-worker"), ClaimExpiresAt: timePtr(now.Add(30 * time.Second)), CreatedAt: now.Add(-50 * time.Second), StartedAt: timePtr(now.Add(-40 * time.Second)), Image: "alpine", WorkingDir: ".", Command: []string{"sh"}, Environment: map[string]string{}, SpecVersion: 1, ResolvedSpecJSON: `{}`},
			},
		},
	})
	svc.clock = func() time.Time { return now }

	workers, err := svc.ListWorkers(context.Background())
	if err != nil {
		t.Fatalf("ListWorkers returned error: %v", err)
	}
	if len(workers) != 1 {
		t.Fatalf("expected 1 orphan worker, got %d", len(workers))
	}
	if workers[0].ID != "orphan-worker" {
		t.Fatalf("expected orphan worker id, got %q", workers[0].ID)
	}
	if workers[0].Status != domain.WorkerStatusStale || !workers[0].StaleHeartbeat {
		t.Fatalf("expected orphan worker to be surfaced as stale without heartbeat, got %#v", workers[0])
	}
	if !workers[0].LastHeartbeatAt.IsZero() {
		t.Fatalf("expected orphan worker to have no heartbeat timestamp, got %s", workers[0].LastHeartbeatAt)
	}
}

func TestVisibilityService_ListWorkers_StatusSemantics(t *testing.T) {
	now := time.Date(2026, time.May, 24, 12, 0, 0, 0, time.UTC)
	workerRepo := memoryrepo.NewWorkerRepository()
	_, _ = workerRepo.UpsertHeartbeat(context.Background(), domain.WorkerHeartbeat{ID: "idle-worker", Name: "idle-worker", HeartbeatAt: now.Add(-10 * time.Second)})
	_, _ = workerRepo.UpsertHeartbeat(context.Background(), domain.WorkerHeartbeat{ID: "busy-worker", Name: "busy-worker", HeartbeatAt: now.Add(-5 * time.Second)})
	_, _ = workerRepo.UpsertHeartbeat(context.Background(), domain.WorkerHeartbeat{ID: "stale-worker", Name: "stale-worker", HeartbeatAt: now.Add(-3 * time.Minute)})
	_, _ = workerRepo.UpsertHeartbeat(context.Background(), domain.WorkerHeartbeat{ID: "expired-worker", Name: "expired-worker", HeartbeatAt: now.Add(-10 * time.Second)})

	busyBuildID := "build-busy"
	expiredBuildID := "build-expired"
	boundary := fakeVisibilityBuildBoundary{
		builds: []domain.Build{
			{ID: busyBuildID, BuildNumber: 17, ProjectID: "project-a", JobID: stringPtr("job-a"), Status: domain.BuildStatusRunning, CreatedAt: now.Add(-3 * time.Minute)},
			{ID: expiredBuildID, BuildNumber: 23, ProjectID: "project-b", JobID: stringPtr("job-b"), Status: domain.BuildStatusRunning, CreatedAt: now.Add(-2 * time.Minute)},
		},
		jobs: map[string][]domain.ExecutionJob{
			busyBuildID: {
				{
					ID:               "exec-job-1",
					BuildID:          busyBuildID,
					StepID:           "step-job-1",
					Name:             "compile",
					StepIndex:        0,
					AttemptNumber:    1,
					Status:           domain.ExecutionJobStatusRunning,
					Image:            "alpine",
					WorkingDir:       ".",
					Command:          []string{"sh", "-lc", "make"},
					Environment:      map[string]string{},
					SpecVersion:      1,
					ResolvedSpecJSON: `{}`,
					CreatedAt:        now.Add(-25 * time.Second),
					StartedAt:        timePtr(now.Add(-20 * time.Second)),
					ClaimedBy:        stringPtr("busy-worker"),
					ClaimToken:       stringPtr("claim-a"),
					ClaimExpiresAt:   timePtr(now.Add(30 * time.Second)),
				},
			},
		},
		steps: map[string][]domain.BuildStep{
			expiredBuildID: {
				{
					ID:             "step-expired-1",
					BuildID:        expiredBuildID,
					StepIndex:      0,
					Name:           "test",
					Status:         domain.BuildStepStatusRunning,
					WorkerID:       stringPtr("expired-worker"),
					ClaimToken:     stringPtr("claim-b"),
					ClaimedAt:      timePtr(now.Add(-2 * time.Minute)),
					LeaseExpiresAt: timePtr(now.Add(-30 * time.Second)),
				},
			},
		},
	}

	svc := NewVisibilityService(workerRepo, boundary)
	svc.clock = func() time.Time { return now }
	svc.SetStaleAfter(90 * time.Second)

	workers, err := svc.ListWorkers(context.Background())
	if err != nil {
		t.Fatalf("ListWorkers returned error: %v", err)
	}
	if len(workers) != 4 {
		t.Fatalf("expected 4 workers, got %d", len(workers))
	}

	byID := make(map[string]domain.WorkerVisibility, len(workers))
	for _, worker := range workers {
		byID[worker.ID] = worker
	}

	if byID["idle-worker"].Status != domain.WorkerStatusIdle {
		t.Fatalf("expected idle-worker to be idle, got %q", byID["idle-worker"].Status)
	}
	if byID["busy-worker"].Status != domain.WorkerStatusBusy {
		t.Fatalf("expected busy-worker to be busy, got %q", byID["busy-worker"].Status)
	}
	if byID["busy-worker"].CurrentBuildID == nil || *byID["busy-worker"].CurrentBuildID != busyBuildID {
		t.Fatalf("expected busy-worker build %q, got %#v", busyBuildID, byID["busy-worker"].CurrentBuildID)
	}
	if byID["busy-worker"].CurrentStepName == nil || *byID["busy-worker"].CurrentStepName != "compile" {
		t.Fatalf("expected busy-worker step compile, got %#v", byID["busy-worker"].CurrentStepName)
	}
	if byID["stale-worker"].Status != domain.WorkerStatusStale || !byID["stale-worker"].StaleHeartbeat {
		t.Fatalf("expected stale-worker to be stale from heartbeat, got %#v", byID["stale-worker"])
	}
	if byID["expired-worker"].Status != domain.WorkerStatusStale || !byID["expired-worker"].StaleLease {
		t.Fatalf("expected expired-worker to be stale from lease, got %#v", byID["expired-worker"])
	}
}

func TestVisibilityService_ListWorkers_EnrichesProjectAndJobNamesWhenAvailable(t *testing.T) {
	now := time.Date(2026, time.May, 25, 12, 0, 0, 0, time.UTC)
	workerRepo := memoryrepo.NewWorkerRepository()
	_, _ = workerRepo.UpsertHeartbeat(context.Background(), domain.WorkerHeartbeat{ID: "busy-worker", Name: "busy-worker", HeartbeatAt: now.Add(-5 * time.Second)})

	svc := NewVisibilityService(workerRepo, fakeVisibilityBuildBoundary{
		builds: []domain.Build{{ID: "build-1", BuildNumber: 17, ProjectID: "project-a", JobID: stringPtr("job-a"), Status: domain.BuildStatusRunning, CreatedAt: now.Add(-time.Minute)}},
		jobs: map[string][]domain.ExecutionJob{
			"build-1": {
				{ID: "exec-job-1", BuildID: "build-1", StepID: "step-job-1", Name: "compile", StepIndex: 0, Status: domain.ExecutionJobStatusRunning, ClaimedBy: stringPtr("busy-worker"), ClaimExpiresAt: timePtr(now.Add(30 * time.Second)), CreatedAt: now.Add(-40 * time.Second), StartedAt: timePtr(now.Add(-30 * time.Second)), Image: "alpine", WorkingDir: ".", Command: []string{"sh"}, Environment: map[string]string{}, SpecVersion: 1, ResolvedSpecJSON: `{}`},
			},
		},
	})
	svc.clock = func() time.Time { return now }
	svc.SetProjectRepository(fakeVisibilityProjectRepository{projects: map[string]domain.Project{"project-a": {ID: "project-a", Name: "Platform", Slug: "platform"}}})
	svc.SetJobRepository(fakeVisibilityJobRepository{jobs: map[string]domain.Job{"job-a": {ID: "job-a", ProjectID: "project-a", Name: "release"}}})

	workers, err := svc.ListWorkers(context.Background())
	if err != nil {
		t.Fatalf("ListWorkers returned error: %v", err)
	}
	if workers[0].ProjectName == nil || *workers[0].ProjectName != "Platform" {
		t.Fatalf("expected project name enrichment, got %#v", workers[0].ProjectName)
	}
	if workers[0].ProjectSlug == nil || *workers[0].ProjectSlug != "platform" {
		t.Fatalf("expected project slug enrichment, got %#v", workers[0].ProjectSlug)
	}
	if workers[0].JobName == nil || *workers[0].JobName != "release" {
		t.Fatalf("expected job name enrichment, got %#v", workers[0].JobName)
	}
}

func timePtr(value time.Time) *time.Time {
	return &value
}

func stringPtr(value string) *string {
	return &value
}
