package service

import (
	"context"
	"crypto/rand"
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
	objectKey := cacheObjectKey(cacheJobID(harness.build), "go", harness.cacheKey, source.publication.ContentDigest)
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
	if harness.store.restoreCalls != 1 || harness.store.openCalls != 0 {
		t.Fatalf("legacy restore calls=%d open calls=%d, want 1 and 0", harness.store.restoreCalls, harness.store.openCalls)
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

func TestWorkspaceHelperCacheServiceRestoreStreamsCompatibleArchive(t *testing.T) {
	harness := newWorkspaceHelperCacheServiceTestHarness(t)
	source := cacheArchive(t, "cached.txt", "cache hit")
	defer func() { _ = source.archive.Close() }()
	objectKey := cacheObjectKey(cacheJobID(harness.build), "go", harness.cacheKey, source.publication.ContentDigest)
	saved, saveErr := harness.store.SaveArchive(context.Background(), objectKey, source.archive)
	if saveErr != nil {
		t.Fatalf("seed direct cache object: %v", saveErr)
	}
	entry := cacheEntryInput(harness, objectKey)
	entry.SizeBytes = saved.SizeBytes
	entry.Checksum = saved.Checksum
	entry.ContentDigest = source.publication.ContentDigest
	if _, upsertErr := harness.entries.Upsert(context.Background(), entry); upsertErr != nil {
		t.Fatalf("seed cache metadata: %v", upsertErr)
	}

	payload, found, restoreErr := harness.service.Restore(context.Background(), "token", harness.job.ID, "pod-uid", "go", harness.cacheKey)
	if restoreErr != nil || !found {
		t.Fatalf("restore found=%t err=%v", found, restoreErr)
	}
	defer func() { _ = payload.Archive.Close() }()
	if harness.store.restoreCalls != 0 || harness.store.openCalls != 1 {
		t.Fatalf("directory restore calls=%d open calls=%d, want 0 and 1", harness.store.restoreCalls, harness.store.openCalls)
	}
	destination := t.TempDir()
	if archiveRestoreErr := workspace.RestoreArchive(context.Background(), payload.Archive, payload.Publication, destination); archiveRestoreErr != nil {
		t.Fatalf("restore direct archive: %v", archiveRestoreErr)
	}
	contents, readErr := os.ReadFile(filepath.Join(destination, "cached.txt"))
	if readErr != nil || string(contents) != "cache hit" {
		t.Fatalf("restored content=%q err=%v", contents, readErr)
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
	if harness.store.saveCalls != 0 || harness.store.archiveSaveCalls != 1 {
		t.Fatalf("directory saves=%d archive saves=%d, want 0 and 1", harness.store.saveCalls, harness.store.archiveSaveCalls)
	}
	if harness.store.archivePromoteCalls != 1 {
		t.Fatalf("archive promotions=%d, want 1", harness.store.archivePromoteCalls)
	}
	objectKey := cacheObjectKey(cacheJobID(harness.build), "go", harness.cacheKey, archive.publication.ContentDigest)
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

func TestWorkspaceHelperCacheServiceSaveReplacementDeletesPreviousArchive(t *testing.T) {
	harness := newWorkspaceHelperCacheServiceTestHarness(t)
	harness.capabilities.expectedRole = domain.WorkspaceHelperRoleCacheSave
	previous := cacheArchive(t, "cache.txt", "old cache")
	defer func() { _ = previous.archive.Close() }()
	previousObjectKey := cacheObjectKey(cacheJobID(harness.build), "go", harness.cacheKey, previous.publication.ContentDigest)
	previousSaved, previousSaveErr := harness.store.SaveArchive(context.Background(), previousObjectKey, previous.archive)
	if previousSaveErr != nil {
		t.Fatalf("save previous archive: %v", previousSaveErr)
	}
	previousEntry := cacheEntryInput(harness, previousObjectKey)
	previousEntry.SizeBytes = previousSaved.SizeBytes
	previousEntry.Checksum = previousSaved.Checksum
	previousEntry.ContentDigest = previous.publication.ContentDigest
	if _, upsertErr := harness.entries.Upsert(context.Background(), previousEntry); upsertErr != nil {
		t.Fatalf("seed previous cache entry: %v", upsertErr)
	}

	replacement := cacheArchive(t, "cache.txt", "replacement cache")
	defer func() { _ = replacement.archive.Close() }()
	if saveErr := harness.service.Save(context.Background(), "token", harness.job.ID, "pod-uid", "go", harness.cacheKey, replacement.archive, replacement.publication); saveErr != nil {
		t.Fatalf("save replacement: %v", saveErr)
	}
	entry, found, findErr := harness.entries.FindReadyByKey(context.Background(), cacheJobID(harness.build), "go", harness.cacheKey)
	if findErr != nil || !found || entry.ObjectKey == previousObjectKey {
		t.Fatalf("replacement entry=%#v found=%t err=%v", entry, found, findErr)
	}
	if harness.store.archiveDeleteCalls != 1 {
		t.Fatalf("archive delete calls=%d, want 1", harness.store.archiveDeleteCalls)
	}
	previousReader, previousResult, previousOpenErr := harness.store.inner.Open(context.Background(), previousObjectKey)
	if previousOpenErr != nil || previousResult.Hit || previousReader != nil {
		t.Fatalf("previous archive hit=%t reader=%v err=%v", previousResult.Hit, previousReader, previousOpenErr)
	}
	currentReader, currentResult, currentOpenErr := harness.store.inner.Open(context.Background(), entry.ObjectKey)
	if currentOpenErr != nil || !currentResult.Hit || currentReader == nil {
		t.Fatalf("replacement archive hit=%t reader=%v err=%v", currentResult.Hit, currentReader, currentOpenErr)
	}
	defer func() { _ = currentReader.Close() }()
}

func TestWorkspaceHelperCacheServiceSaveRejectsCorruptArchiveWithoutReadyEntry(t *testing.T) {
	harness := newWorkspaceHelperCacheServiceTestHarness(t)
	harness.capabilities.expectedRole = domain.WorkspaceHelperRoleCacheSave
	archive := cacheArchive(t, "paths/000/module", "module data")
	defer func() { _ = archive.archive.Close() }()
	archive.publication.ContentDigest = "sha256:" + strings.Repeat("0", 64)

	saveErr := harness.service.Save(context.Background(), "token", harness.job.ID, "pod-uid", "go", harness.cacheKey, archive.archive, archive.publication)
	if !errors.Is(saveErr, ErrWorkspaceHelperCacheInvalidInput) || !errors.Is(saveErr, workspace.ErrWorkspaceRevisionDigestMismatch) {
		t.Fatalf("save corrupt archive: %v", saveErr)
	}
	if _, found, findErr := harness.entries.FindReadyByKey(context.Background(), cacheJobID(harness.build), "go", harness.cacheKey); findErr != nil || found {
		t.Fatalf("ready cache found=%t err=%v", found, findErr)
	}
}

func TestWorkspaceHelperCacheServiceSaveLateValidationFailureDoesNotPublishFinalArchive(t *testing.T) {
	harness := newWorkspaceHelperCacheServiceTestHarness(t)
	harness.capabilities.expectedRole = domain.WorkspaceHelperRoleCacheSave
	archive := cacheArchiveWithRandomContent(t, 128*1024)
	defer func() { _ = archive.archive.Close() }()
	archive.publication.ContentDigest = "sha256:" + strings.Repeat("0", 64)
	objectKey := cacheObjectKey(cacheJobID(harness.build), "go", harness.cacheKey, archive.publication.ContentDigest)

	saveErr := harness.service.Save(context.Background(), "token", harness.job.ID, "pod-uid", "go", harness.cacheKey, archive.archive, archive.publication)
	if !errors.Is(saveErr, ErrWorkspaceHelperCacheInvalidInput) || !errors.Is(saveErr, workspace.ErrWorkspaceRevisionDigestMismatch) {
		t.Fatalf("save late-invalid archive: %v", saveErr)
	}
	if harness.store.archiveBytes < 64*1024 {
		t.Fatalf("archive bytes streamed=%d, want at least 65536", harness.store.archiveBytes)
	}
	if harness.store.archivePromoteCalls != 0 {
		t.Fatalf("archive promotions=%d, want 0", harness.store.archivePromoteCalls)
	}
	if harness.store.archiveDeleteCalls != 1 {
		t.Fatalf("archive cleanup calls=%d, want 1", harness.store.archiveDeleteCalls)
	}
	stored, result, openErr := harness.store.inner.Open(context.Background(), objectKey)
	if openErr != nil || result.Hit || stored != nil {
		t.Fatalf("final archive hit=%t reader=%v err=%v", result.Hit, stored, openErr)
	}
}

func TestWorkspaceHelperCacheServiceSaveSkipsUnchangedContent(t *testing.T) {
	harness := newWorkspaceHelperCacheServiceTestHarness(t)
	harness.capabilities.expectedRole = domain.WorkspaceHelperRoleCacheSave
	archive := cacheArchive(t, "module", "unchanged")
	defer func() { _ = archive.archive.Close() }()
	if _, upsertErr := harness.entries.Upsert(context.Background(), repository.CacheEntryUpsertInput{JobID: cacheJobID(harness.build), Preset: "go", CacheKey: harness.cacheKey, StorageProvider: harness.store.Provider(), ObjectKey: "ready", ContentDigest: archive.publication.ContentDigest, Compression: "tar.gz", Status: domain.CacheEntryStatusReady, CreatedByBuildID: harness.build.ID, CreatedByStepID: harness.step.ID}); upsertErr != nil {
		t.Fatalf("seed entry: %v", upsertErr)
	}
	if saveErr := harness.service.Save(context.Background(), "token", harness.job.ID, "pod-uid", "go", harness.cacheKey, archive.archive, archive.publication); saveErr != nil {
		t.Fatalf("save unchanged: %v", saveErr)
	}
	if harness.store.saveCalls != 0 || harness.store.archiveSaveCalls != 0 {
		t.Fatalf("store saves=%d archive saves=%d, want 0", harness.store.saveCalls, harness.store.archiveSaveCalls)
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

func TestWorkspaceHelperCacheServiceSaveAcceptsArchiveAtConfiguredEntryLimit(t *testing.T) {
	harness := newWorkspaceHelperCacheServiceTestHarness(t)
	harness.capabilities.expectedRole = domain.WorkspaceHelperRoleCacheSave
	harness.service.maxArchiveEntries = 5
	archive := cacheArchiveWithFiles(t, 3)
	defer func() { _ = archive.archive.Close() }()

	if err := harness.service.Save(context.Background(), "token", harness.job.ID, "pod-uid", "go", harness.cacheKey, archive.archive, archive.publication); err != nil {
		t.Fatalf("save archive at configured entry limit: %v", err)
	}
}

func TestWorkspaceHelperCacheServiceSaveRejectsArchiveAboveConfiguredEntryLimit(t *testing.T) {
	harness := newWorkspaceHelperCacheServiceTestHarness(t)
	harness.capabilities.expectedRole = domain.WorkspaceHelperRoleCacheSave
	harness.service.maxArchiveEntries = 5
	archive := cacheArchiveWithFiles(t, 4)
	defer func() { _ = archive.archive.Close() }()

	err := harness.service.Save(context.Background(), "token", harness.job.ID, "pod-uid", "go", harness.cacheKey, archive.archive, archive.publication)
	if !errors.Is(err, ErrWorkspaceHelperCacheInvalidInput) {
		t.Fatalf("save error=%v, want invalid cache input", err)
	}
	var entryLimitErr *workspace.WorkspaceRevisionEntryLimitError
	if !errors.As(err, &entryLimitErr) || entryLimitErr.MaxEntries != 5 {
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

func TestWorkspaceHelperCacheServiceAcceptsGoCacheComponents(t *testing.T) {
	harness := newWorkspaceHelperCacheServiceTestHarness(t)
	for _, preset := range []string{"go-module", "go-build"} {
		key := preset + ":" + strings.Repeat("a", 64)
		if _, _, err := harness.service.Restore(context.Background(), "token", harness.job.ID, "pod-uid", preset, key); err != nil {
			t.Fatalf("restore component %q: %v", preset, err)
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
	if harness.store.saveCalls != 0 || harness.store.archiveSaveCalls != 1 {
		t.Fatalf("store saves=%d archive saves=%d, want 0 and 1", harness.store.saveCalls, harness.store.archiveSaveCalls)
	}
	entry, found, err := harness.entries.FindReadyByKey(context.Background(), cacheJobID(harness.build), "go", harness.cacheKey)
	if err != nil || !found || entry.ObjectKey != cacheObjectKey(cacheJobID(harness.build), "go", harness.cacheKey, entry.ContentDigest) {
		t.Fatalf("ready entry=%#v found=%t err=%v", entry, found, err)
	}
}

func TestWorkspaceHelperCacheServiceStalePublisherCannotReplaceReclaimedCache(t *testing.T) {
	harness := newWorkspaceHelperCacheServiceTestHarness(t)
	now := time.Date(2026, time.September, 15, 12, 0, 0, 0, time.UTC)
	publisherA := cacheArchive(t, "cache.txt", "from publisher A")
	defer func() { _ = publisherA.archive.Close() }()
	publisherB := cacheArchive(t, "cache.txt", "from publisher B")
	defer func() { _ = publisherB.archive.Close() }()
	jobID := cacheJobID(harness.build)
	claimA, acquired, claimAErr := harness.entries.TryAcquirePublishClaim(context.Background(), jobID, "go", harness.cacheKey, "publisher-a", now, time.Minute)
	if claimAErr != nil || !acquired {
		t.Fatalf("publisher A claim acquired=%t err=%v", acquired, claimAErr)
	}
	objectA := cacheObjectKey(jobID, "go", harness.cacheKey, publisherA.publication.ContentDigest)
	if _, saveErr := harness.store.Save(context.Background(), objectA, publisherA.root); saveErr != nil {
		t.Fatalf("save publisher A object: %v", saveErr)
	}
	claimB, acquired, claimBErr := harness.entries.TryAcquirePublishClaim(context.Background(), jobID, "go", harness.cacheKey, "publisher-b", now.Add(2*time.Minute), time.Minute)
	if claimBErr != nil || !acquired || !claimB.Reclaimed {
		t.Fatalf("publisher B claim=%+v acquired=%t err=%v", claimB, acquired, claimBErr)
	}
	objectB := cacheObjectKey(jobID, "go", harness.cacheKey, publisherB.publication.ContentDigest)
	savedB, saveBErr := harness.store.Save(context.Background(), objectB, publisherB.root)
	if saveBErr != nil {
		t.Fatalf("save publisher B object: %v", saveBErr)
	}
	inputB := cacheEntryInput(harness, objectB)
	inputB.Checksum = savedB.Checksum
	inputB.ContentDigest = publisherB.publication.ContentDigest
	if _, completeBErr := harness.entries.CompletePublishClaim(context.Background(), claimB, inputB, now.Add(2*time.Minute)); completeBErr != nil {
		t.Fatalf("complete publisher B: %v", completeBErr)
	}
	inputA := cacheEntryInput(harness, objectA)
	inputA.ContentDigest = publisherA.publication.ContentDigest
	if _, completeAErr := harness.entries.CompletePublishClaim(context.Background(), claimA, inputA, now.Add(2*time.Minute)); !errors.Is(completeAErr, repository.ErrCachePublishClaimStale) {
		t.Fatalf("complete publisher A err=%v, want stale claim", completeAErr)
	}
	entry, found, findErr := harness.entries.FindReadyByKey(context.Background(), jobID, "go", harness.cacheKey)
	if findErr != nil || !found || entry.ObjectKey != objectB {
		t.Fatalf("ready entry=%+v found=%t err=%v", entry, found, findErr)
	}
	destination := t.TempDir()
	if restored, restoreErr := harness.store.Restore(context.Background(), entry.ObjectKey, destination); restoreErr != nil || !restored.Hit {
		t.Fatalf("restore publisher B cache=%+v err=%v", restored, restoreErr)
	}
	contents, readErr := os.ReadFile(filepath.Join(destination, "cache.txt"))
	if readErr != nil || string(contents) != "from publisher B" {
		t.Fatalf("restored contents=%q err=%v", contents, readErr)
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
	inner interface {
		cachepkg.Store
		cachepkg.ArchiveStore
	}
	saveErr             error
	saveCalls           int
	archiveSaveCalls    int
	archivePromoteCalls int
	archiveDeleteCalls  int
	archiveBytes        int64
	restoreCalls        int
	openCalls           int
}

func (f *workspaceHelperCacheStoreFake) Provider() domain.StorageProvider { return f.inner.Provider() }
func (f *workspaceHelperCacheStoreFake) Restore(ctx context.Context, key string, destination string) (cachepkg.RestoreResult, error) {
	f.restoreCalls++
	return f.inner.Restore(ctx, key, destination)
}
func (f *workspaceHelperCacheStoreFake) Save(ctx context.Context, key string, source string) (cachepkg.SaveResult, error) {
	f.saveCalls++
	if f.saveErr != nil {
		return cachepkg.SaveResult{}, f.saveErr
	}
	return f.inner.Save(ctx, key, source)
}
func (f *workspaceHelperCacheStoreFake) Open(ctx context.Context, key string) (io.ReadCloser, cachepkg.RestoreResult, error) {
	f.openCalls++
	return f.inner.Open(ctx, key)
}
func (f *workspaceHelperCacheStoreFake) SaveArchive(ctx context.Context, key string, archive io.Reader) (cachepkg.SaveResult, error) {
	f.archiveSaveCalls++
	if f.saveErr != nil {
		return cachepkg.SaveResult{}, f.saveErr
	}
	return f.inner.SaveArchive(ctx, key, &countingCacheArchiveReader{reader: archive, count: &f.archiveBytes})
}
func (f *workspaceHelperCacheStoreFake) PromoteArchive(ctx context.Context, sourceKey string, destinationKey string) error {
	f.archivePromoteCalls++
	return f.inner.PromoteArchive(ctx, sourceKey, destinationKey)
}
func (f *workspaceHelperCacheStoreFake) DeleteArchive(ctx context.Context, key string) error {
	f.archiveDeleteCalls++
	return f.inner.DeleteArchive(ctx, key)
}

type countingCacheArchiveReader struct {
	reader io.Reader
	count  *int64
}

func (r *countingCacheArchiveReader) Read(data []byte) (int, error) {
	read, err := r.reader.Read(data)
	*r.count += int64(read)
	return read, err
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

func cacheArchiveWithRandomContent(t *testing.T, size int) cacheServiceArchive {
	t.Helper()
	contents := make([]byte, size)
	if _, randomErr := rand.Read(contents); randomErr != nil {
		t.Fatalf("generate random cache content: %v", randomErr)
	}
	root := t.TempDir()
	if writeErr := os.WriteFile(filepath.Join(root, "cache.bin"), contents, 0o644); writeErr != nil {
		t.Fatalf("write random cache content: %v", writeErr)
	}
	archive, publication, archiveErr := workspace.ArchiveDirectory(context.Background(), root)
	if archiveErr != nil {
		t.Fatalf("archive random cache content: %v", archiveErr)
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
