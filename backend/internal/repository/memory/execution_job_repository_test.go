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

func TestExecutionJobRepository_ClaimNextRunnableJob_RespectsDependencies(t *testing.T) {
	repo := NewExecutionJobRepository()
	now := time.Now().UTC()
	_, err := repo.CreateJobsForBuild(context.Background(), []domain.ExecutionJob{
		{
			ID:               "job-setup",
			BuildID:          "build-graph",
			StepID:           "step-setup",
			NodeID:           "node-setup",
			Name:             "setup",
			StepIndex:        0,
			Status:           domain.ExecutionJobStatusQueued,
			Image:            "alpine:3.20",
			WorkingDir:       ".",
			Command:          []string{"sh", "-c", "echo setup"},
			Environment:      map[string]string{},
			SpecVersion:      1,
			ResolvedSpecJSON: "{}",
			CreatedAt:        now,
		},
		{
			ID:               "job-unit",
			BuildID:          "build-graph",
			StepID:           "step-unit",
			NodeID:           "node-unit",
			Name:             "unit",
			StepIndex:        1,
			DependsOnNodeIDs: []string{"node-setup"},
			Status:           domain.ExecutionJobStatusQueued,
			Image:            "alpine:3.20",
			WorkingDir:       ".",
			Command:          []string{"sh", "-c", "echo unit"},
			Environment:      map[string]string{},
			SpecVersion:      1,
			ResolvedSpecJSON: "{}",
			CreatedAt:        now.Add(time.Second),
		},
		{
			ID:               "job-integration",
			BuildID:          "build-graph",
			StepID:           "step-integration",
			NodeID:           "node-integration",
			Name:             "integration",
			StepIndex:        2,
			DependsOnNodeIDs: []string{"node-setup"},
			Status:           domain.ExecutionJobStatusQueued,
			Image:            "alpine:3.20",
			WorkingDir:       ".",
			Command:          []string{"sh", "-c", "echo integration"},
			Environment:      map[string]string{},
			SpecVersion:      1,
			ResolvedSpecJSON: "{}",
			CreatedAt:        now.Add(2 * time.Second),
		},
	})
	if err != nil {
		t.Fatalf("create jobs failed: %v", err)
	}

	claimSetup := repository.StepClaim{WorkerID: "worker-a", ClaimToken: "claim-a", ClaimedAt: now.Add(3 * time.Second), LeaseExpiresAt: now.Add(30 * time.Second)}
	setup, found, err := repo.ClaimNextRunnableJob(context.Background(), claimSetup)
	if err != nil {
		t.Fatalf("claim setup failed: %v", err)
	}
	if !found || setup.Name != "setup" {
		t.Fatalf("expected setup to be first runnable, got found=%v job=%+v", found, setup)
	}

	_, outcome, completeErr := repo.CompleteJobSuccess(context.Background(), setup.ID, claimSetup.ClaimToken, now.Add(4*time.Second), 0, nil)
	if completeErr != nil {
		t.Fatalf("complete setup failed: %v", completeErr)
	} else if outcome != repository.StepCompletionCompleted {
		t.Fatalf("unexpected setup completion outcome %q", outcome)
	}

	claimOne := repository.StepClaim{WorkerID: "worker-b", ClaimToken: "claim-b", ClaimedAt: now.Add(5 * time.Second), LeaseExpiresAt: now.Add(35 * time.Second)}
	firstBranch, found, err := repo.ClaimNextRunnableJob(context.Background(), claimOne)
	if err != nil {
		t.Fatalf("claim first branch failed: %v", err)
	}
	if !found {
		t.Fatal("expected first branch runnable")
	}

	claimTwo := repository.StepClaim{WorkerID: "worker-c", ClaimToken: "claim-c", ClaimedAt: now.Add(6 * time.Second), LeaseExpiresAt: now.Add(36 * time.Second)}
	secondBranch, found, err := repo.ClaimNextRunnableJob(context.Background(), claimTwo)
	if err != nil {
		t.Fatalf("claim second branch failed: %v", err)
	}
	if !found {
		t.Fatal("expected second branch runnable")
	}
	if firstBranch.ID == secondBranch.ID {
		t.Fatalf("expected two distinct runnable jobs, got %s", firstBranch.ID)
	}
}

