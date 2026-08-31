package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func TestWorkspaceRevisionRepositoryPublicationLifecycle(t *testing.T) {
	now := time.Now().UTC()
	executionJobs := NewExecutionJobRepository()
	createExecutionJob(t, executionJobs, "job-1", "build-1", "compile", 1, now)
	repo := NewWorkspaceRevisionRepository(executionJobs)
	revision := testWorkspaceRevision("revision-1", "job-1", "build-1", "compile", 1, now)

	if _, err := repo.CreatePublishing(context.Background(), revision); err != nil {
		t.Fatalf("create publishing revision: %v", err)
	}
	if _, err := repo.CreatePublishing(context.Background(), revision); err != nil {
		t.Fatalf("idempotent create: %v", err)
	}
	conflict := testWorkspaceRevision("revision-2", "job-1", "build-1", "compile", 1, now)
	if _, err := repo.CreatePublishing(context.Background(), conflict); !errors.Is(err, repository.ErrWorkspaceRevisionConflict) {
		t.Fatalf("expected create conflict, got %v", err)
	}
	if _, err := repo.GetPublishedByBuildNode(context.Background(), "build-1", "compile"); !errors.Is(err, repository.ErrWorkspaceRevisionNotFound) {
		t.Fatalf("publishing revision must remain hidden, got %v", err)
	}

	claimExecutionJob(t, executionJobs, "step-job-1", "claim-active", now)
	publication := domain.WorkspaceRevisionPublication{ContentDigest: "sha256:one", StorageKey: "revisions/revision-1"}
	published, err := repo.MarkPublishedIfClaimed(context.Background(), revision.ID, "claim-active", publication, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("publish active claim: %v", err)
	}
	if published.Status != domain.WorkspaceRevisionStatusPublished {
		t.Fatalf("expected published status, got %q", published.Status)
	}
	if _, idempotentPublishErr := repo.MarkPublishedIfClaimed(context.Background(), revision.ID, "claim-active", publication, now.Add(2*time.Minute)); idempotentPublishErr != nil {
		t.Fatalf("idempotent publication: %v", idempotentPublishErr)
	}
	if _, conflictingDigestErr := repo.MarkPublishedIfClaimed(context.Background(), revision.ID, "claim-active", domain.WorkspaceRevisionPublication{ContentDigest: "sha256:two", StorageKey: publication.StorageKey}, now.Add(2*time.Minute)); !errors.Is(conflictingDigestErr, repository.ErrWorkspaceRevisionConflict) {
		t.Fatalf("expected conflicting digest to be rejected, got %v", conflictingDigestErr)
	}
	if _, conflictingStorageKeyErr := repo.MarkPublishedIfClaimed(context.Background(), revision.ID, "claim-active", domain.WorkspaceRevisionPublication{ContentDigest: publication.ContentDigest, StorageKey: "revisions/other"}, now.Add(2*time.Minute)); !errors.Is(conflictingStorageKeyErr, repository.ErrWorkspaceRevisionConflict) {
		t.Fatalf("expected conflicting storage key to be rejected, got %v", conflictingStorageKeyErr)
	}
	completeExecutionJob(t, executionJobs, "job-1", "claim-active", now.Add(2*time.Minute))

	lookup, err := repo.GetPublishedByBuildNode(context.Background(), "build-1", "compile")
	if err != nil || lookup.ID != revision.ID {
		t.Fatalf("lookup published revision: revision=%#v err=%v", lookup, err)
	}
	if _, err := repo.MarkDeleted(context.Background(), revision.ID, now.Add(3*time.Minute)); err != nil {
		t.Fatalf("delete published revision: %v", err)
	}
	if _, err := repo.GetPublishedByBuildNode(context.Background(), "build-1", "compile"); !errors.Is(err, repository.ErrWorkspaceRevisionNotFound) {
		t.Fatalf("deleted revision must remain hidden, got %v", err)
	}
	if _, err := repo.MarkPublishedIfClaimed(context.Background(), revision.ID, "claim-active", publication, now.Add(4*time.Minute)); !errors.Is(err, repository.ErrWorkspaceRevisionConflict) {
		t.Fatalf("deleted revision cannot republish, got %v", err)
	}
}

