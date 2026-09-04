package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	workspacepkg "github.com/radiation/coyote-ci/backend/internal/workspace"
)

func TestWorkspacePublishServicePublishesCanonicalArchiveWithServerDerivedIdentity(t *testing.T) {
	harness := newWorkspacePublishServiceHarness(t)
	archive := workspacePublishArchiveForTest(t, "output")
	published, publishErr := harness.service.Publish(context.Background(), "publish-capability", harness.job.ID, "pod-1", bytes.NewReader(archive))
	if publishErr != nil {
		t.Fatalf("publish: %v", publishErr)
	}
	if harness.capabilities.role != domain.WorkspaceHelperRolePublish || published.ID != domain.WorkspaceRevisionIDForExecutionJob(harness.job.ID) || published.BuildID != harness.job.BuildID || published.NodeID != harness.job.NodeID || published.AttemptNumber != harness.job.AttemptNumber {
		t.Fatalf("role=%q revision=%#v", harness.capabilities.role, published)
	}
	if harness.store.calls != 1 || published.ContentDigest == nil || published.SizeBytes == nil {
		t.Fatalf("store calls=%d revision=%#v", harness.store.calls, published)
	}
}

func TestWorkspacePublishServiceRejectsInvalidArchiveAndStoreFailure(t *testing.T) {
	harness := newWorkspacePublishServiceHarness(t)
	if _, publishErr := harness.service.Publish(context.Background(), "publish-capability", harness.job.ID, "pod-1", bytes.NewBufferString("not a gzip archive")); !errors.Is(publishErr, ErrWorkspacePublishInvalidArchive) {
		t.Fatalf("invalid archive publish: %v", publishErr)
	}
	harness.store.err = errors.New("store failure")
	if _, publishErr := harness.service.Publish(context.Background(), "publish-capability", harness.job.ID, "pod-1", bytes.NewReader(workspacePublishArchiveForTest(t, "output"))); !errors.Is(publishErr, harness.store.err) {
		t.Fatalf("store publish: %v", publishErr)
	}
}

func TestWorkspacePublishServiceRejectsOversizedArchiveWithoutStoreOrTemporaryFile(t *testing.T) {
	harness := newWorkspacePublishServiceHarness(t)
	service, serviceErr := NewWorkspacePublishService(WorkspacePublishServiceConfig{CapabilityAuthorizer: harness.capabilities, ExecutionJobs: &workspacePublishJobFake{harness: harness}, WorkspaceRevisions: harness.revisions, RevisionStore: harness.store, MaxUploadBytes: 4})
	if serviceErr != nil {
		t.Fatalf("new bounded service: %v", serviceErr)
	}
	temporaryDirectory := t.TempDir()
	originalCreateTemp := workspacePublishCreateTemp
	workspacePublishCreateTemp = func(_ string, pattern string) (*os.File, error) {
		return os.CreateTemp(temporaryDirectory, pattern)
	}
	t.Cleanup(func() { workspacePublishCreateTemp = originalCreateTemp })
	if _, publishErr := service.Publish(context.Background(), "publish-capability", harness.job.ID, "pod-1", bytes.NewBufferString("oversized")); !errors.Is(publishErr, ErrWorkspacePublishArchiveTooLarge) {
		t.Fatalf("publish: %v", publishErr)
	}
	if harness.store.calls != 0 {
		t.Fatalf("store calls=%d, want 0", harness.store.calls)
	}
	entries, readErr := os.ReadDir(temporaryDirectory)
	if readErr != nil || len(entries) != 0 {
		t.Fatalf("temporary entries=%v err=%v", entries, readErr)
	}
}

