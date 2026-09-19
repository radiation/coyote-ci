package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log"
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
	logs := captureCacheTransferLogs(t)

	payload, found, err := harness.service.Restore(context.Background(), "token", harness.job.ID, "pod-uid", "go", harness.cacheKey)
	if err != nil || !found {
		t.Fatalf("restore found=%t err=%v", found, err)
	}
	if harness.store.restoreCalls != 1 || harness.store.openCalls != 0 {
		t.Fatalf("legacy restore calls=%d open calls=%d, want 1 and 0", harness.store.restoreCalls, harness.store.openCalls)
	}
	if !strings.Contains(logs.String(), "transfer_mode=legacy_materialized") {
		t.Fatalf("restore logs=%q, want legacy materialized mode", logs.String())
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

func TestWorkspaceHelperCacheServiceLegacyStorageMissRecordsMiss(t *testing.T) {
	harness := newWorkspaceHelperCacheServiceTestHarness(t)
	objectKey := cacheObjectKey(cacheJobID(harness.build), "go", harness.cacheKey, "sha256:"+strings.Repeat("a", 64))
	if _, upsertErr := harness.entries.Upsert(context.Background(), cacheEntryInput(harness, objectKey)); upsertErr != nil {
		t.Fatalf("seed cache metadata: %v", upsertErr)
	}
	harness.store.restoreMiss = true
	logs := captureCacheTransferLogs(t)

	_, found, restoreErr := harness.service.Restore(context.Background(), "token", harness.job.ID, "pod-uid", "go", harness.cacheKey)
	if restoreErr != nil || found {
		t.Fatalf("restore found=%t err=%v", found, restoreErr)
	}
	if !strings.Contains(logs.String(), "outcome=miss") || strings.Contains(logs.String(), "outcome=restore_error") {
		t.Fatalf("restore logs=%q, want miss without restore_error", logs.String())
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
	logs := captureCacheTransferLogs(t)

	payload, found, restoreErr := harness.service.Restore(context.Background(), "token", harness.job.ID, "pod-uid", "go", harness.cacheKey)
	if restoreErr != nil || !found {
		t.Fatalf("restore found=%t err=%v", found, restoreErr)
	}
	defer func() { _ = payload.Archive.Close() }()
	if harness.store.restoreCalls != 0 || harness.store.openCalls != 1 {
		t.Fatalf("directory restore calls=%d open calls=%d, want 0 and 1", harness.store.restoreCalls, harness.store.openCalls)
	}
	if payload.Publication.ContentDigest != source.publication.ContentDigest {
		t.Fatalf("publication digest=%q, want %q", payload.Publication.ContentDigest, source.publication.ContentDigest)
	}
	if !strings.Contains(logs.String(), "transfer_mode=direct") {
		t.Fatalf("restore logs=%q, want direct mode", logs.String())
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

func TestWorkspaceHelperCacheServiceRestoreStreamsChecksumOnlyLegacyArchive(t *testing.T) {
	harness := newWorkspaceHelperCacheServiceTestHarness(t)
	source := cacheArchive(t, "cached.txt", "cache hit")
	defer func() { _ = source.archive.Close() }()
	objectKey := cacheObjectKey(cacheJobID(harness.build), "go", harness.cacheKey, source.publication.ContentDigest)
	saved, saveErr := harness.store.SaveArchive(context.Background(), objectKey, source.archive)
	if saveErr != nil {
		t.Fatalf("seed legacy cache object: %v", saveErr)
	}
	entry := cacheEntryInput(harness, objectKey)
	entry.SizeBytes = saved.SizeBytes
	entry.Checksum = saved.Checksum
	entry.ContentDigest = ""
	if _, upsertErr := harness.entries.Upsert(context.Background(), entry); upsertErr != nil {
		t.Fatalf("seed legacy cache metadata: %v", upsertErr)
	}
	logs := captureCacheTransferLogs(t)

	payload, found, restoreErr := harness.service.Restore(context.Background(), "token", harness.job.ID, "pod-uid", "go", harness.cacheKey)
	if restoreErr != nil || !found {
		t.Fatalf("restore found=%t err=%v", found, restoreErr)
	}
	defer func() { _ = payload.Archive.Close() }()
	if harness.store.restoreCalls != 0 || harness.store.openCalls != 1 {
		t.Fatalf("directory restore calls=%d open calls=%d, want 0 and 1", harness.store.restoreCalls, harness.store.openCalls)
	}
	wantDigest := "sha256:" + saved.Checksum
	if payload.Publication.ContentDigest != wantDigest {
		t.Fatalf("publication digest=%q, want %q", payload.Publication.ContentDigest, wantDigest)
	}
	if !strings.Contains(logs.String(), "transfer_mode=direct") {
		t.Fatalf("restore logs=%q, want direct mode", logs.String())
	}
}

func TestWorkspaceHelperCacheServiceRestoreMaterializesUnsupportedLegacyArchives(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		checksum    string
		compression string
	}{
		{name: "malformed checksum", checksum: "not-a-sha256", compression: "tar.gz"},
		{name: "unsupported compression", checksum: strings.Repeat("a", 64), compression: "zip"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			harness := newWorkspaceHelperCacheServiceTestHarness(t)
			source := cacheArchive(t, "cached.txt", "cache hit")
			objectKey := cacheObjectKey(cacheJobID(harness.build), "go", harness.cacheKey, source.publication.ContentDigest)
			if _, saveErr := harness.store.Save(context.Background(), objectKey, source.root); saveErr != nil {
				t.Fatalf("seed cache object: %v", saveErr)
			}
			entry := cacheEntryInput(harness, objectKey)
			entry.Checksum = testCase.checksum
			entry.Compression = testCase.compression
			if _, upsertErr := harness.entries.Upsert(context.Background(), entry); upsertErr != nil {
				t.Fatalf("seed cache metadata: %v", upsertErr)
			}
			logs := captureCacheTransferLogs(t)

			payload, found, restoreErr := harness.service.Restore(context.Background(), "token", harness.job.ID, "pod-uid", "go", harness.cacheKey)
			if restoreErr != nil || !found {
				t.Fatalf("restore found=%t err=%v", found, restoreErr)
			}
			defer func() { _ = payload.Archive.Close() }()
			if harness.store.restoreCalls != 1 || harness.store.openCalls != 0 {
				t.Fatalf("directory restore calls=%d open calls=%d, want 1 and 0", harness.store.restoreCalls, harness.store.openCalls)
			}
			if !strings.Contains(logs.String(), "transfer_mode=legacy_materialized") {
				t.Fatalf("restore logs=%q, want legacy materialized mode", logs.String())
			}
		})
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

func TestWorkspaceHelperCacheServiceSaveSkipsCanonicalUnchangedContentForAllPresets(t *testing.T) {
	for _, preset := range []string{"go-module", "go-build", "node"} {
		t.Run(preset, func(t *testing.T) {
			harness := newWorkspaceHelperCacheServiceTestHarnessForPreset(t, preset)
			harness.capabilities.expectedRole = domain.WorkspaceHelperRoleCacheSave
			archive := cacheArchive(t, "module", "unchanged")
			archiveBytes, readErr := io.ReadAll(archive.archive)
			if readErr != nil {
				t.Fatalf("read archive: %v", readErr)
			}
			if closeErr := archive.archive.Close(); closeErr != nil {
				t.Fatalf("close archive: %v", closeErr)
			}
			input := canonicalCacheEntryInput(harness, preset, harness.cacheKey, archive.publication)
			if _, upsertErr := harness.entries.Upsert(context.Background(), input); upsertErr != nil {
				t.Fatalf("seed entry: %v", upsertErr)
			}
			logs := captureCacheTransferLogs(t)

			if saveErr := harness.service.Save(context.Background(), "token", harness.job.ID, "pod-uid", preset, harness.cacheKey, bytes.NewReader(archiveBytes), archive.publication); saveErr != nil {
				t.Fatalf("save unchanged: %v", saveErr)
			}
			if harness.store.saveCalls != 0 || harness.store.archiveSaveCalls != 0 {
				t.Fatalf("store saves=%d archive saves=%d, want 0", harness.store.saveCalls, harness.store.archiveSaveCalls)
			}
			if !strings.Contains(logs.String(), "outcome=skipped_publish") {
				t.Fatalf("save logs=%q, want skipped_publish", logs.String())
			}
		})
	}
}

func TestWorkspaceHelperCacheServiceClaimedSaveCompletesExistingClaim(t *testing.T) {
	harness := newWorkspaceHelperCacheServiceTestHarness(t)
	harness.capabilities.expectedRole = domain.WorkspaceHelperRoleCacheSave
	claim, claimErr := harness.service.ClaimPublish(context.Background(), "token", harness.job.ID, "pod-uid", "go", harness.cacheKey)
	if claimErr != nil || !claim.Acquired || claim.ClaimToken == "" {
		t.Fatalf("claim=%+v err=%v", claim, claimErr)
	}
	archive := cacheArchive(t, "module", "claimed")
	defer func() { _ = archive.archive.Close() }()

	if saveErr := harness.service.SaveClaimed(context.Background(), "token", harness.job.ID, "pod-uid", "go", harness.cacheKey, claim.ClaimToken, archive.archive, archive.publication); saveErr != nil {
		t.Fatalf("save claimed cache: %v", saveErr)
	}
	entry, found, findErr := harness.entries.FindReadyByKey(context.Background(), cacheJobID(harness.build), "go", harness.cacheKey)
	if findErr != nil || !found || entry.ContentDigest != archive.publication.ContentDigest {
		t.Fatalf("entry=%+v found=%t err=%v", entry, found, findErr)
	}
	claimState := domain.CachePublishClaim{JobID: cacheJobID(harness.build), Preset: "go", CacheKey: harness.cacheKey, ClaimToken: claim.ClaimToken}
	if validateErr := harness.entries.ValidatePublishClaim(context.Background(), claimState, harness.job.ID, harness.service.now()); !errors.Is(validateErr, repository.ErrCachePublishClaimStale) {
		t.Fatalf("completed claim validation error=%v, want stale", validateErr)
	}
}

func TestWorkspaceHelperCacheServiceClaimedSaveCompletesUnreclaimedExpiredClaim(t *testing.T) {
	harness := newWorkspaceHelperCacheServiceTestHarness(t)
	harness.capabilities.expectedRole = domain.WorkspaceHelperRoleCacheSave
	now := time.Date(2026, time.September, 19, 12, 0, 0, 0, time.UTC)
	harness.service.now = func() time.Time { return now }
	claim, claimErr := harness.service.ClaimPublish(context.Background(), "token", harness.job.ID, "pod-uid", "go", harness.cacheKey)
	if claimErr != nil || !claim.Acquired {
		t.Fatalf("claim=%+v err=%v", claim, claimErr)
	}
	harness.service.now = func() time.Time { return now.Add(defaultWorkspaceHelperCachePublishLease + time.Second) }
	archive := cacheArchive(t, "module", "completed after archive delay")
	defer func() { _ = archive.archive.Close() }()

	if saveErr := harness.service.SaveClaimed(context.Background(), "token", harness.job.ID, "pod-uid", "go", harness.cacheKey, claim.ClaimToken, archive.archive, archive.publication); saveErr != nil {
		t.Fatalf("save unreclaimed expired claim: %v", saveErr)
	}
	entry, found, findErr := harness.entries.FindReadyByKey(context.Background(), cacheJobID(harness.build), "go", harness.cacheKey)
	if findErr != nil || !found || entry.ContentDigest != archive.publication.ContentDigest {
		t.Fatalf("entry=%+v found=%t err=%v", entry, found, findErr)
	}
}

func TestWorkspaceHelperCacheServiceDeduplicatesOnlyAfterClaimReplacement(t *testing.T) {
	harness := newWorkspaceHelperCacheServiceTestHarness(t)
	harness.capabilities.expectedRole = domain.WorkspaceHelperRoleCacheSave
	now := time.Date(2026, time.September, 19, 12, 0, 0, 0, time.UTC)
	harness.service.now = func() time.Time { return now }
	harness.store.archiveSaveHook = func() {
		now = now.Add(defaultWorkspaceHelperCachePublishLease)
		if _, acquired, claimErr := harness.entries.TryAcquirePublishClaim(context.Background(), cacheJobID(harness.build), "go", harness.cacheKey, "replacement-publisher", now, defaultWorkspaceHelperCachePublishLease); claimErr != nil || !acquired {
			t.Fatalf("replace active claim acquired=%t err=%v", acquired, claimErr)
		}
	}
	archive := cacheArchive(t, "module", "publisher A")
	defer func() { _ = archive.archive.Close() }()

	if saveErr := harness.service.Save(context.Background(), "token", harness.job.ID, "pod-uid", "go", harness.cacheKey, archive.archive, archive.publication); saveErr != nil {
		t.Fatalf("save after replacement: %v", saveErr)
	}
}

func TestWorkspaceHelperCacheServiceClaimedSaveRejectsInvalidAndReplacedClaimsBeforeUpload(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		claim func(*workspaceHelperCacheServiceTestHarness) (string, string)
	}{
		{name: "invalid token", claim: func(*workspaceHelperCacheServiceTestHarness) (string, string) {
			return "execution-job", "invalid"
		}},
		{name: "wrong owner", claim: func(harness *workspaceHelperCacheServiceTestHarness) (string, string) {
			claim, claimErr := harness.service.ClaimPublish(context.Background(), "token", harness.job.ID, "pod-uid", "go", harness.cacheKey)
			if claimErr != nil || !claim.Acquired {
				t.Fatalf("claim=%+v err=%v", claim, claimErr)
			}
			harness.capabilities.expectedJobID = "other-execution-job"
			return "other-execution-job", claim.ClaimToken
		}},
		{name: "replaced token", claim: func(harness *workspaceHelperCacheServiceTestHarness) (string, string) {
			now := time.Date(2026, time.September, 19, 12, 0, 0, 0, time.UTC)
			harness.service.now = func() time.Time { return now }
			claim, claimErr := harness.service.ClaimPublish(context.Background(), "token", harness.job.ID, "pod-uid", "go", harness.cacheKey)
			if claimErr != nil || !claim.Acquired {
				t.Fatalf("claim=%+v err=%v", claim, claimErr)
			}
			harness.service.now = func() time.Time { return now.Add(defaultWorkspaceHelperCachePublishLease) }
			replacement, replacementErr := harness.service.ClaimPublish(context.Background(), "token", harness.job.ID, "pod-uid", "go", harness.cacheKey)
			if replacementErr != nil || !replacement.Acquired || !replacement.Reclaimed {
				t.Fatalf("replacement=%+v err=%v", replacement, replacementErr)
			}
			return harness.job.ID, claim.ClaimToken
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			harness := newWorkspaceHelperCacheServiceTestHarness(t)
			harness.capabilities.expectedRole = domain.WorkspaceHelperRoleCacheSave
			archive := cacheArchive(t, "module", "rejected")
			defer func() { _ = archive.archive.Close() }()
			executionJobID, claimToken := testCase.claim(harness)

			saveErr := harness.service.SaveClaimed(context.Background(), "token", executionJobID, "pod-uid", "go", harness.cacheKey, claimToken, archive.archive, archive.publication)
			if !errors.Is(saveErr, ErrWorkspaceHelperCachePublishClaimInvalid) {
				t.Fatalf("save error=%v, want invalid publish claim", saveErr)
			}
			if harness.store.archiveSaveCalls != 0 {
				t.Fatalf("archive uploads=%d, want 0", harness.store.archiveSaveCalls)
			}
		})
	}
}

func TestWorkspaceHelperCacheServicePublishClaimDeduplicatesAndRecoversStaleClaim(t *testing.T) {
	harness := newWorkspaceHelperCacheServiceTestHarness(t)
	harness.capabilities.expectedRole = domain.WorkspaceHelperRoleCacheSave
	now := time.Date(2026, time.September, 19, 12, 0, 0, 0, time.UTC)
	harness.service.now = func() time.Time { return now }
	first, firstErr := harness.service.ClaimPublish(context.Background(), "token", harness.job.ID, "pod-uid", "go", harness.cacheKey)
	if firstErr != nil || !first.Acquired {
		t.Fatalf("first claim=%+v err=%v", first, firstErr)
	}
	second, secondErr := harness.service.ClaimPublish(context.Background(), "token", harness.job.ID, "pod-uid", "go", harness.cacheKey)
	if secondErr != nil || second.Acquired {
		t.Fatalf("concurrent claim=%+v err=%v", second, secondErr)
	}

	harness.service.now = func() time.Time { return now.Add(defaultWorkspaceHelperCachePublishLease) }
	recovered, recoveredErr := harness.service.ClaimPublish(context.Background(), "token", harness.job.ID, "pod-uid", "go", harness.cacheKey)
	if recoveredErr != nil || !recovered.Acquired || !recovered.Reclaimed || recovered.ClaimToken == first.ClaimToken {
		t.Fatalf("recovered claim=%+v err=%v", recovered, recoveredErr)
	}
}

func TestWorkspaceHelperCacheServiceFailedClaimedSaveReleasesClaim(t *testing.T) {
	harness := newWorkspaceHelperCacheServiceTestHarness(t)
	harness.capabilities.expectedRole = domain.WorkspaceHelperRoleCacheSave
	claim, claimErr := harness.service.ClaimPublish(context.Background(), "token", harness.job.ID, "pod-uid", "go", harness.cacheKey)
	if claimErr != nil || !claim.Acquired {
		t.Fatalf("claim=%+v err=%v", claim, claimErr)
	}
	harness.store.saveErr = errors.New("store unavailable")
	archive := cacheArchive(t, "module", "failed")
	defer func() { _ = archive.archive.Close() }()
	if saveErr := harness.service.SaveClaimed(context.Background(), "token", harness.job.ID, "pod-uid", "go", harness.cacheKey, claim.ClaimToken, archive.archive, archive.publication); saveErr == nil {
		t.Fatal("expected save failure")
	}
	harness.store.saveErr = nil

	retry, retryErr := harness.service.ClaimPublish(context.Background(), "token", harness.job.ID, "pod-uid", "go", harness.cacheKey)
	if retryErr != nil || !retry.Acquired {
		t.Fatalf("retry claim=%+v err=%v", retry, retryErr)
	}
}

func TestWorkspaceHelperCacheServiceReleasePublishClaimAllowsImmediateRetry(t *testing.T) {
	harness := newWorkspaceHelperCacheServiceTestHarness(t)
	harness.capabilities.expectedRole = domain.WorkspaceHelperRoleCacheSave
	claim, claimErr := harness.service.ClaimPublish(context.Background(), "token", harness.job.ID, "pod-uid", "go", harness.cacheKey)
	if claimErr != nil || !claim.Acquired {
		t.Fatalf("claim=%+v err=%v", claim, claimErr)
	}
	if releaseErr := harness.service.ReleasePublishClaim(context.Background(), "token", harness.job.ID, "pod-uid", "go", harness.cacheKey, claim.ClaimToken); releaseErr != nil {
		t.Fatalf("release claim: %v", releaseErr)
	}
	retry, retryErr := harness.service.ClaimPublish(context.Background(), "token", harness.job.ID, "pod-uid", "go", harness.cacheKey)
	if retryErr != nil || !retry.Acquired {
		t.Fatalf("retry claim=%+v err=%v", retry, retryErr)
	}
}

func TestWorkspaceHelperCacheServiceReleasePublishClaimCannotReleaseOtherOwner(t *testing.T) {
	harness := newWorkspaceHelperCacheServiceTestHarness(t)
	harness.capabilities.expectedRole = domain.WorkspaceHelperRoleCacheSave
	claim, claimErr := harness.service.ClaimPublish(context.Background(), "token", harness.job.ID, "pod-uid", "go", harness.cacheKey)
	if claimErr != nil || !claim.Acquired {
		t.Fatalf("claim=%+v err=%v", claim, claimErr)
	}
	harness.capabilities.expectedJobID = "other-execution-job"
	if releaseErr := harness.service.ReleasePublishClaim(context.Background(), "token", "other-execution-job", "pod-uid", "go", harness.cacheKey, claim.ClaimToken); releaseErr != nil {
		t.Fatalf("wrong-owner release: %v", releaseErr)
	}
	harness.capabilities.expectedJobID = harness.job.ID
	if releaseErr := harness.service.ReleasePublishClaim(context.Background(), "token", harness.job.ID, "pod-uid", "go", harness.cacheKey, "wrong-token"); releaseErr != nil {
		t.Fatalf("wrong-token release: %v", releaseErr)
	}
	claimState := domain.CachePublishClaim{JobID: cacheJobID(harness.build), Preset: "go", CacheKey: harness.cacheKey, ClaimToken: claim.ClaimToken}
	if validateErr := harness.entries.ValidatePublishClaim(context.Background(), claimState, harness.job.ID, harness.service.now()); validateErr != nil {
		t.Fatalf("owner claim was released: %v", validateErr)
	}
}

func TestWorkspaceHelperCacheServiceReleasePublishClaimIsIdempotentAfterCompletion(t *testing.T) {
	harness := newWorkspaceHelperCacheServiceTestHarness(t)
	harness.capabilities.expectedRole = domain.WorkspaceHelperRoleCacheSave
	claim, claimErr := harness.service.ClaimPublish(context.Background(), "token", harness.job.ID, "pod-uid", "go", harness.cacheKey)
	if claimErr != nil || !claim.Acquired {
		t.Fatalf("claim=%+v err=%v", claim, claimErr)
	}
	archive := cacheArchive(t, "module", "completed")
	defer func() { _ = archive.archive.Close() }()
	if saveErr := harness.service.SaveClaimed(context.Background(), "token", harness.job.ID, "pod-uid", "go", harness.cacheKey, claim.ClaimToken, archive.archive, archive.publication); saveErr != nil {
		t.Fatalf("complete claimed save: %v", saveErr)
	}
	if releaseErr := harness.service.ReleasePublishClaim(context.Background(), "token", harness.job.ID, "pod-uid", "go", harness.cacheKey, claim.ClaimToken); releaseErr != nil {
		t.Fatalf("release completed claim: %v", releaseErr)
	}
}

func TestWorkspaceHelperCacheServiceClaimedSavePreservesCanonicalSkip(t *testing.T) {
	harness := newWorkspaceHelperCacheServiceTestHarness(t)
	harness.capabilities.expectedRole = domain.WorkspaceHelperRoleCacheSave
	archive := cacheArchive(t, "module", "unchanged")
	defer func() { _ = archive.archive.Close() }()
	if _, upsertErr := harness.entries.Upsert(context.Background(), canonicalCacheEntryInput(harness, "go", harness.cacheKey, archive.publication)); upsertErr != nil {
		t.Fatalf("seed canonical entry: %v", upsertErr)
	}
	claim, claimErr := harness.service.ClaimPublish(context.Background(), "token", harness.job.ID, "pod-uid", "go", harness.cacheKey)
	if claimErr != nil || !claim.Acquired {
		t.Fatalf("claim=%+v err=%v", claim, claimErr)
	}

	if saveErr := harness.service.SaveClaimed(context.Background(), "token", harness.job.ID, "pod-uid", "go", harness.cacheKey, claim.ClaimToken, archive.archive, archive.publication); saveErr != nil {
		t.Fatalf("save canonical cache: %v", saveErr)
	}
	if harness.store.archiveSaveCalls != 0 {
		t.Fatalf("archive uploads=%d, want 0", harness.store.archiveSaveCalls)
	}
	retry, retryErr := harness.service.ClaimPublish(context.Background(), "token", harness.job.ID, "pod-uid", "go", harness.cacheKey)
	if retryErr != nil || !retry.Acquired {
		t.Fatalf("claim after canonical skip=%+v err=%v", retry, retryErr)
	}
}

func TestWorkspaceHelperCacheServiceSaveRepairsDivergentReadyEntry(t *testing.T) {
	testCases := []struct {
		name   string
		mutate func(*repository.CacheEntryUpsertInput)
	}{
		{name: "stale checksum", mutate: func(input *repository.CacheEntryUpsertInput) {
			input.Checksum = strings.Repeat("0", 64)
		}},
		{name: "wrong size", mutate: func(input *repository.CacheEntryUpsertInput) {
			input.SizeBytes++
		}},
		{name: "legacy mutable object key", mutate: func(input *repository.CacheEntryUpsertInput) {
			input.ObjectKey = "v1/jobs/logical-job/go/mutable"
		}},
		{name: "unsupported compression", mutate: func(input *repository.CacheEntryUpsertInput) {
			input.Compression = "zip"
		}},
		{name: "wrong storage provider", mutate: func(input *repository.CacheEntryUpsertInput) {
			input.StorageProvider = domain.StorageProviderGCS
		}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			harness := newWorkspaceHelperCacheServiceTestHarness(t)
			harness.capabilities.expectedRole = domain.WorkspaceHelperRoleCacheSave
			archive := cacheArchive(t, "module", "repair")
			archiveBytes, readErr := io.ReadAll(archive.archive)
			if readErr != nil {
				t.Fatalf("read archive: %v", readErr)
			}
			if closeErr := archive.archive.Close(); closeErr != nil {
				t.Fatalf("close archive: %v", closeErr)
			}
			input := canonicalCacheEntryInput(harness, "go", harness.cacheKey, archive.publication)
			testCase.mutate(&input)
			if _, upsertErr := harness.entries.Upsert(context.Background(), input); upsertErr != nil {
				t.Fatalf("seed divergent entry: %v", upsertErr)
			}
			logs := captureCacheTransferLogs(t)

			if saveErr := harness.service.Save(context.Background(), "token", harness.job.ID, "pod-uid", "go", harness.cacheKey, bytes.NewReader(archiveBytes), archive.publication); saveErr != nil {
				t.Fatalf("repair save: %v", saveErr)
			}
			if harness.store.archiveSaveCalls != 1 || harness.store.archivePromoteCalls != 1 {
				t.Fatalf("archive saves=%d promotions=%d, want 1 and 1", harness.store.archiveSaveCalls, harness.store.archivePromoteCalls)
			}
			if strings.Contains(logs.String(), "outcome=skipped_publish") {
				t.Fatalf("repair logs=%q, did not want skipped_publish", logs.String())
			}
			entry, found, findErr := harness.entries.FindReadyByKey(context.Background(), cacheJobID(harness.build), "go", harness.cacheKey)
			if findErr != nil || !found {
				t.Fatalf("repaired entry found=%t err=%v", found, findErr)
			}
			wantObjectKey := cacheObjectKey(cacheJobID(harness.build), "go", harness.cacheKey, archive.publication.ContentDigest)
			wantChecksum := strings.TrimPrefix(archive.publication.ContentDigest, "sha256:")
			if entry.Status != domain.CacheEntryStatusReady ||
				entry.ContentDigest != archive.publication.ContentDigest ||
				entry.Compression != "tar.gz" ||
				entry.Checksum != wantChecksum ||
				entry.SizeBytes != *archive.publication.SizeBytes ||
				entry.ObjectKey != wantObjectKey ||
				entry.StorageProvider != harness.store.Provider() {
				t.Fatalf("repaired entry=%#v, want canonical publication metadata", entry)
			}

			logs.Reset()
			if saveErr := harness.service.Save(context.Background(), "token", harness.job.ID, "pod-uid", "go", harness.cacheKey, bytes.NewReader(archiveBytes), archive.publication); saveErr != nil {
				t.Fatalf("save after repair: %v", saveErr)
			}
			if harness.store.archiveSaveCalls != 1 || harness.store.archivePromoteCalls != 1 {
				t.Fatalf("archive saves=%d promotions=%d after retry, want unchanged", harness.store.archiveSaveCalls, harness.store.archivePromoteCalls)
			}
			if !strings.Contains(logs.String(), "outcome=skipped_publish") {
				t.Fatalf("save-after-repair logs=%q, want skipped_publish", logs.String())
			}
		})
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
		{name: "publish claim role", role: domain.WorkspaceHelperRoleCacheRestore, jobID: "execution-job", podUID: "pod-uid", operation: func(h *workspaceHelperCacheServiceTestHarness) error {
			_, err := h.service.ClaimPublish(context.Background(), "token", h.job.ID, "pod-uid", "go", h.cacheKey)
			return err
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
	return newWorkspaceHelperCacheServiceTestHarnessForPreset(t, "go")
}

func newWorkspaceHelperCacheServiceTestHarnessForPreset(t *testing.T, preset string) *workspaceHelperCacheServiceTestHarness {
	t.Helper()
	logicalJobID := "logical-job"
	build := domain.Build{ID: "build-1", JobID: &logicalJobID}
	stepPreset := preset
	if preset == "go-module" || preset == "go-build" {
		stepPreset = "go"
	}
	step := domain.BuildStep{ID: "step-1", BuildID: build.ID, WorkingDir: ".", Cache: &domain.StepCacheConfig{Preset: stepPreset}}
	job := domain.ExecutionJob{ID: "execution-job", BuildID: build.ID, StepID: step.ID}
	capabilities := &workspaceHelperCacheCapabilityFake{expectedRole: domain.WorkspaceHelperRoleCacheRestore, expectedJobID: job.ID, expectedPodUID: "pod-uid"}
	store := &workspaceHelperCacheStoreFake{inner: cachepkg.NewFilesystemStore(t.TempDir())}
	entries := memoryrepo.NewCacheEntryRepository()
	service, err := NewWorkspaceHelperCacheService(WorkspaceHelperCacheServiceConfig{CapabilityAuthorizer: capabilities, ExecutionJobs: &workspaceHelperCacheExecutionJobFake{job: job}, Builds: &workspaceHelperCacheBuildFake{build: build, steps: []domain.BuildStep{step}}, Entries: entries, Store: store})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	return &workspaceHelperCacheServiceTestHarness{service: service, capabilities: capabilities, entries: entries, store: store, build: build, job: job, step: step, cacheKey: preset + ":" + strings.Repeat("a", 64)}
}

func captureCacheTransferLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	buffer := &bytes.Buffer{}
	previous := log.Writer()
	log.SetOutput(buffer)
	t.Cleanup(func() { log.SetOutput(previous) })
	return buffer
}

func cacheEntryInput(harness *workspaceHelperCacheServiceTestHarness, objectKey string) repository.CacheEntryUpsertInput {
	return repository.CacheEntryUpsertInput{JobID: cacheJobID(harness.build), Preset: "go", CacheKey: harness.cacheKey, StorageProvider: harness.store.Provider(), ObjectKey: objectKey, SizeBytes: 1, Checksum: "checksum", Compression: "tar.gz", Status: domain.CacheEntryStatusReady, CreatedByBuildID: harness.build.ID, CreatedByStepID: harness.step.ID}
}

func canonicalCacheEntryInput(harness *workspaceHelperCacheServiceTestHarness, preset string, cacheKey string, publication domain.WorkspaceRevisionPublication) repository.CacheEntryUpsertInput {
	return repository.CacheEntryUpsertInput{
		JobID:            cacheJobID(harness.build),
		Preset:           preset,
		CacheKey:         cacheKey,
		StorageProvider:  harness.store.Provider(),
		ObjectKey:        cacheObjectKey(cacheJobID(harness.build), preset, cacheKey, publication.ContentDigest),
		SizeBytes:        *publication.SizeBytes,
		Checksum:         strings.TrimPrefix(publication.ContentDigest, "sha256:"),
		ContentDigest:    publication.ContentDigest,
		Compression:      "tar.gz",
		Status:           domain.CacheEntryStatusReady,
		CreatedByBuildID: harness.build.ID,
		CreatedByStepID:  harness.step.ID,
	}
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
	archiveSaveHook     func()
	restoreMiss         bool
	restoreCalls        int
	openCalls           int
}

func (f *workspaceHelperCacheStoreFake) Provider() domain.StorageProvider { return f.inner.Provider() }
func (f *workspaceHelperCacheStoreFake) Restore(ctx context.Context, key string, destination string) (cachepkg.RestoreResult, error) {
	f.restoreCalls++
	if f.restoreMiss {
		return cachepkg.RestoreResult{}, nil
	}
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
	if f.archiveSaveHook != nil {
		f.archiveSaveHook()
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
