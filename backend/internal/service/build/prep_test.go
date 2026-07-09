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
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/artifact"
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

	dir := t.TempDir()

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

	dir := t.TempDir()

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

func TestPrepareBuildExecution_PersistsCommitMetadataWhenAvailable(t *testing.T) {
	repoURL := "https://github.com/example/repo.git"
	ref := "main"

	repo := &fakeBuildRepository{
		build: domain.Build{
			ID:     "build-metadata",
			Status: domain.BuildStatusQueued,
			Source: domain.NewSourceSpec(repoURL, ref, ""),
		},
	}

	originalReadMetadata := readWorkspaceCommitMetadata
	t.Cleanup(func() {
		readWorkspaceCommitMetadata = originalReadMetadata
	})
	readWorkspaceCommitMetadata = func(context.Context, string) (source.CommitMetadata, error) {
		return source.CommitMetadata{
			AuthorName:     "Ada Lovelace",
			AuthorEmail:    "ada@example.com",
			CommitterName:  "Grace Hopper",
			CommitterEmail: "grace@example.com",
		}, nil
	}

	resolver := &fakeWorkspaceSourceResolver{resolvedCommit: "deadbeef"}
	svc := NewBuildService(repo, nil, nil)
	svc.SetSourceResolver(resolver)
	svc.SetExecutionWorkspaceRoot(t.TempDir())

	build, prepErr := svc.PrepareBuildExecution(context.Background(), "build-metadata")
	if prepErr != nil {
		t.Fatalf("prep error: %v", prepErr)
	}
	if build.SourceAuthorEmail == nil || *build.SourceAuthorEmail != "ada@example.com" {
		t.Fatalf("expected source author email to persist, got %v", build.SourceAuthorEmail)
	}
	if build.SourceCommitterEmail == nil || *build.SourceCommitterEmail != "grace@example.com" {
		t.Fatalf("expected source committer email to persist, got %v", build.SourceCommitterEmail)
	}
}