func TestWorkspacePublishServicePublishesArchiveWithinConfiguredLimit(t *testing.T) {
	harness := newWorkspacePublishServiceHarness(t)
	archive := workspacePublishArchiveForTest(t, "output")
	service, serviceErr := NewWorkspacePublishService(WorkspacePublishServiceConfig{CapabilityAuthorizer: harness.capabilities, ExecutionJobs: &workspacePublishJobFake{harness: harness}, WorkspaceRevisions: harness.revisions, RevisionStore: harness.store, MaxUploadBytes: int64(len(archive))})
	if serviceErr != nil {
		t.Fatalf("new bounded service: %v", serviceErr)
	}
	if _, publishErr := service.Publish(context.Background(), "publish-capability", harness.job.ID, "pod-1", bytes.NewReader(archive)); publishErr != nil {
		t.Fatalf("publish within limit: %v", publishErr)
	}
	if harness.store.calls != 1 {
		t.Fatalf("store calls=%d, want 1", harness.store.calls)
	}
}

func TestCopyWorkspacePublishArchiveStopsOnCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, copyErr := copyWorkspacePublishArchive(ctx, io.Discard, bytes.NewBufferString("archive"), 1024); !errors.Is(copyErr, context.Canceled) {
		t.Fatalf("copy archive: %v", copyErr)
	}
}

func TestWorkspacePublishServiceIsIdempotentAndRejectsClaimRace(t *testing.T) {
	harness := newWorkspacePublishServiceHarness(t)
	archive := workspacePublishArchiveForTest(t, "output")
	first, firstErr := harness.service.Publish(context.Background(), "publish-capability", harness.job.ID, "pod-1", bytes.NewReader(archive))
	if firstErr != nil {
		t.Fatalf("first publish: %v", firstErr)
	}
	second, secondErr := harness.service.Publish(context.Background(), "publish-capability", harness.job.ID, "pod-1", bytes.NewReader(archive))
	if secondErr != nil || second.ID != first.ID || harness.store.calls != 2 {
		t.Fatalf("second publish revision=%#v err=%v store calls=%d", second, secondErr, harness.store.calls)
	}

	raceHarness := newWorkspacePublishServiceHarness(t)
	raceHarness.store.afterPublish = func() { raceHarness.revisions.markErr = repository.ErrWorkspaceRevisionStaleClaim }
	if _, raceErr := raceHarness.service.Publish(context.Background(), "publish-capability", raceHarness.job.ID, "pod-1", bytes.NewReader(workspacePublishArchiveForTest(t, "output"))); !errors.Is(raceErr, repository.ErrWorkspaceRevisionStaleClaim) {
		t.Fatalf("claim race: %v", raceErr)
	}
}

type workspacePublishServiceHarness struct {
	service      *WorkspacePublishService
	capabilities *workspacePublishCapabilityFake
	job          domain.ExecutionJob
	store        *workspacePublishStoreFake
	revisions    *workspacePublishRevisionFake
}

func newWorkspacePublishServiceHarness(t *testing.T) *workspacePublishServiceHarness {
	t.Helper()
	claim := "claim-1"
	expiresAt := time.Now().Add(time.Hour)
	harness := &workspacePublishServiceHarness{capabilities: &workspacePublishCapabilityFake{}, job: domain.ExecutionJob{ID: "execution-1", BuildID: "build-1", NodeID: "compile", AttemptNumber: 2, Status: domain.ExecutionJobStatusRunning, ClaimToken: &claim, ClaimExpiresAt: &expiresAt}, store: &workspacePublishStoreFake{}}
	jobs := &workspacePublishJobFake{harness: harness}
	revisions := &workspacePublishRevisionFake{}
	service, serviceErr := NewWorkspacePublishService(WorkspacePublishServiceConfig{CapabilityAuthorizer: harness.capabilities, ExecutionJobs: jobs, WorkspaceRevisions: revisions, RevisionStore: harness.store})
	if serviceErr != nil {
		t.Fatalf("new publish service: %v", serviceErr)
	}
	harness.service = service
	harness.revisions = revisions
	return harness
}

func workspacePublishArchiveForTest(t *testing.T, contents string) []byte {
	t.Helper()
	root := t.TempDir()
	if writeErr := os.WriteFile(filepath.Join(root, "output.txt"), []byte(contents), 0o644); writeErr != nil {
		t.Fatalf("write archive source: %v", writeErr)
	}
	archive, _, archiveErr := workspacepkg.ArchiveDirectory(context.Background(), root)
	if archiveErr != nil {
		t.Fatalf("archive source: %v", archiveErr)
	}
	defer func() { _ = archive.Close() }()
	payload, readErr := io.ReadAll(archive)
	if readErr != nil {
		t.Fatalf("read archive: %v", readErr)
	}
	return payload
}

