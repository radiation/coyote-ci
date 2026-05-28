package execution

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cachepkg "github.com/radiation/coyote-ci/backend/internal/cache"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/logs"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	memoryrepo "github.com/radiation/coyote-ci/backend/internal/repository/memory"
	"github.com/radiation/coyote-ci/backend/internal/runner"
)

func TestStepCacheManager_PrepareRestoreHitAddsMountsAndMarksEntry(t *testing.T) {
	ctx := context.Background()
	executionRoot := t.TempDir()
	executionContext := cacheExecutionContext(t, executionRoot, "package-lock.json")
	logSink := &recordingLogSink{}
	executionContext.ChunkAppender = logSink
	executionContext.HasChunkAppender = true
	logManager := NewExecutionLogManager(logSink, executionContext)
	entryRepo := memoryrepo.NewCacheEntryRepository()
	store := &recordingCacheStore{restoreResult: cachepkg.RestoreResult{Hit: true, SizeBytes: 123, Compression: cacheCompressionTarGz}}
	manager := NewStepCacheManager(store, entryRepo, executionRoot)

	cacheKey := expectedCacheKey(t, executionRoot, executionContext.Build.ID, "node", ".")
	entry, upsertErr := entryRepo.Upsert(ctx, repository.CacheEntryUpsertInput{
		JobID:           "job-1",
		Preset:          "node",
		CacheKey:        cacheKey,
		StorageProvider: domain.StorageProviderFilesystem,
		ObjectKey:       "objects/cache.tar.gz",
		Compression:     cacheCompressionTarGz,
		Status:          domain.CacheEntryStatusReady,
	})
	if upsertErr != nil {
		t.Fatalf("seed cache entry: %v", upsertErr)
	}

	prepared, prepareErr := manager.Prepare(ctx, executionContext, logManager)
	if prepareErr != nil {
		t.Fatalf("prepare cache: %v", prepareErr)
	}
	if !prepared.Enabled {
		t.Fatal("expected prepared cache to be enabled")
	}
	if prepared.CacheKey != cacheKey || prepared.Fingerprint != strings.TrimPrefix(cacheKey, "node:") {
		t.Fatalf("expected cache key %q and fingerprint derived from it, got key=%q fingerprint=%q", cacheKey, prepared.CacheKey, prepared.Fingerprint)
	}
	if len(prepared.Mounts) != 1 || prepared.Mounts[0].ContainerPath != "/root/.npm" {
		t.Fatalf("expected node cache mount, got %+v", prepared.Mounts)
	}
	if store.restoreKey != entry.ObjectKey || store.restoreDestination == "" {
		t.Fatalf("expected restore for object %q, got key=%q destination=%q", entry.ObjectKey, store.restoreKey, store.restoreDestination)
	}
	if prepared.MetadataEntry == nil || prepared.MetadataEntry.ID != entry.ID {
		t.Fatalf("expected metadata entry %q, got %+v", entry.ID, prepared.MetadataEntry)
	}
	updated, found, findErr := entryRepo.FindReadyByKey(ctx, "job-1", "node", cacheKey)
	if findErr != nil {
		t.Fatalf("find updated cache entry: %v", findErr)
	}
	if !found || updated.LastAccessedAt == nil {
		t.Fatalf("expected cache entry access timestamp, got found=%v entry=%+v", found, updated)
	}
	if !containsRecordedChunk(logSink.chunks, "cache lookup: preset=node") || !containsRecordedChunk(logSink.chunks, "hit=true") {
		t.Fatalf("expected cache hit log chunks, got %#v", logSink.chunks)
	}
}

func TestStepCacheManager_PrepareMissingLockfileSkipsRestore(t *testing.T) {
	ctx := context.Background()
	executionRoot := t.TempDir()
	executionContext := cacheExecutionContext(t, executionRoot, "")
	logSink := &recordingLogSink{}
	executionContext.ChunkAppender = logSink
	executionContext.HasChunkAppender = true
	store := &recordingCacheStore{}
	manager := NewStepCacheManager(store, memoryrepo.NewCacheEntryRepository(), executionRoot)

	prepared, prepareErr := manager.Prepare(ctx, executionContext, NewExecutionLogManager(logSink, executionContext))
	if prepareErr != nil {
		t.Fatalf("prepare cache: %v", prepareErr)
	}
	if prepared.Enabled {
		t.Fatalf("expected cache to be disabled when lockfile is missing, got %+v", prepared)
	}
	if store.restoreCalls != 0 {
		t.Fatalf("expected no restore calls, got %d", store.restoreCalls)
	}
	if !containsRecordedChunk(logSink.chunks, "reason=lockfile_missing") {
		t.Fatalf("expected missing lockfile log chunk, got %#v", logSink.chunks)
	}
}