func TestWorkspaceRevisionRepositoryRejectsStaleClaimAndSelectsLatestSuccessfulAttempt(t *testing.T) {
	now := time.Now().UTC()
	executionJobs := NewExecutionJobRepository()
	createExecutionJob(t, executionJobs, "job-old", "build-1", "compile", 1, now)
	createExecutionJob(t, executionJobs, "job-new", "build-1", "compile", 2, now.Add(time.Minute))
	repo := NewWorkspaceRevisionRepository(executionJobs)
	oldRevision := testWorkspaceRevision("revision-old", "job-old", "build-1", "compile", 1, now)
	newRevision := testWorkspaceRevision("revision-new", "job-new", "build-1", "compile", 2, now.Add(time.Minute))
	for _, revision := range []domain.WorkspaceRevision{oldRevision, newRevision} {
		if _, err := repo.CreatePublishing(context.Background(), revision); err != nil {
			t.Fatalf("create %s: %v", revision.ID, err)
		}
	}
	publication := domain.WorkspaceRevisionPublication{ContentDigest: "sha256:one", StorageKey: "revisions/shared"}
	if _, err := repo.MarkPublishedIfClaimed(context.Background(), oldRevision.ID, "wrong", publication, now); !errors.Is(err, repository.ErrWorkspaceRevisionStaleClaim) {
		t.Fatalf("expected stale claim, got %v", err)
	}
	claimExecutionJob(t, executionJobs, "step-job-old", "claim-old", now)
	claimExecutionJob(t, executionJobs, "step-job-new", "claim-new", now)
	if _, err := repo.MarkPublishedIfClaimed(context.Background(), oldRevision.ID, "claim-old", publication, now); err != nil {
		t.Fatalf("publish old revision: %v", err)
	}
	completeExecutionJob(t, executionJobs, "job-old", "claim-old", now.Add(time.Second))
	if _, err := repo.MarkPublishedIfClaimed(context.Background(), newRevision.ID, "claim-new", publication, now); err != nil {
		t.Fatalf("publish new revision: %v", err)
	}
	lookup, err := repo.GetPublishedByBuildNode(context.Background(), "build-1", "compile")
	if err != nil || lookup.ID != oldRevision.ID {
		t.Fatalf("expected latest successful attempt, got %#v err=%v", lookup, err)
	}
}

func TestWorkspaceRevisionRepositoryRejectsExpiredClaimAndMismatchedJobMetadata(t *testing.T) {
	now := time.Now().UTC()
	executionJobs := NewExecutionJobRepository()
	createExecutionJob(t, executionJobs, "job-1", "build-1", "compile", 1, now)
	repo := NewWorkspaceRevisionRepository(executionJobs)
	mismatched := testWorkspaceRevision("revision-mismatch", "job-1", "other-build", "compile", 1, now)
	if _, err := repo.CreatePublishing(context.Background(), mismatched); !errors.Is(err, repository.ErrWorkspaceRevisionConflict) {
		t.Fatalf("expected owner metadata conflict, got %v", err)
	}
	revision := testWorkspaceRevision("revision-1", "job-1", "build-1", "compile", 1, now)
	if _, err := repo.CreatePublishing(context.Background(), revision); err != nil {
		t.Fatalf("create revision: %v", err)
	}
	claimExecutionJob(t, executionJobs, "step-job-1", "claim-expired", now.Add(-2*time.Minute))
	_, err := repo.MarkPublishedIfClaimed(context.Background(), revision.ID, "claim-expired", domain.WorkspaceRevisionPublication{ContentDigest: "sha256:one", StorageKey: "revisions/revision-1"}, now)
	if !errors.Is(err, repository.ErrWorkspaceRevisionStaleClaim) {
		t.Fatalf("expected expired lease to reject publication, got %v", err)
	}
}

func createExecutionJob(t *testing.T, repo *ExecutionJobRepository, jobID string, buildID string, nodeID string, attempt int, now time.Time) {
	t.Helper()
	_, err := repo.CreateJobsForBuild(context.Background(), []domain.ExecutionJob{{ID: jobID, BuildID: buildID, StepID: "step-" + jobID, NodeID: nodeID, Name: nodeID, StepIndex: 0, AttemptNumber: attempt, Status: domain.ExecutionJobStatusQueued, ResolvedSpecJSON: "{}", CreatedAt: now}})
	if err != nil {
		t.Fatalf("create execution job: %v", err)
	}
}

func claimExecutionJob(t *testing.T, repo *ExecutionJobRepository, stepID string, token string, now time.Time) {
	t.Helper()
	_, claimed, err := repo.ClaimJobByStepID(context.Background(), stepID, repository.StepClaim{WorkerID: "worker-1", ClaimToken: token, ClaimedAt: now, LeaseExpiresAt: now.Add(time.Minute)})
	if err != nil || !claimed {
		t.Fatalf("claim execution job: claimed=%v err=%v", claimed, err)
	}
}

func completeExecutionJob(t *testing.T, repo *ExecutionJobRepository, jobID string, token string, finishedAt time.Time) {
	t.Helper()
	_, outcome, err := repo.CompleteJobSuccess(context.Background(), jobID, token, finishedAt, 0, nil)
	if err != nil || outcome != repository.StepCompletionCompleted {
		t.Fatalf("complete execution job: outcome=%q err=%v", outcome, err)
	}
}

func testWorkspaceRevision(id string, jobID string, buildID string, nodeID string, attempt int, createdAt time.Time) domain.WorkspaceRevision {
	return domain.WorkspaceRevision{ID: id, ProducingExecutionJobID: jobID, BuildID: buildID, NodeID: nodeID, AttemptNumber: attempt, Status: domain.WorkspaceRevisionStatusPublishing, CreatedAt: createdAt}
}
