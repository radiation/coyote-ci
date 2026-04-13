package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	cachepkg "github.com/radiation/coyote-ci/backend/internal/cache"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/repository/memory"
	"github.com/radiation/coyote-ci/backend/internal/runner"
)

type fakeCacheStore struct {
	restoreCalls int
	saveCalls    int
	saveErr      error
	objects      map[string]cachepkg.SaveResult
}

func (s *fakeCacheStore) Provider() domain.StorageProvider {
	return domain.StorageProviderFilesystem
}

func (s *fakeCacheStore) Restore(_ context.Context, key string, _ string) (cachepkg.RestoreResult, error) {
	s.restoreCalls++
	value, ok := s.objects[key]
	if !ok {
		return cachepkg.RestoreResult{Hit: false, Compression: "tar.gz"}, nil
	}
	return cachepkg.RestoreResult{Hit: true, SizeBytes: value.SizeBytes, Compression: value.Compression}, nil
}

func (s *fakeCacheStore) Save(_ context.Context, key string, _ string) (cachepkg.SaveResult, error) {
	s.saveCalls++
	if s.saveErr != nil {
		return cachepkg.SaveResult{}, s.saveErr
	}
	result := cachepkg.SaveResult{SizeBytes: 42, Checksum: "sum", Compression: "tar.gz"}
	s.objects[key] = result
	return result, nil
}

type failingUpsertRepo struct {
	inner repository.CacheEntryRepository
}

func (r *failingUpsertRepo) FindReadyByKey(ctx context.Context, jobID string, preset string, cacheKey string) (domain.CacheEntry, bool, error) {
	return r.inner.FindReadyByKey(ctx, jobID, preset, cacheKey)
}

func (r *failingUpsertRepo) Upsert(_ context.Context, _ repository.CacheEntryUpsertInput) (domain.CacheEntry, error) {
	return domain.CacheEntry{}, errors.New("metadata upsert failed")
}

func (r *failingUpsertRepo) MarkAccessed(ctx context.Context, id string, accessedAt time.Time) error {
	return r.inner.MarkAccessed(ctx, id, accessedAt)
}

func TestStepCacheManager_PushPolicySkipsRestoreAndSaves(t *testing.T) {
	workspaceRoot := t.TempDir()
	buildID := "build-1"
	buildWorkspace := filepath.Join(workspaceRoot, buildID)
	if err := os.MkdirAll(buildWorkspace, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(buildWorkspace, "go.sum"), []byte("sum"), 0o644); err != nil {
		t.Fatalf("write go.sum: %v", err)
	}

	store := &fakeCacheStore{objects: map[string]cachepkg.SaveResult{}}
	repo := memory.NewCacheEntryRepository()
	manager := NewStepCacheManager(store, repo, workspaceRoot)

	svc := NewBuildService(&fakeBuildRepository{}, &fakeRunner{}, &fakeLogSink{})
	ctx := stepContext(buildID, "job-1", "step-1", "go", domain.CachePolicyPush)
	logManager := NewExecutionLogManager(svc, ctx)

	prepared, err := manager.Prepare(context.Background(), ctx, logManager)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if !prepared.Enabled {
		t.Fatal("expected prepared cache to be enabled for push policy")
	}
	if store.restoreCalls != 0 {
		t.Fatalf("expected no restore calls for push policy, got %d", store.restoreCalls)
	}

	if err := manager.Save(context.Background(), ctx, logManager, prepared, runner.RunStepResult{Status: runner.RunStepStatusSuccess}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if store.saveCalls != 1 {
		t.Fatalf("expected one save call, got %d", store.saveCalls)
	}
}

func TestStepCacheManager_FailedReplacementDoesNotClobberReadyCache(t *testing.T) {
	workspaceRoot := t.TempDir()
	buildID := "build-1"
	buildWorkspace := filepath.Join(workspaceRoot, buildID)
	if err := os.MkdirAll(buildWorkspace, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(buildWorkspace, "go.sum"), []byte("sum"), 0o644); err != nil {
		t.Fatalf("write go.sum: %v", err)
	}

	store := &fakeCacheStore{objects: map[string]cachepkg.SaveResult{}}
	repo := memory.NewCacheEntryRepository()
	manager := NewStepCacheManager(store, repo, workspaceRoot)
	svc := NewBuildService(&fakeBuildRepository{}, &fakeRunner{}, &fakeLogSink{})

	ctx := stepContext(buildID, "job-1", "step-1", "go", domain.CachePolicyPullPush)
	cacheJobID := effectiveJobID(ctx)
	logManager := NewExecutionLogManager(svc, ctx)
	prepared, err := manager.Prepare(context.Background(), ctx, logManager)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}

	store.objects["old-object"] = cachepkg.SaveResult{SizeBytes: 11, Checksum: "old", Compression: "tar.gz"}
	_, err = repo.Upsert(context.Background(), repository.CacheEntryUpsertInput{
		JobID:            cacheJobID,
		Preset:           "go",
		CacheKey:         prepared.CacheKey,
		StorageProvider:  domain.StorageProviderFilesystem,
		ObjectKey:        "old-object",
		SizeBytes:        11,
		Checksum:         "old",
		Compression:      "tar.gz",
		Status:           domain.CacheEntryStatusReady,
		CreatedByBuildID: buildID,
		CreatedByStepID:  "step-1",
	})
	if err != nil {
		t.Fatalf("seed ready cache entry: %v", err)
	}

	store.saveErr = errors.New("upload failed")
	err = manager.Save(context.Background(), ctx, logManager, prepared, runner.RunStepResult{Status: runner.RunStepStatusSuccess})
	if err == nil {
		t.Fatal("expected save error")
	}

	entry, found, err := repo.FindReadyByKey(context.Background(), cacheJobID, "go", prepared.CacheKey)
	if err != nil {
		t.Fatalf("find ready after failed refresh: %v", err)
	}
	if !found {
		t.Fatal("expected previous ready cache to remain")
	}
	if entry.ObjectKey != "old-object" {
		t.Fatalf("expected old object key to remain, got %q", entry.ObjectKey)
	}

	store.saveErr = nil
	logManager2 := NewExecutionLogManager(svc, ctx)
	prepared2, err := manager.Prepare(context.Background(), ctx, logManager2)
	if err != nil {
		t.Fatalf("prepare second run: %v", err)
	}
	if prepared2.MetadataEntry == nil || prepared2.MetadataEntry.ObjectKey != "old-object" {
		t.Fatal("expected restore metadata to still point at old ready cache")
	}
}

