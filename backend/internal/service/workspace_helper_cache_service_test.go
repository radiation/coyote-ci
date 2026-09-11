package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	cachepkg "github.com/radiation/coyote-ci/backend/internal/cache"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	memoryrepo "github.com/radiation/coyote-ci/backend/internal/repository/memory"
	"github.com/radiation/coyote-ci/backend/internal/workspace"
)

func TestWorkspaceHelperCacheServiceRestoreHitMarksAccessed(t *testing.T) {
	harness := newWorkspaceHelperCacheServiceTestHarness(t)
	source := cacheArchive(t, "cached.txt", "cache hit")
	objectKey := cacheObjectKey(cacheJobID(harness.build), "go", harness.cacheKey)
	if _, err := harness.store.Save(context.Background(), objectKey, source.root); err != nil {
		t.Fatalf("seed cache object: %v", err)
	}
	if _, err := harness.entries.Upsert(context.Background(), cacheEntryInput(harness, objectKey)); err != nil {
		t.Fatalf("seed cache metadata: %v", err)
	}
	accessedAt := time.Date(2026, time.September, 8, 12, 0, 0, 0, time.UTC)
	harness.service.now = func() time.Time { return accessedAt }

	payload, found, err := harness.service.Restore(context.Background(), "token", harness.job.ID, "pod-uid", "go", harness.cacheKey)
	if err != nil || !found {
		t.Fatalf("restore found=%t err=%v", found, err)
	}
	defer func() { _ = payload.Archive.Close() }()
	destination := t.TempDir()
	if archiveRestoreErr := workspace.RestoreArchive(context.Background(), payload.Archive, payload.Publication, destination); archiveRestoreErr != nil {
		t.Fatalf("restore returned archive: %v", archiveRestoreErr)
	}
	contents, readErr := os.ReadFile(filepath.Join(destination, "cached.txt"))
	if readErr != nil || string(contents) != "cache hit" {
		t.Fatalf("restored content=%q err=%v", contents, readErr)
	}
	entry, found, findErr := harness.entries.FindReadyByKey(context.Background(), cacheJobID(harness.build), "go", harness.cacheKey)
	if findErr != nil || !found || entry.LastAccessedAt == nil || !entry.LastAccessedAt.Equal(accessedAt) {
		t.Fatalf("accessed entry=%#v found=%t err=%v", entry, found, findErr)
	}
}

func TestWorkspaceHelperCacheServiceRestoreMiss(t *testing.T) {
	harness := newWorkspaceHelperCacheServiceTestHarness(t)
	_, found, err := harness.service.Restore(context.Background(), "token", harness.job.ID, "pod-uid", "go", harness.cacheKey)
	if err != nil || found {
		t.Fatalf("restore miss found=%t err=%v", found, err)
	}
}

func TestWorkspaceHelperCacheServiceSaveUpsertsReadyEntry(t *testing.T) {
	harness := newWorkspaceHelperCacheServiceTestHarness(t)
	harness.capabilities.expectedRole = domain.WorkspaceHelperRoleCacheSave
	archive := cacheArchive(t, "paths/000/module", "module data")
	defer func() { _ = archive.archive.Close() }()

	if err := harness.service.Save(context.Background(), "token", harness.job.ID, "pod-uid", "go", harness.cacheKey, archive.archive, archive.publication); err != nil {
		t.Fatalf("save: %v", err)
	}
	objectKey := cacheObjectKey(cacheJobID(harness.build), "go", harness.cacheKey)
	entry, found, findErr := harness.entries.FindReadyByKey(context.Background(), cacheJobID(harness.build), "go", harness.cacheKey)
	if findErr != nil || !found || entry.ObjectKey != objectKey || entry.CreatedByBuildID != harness.build.ID || entry.CreatedByStepID != harness.step.ID {
		t.Fatalf("saved entry=%#v found=%t err=%v", entry, found, findErr)
	}
	restored := t.TempDir()
	result, restoreErr := harness.store.Restore(context.Background(), objectKey, restored)
	if restoreErr != nil || !result.Hit {
		t.Fatalf("stored payload result=%#v err=%v", result, restoreErr)
	}
}