func TestExecutionJobRepository_ClaimNextRunnableJob_JoinBlocksUntilAllDepsSucceed(t *testing.T) {
	repo := NewExecutionJobRepository()
	now := time.Now().UTC()

	_, err := repo.CreateJobsForBuild(context.Background(), []domain.ExecutionJob{
		{
			ID:               "job-setup",
			BuildID:          "build-join",
			StepID:           "step-setup",
			NodeID:           "node-setup",
			Name:             "setup",
			StepIndex:        0,
			Status:           domain.ExecutionJobStatusQueued,
			Image:            "alpine:3.20",
			WorkingDir:       ".",
			Command:          []string{"sh", "-c", "echo setup"},
			Environment:      map[string]string{},
			SpecVersion:      1,
			ResolvedSpecJSON: "{}",
			CreatedAt:        now,
		},
		{
			ID:               "job-a",
			BuildID:          "build-join",
			StepID:           "step-a",
			NodeID:           "node-a",
			Name:             "branch-a",
			StepIndex:        1,
			DependsOnNodeIDs: []string{"node-setup"},
			Status:           domain.ExecutionJobStatusQueued,
			Image:            "alpine:3.20",
			WorkingDir:       ".",
			Command:          []string{"sh", "-c", "echo a"},
			Environment:      map[string]string{},
			SpecVersion:      1,
			ResolvedSpecJSON: "{}",
			CreatedAt:        now.Add(time.Second),
		},
		{
			ID:               "job-b",
			BuildID:          "build-join",
			StepID:           "step-b",
			NodeID:           "node-b",
			Name:             "branch-b",
			StepIndex:        2,
			DependsOnNodeIDs: []string{"node-setup"},
			Status:           domain.ExecutionJobStatusQueued,
			Image:            "alpine:3.20",
			WorkingDir:       ".",
			Command:          []string{"sh", "-c", "echo b"},
			Environment:      map[string]string{},
			SpecVersion:      1,
			ResolvedSpecJSON: "{}",
			CreatedAt:        now.Add(2 * time.Second),
		},
		{
			ID:               "job-join",
			BuildID:          "build-join",
			StepID:           "step-join",
			NodeID:           "node-join",
			Name:             "join",
			StepIndex:        3,
			DependsOnNodeIDs: []string{"node-a", "node-b"},
			Status:           domain.ExecutionJobStatusQueued,
			Image:            "alpine:3.20",
			WorkingDir:       ".",
			Command:          []string{"sh", "-c", "echo join"},
			Environment:      map[string]string{},
			SpecVersion:      1,
			ResolvedSpecJSON: "{}",
			CreatedAt:        now.Add(3 * time.Second),
		},
	})
	if err != nil {
		t.Fatalf("create jobs failed: %v", err)
	}

	claimSetup := repository.StepClaim{WorkerID: "worker-setup", ClaimToken: "claim-setup", ClaimedAt: now.Add(4 * time.Second), LeaseExpiresAt: now.Add(64 * time.Second)}
	setup, found, err := repo.ClaimNextRunnableJob(context.Background(), claimSetup)
	if err != nil || !found || setup.ID != "job-setup" {
		t.Fatalf("expected setup to claim first, found=%v id=%q err=%v", found, setup.ID, err)
	}
	if _, outcome, completeErr := repo.CompleteJobSuccess(context.Background(), setup.ID, claimSetup.ClaimToken, now.Add(5*time.Second), 0, nil); completeErr != nil || outcome != repository.StepCompletionCompleted {
		t.Fatalf("complete setup failed outcome=%q err=%v", outcome, completeErr)
	}

	claimA := repository.StepClaim{WorkerID: "worker-a", ClaimToken: "claim-a", ClaimedAt: now.Add(6 * time.Second), LeaseExpiresAt: now.Add(66 * time.Second)}
	branchA, found, err := repo.ClaimNextRunnableJob(context.Background(), claimA)
	if err != nil || !found {
		t.Fatalf("expected first branch claim, found=%v err=%v", found, err)
	}
	if branchA.ID == "job-join" {
		t.Fatal("join should not be runnable before branch dependencies succeed")
	}

	claimB := repository.StepClaim{WorkerID: "worker-b", ClaimToken: "claim-b", ClaimedAt: now.Add(7 * time.Second), LeaseExpiresAt: now.Add(67 * time.Second)}
	branchB, found, err := repo.ClaimNextRunnableJob(context.Background(), claimB)
	if err != nil || !found {
		t.Fatalf("expected second branch claim, found=%v err=%v", found, err)
	}
	if branchB.ID == "job-join" {
		t.Fatal("join should not be runnable before branch dependencies succeed")
	}

	if _, outcome, completeErr := repo.CompleteJobSuccess(context.Background(), branchA.ID, claimA.ClaimToken, now.Add(8*time.Second), 0, nil); completeErr != nil || outcome != repository.StepCompletionCompleted {
		t.Fatalf("complete first branch failed outcome=%q err=%v", outcome, completeErr)
	}

	claimJoinEarly := repository.StepClaim{WorkerID: "worker-join-early", ClaimToken: "claim-join-early", ClaimedAt: now.Add(9 * time.Second), LeaseExpiresAt: now.Add(69 * time.Second)}
	next, found, err := repo.ClaimNextRunnableJob(context.Background(), claimJoinEarly)
	if err != nil {
		t.Fatalf("unexpected error while checking join readiness: %v", err)
	}
	if found {
		t.Fatalf("expected no runnable job before all join deps succeed, got id=%q", next.ID)
	}

	if _, outcome, completeErr := repo.CompleteJobSuccess(context.Background(), branchB.ID, claimB.ClaimToken, now.Add(10*time.Second), 0, nil); completeErr != nil || outcome != repository.StepCompletionCompleted {
		t.Fatalf("complete second branch failed outcome=%q err=%v", outcome, completeErr)
	}

	claimJoin := repository.StepClaim{WorkerID: "worker-join", ClaimToken: "claim-join", ClaimedAt: now.Add(11 * time.Second), LeaseExpiresAt: now.Add(71 * time.Second)}
	join, found, err := repo.ClaimNextRunnableJob(context.Background(), claimJoin)
	if err != nil || !found {
		t.Fatalf("expected join claim after dependencies, found=%v err=%v", found, err)
	}
	if join.ID != "job-join" {
		t.Fatalf("expected join job, got %q", join.ID)
	}
}