func TestStepCacheManager_MissingLockfileSkipsCache(t *testing.T) {
	workspaceRoot := t.TempDir()
	buildID := "build-1"
	buildWorkspace := filepath.Join(workspaceRoot, buildID)
	if err := os.MkdirAll(buildWorkspace, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	store := &fakeCacheStore{objects: map[string]cachepkg.SaveResult{}}
	repo := memory.NewCacheEntryRepository()
	manager := NewStepCacheManager(store, repo, workspaceRoot)
	svc := NewBuildService(&fakeBuildRepository{}, &fakeRunner{}, &fakeLogSink{})
	ctx := stepContext(buildID, "job-1", "step-1", "go", domain.CachePolicyPullPush)
	logManager := NewExecutionLogManager(svc, ctx)

	prepared, err := manager.Prepare(context.Background(), ctx, logManager)
	if err != nil {
		t.Fatalf("prepare should not fail on missing lockfile: %v", err)
	}
	if prepared.Enabled {
		t.Fatal("expected cache to be skipped when lockfile is missing")
	}
	if store.restoreCalls != 0 {
		t.Fatalf("expected no restore calls, got %d", store.restoreCalls)
	}

	if err := manager.Save(context.Background(), ctx, logManager, prepared, runner.RunStepResult{Status: runner.RunStepStatusSuccess}); err != nil {
		t.Fatalf("save should be noop when cache is skipped: %v", err)
	}
	if store.saveCalls != 0 {
		t.Fatalf("expected no save calls, got %d", store.saveCalls)
	}
}

func TestStepCacheManager_MetadataWriteFailureIsReturned(t *testing.T) {
	workspaceRoot := t.TempDir()
	buildID := "build-1"
	buildWorkspace := filepath.Join(workspaceRoot, buildID)
	if err := os.MkdirAll(buildWorkspace, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(buildWorkspace, "go.sum"), []byte("sum"), 0o644); err != nil {
		t.Fatalf("write go.sum: %v", err)
	}

	store := &fakeCacheStore{objects: map[string]cachepkg.SaveResult{}}
	repo := &failingUpsertRepo{inner: memory.NewCacheEntryRepository()}
	manager := NewStepCacheManager(store, repo, workspaceRoot)
	svc := NewBuildService(&fakeBuildRepository{}, &fakeRunner{}, &fakeLogSink{})
	ctx := stepContext(buildID, "job-1", "step-1", "go", domain.CachePolicyPush)
	logManager := NewExecutionLogManager(svc, ctx)

	prepared, err := manager.Prepare(context.Background(), ctx, logManager)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	err = manager.Save(context.Background(), ctx, logManager, prepared, runner.RunStepResult{Status: runner.RunStepStatusSuccess})
	if err == nil {
		t.Fatal("expected metadata write failure to be returned")
	}
}

func TestStepCacheManager_PolicySemantics(t *testing.T) {
	workspaceRoot := t.TempDir()
	buildID := "build-1"
	buildWorkspace := filepath.Join(workspaceRoot, buildID)
	if err := os.MkdirAll(buildWorkspace, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(buildWorkspace, "go.sum"), []byte("sum"), 0o644); err != nil {
		t.Fatalf("write go.sum: %v", err)
	}

	cases := []struct {
		name                string
		policy              domain.CachePolicy
		expectPrepareEnable bool
		expectRestoreCalls  int
		expectSaveCalls     int
	}{
		{name: "off", policy: domain.CachePolicyOff, expectPrepareEnable: false, expectRestoreCalls: 0, expectSaveCalls: 0},
		{name: "pull", policy: domain.CachePolicyPull, expectPrepareEnable: true, expectRestoreCalls: 1, expectSaveCalls: 0},
		{name: "push", policy: domain.CachePolicyPush, expectPrepareEnable: true, expectRestoreCalls: 0, expectSaveCalls: 1},
		{name: "pull-push", policy: domain.CachePolicyPullPush, expectPrepareEnable: true, expectRestoreCalls: 1, expectSaveCalls: 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeCacheStore{objects: map[string]cachepkg.SaveResult{}}
			repo := memory.NewCacheEntryRepository()
			manager := NewStepCacheManager(store, repo, workspaceRoot)
			svc := NewBuildService(&fakeBuildRepository{}, &fakeRunner{}, &fakeLogSink{})
			ctx := stepContext(buildID, "job-1", "step-1", "go", tc.policy)
			cacheJobID := effectiveJobID(ctx)
			logManager := NewExecutionLogManager(svc, ctx)

			// Seed a ready entry so restore path is exercised for pull and pull-push.
			preparedSeed, err := manager.Prepare(context.Background(), stepContext(buildID, "job-1", "step-1", "go", domain.CachePolicyPush), logManager)
			if err != nil {
				t.Fatalf("seed prepare: %v", err)
			}
			store.objects["ready-object"] = cachepkg.SaveResult{SizeBytes: 9, Checksum: "seed", Compression: "tar.gz"}
			_, err = repo.Upsert(context.Background(), repository.CacheEntryUpsertInput{
				JobID:            cacheJobID,
				Preset:           "go",
				CacheKey:         preparedSeed.CacheKey,
				StorageProvider:  domain.StorageProviderFilesystem,
				ObjectKey:        "ready-object",
				SizeBytes:        9,
				Checksum:         "seed",
				Compression:      "tar.gz",
				Status:           domain.CacheEntryStatusReady,
				CreatedByBuildID: buildID,
				CreatedByStepID:  "step-1",
			})
			if err != nil {
				t.Fatalf("seed upsert: %v", err)
			}

			prepared, err := manager.Prepare(context.Background(), ctx, logManager)
			if err != nil {
				t.Fatalf("prepare: %v", err)
			}
			if prepared.Enabled != tc.expectPrepareEnable {
				t.Fatalf("expected prepared enabled=%t, got %t", tc.expectPrepareEnable, prepared.Enabled)
			}
			if store.restoreCalls != tc.expectRestoreCalls {
				t.Fatalf("expected restore calls=%d, got %d", tc.expectRestoreCalls, store.restoreCalls)
			}

			err = manager.Save(context.Background(), ctx, logManager, prepared, runner.RunStepResult{Status: runner.RunStepStatusSuccess})
			if err != nil {
				t.Fatalf("save: %v", err)
			}
			if store.saveCalls != tc.expectSaveCalls {
				t.Fatalf("expected save calls=%d, got %d", tc.expectSaveCalls, store.saveCalls)
			}
		})
	}
}

