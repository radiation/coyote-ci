package memory

import (
	"context"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func TestExecutionJobRepository_ClaimRenewAndComplete(t *testing.T) {
	repo := NewExecutionJobRepository()
	now := time.Now().UTC()
	timeout := 30
	jobs, err := repo.CreateJobsForBuild(context.Background(), []domain.ExecutionJob{{
		ID:             "job-1",
		BuildID:        "build-1",
		StepID:         "step-1",
		Name:           "test",
		StepIndex:      0,
		Status:         domain.ExecutionJobStatusQueued,
		Image:          "golang:1.24",
		WorkingDir:     "backend",
		Command:        []string{"sh", "-c", "go test ./..."},
		Environment:    map[string]string{"GOFLAGS": "-mod=readonly"},
		TimeoutSeconds: &timeout,
		Source: domain.SourceSnapshotRef{
			RepositoryURL: "https://github.com/acme/repo.git",
			CommitSHA:     "abc123",
		},
		SpecVersion:      1,
		ResolvedSpecJSON: "{}",
		CreatedAt:        now,
	}})
	if err != nil {
		t.Fatalf("create jobs failed: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected one job, got %d", len(jobs))
	}

	claim := repository.StepClaim{WorkerID: "worker-1", ClaimToken: "claim-1", ClaimedAt: now, LeaseExpiresAt: now.Add(45 * time.Second)}
	claimed, ok, err := repo.ClaimJobByStepID(context.Background(), "step-1", claim)
	if err != nil {
		t.Fatalf("claim by step failed: %v", err)
	}
	if !ok {
		t.Fatal("expected claim to succeed")
	}
	if claimed.Status != domain.ExecutionJobStatusRunning {
		t.Fatalf("expected running status, got %q", claimed.Status)
	}

	renewed, outcome, err := repo.RenewJobLease(context.Background(), "job-1", "claim-1", now.Add(90*time.Second))
	if err != nil {
		t.Fatalf("renew lease failed: %v", err)
	}
	if outcome != repository.StepCompletionCompleted {
		t.Fatalf("expected completed renew outcome, got %q", outcome)
	}
	if renewed.ClaimExpiresAt == nil {
		t.Fatal("expected claim expiry to be set")
	}

	completed, completeOutcome, err := repo.CompleteJobSuccess(context.Background(), "job-1", "claim-1", now.Add(2*time.Minute), 0, []domain.ArtifactRef{{Name: "dist/app", URI: "s3://bucket/build-1/dist/app"}})
	if err != nil {
		t.Fatalf("complete success failed: %v", err)
	}
	if completeOutcome != repository.StepCompletionCompleted {
		t.Fatalf("expected completed outcome, got %q", completeOutcome)
	}
	if completed.Status != domain.ExecutionJobStatusSuccess {
		t.Fatalf("expected success status, got %q", completed.Status)
	}
	if len(completed.OutputRefs) != 1 {
		t.Fatalf("expected one output ref, got %d", len(completed.OutputRefs))
	}
}

func TestExecutionJobRepository_ImmutabilityForSpecFields(t *testing.T) {
	repo := NewExecutionJobRepository()
	now := time.Now().UTC()
	timeout := 10
	_, err := repo.CreateJobsForBuild(context.Background(), []domain.ExecutionJob{{
		ID:               "job-immut",
		BuildID:          "build-immut",
		StepID:           "step-immut",
		Name:             "immut",
		StepIndex:        0,
		Status:           domain.ExecutionJobStatusQueued,
		Image:            "alpine:3.20",
		WorkingDir:       ".",
		Command:          []string{"sh", "-c", "echo hi"},
		Environment:      map[string]string{"A": "1"},
		TimeoutSeconds:   &timeout,
		SpecVersion:      1,
		ResolvedSpecJSON: `{"command":["sh","-c","echo hi"]}`,
		CreatedAt:        now,
		Source: domain.SourceSnapshotRef{
			RepositoryURL: "https://github.com/acme/repo.git",
			CommitSHA:     "abc123",
		},
	}})
	if err != nil {
		t.Fatalf("create jobs failed: %v", err)
	}

	claim := repository.StepClaim{WorkerID: "w", ClaimToken: "c", ClaimedAt: now, LeaseExpiresAt: now.Add(time.Minute)}
	if _, _, claimErr := repo.ClaimJobByStepID(context.Background(), "step-immut", claim); claimErr != nil {
		t.Fatalf("claim failed: %v", claimErr)
	}
	if _, _, completeErr := repo.CompleteJobFailure(context.Background(), "job-immut", "c", now.Add(time.Minute), "boom", nil, nil); completeErr != nil {
		t.Fatalf("complete failure failed: %v", completeErr)
	}

	job, err := repo.GetJobByID(context.Background(), "job-immut")
	if err != nil {
		t.Fatalf("get by id failed: %v", err)
	}
	if job.Image != "alpine:3.20" {
		t.Fatalf("expected immutable image, got %q", job.Image)
	}
	if len(job.Command) != 3 || job.Command[2] != "echo hi" {
		t.Fatalf("expected immutable command, got %#v", job.Command)
	}
	if job.Source.CommitSHA != "abc123" {
		t.Fatalf("expected immutable source commit, got %q", job.Source.CommitSHA)
	}
}

// TestIsJobRunnable covers graph readiness evaluation including the root-node case.
func TestIsJobRunnable(t *testing.T) {
	successJob := domain.ExecutionJob{NodeID: "node-000", Status: domain.ExecutionJobStatusSuccess}
	runningJob := domain.ExecutionJob{NodeID: "node-001", Status: domain.ExecutionJobStatusRunning}

	latestByNode := map[string]domain.ExecutionJob{
		"node-000": successJob,
		"node-001": runningJob,
	}

	tests := []struct {
		name         string
		job          domain.ExecutionJob
		latestByNode map[string]domain.ExecutionJob
		wantRunnable bool
	}{
		{
			name:         "root node with no dependencies is immediately runnable",
			job:          domain.ExecutionJob{NodeID: "node-002", DependsOnNodeIDs: []string{}},
			latestByNode: latestByNode,
			wantRunnable: true,
		},
		{
			name:         "nil dependencies treated as root node",
			job:          domain.ExecutionJob{NodeID: "node-002", DependsOnNodeIDs: nil},
			latestByNode: latestByNode,
			wantRunnable: true,
		},
		{
			name:         "single successful dependency is runnable",
			job:          domain.ExecutionJob{NodeID: "node-003", DependsOnNodeIDs: []string{"node-000"}},
			latestByNode: latestByNode,
			wantRunnable: true,
		},
		{
			name:         "dependency still running blocks job",
			job:          domain.ExecutionJob{NodeID: "node-003", DependsOnNodeIDs: []string{"node-001"}},
			latestByNode: latestByNode,
			wantRunnable: false,
		},
		{
			name:         "unknown dependency blocks job",
			job:          domain.ExecutionJob{NodeID: "node-003", DependsOnNodeIDs: []string{"node-999"}},
			latestByNode: latestByNode,
			wantRunnable: false,
		},
		{
			name:         "multiple deps: all success is runnable",
			job:          domain.ExecutionJob{NodeID: "node-004", DependsOnNodeIDs: []string{"node-000"}},
			latestByNode: latestByNode,
			wantRunnable: true,
		},
		{
			name:         "multiple deps: one still running blocks",
			job:          domain.ExecutionJob{NodeID: "node-004", DependsOnNodeIDs: []string{"node-000", "node-001"}},
			latestByNode: latestByNode,
			wantRunnable: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isJobRunnable(tc.job, tc.latestByNode)
			if got != tc.wantRunnable {
				t.Fatalf("isJobRunnable = %v, want %v", got, tc.wantRunnable)
			}
		})
	}
}