func TestStepCacheManager_SaveStoresSuccessfulCacheMetadata(t *testing.T) {
	ctx := context.Background()
	executionRoot := t.TempDir()
	executionContext := cacheExecutionContext(t, executionRoot, "package-lock.json")
	logSink := &recordingLogSink{}
	executionContext.ChunkAppender = logSink
	executionContext.HasChunkAppender = true
	entryRepo := memoryrepo.NewCacheEntryRepository()
	store := &recordingCacheStore{saveResult: cachepkg.SaveResult{SizeBytes: 456, Checksum: "sha256:abc", Compression: cacheCompressionTarGz}}
	manager := NewStepCacheManager(store, entryRepo, executionRoot)
	prepared := PreparedStepCache{
		Enabled:    true,
		Policy:     domain.CachePolicyPullPush,
		Preset:     cachepkg.Preset{Name: "node", CachePaths: []string{"/root/.npm"}},
		CacheKey:   "node:fingerprint",
		RuntimeDir: t.TempDir(),
	}

	saveErr := manager.Save(ctx, executionContext, NewExecutionLogManager(logSink, executionContext), prepared, runner.RunStepResult{Status: runner.RunStepStatusSuccess})
	if saveErr != nil {
		t.Fatalf("save cache: %v", saveErr)
	}
	if store.saveCalls != 1 || store.saveSource != prepared.RuntimeDir {
		t.Fatalf("expected one save from runtime dir, got calls=%d source=%q", store.saveCalls, store.saveSource)
	}
	if !strings.HasPrefix(store.saveKey, "v1/jobs/job-1/node/") {
		t.Fatalf("expected sanitized job-scoped object key, got %q", store.saveKey)
	}
	entry, found, findErr := entryRepo.FindReadyByKey(ctx, "job-1", "node", prepared.CacheKey)
	if findErr != nil {
		t.Fatalf("find saved cache metadata: %v", findErr)
	}
	if !found {
		t.Fatal("expected saved cache metadata")
	}
	if entry.ObjectKey != store.saveKey || entry.SizeBytes != 456 || entry.CreatedByBuildID != executionContext.Build.ID || entry.CreatedByStepID != executionContext.ExecutionRequest.StepID {
		t.Fatalf("unexpected cache metadata: %+v", entry)
	}
	if !containsRecordedChunk(logSink.chunks, "cache save end: preset=node") {
		t.Fatalf("expected cache save log chunk, got %#v", logSink.chunks)
	}
}

func TestStepCacheManager_SaveSkipsFailedStep(t *testing.T) {
	ctx := context.Background()
	executionRoot := t.TempDir()
	executionContext := cacheExecutionContext(t, executionRoot, "package-lock.json")
	logSink := &recordingLogSink{}
	executionContext.ChunkAppender = logSink
	executionContext.HasChunkAppender = true
	store := &recordingCacheStore{}
	manager := NewStepCacheManager(store, memoryrepo.NewCacheEntryRepository(), executionRoot)
	prepared := PreparedStepCache{Enabled: true, Policy: domain.CachePolicyPullPush, Preset: cachepkg.Preset{Name: "node"}, CacheKey: "node:fingerprint", RuntimeDir: t.TempDir()}

	saveErr := manager.Save(ctx, executionContext, NewExecutionLogManager(logSink, executionContext), prepared, runner.RunStepResult{Status: runner.RunStepStatusFailed})
	if saveErr != nil {
		t.Fatalf("save cache after failed step: %v", saveErr)
	}
	if store.saveCalls != 0 {
		t.Fatalf("expected no save calls, got %d", store.saveCalls)
	}
	if !containsRecordedChunk(logSink.chunks, "step not successful") {
		t.Fatalf("expected skip log chunk, got %#v", logSink.chunks)
	}
}