func TestExecutionJobRepository_ClaimNextRunnableJob_NodeIDFallbackConsistency(t *testing.T) {
	repo := NewExecutionJobRepository()
	now := time.Now().UTC()

	_, err := repo.CreateJobsForBuild(context.Background(), []domain.ExecutionJob{
		{
			ID:               "job-root",
			BuildID:          "build-fallback",
			StepID:           "step-root",
			Name:             "root",
			StepIndex:        0,
			Status:           domain.ExecutionJobStatusQueued,
			Image:            "alpine:3.20",
			WorkingDir:       ".",
			Command:          []string{"sh", "-c", "echo root"},
			Environment:      map[string]string{},
			SpecVersion:      1,
			ResolvedSpecJSON: "{}",
			CreatedAt:        now,
		},
		{
			ID:               "job-child",
			BuildID:          "build-fallback",
			StepID:           "step-child",
			NodeID:           "node-child",
			Name:             "child",
			StepIndex:        1,
			DependsOnNodeIDs: []string{domain.FallbackNodeID(0)},
			Status:           domain.ExecutionJobStatusQueued,
			Image:            "alpine:3.20",
			WorkingDir:       ".",
			Command:          []string{"sh", "-c", "echo child"},
			Environment:      map[string]string{},
			SpecVersion:      1,
			ResolvedSpecJSON: "{}",
			CreatedAt:        now.Add(time.Second),
		},
	})
	if err != nil {
		t.Fatalf("create jobs failed: %v", err)
	}

	claimRoot := repository.StepClaim{WorkerID: "worker-root", ClaimToken: "claim-root", ClaimedAt: now.Add(2 * time.Second), LeaseExpiresAt: now.Add(62 * time.Second)}
	root, found, err := repo.ClaimNextRunnableJob(context.Background(), claimRoot)
	if err != nil || !found {
		t.Fatalf("expected root to claim, found=%v err=%v", found, err)
	}
	if root.ID != "job-root" {
		t.Fatalf("expected root job, got %q", root.ID)
	}

	if _, outcome, completeErr := repo.CompleteJobSuccess(context.Background(), root.ID, claimRoot.ClaimToken, now.Add(3*time.Second), 0, nil); completeErr != nil || outcome != repository.StepCompletionCompleted {
		t.Fatalf("complete root failed outcome=%q err=%v", outcome, completeErr)
	}

	claimChild := repository.StepClaim{WorkerID: "worker-child", ClaimToken: "claim-child", ClaimedAt: now.Add(4 * time.Second), LeaseExpiresAt: now.Add(64 * time.Second)}
	child, found, err := repo.ClaimNextRunnableJob(context.Background(), claimChild)
	if err != nil || !found {
		t.Fatalf("expected child to claim after fallback dependency success, found=%v err=%v", found, err)
	}
	if child.ID != "job-child" {
		t.Fatalf("expected child job, got %q", child.ID)
	}
}