// TestClaimNextRunnableJob_RootNodeIsRunnable verifies that a job with no
// dependencies can be claimed from a running build without any prior completions.
func TestClaimNextRunnableJob_RootNodeIsRunnable(t *testing.T) {
	repo := NewExecutionJobRepository()
	now := time.Now().UTC()

	_, err := repo.CreateJobsForBuild(context.Background(), []domain.ExecutionJob{{
		ID:               "job-root",
		BuildID:          "build-1",
		StepID:           "step-1",
		Name:             "root",
		NodeID:           "node-000",
		DependsOnNodeIDs: []string{},
		StepIndex:        0,
		Status:           domain.ExecutionJobStatusQueued,
		ResolvedSpecJSON: "{}",
		CreatedAt:        now,
	}})
	if err != nil {
		t.Fatalf("create jobs: %v", err)
	}

	claim := repository.StepClaim{WorkerID: "w1", ClaimToken: "tok1", ClaimedAt: now, LeaseExpiresAt: now.Add(30 * time.Second)}
	job, ok, err := repo.ClaimNextRunnableJob(context.Background(), claim)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if !ok {
		t.Fatal("expected root job to be claimable")
	}
	if job.NodeID != "node-000" {
		t.Fatalf("expected node-000, got %q", job.NodeID)
	}
}

