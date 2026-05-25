package worker

import (
	"context"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	memoryrepo "github.com/radiation/coyote-ci/backend/internal/repository/memory"
)

type fakeVisibilityBuildBoundary struct {
	builds []domain.Build
	steps  map[string][]domain.BuildStep
	jobs   map[string][]domain.ExecutionJob
}

func (f fakeVisibilityBuildBoundary) ListBuilds(_ context.Context) ([]domain.Build, error) {
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

func timePtr(value time.Time) *time.Time {
	return &value
}

func stringPtr(value string) *string {
	return &value
}
