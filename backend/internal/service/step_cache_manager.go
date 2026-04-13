package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	cachepkg "github.com/radiation/coyote-ci/backend/internal/cache"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/runner"
)

type preparedStepCache struct {
	Enabled    bool
	Key        string
	Scope      domain.CacheScope
	RuntimeDir string
	Mounts     []runner.CacheMount
}

type StepCacheManager struct {
	store             cachepkg.Store
	executionRootPath string
}

type cacheStoreSizeReporter interface {
	TotalSizeBytes() (int64, error)
}

func NewStepCacheManager(store cachepkg.Store, executionRootPath string) *StepCacheManager {
	return &StepCacheManager{
		store:             store,
		executionRootPath: strings.TrimSpace(executionRootPath),
	}
}

func (m *StepCacheManager) Prepare(ctx context.Context, executionContext StepExecutionContext, logManager *ExecutionLogManager) (preparedStepCache, error) {
	if m == nil || m.store == nil {
		return preparedStepCache{}, nil
	}
	if executionContext.Step == nil || executionContext.Step.Cache == nil {
		logManager.EmitSystemLine(ctx, "Cache: not configured")
		return preparedStepCache{}, nil
	}

	cacheConfig := executionContext.Step.Cache.Clone()
	workspaceRoot, err := m.workspaceRootForBuild(executionContext.Build.ID)
	if err != nil {
		return preparedStepCache{}, err
	}

	filesDigest, err := digestCacheKeyFiles(workspaceRoot, cacheConfig.KeyFiles)
	if err != nil {
		return preparedStepCache{}, err
	}

	key, err := cachepkg.ResolveKey(cachepkg.KeyInput{
		Scope:          string(cacheConfig.Scope),
		BuildID:        buildScopedID(cacheConfig.Scope, executionContext.Build.ID),
		JobIdentity:    jobScopedIdentity(cacheConfig.Scope, executionContext),
		Image:          executionContext.ExecutionImage,
		Platform:       runtime.GOOS + "/" + runtime.GOARCH,
		Paths:          cacheConfig.Paths,
		KeyFilesDigest: filesDigest,
	})
	if err != nil {
		return preparedStepCache{}, err
	}

	runtimeDir := filepath.Join(workspaceRoot, ".coyote", "cache-runtime", sanitizeStepDirName(executionContext.ExecutionRequest.StepID, executionContext.ExecutionRequest.StepIndex))
	if removeErr := os.RemoveAll(runtimeDir); removeErr != nil {
		return preparedStepCache{}, removeErr
	}
	if mkdirErr := os.MkdirAll(runtimeDir, 0o755); mkdirErr != nil {
		return preparedStepCache{}, mkdirErr
	}

	restoreStarted := time.Now()
	hit, err := m.store.Restore(ctx, key, runtimeDir)
	if err != nil {
		return preparedStepCache{}, err
	}

	mounts := make([]runner.CacheMount, 0, len(cacheConfig.Paths))
	for idx, cachePath := range cacheConfig.Paths {
		hostPath := filepath.Join(runtimeDir, "paths", fmt.Sprintf("%03d", idx))
		if mkdirErr := os.MkdirAll(hostPath, 0o755); mkdirErr != nil {
			return preparedStepCache{}, mkdirErr
		}
		mounts = append(mounts, runner.CacheMount{HostPath: hostPath, ContainerPath: cachePath})
	}

	restoredBytes, sizeErr := dirFileBytes(filepath.Join(runtimeDir, "paths"))
	if sizeErr != nil {
		logManager.EmitSystemLine(ctx, fmt.Sprintf("cache restore bytes unavailable: %s", sizeErr.Error()))
		restoredBytes = 0
	}
	logManager.EmitSystemLine(ctx, fmt.Sprintf("cache restore: hit=%t path_count=%d bytes=%d duration_ms=%d", hit, len(cacheConfig.Paths), restoredBytes, time.Since(restoreStarted).Milliseconds()))

	logManager.EmitSystemLine(ctx, fmt.Sprintf("Cache: configured scope=%s key=%s", cacheConfig.Scope, key))
	if hit {
		logManager.EmitSystemLine(ctx, "Cache: hit (restored)")
	} else {
		logManager.EmitSystemLine(ctx, "Cache: miss")
	}

	return preparedStepCache{
		Enabled:    true,
		Key:        key,
		Scope:      cacheConfig.Scope,
		RuntimeDir: runtimeDir,
		Mounts:     mounts,
	}, nil
}