// TestClaimNextRunnableJob_ParallelFanOut verifies that after a shared upstream
// dependency completes, all parallel fan-out jobs become claimable independently.
func TestClaimNextRunnableJob_ParallelFanOut(t *testing.T) {
	repo := NewExecutionJobRepository()
	now := time.Now().UTC()

	// node-000 is the gate step; node-001 and node-002 are parallel and both depend on it.
	_, err := repo.CreateJobsForBuild(context.Background(), []domain.ExecutionJob{
		{
			ID:               "job-gate",
			BuildID:          "build-2",
			StepID:           "step-gate",
			Name:             "gate",
			NodeID:           "node-000",
			DependsOnNodeIDs: []string{},
			StepIndex:        0,
			Status:           domain.ExecutionJobStatusQueued,
			ResolvedSpecJSON: "{}",
			CreatedAt:        now,
		},
		{
			ID:               "job-parallel-a",
			BuildID:          "build-2",
			StepID:           "step-a",
			Name:             "parallel-a",
			NodeID:           "node-001",
			DependsOnNodeIDs: []string{"node-000"},
			StepIndex:        1,
			Status:           domain.ExecutionJobStatusQueued,
			ResolvedSpecJSON: "{}",
			CreatedAt:        now,
		},
		{
			ID:               "job-parallel-b",
			BuildID:          "build-2",
			StepID:           "step-b",
			Name:             "parallel-b",
			NodeID:           "node-002",
			DependsOnNodeIDs: []string{"node-000"},
			StepIndex:        2,
			Status:           domain.ExecutionJobStatusQueued,
			ResolvedSpecJSON: "{}",
			CreatedAt:        now,
		},
	})
	if err != nil {
		t.Fatalf("create jobs: %v", err)
	}

	// Only the gate step is claimable initially.
	claim1 := repository.StepClaim{WorkerID: "w1", ClaimToken: "tok-gate", ClaimedAt: now, LeaseExpiresAt: now.Add(30 * time.Second)}
	gateJob, ok, err := repo.ClaimNextRunnableJob(context.Background(), claim1)
	if err != nil {
		t.Fatalf("claim gate: %v", err)
	}
	if !ok {
		t.Fatal("expected gate job to be claimable")
	}
	if gateJob.NodeID != "node-000" {
		t.Fatalf("expected node-000, got %q", gateJob.NodeID)
	}

	// Neither parallel job is claimable while gate is still running.
	claim2 := repository.StepClaim{WorkerID: "w2", ClaimToken: "tok-a", ClaimedAt: now, LeaseExpiresAt: now.Add(30 * time.Second)}
	_, blockedOK, err := repo.ClaimNextRunnableJob(context.Background(), claim2)
	if err != nil {
		t.Fatalf("claim while blocked: %v", err)
	}
	if blockedOK {
		t.Fatal("expected parallel jobs to be blocked while gate is running")
	}

	// Complete the gate job.
	_, outcome, err := repo.CompleteJobSuccess(context.Background(), "job-gate", "tok-gate", now.Add(time.Minute), 0, nil)
	if err != nil {
		t.Fatalf("complete gate: %v", err)
	}
	if outcome != repository.StepCompletionCompleted {
		t.Fatalf("expected completed outcome, got %q", outcome)
	}

	// After gate success, both parallel jobs should be independently claimable.
	claimA := repository.StepClaim{WorkerID: "w2", ClaimToken: "tok-a2", ClaimedAt: now, LeaseExpiresAt: now.Add(30 * time.Second)}
	jobA, okA, err := repo.ClaimNextRunnableJob(context.Background(), claimA)
	if err != nil {
		t.Fatalf("claim parallel-a: %v", err)
	}
	if !okA {
		t.Fatal("expected parallel-a to be claimable after gate success")
	}

	claimB := repository.StepClaim{WorkerID: "w3", ClaimToken: "tok-b2", ClaimedAt: now, LeaseExpiresAt: now.Add(30 * time.Second)}
	jobB, okB, err := repo.ClaimNextRunnableJob(context.Background(), claimB)
	if err != nil {
		t.Fatalf("claim parallel-b: %v", err)
	}
	if !okB {
		t.Fatal("expected parallel-b to be claimable after gate success")
	}

	// Both fan-out jobs were claimed concurrently and their NodeIDs are distinct.
	if jobA.NodeID == jobB.NodeID {
		t.Fatalf("expected distinct node IDs for parallel jobs, both got %q", jobA.NodeID)
	}
	parallelIDs := map[string]bool{jobA.NodeID: true, jobB.NodeID: true}
	if !parallelIDs["node-001"] || !parallelIDs["node-002"] {
		t.Fatalf("expected node-001 and node-002 claimed, got %q and %q", jobA.NodeID, jobB.NodeID)
	}
}
