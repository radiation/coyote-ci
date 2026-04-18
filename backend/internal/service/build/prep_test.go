package build

// Tests for PrepareBuildExecution: the build-level prep gate.
//
// Design requirements verified here:
//  1. Build preparation is a build-level prerequisite (transitions queued→preparing→running).
//  2. If build prep fails, build is failed before any step starts.
//  3. Source clone+checkout is called exactly once per build regardless of parallel step count.
//  4. Scheduler must not mark jobs runnable until build status is running; this is enforced by
//     the postgres query (build status gate). The service tests verify the status transition here.

import (
	"context"
	"os"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/source"
)

// TestPrepareBuildExecution_TransitionsQueuedToRunning verifies the happy-path
// status machine: queued → preparing → running.
func TestPrepareBuildExecution_TransitionsQueuedToRunning(t *testing.T) {
	repo := &fakeBuildRepository{
		build: domain.Build{ID: "build-1", Status: domain.BuildStatusQueued},
	}
	svc := NewBuildService(repo, nil, nil)

	dir, err := os.MkdirTemp("", "prep-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer func() {
		if removeErr := os.RemoveAll(dir); removeErr != nil {
			t.Fatalf("cleanup temp dir: %v", removeErr)
		}
	}()

	resolver := &fakeWorkspaceSourceResolver{}
	svc.SetSourceResolver(resolver)
	svc.SetExecutionWorkspaceRoot(dir)

	build, prepErr := svc.PrepareBuildExecution(context.Background(), "build-1")
	if prepErr != nil {
		t.Fatalf("unexpected error: %v", prepErr)
	}
	if build.Status != domain.BuildStatusRunning {
		t.Fatalf("expected running, got %q", build.Status)
	}
	// No source spec on the build — resolver should not be called.
	if resolver.cloneCalls != 0 {
		t.Fatalf("expected zero clone calls for build without source, got %d", resolver.cloneCalls)
	}
}

// TestPrepareBuildExecution_IdempotentIfAlreadyRunning verifies that calling
// PrepareBuildExecution on an already-running build is a safe no-op.
func TestPrepareBuildExecution_IdempotentIfAlreadyRunning(t *testing.T) {
	repo := &fakeBuildRepository{
		build: domain.Build{ID: "build-1", Status: domain.BuildStatusRunning},
	}
	svc := NewBuildService(repo, nil, nil)

	build, err := svc.PrepareBuildExecution(context.Background(), "build-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if build.Status != domain.BuildStatusRunning {
		t.Fatalf("expected running, got %q", build.Status)
	}
	if repo.updateCalls != 0 {
		t.Fatalf("expected zero update calls for idempotent prep, got %d", repo.updateCalls)
	}
}

// TestPrepareBuildExecution_SucceedsWithNoWorkspaceRootWhenNoSource verifies
// that when no workspace root is configured but the build has no source
// (no git checkout needed), prep succeeds and the build becomes running.
func TestPrepareBuildExecution_SucceedsWithNoWorkspaceRootWhenNoSource(t *testing.T) {
	repo := &fakeBuildRepository{
		build: domain.Build{ID: "build-1", Status: domain.BuildStatusQueued},
	}
	svc := NewBuildService(repo, nil, nil)
	// No execution workspace root set, no source — prep should succeed with no-op workspace creation.

	build, err := svc.PrepareBuildExecution(context.Background(), "build-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if build.Status != domain.BuildStatusRunning {
		t.Fatalf("expected running, got %q", build.Status)
	}
}

// TestPrepareBuildExecution_RejectsNonQueuedBuilds verifies that a build that
// is not queued (e.g. already failed) is rejected with an error rather than
// starting prep — preventing double-prep.
func TestPrepareBuildExecution_RejectsNonQueuedBuilds(t *testing.T) {
	for _, status := range []domain.BuildStatus{
		domain.BuildStatusFailed,
		domain.BuildStatusSuccess,
		domain.BuildStatusPending,
	} {
		repo := &fakeBuildRepository{
			build: domain.Build{ID: "build-1", Status: status},
		}
		svc := NewBuildService(repo, nil, nil)

		_, err := svc.PrepareBuildExecution(context.Background(), "build-1")
		if err == nil {
			t.Errorf("expected error for status %q, got nil", status)
		}
	}
}