func TestWorkspaceHelperCacheServiceSaveRejectsArchiveExceedingRestoreLimits(t *testing.T) {
	harness := newWorkspaceHelperCacheServiceTestHarness(t)
	harness.capabilities.expectedRole = domain.WorkspaceHelperRoleCacheSave
	harness.service.maxUncompressedBytes = 1
	archive := cacheArchive(t, "paths/000/module", "module data")
	defer func() { _ = archive.archive.Close() }()

	err := harness.service.Save(context.Background(), "token", harness.job.ID, "pod-uid", "go", harness.cacheKey, archive.archive, archive.publication)
	if !errors.Is(err, ErrWorkspaceHelperCacheInvalidInput) {
		t.Fatalf("save error=%v, want invalid cache input", err)
	}
	var sizeLimitErr *workspace.WorkspaceRevisionSizeLimitError
	if !errors.As(err, &sizeLimitErr) || sizeLimitErr.MaxBytes != 1 {
		t.Fatalf("save error=%v, want size limit error", err)
	}
	if _, found, findErr := harness.entries.FindReadyByKey(context.Background(), cacheJobID(harness.build), "go", harness.cacheKey); findErr != nil || found {
		t.Fatalf("cache entry found=%t err=%v", found, findErr)
	}
}

func TestWorkspaceHelperCacheServiceSaveAcceptsArchiveAboveLegacyEntryLimit(t *testing.T) {
	harness := newWorkspaceHelperCacheServiceTestHarness(t)
	harness.capabilities.expectedRole = domain.WorkspaceHelperRoleCacheSave
	archive := cacheArchiveWithFiles(t, 10001)
	defer func() { _ = archive.archive.Close() }()

	if err := harness.service.Save(context.Background(), "token", harness.job.ID, "pod-uid", "go", harness.cacheKey, archive.archive, archive.publication); err != nil {
		t.Fatalf("save archive above legacy entry limit: %v", err)
	}
}

func TestWorkspaceHelperCacheServiceSaveRejectsArchiveAboveNewEntryLimit(t *testing.T) {
	harness := newWorkspaceHelperCacheServiceTestHarness(t)
	harness.capabilities.expectedRole = domain.WorkspaceHelperRoleCacheSave
	harness.service.maxArchiveEntries = 100000
	archive := cacheArchiveWithFiles(t, 100001)
	defer func() { _ = archive.archive.Close() }()

	err := harness.service.Save(context.Background(), "token", harness.job.ID, "pod-uid", "go", harness.cacheKey, archive.archive, archive.publication)
	if !errors.Is(err, ErrWorkspaceHelperCacheInvalidInput) {
		t.Fatalf("save error=%v, want invalid cache input", err)
	}
	var entryLimitErr *workspace.WorkspaceRevisionEntryLimitError
	if !errors.As(err, &entryLimitErr) || entryLimitErr.MaxEntries != 100000 {
		t.Fatalf("save error=%v, want entry limit error", err)
	}
}

func TestWorkspaceHelperCacheServiceDefaultsSupportExpandedGoCaches(t *testing.T) {
	harness := newWorkspaceHelperCacheServiceTestHarness(t)
	if harness.service.maxUncompressedBytes != 4*1024*1024*1024 || harness.service.maxUncompressedBytes <= 1024*1024*1024 {
		t.Fatalf("max uncompressed bytes=%d, want 4 GiB", harness.service.maxUncompressedBytes)
	}
	if harness.service.maxArchiveEntries != 100000 {
		t.Fatalf("max archive entries=%d, want 100000", harness.service.maxArchiveEntries)
	}
}

func TestWorkspaceHelperCacheServiceRejectsWrongRoleAndIdentity(t *testing.T) {
	cases := []struct {
		name      string
		role      domain.WorkspaceHelperRole
		jobID     string
		podUID    string
		operation func(*workspaceHelperCacheServiceTestHarness) error
	}{
		{name: "restore role", role: domain.WorkspaceHelperRoleCacheSave, jobID: "execution-job", podUID: "pod-uid", operation: func(h *workspaceHelperCacheServiceTestHarness) error {
			_, _, err := h.service.Restore(context.Background(), "token", h.job.ID, "pod-uid", "go", h.cacheKey)
			return err
		}},
		{name: "save role", role: domain.WorkspaceHelperRoleCacheRestore, jobID: "execution-job", podUID: "pod-uid", operation: func(h *workspaceHelperCacheServiceTestHarness) error {
			archive := cacheArchive(t, "file", "value")
			defer func() { _ = archive.archive.Close() }()
			return h.service.Save(context.Background(), "token", h.job.ID, "pod-uid", "go", h.cacheKey, archive.archive, archive.publication)
		}},
		{name: "execution job", role: domain.WorkspaceHelperRoleCacheRestore, jobID: "other-job", podUID: "pod-uid", operation: func(h *workspaceHelperCacheServiceTestHarness) error {
			_, _, err := h.service.Restore(context.Background(), "token", "other-job", "pod-uid", "go", h.cacheKey)
			return err
		}},
		{name: "pod uid", role: domain.WorkspaceHelperRoleCacheRestore, jobID: "execution-job", podUID: "other-pod", operation: func(h *workspaceHelperCacheServiceTestHarness) error {
			_, _, err := h.service.Restore(context.Background(), "token", h.job.ID, "other-pod", "go", h.cacheKey)
			return err
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			harness := newWorkspaceHelperCacheServiceTestHarness(t)
			harness.capabilities.expectedRole = testCase.role
			if err := testCase.operation(harness); err == nil {
				t.Fatal("expected authorization rejection")
			}
		})
	}
}