func TestPrepareBuildExecution_IgnoresCommitMetadataFailures(t *testing.T) {
	repoURL := "https://github.com/example/repo.git"
	ref := "main"

	repo := &fakeBuildRepository{
		build: domain.Build{
			ID:     "build-metadata-missing",
			Status: domain.BuildStatusQueued,
			Source: domain.NewSourceSpec(repoURL, ref, ""),
		},
	}

	originalReadMetadata := readWorkspaceCommitMetadata
	t.Cleanup(func() {
		readWorkspaceCommitMetadata = originalReadMetadata
	})
	readWorkspaceCommitMetadata = func(context.Context, string) (source.CommitMetadata, error) {
		return source.CommitMetadata{}, errors.New("git show failed")
	}

	resolver := &fakeWorkspaceSourceResolver{resolvedCommit: "cafebabe"}
	svc := NewBuildService(repo, nil, nil)
	svc.SetSourceResolver(resolver)
	svc.SetExecutionWorkspaceRoot(t.TempDir())

	build, prepErr := svc.PrepareBuildExecution(context.Background(), "build-metadata-missing")
	if prepErr != nil {
		t.Fatalf("prep error: %v", prepErr)
	}
	if build.Status != domain.BuildStatusRunning {
		t.Fatalf("expected running build after metadata failure, got %q", build.Status)
	}
	if build.SourceAuthorEmail != nil || build.SourceCommitterEmail != nil {
		t.Fatalf("expected missing metadata to remain nil, got author=%v committer=%v", build.SourceAuthorEmail, build.SourceCommitterEmail)
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

	dir := t.TempDir()

	resolver := &fakeWorkspaceSourceResolver{cloneErr: fmt.Errorf("%w: authentication failed", source.ErrCloneFailed)}
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
	if build.ErrorMessage == nil || !strings.Contains(*build.ErrorMessage, "authentication failed") {
		t.Fatalf("expected detailed clone failure, got %v", build.ErrorMessage)
	}
}

func TestPrepareBuildExecution_EmitsOneTimeBuildPrepLogMessages(t *testing.T) {
	repoURL := "https://github.com/example/repo.git"
	ref := "main"

	repo := &fakeBuildRepository{
		build: domain.Build{
			ID:     "build-log-prep",
			Status: domain.BuildStatusQueued,
			Source: domain.NewSourceSpec(repoURL, ref, ""),
		},
	}

	dir := t.TempDir()

	logSink := &fakeLogSink{}
	resolver := &fakeWorkspaceSourceResolver{resolvedCommit: "cafebabe"}
	svc := NewBuildService(repo, nil, logSink)
	svc.SetSourceResolver(resolver)
	svc.SetExecutionWorkspaceRoot(dir)

	build, prepErr := svc.PrepareBuildExecution(context.Background(), "build-log-prep")
	if prepErr != nil {
		t.Fatalf("unexpected prep error: %v", prepErr)
	}
	if build.Status != domain.BuildStatusRunning {
		t.Fatalf("expected running build after prep, got %q", build.Status)
	}

	expected := []string{
		"Preparing build workspace",
		"Checking out source",
		"Source checkout complete",
		"Build workspace ready",
	}
	for _, message := range expected {
		if !slices.Contains(logSink.lines, message) {
			t.Fatalf("expected build prep log message %q, got %#v", message, logSink.lines)
		}
	}
}

func TestPrepareBuildExecution_HandsOffTriggerArtifactIntoWorkspace(t *testing.T) {
	ctx := context.Background()
	workspaceRoot := t.TempDir()
	storageRoot := t.TempDir()
	body := []byte("artifact-body")
	checksumBytes := sha256.Sum256(body)
	checksum := hex.EncodeToString(checksumBytes[:])
	store := artifact.NewFilesystemStore(storageRoot)
	if _, saveErr := store.Save(ctx, "producer/dist/app.tgz", strings.NewReader(string(body))); saveErr != nil {
		t.Fatalf("seed artifact store: %v", saveErr)
	}

	producerProjectID := "project-1"
	producerBuildID := "build-upstream"
	artifactID := "artifact-1"
	artifactPath := "dist/app.tgz"
	repo := &fakeBuildRepository{build: domain.Build{
		ID:        "build-downstream",
		ProjectID: producerProjectID,
		Status:    domain.BuildStatusQueued,
		Trigger: domain.BuildTrigger{
			Kind:                   domain.BuildTriggerKindArtifact,
			ProducerProjectID:      &producerProjectID,
			ProducerBuildID:        &producerBuildID,
			ArtifactID:             &artifactID,
			ArtifactPath:           &artifactPath,
			ArtifactChecksumSHA256: &checksum,
		},
	}}
	artifactRepo := &fakeArtifactRepository{artifacts: map[string][]domain.BuildArtifact{
		producerBuildID: {{
			ID:              artifactID,
			BuildID:         producerBuildID,
			LogicalPath:     artifactPath,
			StorageKey:      "producer/dist/app.tgz",
			StorageProvider: domain.StorageProviderFilesystem,
			SizeBytes:       int64(len(body)),
			ChecksumSHA256:  &checksum,
		}},
	}}
	logSink := &fakeLogSink{}

	svc := NewBuildService(repo, nil, logSink)
	svc.SetArtifactPersistence(artifactRepo, testStoreResolver(store), workspaceRoot)

	build, prepErr := svc.PrepareBuildExecution(ctx, "build-downstream")
	if prepErr != nil {
		t.Fatalf("prepare build execution: %v", prepErr)
	}
	if build.Status != domain.BuildStatusRunning {
		t.Fatalf("expected running build after handoff, got %q", build.Status)
	}

	handedOffPath := filepath.Join(workspaceRoot, "build-downstream", ".coyote", "trigger-artifacts", "dist", "app.tgz")
	handedOffBody, readErr := os.ReadFile(handedOffPath)
	if readErr != nil {
		t.Fatalf("read handed off artifact: %v", readErr)
	}
	if string(handedOffBody) != string(body) {
		t.Fatalf("expected handed off artifact body %q, got %q", string(body), string(handedOffBody))
	}
	if !slices.Contains(logSink.lines, "Preparing trigger artifact handoff") || !slices.Contains(logSink.lines, "Trigger artifact handoff complete") {
		t.Fatalf("expected trigger handoff log lines, got %#v", logSink.lines)
	}
}

func TestPrepareBuildExecution_FailsBuildWhenTriggerArtifactMissing(t *testing.T) {
	producerProjectID := "project-1"
	producerBuildID := "build-upstream"
	artifactID := "artifact-1"
	artifactPath := "dist/app.tgz"
	repo := &fakeBuildRepository{build: domain.Build{
		ID:        "build-downstream",
		ProjectID: producerProjectID,
		Status:    domain.BuildStatusQueued,
		Trigger: domain.BuildTrigger{
			Kind:              domain.BuildTriggerKindArtifact,
			ProducerProjectID: &producerProjectID,
			ProducerBuildID:   &producerBuildID,
			ArtifactID:        &artifactID,
			ArtifactPath:      &artifactPath,
		},
	}}

	svc := NewBuildService(repo, nil, &fakeLogSink{})
	svc.SetArtifactPersistence(&fakeArtifactRepository{}, testStoreResolver(artifact.NewFilesystemStore(t.TempDir())), t.TempDir())

	build, prepErr := svc.PrepareBuildExecution(context.Background(), "build-downstream")
	if prepErr != nil {
		t.Fatalf("expected build failure without hard error, got %v", prepErr)
	}
	if build.Status != domain.BuildStatusFailed {
		t.Fatalf("expected failed build when trigger artifact is missing, got %q", build.Status)
	}
	if build.ErrorMessage == nil || !strings.Contains(*build.ErrorMessage, "artifact not found") {
		t.Fatalf("expected missing artifact error message, got %v", build.ErrorMessage)
	}
}

func TestPrepareBuildExecution_FailsBuildWhenTriggerArtifactChecksumMismatches(t *testing.T) {
	ctx := context.Background()
	workspaceRoot := t.TempDir()
	storageRoot := t.TempDir()
	store := artifact.NewFilesystemStore(storageRoot)
	if _, saveErr := store.Save(ctx, "producer/dist/app.tgz", strings.NewReader("artifact-body")); saveErr != nil {
		t.Fatalf("seed artifact store: %v", saveErr)
	}

	producerProjectID := "project-1"
	producerBuildID := "build-upstream"
	artifactID := "artifact-1"
	artifactPath := "dist/app.tgz"
	wrongChecksum := strings.Repeat("a", 64)
	repo := &fakeBuildRepository{build: domain.Build{
		ID:        "build-downstream",
		ProjectID: producerProjectID,
		Status:    domain.BuildStatusQueued,
		Trigger: domain.BuildTrigger{
			Kind:                   domain.BuildTriggerKindArtifact,
			ProducerProjectID:      &producerProjectID,
			ProducerBuildID:        &producerBuildID,
			ArtifactID:             &artifactID,
			ArtifactPath:           &artifactPath,
			ArtifactChecksumSHA256: &wrongChecksum,
		},
	}}
	artifactRepo := &fakeArtifactRepository{artifacts: map[string][]domain.BuildArtifact{
		producerBuildID: {{
			ID:              artifactID,
			BuildID:         producerBuildID,
			LogicalPath:     artifactPath,
			StorageKey:      "producer/dist/app.tgz",
			StorageProvider: domain.StorageProviderFilesystem,
		}},
	}}

	svc := NewBuildService(repo, nil, &fakeLogSink{})
	svc.SetArtifactPersistence(artifactRepo, testStoreResolver(store), workspaceRoot)

	build, prepErr := svc.PrepareBuildExecution(ctx, "build-downstream")
	if prepErr != nil {
		t.Fatalf("expected build failure without hard error, got %v", prepErr)
	}
	if build.Status != domain.BuildStatusFailed {
		t.Fatalf("expected failed build on checksum mismatch, got %q", build.Status)
	}
	if build.ErrorMessage == nil || !strings.Contains(*build.ErrorMessage, "checksum mismatch") {
		t.Fatalf("expected checksum mismatch error, got %v", build.ErrorMessage)
	}
}

func TestPrepareBuildExecution_FailsBuildWhenTriggerProducerProjectMismatches(t *testing.T) {
	producerProjectID := "project-1"
	producerBuildID := "build-upstream"
	artifactID := "artifact-1"
	artifactPath := "dist/app.tgz"
	repo := &fakeBuildRepository{build: domain.Build{
		ID:        "build-downstream",
		ProjectID: "project-2",
		Status:    domain.BuildStatusQueued,
		Trigger: domain.BuildTrigger{
			Kind:              domain.BuildTriggerKindArtifact,
			ProducerProjectID: &producerProjectID,
			ProducerBuildID:   &producerBuildID,
			ArtifactID:        &artifactID,
			ArtifactPath:      &artifactPath,
		},
	}}

	svc := NewBuildService(repo, nil, &fakeLogSink{})
	svc.SetArtifactPersistence(&fakeArtifactRepository{}, testStoreResolver(artifact.NewFilesystemStore(t.TempDir())), t.TempDir())

	build, prepErr := svc.PrepareBuildExecution(context.Background(), "build-downstream")
	if prepErr != nil {
		t.Fatalf("expected build failure without hard error, got %v", prepErr)
	}
	if build.Status != domain.BuildStatusFailed {
		t.Fatalf("expected failed build on producer project mismatch, got %q", build.Status)
	}
	if build.ErrorMessage == nil || !strings.Contains(*build.ErrorMessage, "producer project mismatch") {
		t.Fatalf("expected producer project mismatch error, got %v", build.ErrorMessage)
	}
}
