package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
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

func TestWorkspacePublishServiceRejectsCompressedArchiveThatExceedsWorkspaceLimit(t *testing.T) {
	harness := newWorkspacePublishServiceHarness(t)
	service, serviceErr := NewWorkspacePublishService(WorkspacePublishServiceConfig{CapabilityAuthorizer: harness.capabilities, ExecutionJobs: harness.jobs, WorkspaceRevisions: harness.revisions, RevisionStore: harness.store, MaxUncompressedBytes: 1024})
	if serviceErr != nil {
		t.Fatalf("new bounded service: %v", serviceErr)
	}
	archive := workspacePublishArchiveForTest(t, strings.Repeat("x", 64*1024))
	if int64(len(archive)) >= 1024 {
		t.Fatalf("expected compressed archive below extraction limit, got %d bytes", len(archive))
	}
	if _, publishErr := service.Publish(context.Background(), "publish-capability", harness.job.ID, "pod-1", bytes.NewReader(archive)); !errors.Is(publishErr, ErrWorkspacePublishArchiveTooLarge) {
		t.Fatalf("publish: %v", publishErr)
	}
	if harness.store.calls != 0 {
		t.Fatalf("store calls=%d, want 0", harness.store.calls)
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

func TestNewWorkspacePublishServiceUsesDefaultUploadLimit(t *testing.T) {
	harness := newWorkspacePublishServiceHarness(t)
	service, serviceErr := NewWorkspacePublishService(WorkspacePublishServiceConfig{CapabilityAuthorizer: harness.capabilities, ExecutionJobs: harness.jobs, WorkspaceRevisions: harness.revisions, RevisionStore: harness.store})
	if serviceErr != nil || service.maxUploadBytes != defaultWorkspacePublishMaxUploadBytes {
		t.Fatalf("service=%#v err=%v", service, serviceErr)
	}
}

func TestCopyWorkspacePublishArchiveStopsOnCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, copyErr := copyWorkspacePublishArchive(ctx, io.Discard, bytes.NewBufferString("archive"), 1024); !errors.Is(copyErr, context.Canceled) {
		t.Fatalf("copy archive: %v", copyErr)
	}
}

func TestCopyWorkspacePublishArchiveRejectsShortWrite(t *testing.T) {
	shortWriter := workspacePublishShortWriter{}
	if _, copyErr := copyWorkspacePublishArchive(context.Background(), shortWriter, bytes.NewBufferString("archive"), 1024); !errors.Is(copyErr, io.ErrShortWrite) {
		t.Fatalf("copy archive: %v", copyErr)
	}
}

func TestCopyWorkspacePublishArchivePropagatesWriteError(t *testing.T) {
	wantErr := errors.New("write failed")
	if _, copyErr := copyWorkspacePublishArchive(context.Background(), workspacePublishErrorWriter{err: wantErr}, bytes.NewBufferString("archive"), 1024); !errors.Is(copyErr, wantErr) {
		t.Fatalf("copy archive: %v", copyErr)
	}
}

type workspacePublishShortWriter struct{}

func (workspacePublishShortWriter) Write(payload []byte) (int, error) {
	return len(payload) - 1, nil
}

type workspacePublishErrorWriter struct{ err error }

func (w workspacePublishErrorWriter) Write([]byte) (int, error) {
	return 0, w.err
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

	reclaimedHarness := newWorkspacePublishServiceHarness(t)
	reclaimedHarness.capabilities.afterAuthorize = func() {
		replacementClaim := "claim-2"
		reclaimedHarness.job.ClaimToken = &replacementClaim
	}
	if _, reclaimErr := reclaimedHarness.service.Publish(context.Background(), "publish-capability", reclaimedHarness.job.ID, "pod-1", bytes.NewReader(workspacePublishArchiveForTest(t, "output"))); !errors.Is(reclaimErr, repository.ErrWorkspaceRevisionStaleClaim) {
		t.Fatalf("reclaimed claim: %v", reclaimErr)
	}
	if reclaimedHarness.revisions.createCalls != 0 || reclaimedHarness.store.calls != 0 {
		t.Fatalf("create calls=%d store calls=%d, want 0", reclaimedHarness.revisions.createCalls, reclaimedHarness.store.calls)
	}
}

func TestWorkspacePublishServiceUsesClaimScopedObjectKeys(t *testing.T) {
	harness := newWorkspacePublishServiceHarness(t)
	harness.revisions.markErr = repository.ErrWorkspaceRevisionStaleClaim
	archive := workspacePublishArchiveForTest(t, "first")
	if _, publishErr := harness.service.Publish(context.Background(), "publish-capability", harness.job.ID, "pod-1", bytes.NewReader(archive)); !errors.Is(publishErr, repository.ErrWorkspaceRevisionStaleClaim) {
		t.Fatalf("stale publish: %v", publishErr)
	}

	replacementClaim := "claim-2"
	harness.job.ClaimToken = &replacementClaim
	harness.capabilities.capability.ClaimDigest = domain.ExecutionJobClaimDigest(replacementClaim)
	harness.revisions.markErr = nil
	harness.revisions.acceptedClaimToken = replacementClaim
	if _, publishErr := harness.service.Publish(context.Background(), "publish-capability", harness.job.ID, "pod-1", bytes.NewReader(workspacePublishArchiveForTest(t, "second"))); publishErr != nil {
		t.Fatalf("replacement publish: %v", publishErr)
	}
	if len(harness.store.objectIDs) != 2 || harness.store.objectIDs[0] == harness.store.objectIDs[1] {
		t.Fatalf("object ids=%v, want distinct claim-scoped keys", harness.store.objectIDs)
	}
}

type workspacePublishServiceHarness struct {
	service      *WorkspacePublishService
	capabilities *workspacePublishCapabilityFake
	jobs         *workspacePublishJobFake
	job          domain.ExecutionJob
	store        *workspacePublishStoreFake
	revisions    *workspacePublishRevisionFake
	archive      io.Reader
}

func newWorkspacePublishServiceHarness(t *testing.T) *workspacePublishServiceHarness {
	t.Helper()
	claim := "claim-1"
	expiresAt := time.Now().Add(time.Hour)
	harness := &workspacePublishServiceHarness{capabilities: &workspacePublishCapabilityFake{capability: domain.WorkspaceHelperCapability{ClaimDigest: domain.ExecutionJobClaimDigest(claim)}}, job: domain.ExecutionJob{ID: "execution-1", BuildID: "build-1", NodeID: "compile", AttemptNumber: 2, Status: domain.ExecutionJobStatusRunning, ClaimToken: &claim, ClaimExpiresAt: &expiresAt}, store: &workspacePublishStoreFake{}}
	jobs := &workspacePublishJobFake{harness: harness}
	revisions := &workspacePublishRevisionFake{acceptedClaimToken: claim}
	service, serviceErr := NewWorkspacePublishService(WorkspacePublishServiceConfig{CapabilityAuthorizer: harness.capabilities, ExecutionJobs: jobs, WorkspaceRevisions: revisions, RevisionStore: harness.store})
	if serviceErr != nil {
		t.Fatalf("new publish service: %v", serviceErr)
	}
	harness.service = service
	harness.jobs = jobs
	harness.archive = bytes.NewReader(workspacePublishArchiveForTest(t, "output"))
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

type workspacePublishCapabilityFake struct {
	role           domain.WorkspaceHelperRole
	err            error
	capability     domain.WorkspaceHelperCapability
	afterAuthorize func()
}

func (f *workspacePublishCapabilityFake) Authorize(_ context.Context, _ string, _ string, _ string, role domain.WorkspaceHelperRole) (domain.WorkspaceHelperCapability, error) {
	f.role = role
	if f.afterAuthorize != nil {
		f.afterAuthorize()
	}
	return f.capability, f.err
}

type workspacePublishJobFake struct {
	harness *workspacePublishServiceHarness
	err     error
}

func (f *workspacePublishJobFake) GetJobByID(context.Context, string) (domain.ExecutionJob, error) {
	return f.harness.job, f.err
}

type workspacePublishStoreFake struct {
	calls        int
	err          error
	afterPublish func()
	objectIDs    []string
}

func (f *workspacePublishStoreFake) Publish(_ context.Context, revisionID string, sourceRoot string) (domain.WorkspaceRevisionPublication, error) {
	f.calls++
	f.objectIDs = append(f.objectIDs, revisionID)
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
	revision           domain.WorkspaceRevision
	createCalls        int
	createErr          error
	markErr            error
	acceptedClaimToken string
}

func (f *workspacePublishRevisionFake) CreatePublishing(_ context.Context, revision domain.WorkspaceRevision) (domain.WorkspaceRevision, error) {
	f.createCalls++
	if f.createErr != nil {
		return domain.WorkspaceRevision{}, f.createErr
	}
	if f.revision.ID == "" {
		f.revision = revision
	}
	return f.revision, nil
}

func TestWorkspacePublishServiceRejectsDependencyFailuresBeforeStore(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*workspacePublishServiceHarness)
	}{
		{name: "authorization", mutate: func(h *workspacePublishServiceHarness) { h.capabilities.err = errors.New("denied") }},
		{name: "job lookup", mutate: func(h *workspacePublishServiceHarness) { h.jobs.err = errors.New("job lookup failed") }},
		{name: "mismatched job", mutate: func(h *workspacePublishServiceHarness) { h.job.ID = "other-job" }},
		{name: "stale job", mutate: func(h *workspacePublishServiceHarness) { h.job.ClaimToken = nil }},
		{name: "create revision", mutate: func(h *workspacePublishServiceHarness) { h.revisions.createErr = errors.New("create failed") }},
		{name: "missing archive", mutate: func(h *workspacePublishServiceHarness) { h.archive = nil }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			harness := newWorkspacePublishServiceHarness(t)
			testCase.mutate(harness)
			if _, publishErr := harness.service.Publish(context.Background(), "publish-capability", "execution-1", "pod-1", harness.archive); publishErr == nil {
				t.Fatal("expected publish failure")
			}
			if harness.store.calls != 0 {
				t.Fatalf("store calls=%d, want 0", harness.store.calls)
			}
		})
	}
}

func (f *workspacePublishRevisionFake) MarkPublishedIfClaimed(_ context.Context, revisionID string, claimToken string, publication domain.WorkspaceRevisionPublication, publishedAt time.Time) (domain.WorkspaceRevision, error) {
	if f.markErr != nil {
		return domain.WorkspaceRevision{}, f.markErr
	}
	if f.revision.Status == domain.WorkspaceRevisionStatusPublished {
		return f.revision, nil
	}
	if claimToken != f.acceptedClaimToken {
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