func (m *StepCacheManager) Save(ctx context.Context, logManager *ExecutionLogManager, prepared preparedStepCache, result runner.RunStepResult) error {
	if !prepared.Enabled || m == nil || m.store == nil {
		return nil
	}
	if result.Status != runner.RunStepStatusSuccess {
		logManager.EmitSystemLine(ctx, "cache save skipped: step not successful")
		logManager.EmitSystemLine(ctx, "cache save: success=false duration_ms=0")
		return nil
	}

	saveStarted := time.Now()
	pathCount := len(prepared.Mounts)
	savedBytes, bytesErr := dirFileBytes(filepath.Join(prepared.RuntimeDir, "paths"))
	if bytesErr != nil {
		logManager.EmitSystemLine(ctx, fmt.Sprintf("cache save bytes unavailable: %s", bytesErr.Error()))
		savedBytes = 0
	}
	storeSizeBefore, hasStoreSizeBefore := cacheStoreTotalSizeBytes(m.store)
	if hasStoreSizeBefore {
		logManager.EmitSystemLine(ctx, fmt.Sprintf("cache store size before save: bytes=%d", storeSizeBefore))
	}

	if err := m.store.Save(ctx, prepared.Key, prepared.RuntimeDir); err != nil {
		logManager.EmitSystemLine(ctx, fmt.Sprintf("cache save: success=false path_count=%d bytes=%d duration_ms=%d", pathCount, savedBytes, time.Since(saveStarted).Milliseconds()))
		return err
	}
	storeSizeAfter, hasStoreSizeAfter := cacheStoreTotalSizeBytes(m.store)
	if hasStoreSizeAfter {
		logManager.EmitSystemLine(ctx, fmt.Sprintf("cache store size after save: bytes=%d", storeSizeAfter))
	}
	if hasStoreSizeBefore && hasStoreSizeAfter {
		logManager.EmitSystemLine(ctx, fmt.Sprintf("cache save: success=true path_count=%d bytes=%d store_bytes_before=%d store_bytes_after=%d duration_ms=%d", pathCount, savedBytes, storeSizeBefore, storeSizeAfter, time.Since(saveStarted).Milliseconds()))
	} else {
		logManager.EmitSystemLine(ctx, fmt.Sprintf("cache save: success=true path_count=%d bytes=%d duration_ms=%d", pathCount, savedBytes, time.Since(saveStarted).Milliseconds()))
	}
	logManager.EmitSystemLine(ctx, "Cache: saved")
	return nil
}

func cacheStoreTotalSizeBytes(store cachepkg.Store) (int64, bool) {
	reporter, ok := store.(cacheStoreSizeReporter)
	if !ok {
		return 0, false
	}
	total, err := reporter.TotalSizeBytes()
	if err != nil {
		return 0, false
	}
	return total, true
}

func dirFileBytes(root string) (int64, error) {
	if strings.TrimSpace(root) == "" {
		return 0, nil
	}

	total := int64(0)
	err := filepath.WalkDir(root, func(_ string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return infoErr
		}
		total += info.Size()
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	return total, nil
}

func (m *StepCacheManager) workspaceRootForBuild(buildID string) (string, error) {
	root := strings.TrimSpace(m.executionRootPath)
	if root == "" {
		return "", ErrExecutionWorkspaceRootNotConfigured
	}
	return filepath.Join(root, strings.TrimSpace(buildID)), nil
}

func digestCacheKeyFiles(workspaceRoot string, files []string) (string, error) {
	hasher := sha256.New()
	ordered := append([]string(nil), files...)
	sort.Strings(ordered)
	for _, relativePath := range ordered {
		cleaned := filepath.Clean(filepath.FromSlash(relativePath))
		hasher.Write([]byte(relativePath))
		hasher.Write([]byte("\n"))
		fullPath := filepath.Join(workspaceRoot, cleaned)
		data, err := os.ReadFile(fullPath)
		if err != nil {
			if os.IsNotExist(err) {
				hasher.Write([]byte("<missing>\n"))
				continue
			}
			return "", err
		}
		sum := sha256.Sum256(data)
		hasher.Write([]byte(hex.EncodeToString(sum[:])))
		hasher.Write([]byte("\n"))
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func buildScopedID(scope domain.CacheScope, buildID string) string {
	if scope != domain.CacheScopeBuild {
		return ""
	}
	return strings.TrimSpace(buildID)
}

func jobScopedIdentity(scope domain.CacheScope, executionContext StepExecutionContext) string {
	if scope != domain.CacheScopeJob {
		return ""
	}
	if trimmed := strings.TrimSpace(executionContext.ExecutionRequest.JobID); trimmed != "" {
		return trimmed
	}
	if executionContext.Build.JobID != nil {
		if trimmed := strings.TrimSpace(*executionContext.Build.JobID); trimmed != "" {
			return trimmed
		}
	}
	if executionContext.Build.PipelinePath != nil {
		if trimmed := strings.TrimSpace(*executionContext.Build.PipelinePath); trimmed != "" {
			return executionContext.Build.ProjectID + ":" + stableRepoIdentity(executionContext.Build) + ":" + trimmed
		}
	}
	if executionContext.Build.PipelineName != nil {
		if trimmed := strings.TrimSpace(*executionContext.Build.PipelineName); trimmed != "" {
			return executionContext.Build.ProjectID + ":" + stableRepoIdentity(executionContext.Build) + ":" + trimmed
		}
	}
	return executionContext.Build.ProjectID + ":" + stableRepoIdentity(executionContext.Build) + ":adhoc"
}

func stableRepoIdentity(build domain.Build) string {
	if build.Source != nil {
		if trimmed := strings.TrimSpace(build.Source.RepositoryURL); trimmed != "" {
			return trimmed
		}
	}
	if build.RepoURL != nil {
		if trimmed := strings.TrimSpace(*build.RepoURL); trimmed != "" {
			return trimmed
		}
	}
	return "repo-unknown"
}

func sanitizeStepDirName(stepID string, stepIndex int) string {
	trimmed := strings.TrimSpace(stepID)
	if trimmed != "" {
		return strings.ReplaceAll(trimmed, string(filepath.Separator), "-")
	}
	return fmt.Sprintf("step-%d", stepIndex)
}