func TestPresetMounts_ReturnsErrorWhenPathCreationFails(t *testing.T) {
	runtimeDir := t.TempDir()
	pathsFile := filepath.Join(runtimeDir, "paths")
	if err := os.WriteFile(pathsFile, []byte("not-a-directory"), 0o644); err != nil {
		t.Fatalf("seed paths file: %v", err)
	}

	mounts, err := presetMounts(runtimeDir, []string{"/go/pkg/mod"})
	if err == nil {
		t.Fatal("expected presetMounts to fail when parent path is not a directory")
	}
	if mounts != nil {
		t.Fatalf("expected nil mounts on error, got %d", len(mounts))
	}
}

func TestStepCacheManager_UsesStableBuildJobIDAcrossExecutionJobIDs(t *testing.T) {
	workspaceRoot := t.TempDir()
	buildID := "build-1"
	buildWorkspace := filepath.Join(workspaceRoot, buildID)
	if err := os.MkdirAll(buildWorkspace, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(buildWorkspace, "go.sum"), []byte("sum"), 0o644); err != nil {
		t.Fatalf("write go.sum: %v", err)
	}

	store := &fakeCacheStore{objects: map[string]cachepkg.SaveResult{}}
	repo := memory.NewCacheEntryRepository()
	manager := NewStepCacheManager(store, repo, workspaceRoot)
	svc := NewBuildService(&fakeBuildRepository{}, &fakeRunner{}, &fakeLogSink{})

	ctxStep1 := stepContextWithBuildJobID(buildID, "logical-job-1", "exec-job-step-1", "step-1", "go", domain.CachePolicyPullPush)
	logManagerStep1 := NewExecutionLogManager(svc, ctxStep1)
	preparedStep1, err := manager.Prepare(context.Background(), ctxStep1, logManagerStep1)
	if err != nil {
		t.Fatalf("prepare step 1: %v", err)
	}
	err = manager.Save(context.Background(), ctxStep1, logManagerStep1, preparedStep1, runner.RunStepResult{Status: runner.RunStepStatusSuccess})
	if err != nil {
		t.Fatalf("save step 1: %v", err)
	}

	_, found, err := repo.FindReadyByKey(context.Background(), "logical-job-1", "go", preparedStep1.CacheKey)
	if err != nil {
		t.Fatalf("find ready by logical job id: %v", err)
	}
	if !found {
		t.Fatal("expected ready cache entry under logical build job id")
	}
	_, found, err = repo.FindReadyByKey(context.Background(), "exec-job-step-1", "go", preparedStep1.CacheKey)
	if err != nil {
		t.Fatalf("find ready by execution job id: %v", err)
	}
	if found {
		t.Fatal("did not expect ready cache entry under execution job id")
	}

	ctxStep2 := stepContextWithBuildJobID(buildID, "logical-job-1", "exec-job-step-2", "step-2", "go", domain.CachePolicyPullPush)
	logManagerStep2 := NewExecutionLogManager(svc, ctxStep2)
	preparedStep2, err := manager.Prepare(context.Background(), ctxStep2, logManagerStep2)
	if err != nil {
		t.Fatalf("prepare step 2: %v", err)
	}

	if preparedStep2.MetadataEntry == nil {
		t.Fatal("expected cache hit for second step using same logical job id")
	}
	if store.restoreCalls != 1 {
		t.Fatalf("expected one restore call for second-step lookup, got %d", store.restoreCalls)
	}
}