type workspacePublishCapabilityFake struct{ role domain.WorkspaceHelperRole }

func (f *workspacePublishCapabilityFake) Authorize(_ context.Context, _ string, _ string, _ string, role domain.WorkspaceHelperRole) (domain.WorkspaceHelperCapability, error) {
	f.role = role
	return domain.WorkspaceHelperCapability{}, nil
}

type workspacePublishJobFake struct {
	harness *workspacePublishServiceHarness
}

func (f *workspacePublishJobFake) GetJobByID(context.Context, string) (domain.ExecutionJob, error) {
	return f.harness.job, nil
}

type workspacePublishStoreFake struct {
	calls        int
	err          error
	afterPublish func()
}

func (f *workspacePublishStoreFake) Publish(_ context.Context, revisionID string, sourceRoot string) (domain.WorkspaceRevisionPublication, error) {
	f.calls++
	if f.err != nil {
		return domain.WorkspaceRevisionPublication{}, f.err
	}
	if _, statErr := os.Stat(filepath.Join(sourceRoot, "output.txt")); statErr != nil {
		return domain.WorkspaceRevisionPublication{}, statErr
	}
	size := int64(1)
	if f.afterPublish != nil {
		f.afterPublish()
	}
	return domain.WorkspaceRevisionPublication{ContentDigest: "sha256:test", StorageKey: "workspace-revisions/" + revisionID + ".tar.gz", SizeBytes: &size}, nil
}

func (f *workspacePublishStoreFake) Restore(context.Context, domain.WorkspaceRevisionPublication, string) error {
	return nil
}
func (f *workspacePublishStoreFake) Delete(context.Context, domain.WorkspaceRevisionPublication) error {
	return nil
}

var _ workspacepkg.WorkspaceRevisionStore = (*workspacePublishStoreFake)(nil)

type workspacePublishRevisionFake struct {
	revision domain.WorkspaceRevision
	markErr  error
}

func (f *workspacePublishRevisionFake) CreatePublishing(_ context.Context, revision domain.WorkspaceRevision) (domain.WorkspaceRevision, error) {
	if f.revision.ID == "" {
		f.revision = revision
	}
	return f.revision, nil
}

func (f *workspacePublishRevisionFake) MarkPublishedIfClaimed(_ context.Context, revisionID string, claimToken string, publication domain.WorkspaceRevisionPublication, publishedAt time.Time) (domain.WorkspaceRevision, error) {
	if f.markErr != nil {
		return domain.WorkspaceRevision{}, f.markErr
	}
	if f.revision.Status == domain.WorkspaceRevisionStatusPublished {
		return f.revision, nil
	}
	if claimToken != "claim-1" {
		return domain.WorkspaceRevision{}, repository.ErrWorkspaceRevisionStaleClaim
	}
	f.revision.ID = revisionID
	f.revision.Status = domain.WorkspaceRevisionStatusPublished
	f.revision.ContentDigest = &publication.ContentDigest
	f.revision.StorageKey = &publication.StorageKey
	f.revision.SizeBytes = publication.SizeBytes
	f.revision.PublishedAt = &publishedAt
	return f.revision, nil
}

func (f *workspacePublishRevisionFake) GetByProducingExecutionJob(context.Context, string) (domain.WorkspaceRevision, error) {
	return f.revision, nil
}
func (f *workspacePublishRevisionFake) GetPublishedByBuildNode(context.Context, string, string) (domain.WorkspaceRevision, error) {
	return f.revision, nil
}
func (f *workspacePublishRevisionFake) MarkDeleted(context.Context, string, time.Time) (domain.WorkspaceRevision, error) {
	return f.revision, nil
}

var _ repository.WorkspaceRevisionRepository = (*workspacePublishRevisionFake)(nil)
