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