func TestWorkspaceHelperCacheServiceRejectsPresetAndKeyMismatch(t *testing.T) {
	harness := newWorkspaceHelperCacheServiceTestHarness(t)
	for _, testCase := range []struct{ preset, key string }{{preset: "node", key: harness.cacheKey}, {preset: "go", key: "go:not-a-fingerprint"}} {
		_, _, err := harness.service.Restore(context.Background(), "token", harness.job.ID, "pod-uid", testCase.preset, testCase.key)
		if !errors.Is(err, ErrWorkspaceHelperCacheInvalidInput) {
			t.Fatalf("preset=%q key=%q err=%v", testCase.preset, testCase.key, err)
		}
	}
}

func TestWorkspaceHelperCacheServiceFailedSavePreservesReadyEntry(t *testing.T) {
	harness := newWorkspaceHelperCacheServiceTestHarness(t)
	harness.capabilities.expectedRole = domain.WorkspaceHelperRoleCacheSave
	oldObjectKey := "old-object"
	if _, err := harness.entries.Upsert(context.Background(), cacheEntryInput(harness, oldObjectKey)); err != nil {
		t.Fatalf("seed cache metadata: %v", err)
	}
	harness.store.saveErr = errors.New("store unavailable")
	archive := cacheArchive(t, "file", "new data")
	defer func() { _ = archive.archive.Close() }()
	if err := harness.service.Save(context.Background(), "token", harness.job.ID, "pod-uid", "go", harness.cacheKey, archive.archive, archive.publication); err == nil {
		t.Fatal("expected save error")
	}
	entry, found, err := harness.entries.FindReadyByKey(context.Background(), cacheJobID(harness.build), "go", harness.cacheKey)
	if err != nil || !found || entry.ObjectKey != oldObjectKey {
		t.Fatalf("ready entry=%#v found=%t err=%v", entry, found, err)
	}
}

func TestWorkspaceHelperCacheServiceConcurrentReplacementLeavesOneReadyEntry(t *testing.T) {
	harness := newWorkspaceHelperCacheServiceTestHarness(t)
	harness.capabilities.expectedRole = domain.WorkspaceHelperRoleCacheSave
	archives := []cacheServiceArchive{cacheArchive(t, "first", "one"), cacheArchive(t, "second", "two")}
	var waitGroup sync.WaitGroup
	errors := make(chan error, len(archives))
	for _, archive := range archives {
		waitGroup.Add(1)
		go func(archive cacheServiceArchive) {
			defer waitGroup.Done()
			defer func() { _ = archive.archive.Close() }()
			errors <- harness.service.Save(context.Background(), "token", harness.job.ID, "pod-uid", "go", harness.cacheKey, archive.archive, archive.publication)
		}(archive)
	}
	waitGroup.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatalf("concurrent save: %v", err)
		}
	}
	entry, found, err := harness.entries.FindReadyByKey(context.Background(), cacheJobID(harness.build), "go", harness.cacheKey)
	if err != nil || !found || entry.ObjectKey != cacheObjectKey(cacheJobID(harness.build), "go", harness.cacheKey) {
		t.Fatalf("ready entry=%#v found=%t err=%v", entry, found, err)
	}
}

type workspaceHelperCacheServiceTestHarness struct {
	service      *WorkspaceHelperCacheService
	capabilities *workspaceHelperCacheCapabilityFake
	entries      *memoryrepo.CacheEntryRepository
	store        *workspaceHelperCacheStoreFake
	build        domain.Build
	job          domain.ExecutionJob
	step         domain.BuildStep
	cacheKey     string
}