func TestEffectiveJobID_PrefersStableLogicalIdentity(t *testing.T) {
	logicalJobID := "logical-job"
	ctx := StepExecutionContext{
		Build: domain.Build{ID: "build-1", JobID: &logicalJobID},
		PersistedJob: &domain.ExecutionJob{
			ID: "exec-job-id",
		},
		ExecutionRequest: runner.RunStepRequest{JobID: "request-job-id"},
	}

	if got := effectiveJobID(ctx); got != logicalJobID {
		t.Fatalf("expected logical build job id, got %q", got)
	}

	ctx.Build.JobID = nil
	if got := effectiveJobID(ctx); got != "build:build-1" {
		t.Fatalf("expected stable build fallback, got %q", got)
	}

	ctx.Build.ID = ""
	if got := effectiveJobID(ctx); got != "exec-job-id" {
		t.Fatalf("expected persisted execution job id fallback, got %q", got)
	}

	ctx.PersistedJob = nil
	if got := effectiveJobID(ctx); got != "request-job-id" {
		t.Fatalf("expected request execution job id fallback, got %q", got)
	}
}

func stepContext(buildID string, jobID string, stepID string, preset string, policy domain.CachePolicy) StepExecutionContext {
	return StepExecutionContext{
		Build: domain.Build{ID: buildID},
		Step: &domain.BuildStep{
			ID:         stepID,
			WorkingDir: ".",
			Cache: &domain.StepCacheConfig{
				Preset: preset,
				Policy: policy,
			},
		},
		ExecutionRequest: runner.RunStepRequest{
			BuildID: buildID,
			JobID:   jobID,
			StepID:  stepID,
		},
	}
}

func stepContextWithBuildJobID(buildID string, buildJobID string, executionJobID string, stepID string, preset string, policy domain.CachePolicy) StepExecutionContext {
	ctx := stepContext(buildID, executionJobID, stepID, preset, policy)
	ctx.Build.JobID = &buildJobID
	return ctx
}