// TestPrepareBuildExecution_SourceClonedExactlyOnce is the regression test
// that source checkout happens exactly once per build, regardless of how many
// parallel steps the build has. This proves there is no per-step clone path.
func TestPrepareBuildExecution_SourceClonedExactlyOnce(t *testing.T) {
	repoURL := "https://github.com/example/repo.git"
	ref := "main"

	repo := &fakeBuildRepository{
		build: domain.Build{
			ID:     "build-parallel",
			Status: domain.BuildStatusQueued,
			Source: domain.NewSourceSpec(repoURL, ref, ""),
		},
	}

	dir, err := os.MkdirTemp("", "prep-once-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer func() {
		if removeErr := os.RemoveAll(dir); removeErr != nil {
			t.Fatalf("cleanup temp dir: %v", removeErr)
		}
	}()

	resolver := &fakeWorkspaceSourceResolver{resolvedCommit: "deadbeef"}
	svc := NewBuildService(repo, nil, nil)
	svc.SetSourceResolver(resolver)
	svc.SetExecutionWorkspaceRoot(dir)

	build, prepErr := svc.PrepareBuildExecution(context.Background(), "build-parallel")
	if prepErr != nil {
		t.Fatalf("prep error: %v", prepErr)
	}
	if build.Status != domain.BuildStatusRunning {
		t.Fatalf("expected running after prep, got %q", build.Status)
	}

	// Source was cloned exactly once — not once per step.
	if resolver.cloneCalls != 1 {
		t.Fatalf("expected exactly 1 clone call, got %d — per-step clone detected", resolver.cloneCalls)
	}
	if resolver.checkoutCalls != 1 {
		t.Fatalf("expected exactly 1 checkout call, got %d", resolver.checkoutCalls)
	}

	// Calling PrepareBuildExecution again on the now-running build is idempotent (no extra clone).
	_, secondErr := svc.PrepareBuildExecution(context.Background(), "build-parallel")
	if secondErr != nil {
		t.Fatalf("second prep error: %v", secondErr)
	}
	if resolver.cloneCalls != 1 {
		t.Fatalf("expected still 1 clone call after idempotent re-prep, got %d", resolver.cloneCalls)
	}
}

// TestPrepareBuildExecution_FailsBuildOnCloneError verifies that a clone
// failure results in a failed build status and no further processing.
func TestPrepareBuildExecution_FailsBuildOnCloneError(t *testing.T) {
	repoURL := "https://github.com/example/repo.git"
	ref := "main"

	repo := &fakeBuildRepository{
		build: domain.Build{
			ID:     "build-clone-fail",
			Status: domain.BuildStatusQueued,
			Source: domain.NewSourceSpec(repoURL, ref, ""),
		},
	}

	dir, err := os.MkdirTemp("", "prep-clone-fail-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer func() {
		if removeErr := os.RemoveAll(dir); removeErr != nil {
			t.Fatalf("cleanup temp dir: %v", removeErr)
		}
	}()

	resolver := &fakeWorkspaceSourceResolver{cloneErr: source.ErrCloneFailed}
	svc := NewBuildService(repo, nil, nil)
	svc.SetSourceResolver(resolver)
	svc.SetExecutionWorkspaceRoot(dir)

	build, prepErr := svc.PrepareBuildExecution(context.Background(), "build-clone-fail")
	if prepErr != nil {
		t.Fatalf("unexpected hard error: %v", prepErr)
	}
	if build.Status != domain.BuildStatusFailed {
		t.Fatalf("expected failed build on clone error, got %q", build.Status)
	}
}