func newWorkspaceHelperCacheServiceTestHarness(t *testing.T) *workspaceHelperCacheServiceTestHarness {
	t.Helper()
	logicalJobID := "logical-job"
	build := domain.Build{ID: "build-1", JobID: &logicalJobID}
	step := domain.BuildStep{ID: "step-1", BuildID: build.ID, WorkingDir: ".", Cache: &domain.StepCacheConfig{Preset: "go"}}
	job := domain.ExecutionJob{ID: "execution-job", BuildID: build.ID, StepID: step.ID}
	capabilities := &workspaceHelperCacheCapabilityFake{expectedRole: domain.WorkspaceHelperRoleCacheRestore, expectedJobID: job.ID, expectedPodUID: "pod-uid"}
	store := &workspaceHelperCacheStoreFake{inner: cachepkg.NewFilesystemStore(t.TempDir())}
	entries := memoryrepo.NewCacheEntryRepository()
	service, err := NewWorkspaceHelperCacheService(WorkspaceHelperCacheServiceConfig{CapabilityAuthorizer: capabilities, ExecutionJobs: &workspaceHelperCacheExecutionJobFake{job: job}, Builds: &workspaceHelperCacheBuildFake{build: build, steps: []domain.BuildStep{step}}, Entries: entries, Store: store})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	return &workspaceHelperCacheServiceTestHarness{service: service, capabilities: capabilities, entries: entries, store: store, build: build, job: job, step: step, cacheKey: "go:" + strings.Repeat("a", 64)}
}

func cacheEntryInput(harness *workspaceHelperCacheServiceTestHarness, objectKey string) repository.CacheEntryUpsertInput {
	return repository.CacheEntryUpsertInput{JobID: cacheJobID(harness.build), Preset: "go", CacheKey: harness.cacheKey, StorageProvider: harness.store.Provider(), ObjectKey: objectKey, SizeBytes: 1, Checksum: "checksum", Compression: "tar.gz", Status: domain.CacheEntryStatusReady, CreatedByBuildID: harness.build.ID, CreatedByStepID: harness.step.ID}
}

type workspaceHelperCacheCapabilityFake struct {
	expectedRole                  domain.WorkspaceHelperRole
	expectedJobID, expectedPodUID string
}

func (f *workspaceHelperCacheCapabilityFake) Authorize(_ context.Context, _ string, executionJobID string, podUID string, role domain.WorkspaceHelperRole) (domain.WorkspaceHelperCapability, error) {
	if role != f.expectedRole || executionJobID != f.expectedJobID || podUID != f.expectedPodUID {
		return domain.WorkspaceHelperCapability{}, errors.New("capability rejected")
	}
	return domain.WorkspaceHelperCapability{}, nil
}

type workspaceHelperCacheExecutionJobFake struct{ job domain.ExecutionJob }

func (f *workspaceHelperCacheExecutionJobFake) GetJobByID(context.Context, string) (domain.ExecutionJob, error) {
	return f.job, nil
}

type workspaceHelperCacheBuildFake struct {
	build domain.Build
	steps []domain.BuildStep
}

func (f *workspaceHelperCacheBuildFake) GetByID(context.Context, string) (domain.Build, error) {
	return f.build, nil
}
func (f *workspaceHelperCacheBuildFake) GetStepsByBuildID(context.Context, string) ([]domain.BuildStep, error) {
	return f.steps, nil
}

type workspaceHelperCacheStoreFake struct {
	inner   cachepkg.Store
	saveErr error
}

func (f *workspaceHelperCacheStoreFake) Provider() domain.StorageProvider { return f.inner.Provider() }
func (f *workspaceHelperCacheStoreFake) Restore(ctx context.Context, key string, destination string) (cachepkg.RestoreResult, error) {
	return f.inner.Restore(ctx, key, destination)
}
func (f *workspaceHelperCacheStoreFake) Save(ctx context.Context, key string, source string) (cachepkg.SaveResult, error) {
	if f.saveErr != nil {
		return cachepkg.SaveResult{}, f.saveErr
	}
	return f.inner.Save(ctx, key, source)
}

type cacheServiceArchive struct {
	root        string
	archive     io.ReadCloser
	publication domain.WorkspaceRevisionPublication
}

func cacheArchive(t *testing.T, name string, contents string) cacheServiceArchive {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create archive directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write archive payload: %v", err)
	}
	archive, publication, err := workspace.ArchiveDirectory(context.Background(), root)
	if err != nil {
		t.Fatalf("archive cache payload: %v", err)
	}
	return cacheServiceArchive{root: root, archive: archive, publication: publication}
}

func cacheArchiveWithFiles(t *testing.T, count int) cacheServiceArchive {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "modules"), 0o755); err != nil {
		t.Fatalf("create archive directory: %v", err)
	}
	for index := 0; index < count; index++ {
		path := filepath.Join(root, "modules", fmt.Sprintf("entry-%06d", index))
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatalf("write archive entry %d: %v", index, err)
		}
	}
	archive, publication, err := workspace.ArchiveDirectory(context.Background(), root)
	if err != nil {
		t.Fatalf("archive cache payload: %v", err)
	}
	return cacheServiceArchive{root: root, archive: archive, publication: publication}
}