func TestStepCacheManager_EffectiveJobIDFallbacks(t *testing.T) {
	jobID := " job-1 "
	if got := EffectiveJobID(StepExecutionContext{Build: domain.Build{ID: "build-1", JobID: &jobID}}); got != "job-1" {
		t.Fatalf("expected build job id, got %q", got)
	}
	if got := EffectiveJobID(StepExecutionContext{Build: domain.Build{ID: " build-1 "}}); got != "build:build-1" {
		t.Fatalf("expected build-scoped fallback, got %q", got)
	}
	if got := EffectiveJobID(StepExecutionContext{PersistedJob: &domain.ExecutionJob{ID: " persisted-job "}}); got != "persisted-job" {
		t.Fatalf("expected persisted job fallback, got %q", got)
	}
	if got := EffectiveJobID(StepExecutionContext{ExecutionRequest: runner.RunStepRequest{JobID: " request-job "}}); got != "request-job" {
		t.Fatalf("expected request job fallback, got %q", got)
	}
}

func cacheExecutionContext(t *testing.T, executionRoot string, lockfile string) StepExecutionContext {
	t.Helper()
	workspaceRoot := t.TempDir()
	buildID := "build-1"
	buildWorkspace := filepath.Join(executionRoot, buildID)
	if mkdirErr := copyTestWorkspace(workspaceRoot, buildWorkspace, lockfile); mkdirErr != nil {
		t.Fatalf("create workspace: %v", mkdirErr)
	}
	jobID := "job-1"
	executionContext := testExecutionContext(&recordingLogSink{})
	executionContext.Build.ID = buildID
	executionContext.Build.JobID = &jobID
	executionContext.ExecutionRequest.BuildID = buildID
	executionContext.ExecutionRequest.JobID = ""
	executionContext.ExecutionRequest.StepID = "install/npm"
	executionContext.Step = &domain.BuildStep{
		ID:         "install/npm",
		StepIndex:  0,
		Name:       "Install dependencies",
		WorkingDir: ".",
		Cache:      &domain.StepCacheConfig{Preset: "node", Policy: domain.CachePolicyPullPush},
	}
	return executionContext
}

func copyTestWorkspace(sourceRoot string, destinationRoot string, lockfile string) error {
	if mkdirErr := os.MkdirAll(destinationRoot, 0o755); mkdirErr != nil {
		return mkdirErr
	}
	if strings.TrimSpace(lockfile) == "" {
		return nil
	}
	return os.WriteFile(filepath.Join(destinationRoot, lockfile), []byte("lockfile-v1"), 0o644)
}

func expectedCacheKey(t *testing.T, executionRoot string, buildID string, presetName string, workingDir string) string {
	t.Helper()
	preset, presetErr := cachepkg.ResolvePreset(presetName, workingDir)
	if presetErr != nil {
		t.Fatalf("resolve preset: %v", presetErr)
	}
	fingerprint, _, fingerprintErr := cachepkg.ComputeFingerprint(filepath.Join(executionRoot, buildID), preset.FingerprintFiles)
	if fingerprintErr != nil {
		t.Fatalf("compute fingerprint: %v", fingerprintErr)
	}
	return preset.Name + ":" + fingerprint
}

func containsRecordedLine(lines []string, fragment string) bool {
	for _, line := range lines {
		if strings.Contains(line, fragment) {
			return true
		}
	}
	return false
}

func containsRecordedChunk(chunks []logs.StepLogChunk, fragment string) bool {
	for _, chunk := range chunks {
		if strings.Contains(chunk.ChunkText, fragment) {
			return true
		}
	}
	return false
}

type recordingCacheStore struct {
	restoreResult      cachepkg.RestoreResult
	restoreErr         error
	restoreCalls       int
	restoreKey         string
	restoreDestination string
	saveResult         cachepkg.SaveResult
	saveErr            error
	saveCalls          int
	saveKey            string
	saveSource         string
}

func (s *recordingCacheStore) Provider() domain.StorageProvider {
	return domain.StorageProviderFilesystem
}

func (s *recordingCacheStore) Restore(_ context.Context, key string, destinationRoot string) (cachepkg.RestoreResult, error) {
	s.restoreCalls++
	s.restoreKey = key
	s.restoreDestination = destinationRoot
	if s.restoreErr != nil {
		return cachepkg.RestoreResult{}, s.restoreErr
	}
	return s.restoreResult, nil
}

func (s *recordingCacheStore) Save(_ context.Context, key string, sourceRoot string) (cachepkg.SaveResult, error) {
	s.saveCalls++
	s.saveKey = key
	s.saveSource = sourceRoot
	if s.saveErr != nil {
		return cachepkg.SaveResult{}, s.saveErr
	}
	return s.saveResult, nil
}
