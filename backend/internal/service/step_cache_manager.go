package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	cachepkg "github.com/radiation/coyote-ci/backend/internal/cache"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/runner"
)

const cacheCompressionTarGz = "tar.gz"

type preparedStepCache struct {
	Enabled       bool
	Policy        domain.CachePolicy
	Preset        cachepkg.Preset
	Fingerprint   string
	CacheKey      string
	RuntimeDir    string
	Mounts        []runner.CacheMount
	MetadataEntry *domain.CacheEntry
}

type StepCacheManager struct {
	store             cachepkg.Store
	entryRepo         repository.CacheEntryRepository
	executionRootPath string
	now               func() time.Time
}

func NewStepCacheManager(store cachepkg.Store, entryRepo repository.CacheEntryRepository, executionRootPath string) *StepCacheManager {
	return &StepCacheManager{
		store:             store,
		entryRepo:         entryRepo,
		executionRootPath: strings.TrimSpace(executionRootPath),
		now:               func() time.Time { return time.Now().UTC() },
	}
}

func (m *StepCacheManager) Prepare(ctx context.Context, executionContext StepExecutionContext, logManager *ExecutionLogManager) (preparedStepCache, error) {
	if m == nil || m.store == nil || m.entryRepo == nil {
		return preparedStepCache{}, nil
	}
	if executionContext.Step == nil || executionContext.Step.Cache == nil {
		logManager.EmitSystemLine(ctx, "cache restore skipped: step cache not configured")
		return preparedStepCache{}, nil
	}

	policy := domain.NormalizeCachePolicy(executionContext.Step.Cache.Policy)
	if policy == domain.CachePolicyOff {
		logManager.EmitSystemLine(ctx, fmt.Sprintf("cache restore skipped: policy=%s", policy))
		return preparedStepCache{Policy: policy}, nil
	}

	preset, runtimeDir, key, err := m.resolvePreparedIdentity(executionContext)
	if err != nil {
		if err == cachepkg.ErrNoFingerprintFilesFound {
			logManager.EmitSystemLine(ctx, fmt.Sprintf("cache skipped: preset=%s reason=lockfile_missing", strings.TrimSpace(executionContext.Step.Cache.Preset)))
			return preparedStepCache{Policy: policy}, nil
		}
		return preparedStepCache{}, err
	}

	prepared := preparedStepCache{
		Enabled:     true,
		Policy:      policy,
		Preset:      preset,
		CacheKey:    key,
		RuntimeDir:  runtimeDir,
		Fingerprint: strings.TrimPrefix(key, preset.Name+":"),
	}

	mounts, err := presetMounts(runtimeDir, preset.CachePaths)
	if err != nil {
		return preparedStepCache{}, err
	}
	prepared.Mounts = mounts

	if policy == domain.CachePolicyPush {
		logManager.EmitSystemLine(ctx, fmt.Sprintf("cache restore skipped: policy=%s", policy))
		return prepared, nil
	}

	jobID := effectiveJobID(executionContext)
	entry, found, err := m.entryRepo.FindReadyByKey(ctx, jobID, preset.Name, key)
	if err != nil {
		return preparedStepCache{}, err
	}

	lookupStart := time.Now()
	if !found {
		logManager.EmitSystemLine(ctx, fmt.Sprintf("cache lookup: preset=%s key=%s hit=false job_id=%s", preset.Name, key, jobID))
		return prepared, nil
	}

	restoreStart := time.Now()
	restoreResult, err := m.store.Restore(ctx, entry.ObjectKey, runtimeDir)
	if err != nil {
		return preparedStepCache{}, err
	}
	if !restoreResult.Hit {
		logManager.EmitSystemLine(ctx, fmt.Sprintf("cache lookup: preset=%s key=%s hit=false job_id=%s", preset.Name, key, jobID))
		return prepared, nil
	}

	if markErr := m.entryRepo.MarkAccessed(ctx, entry.ID, m.now()); markErr != nil && markErr != repository.ErrCacheEntryNotFound {
		return preparedStepCache{}, markErr
	}

	prepared.MetadataEntry = &entry
	logManager.EmitSystemLine(ctx, fmt.Sprintf("cache lookup: preset=%s key=%s hit=true job_id=%s duration_ms=%d", preset.Name, key, jobID, time.Since(lookupStart).Milliseconds()))
	logManager.EmitSystemLine(ctx, fmt.Sprintf("cache restore end: preset=%s key=%s bytes=%d duration_ms=%d", preset.Name, key, restoreResult.SizeBytes, time.Since(restoreStart).Milliseconds()))
	return prepared, nil
}

func (m *StepCacheManager) Save(ctx context.Context, executionContext StepExecutionContext, logManager *ExecutionLogManager, prepared preparedStepCache, result runner.RunStepResult) error {
	if !prepared.Enabled || m == nil || m.store == nil || m.entryRepo == nil {
		return nil
	}
	if result.Status != runner.RunStepStatusSuccess {
		logManager.EmitSystemLine(ctx, "cache save skipped: step not successful")
		return nil
	}
	if prepared.Policy == domain.CachePolicyPull || prepared.Policy == domain.CachePolicyOff {
		logManager.EmitSystemLine(ctx, fmt.Sprintf("cache save skipped: policy=%s", prepared.Policy))
		return nil
	}

	jobID := effectiveJobID(executionContext)
	objectKey := m.objectKey(jobID, prepared.Preset.Name, prepared.CacheKey)

	start := time.Now()
	saveResult, err := m.store.Save(ctx, objectKey, prepared.RuntimeDir)
	if err != nil {
		return err
	}

	_, err = m.entryRepo.Upsert(ctx, repository.CacheEntryUpsertInput{
		JobID:            jobID,
		Preset:           prepared.Preset.Name,
		CacheKey:         prepared.CacheKey,
		StorageProvider:  m.store.Provider(),
		ObjectKey:        objectKey,
		SizeBytes:        saveResult.SizeBytes,
		Checksum:         saveResult.Checksum,
		Compression:      saveResult.Compression,
		Status:           domain.CacheEntryStatusReady,
		CreatedByBuildID: executionContext.Build.ID,
		CreatedByStepID:  executionContext.ExecutionRequest.StepID,
	})
	if err != nil {
		return err
	}

	logManager.EmitSystemLine(ctx, fmt.Sprintf("cache save end: preset=%s key=%s bytes=%d duration_ms=%d", prepared.Preset.Name, prepared.CacheKey, saveResult.SizeBytes, time.Since(start).Milliseconds()))
	return nil
}

func (m *StepCacheManager) resolvePreparedIdentity(executionContext StepExecutionContext) (cachepkg.Preset, string, string, error) {
	workspaceRoot, err := m.workspaceRootForBuild(executionContext.Build.ID)
	if err != nil {
		return cachepkg.Preset{}, "", "", err
	}

	stepWorkingDir := "."
	if executionContext.Step != nil {
		stepWorkingDir = executionContext.Step.WorkingDir
	}

	preset, err := cachepkg.ResolvePreset(executionContext.Step.Cache.Preset, stepWorkingDir)
	if err != nil {
		return cachepkg.Preset{}, "", "", err
	}

	fingerprint, _, err := cachepkg.ComputeFingerprint(workspaceRoot, preset.FingerprintFiles)
	if err != nil {
		if err == cachepkg.ErrNoFingerprintFilesFound {
			return cachepkg.Preset{}, "", "", err
		}
		return cachepkg.Preset{}, "", "", err
	}

	cacheKey := fmt.Sprintf("%s:%s", preset.Name, fingerprint)
	runtimeDir := filepath.Join(workspaceRoot, ".coyote", "cache-runtime", sanitizeStepDirName(executionContext.ExecutionRequest.StepID, executionContext.ExecutionRequest.StepIndex))
	if err := os.RemoveAll(runtimeDir); err != nil {
		return cachepkg.Preset{}, "", "", err
	}
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		return cachepkg.Preset{}, "", "", err
	}
	return preset, runtimeDir, cacheKey, nil
}

func (m *StepCacheManager) workspaceRootForBuild(buildID string) (string, error) {
	executionRoot := strings.TrimSpace(m.executionRootPath)
	if executionRoot == "" {
		return "", ErrExecutionWorkspaceRootNotConfigured
	}

	trimmedBuildID := strings.TrimSpace(buildID)
	if trimmedBuildID == "" {
		return "", fmt.Errorf("build id is required")
	}

	workspaceRoot := filepath.Join(executionRoot, trimmedBuildID)
	cleanRoot := filepath.Clean(executionRoot)
	cleanWorkspace := filepath.Clean(workspaceRoot)
	rel, err := filepath.Rel(cleanRoot, cleanWorkspace)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("workspace root escapes execution root")
	}

	if _, err := os.Stat(workspaceRoot); err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("build workspace not found: %s", workspaceRoot)
		}
		return "", err
	}

	return workspaceRoot, nil
}

func presetMounts(runtimeDir string, cachePaths []string) ([]runner.CacheMount, error) {
	mounts := make([]runner.CacheMount, 0, len(cachePaths))
	for idx, cachePath := range cachePaths {
		hostPath := filepath.Join(runtimeDir, "paths", fmt.Sprintf("%03d", idx))
		if err := os.MkdirAll(hostPath, 0o755); err != nil {
			return nil, fmt.Errorf("create cache mount path %s: %w", hostPath, err)
		}
		mounts = append(mounts, runner.CacheMount{HostPath: hostPath, ContainerPath: cachePath})
	}
	return mounts, nil
}

func effectiveJobID(executionContext StepExecutionContext) string {
	if executionContext.Build.JobID != nil && strings.TrimSpace(*executionContext.Build.JobID) != "" {
		return strings.TrimSpace(*executionContext.Build.JobID)
	}
	if strings.TrimSpace(executionContext.Build.ID) != "" {
		return "build:" + strings.TrimSpace(executionContext.Build.ID)
	}
	if executionContext.PersistedJob != nil && strings.TrimSpace(executionContext.PersistedJob.ID) != "" {
		return strings.TrimSpace(executionContext.PersistedJob.ID)
	}
	if strings.TrimSpace(executionContext.ExecutionRequest.JobID) != "" {
		return strings.TrimSpace(executionContext.ExecutionRequest.JobID)
	}
	return "build:" + strings.TrimSpace(executionContext.Build.ID)
}

func (m *StepCacheManager) objectKey(jobID string, preset string, cacheKey string) string {
	hash := sha256.Sum256([]byte(strings.TrimSpace(cacheKey)))
	return fmt.Sprintf("v1/jobs/%s/%s/%s", sanitizeKeyPart(jobID), sanitizeKeyPart(preset), hex.EncodeToString(hash[:]))
}

func sanitizeStepDirName(stepID string, stepIndex int) string {
	trimmed := strings.TrimSpace(stepID)
	if trimmed == "" {
		return fmt.Sprintf("step-%d", stepIndex)
	}

	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", " ", "_")
	sanitized := replacer.Replace(trimmed)
	if sanitized == "" {
		return fmt.Sprintf("step-%d", stepIndex)
	}
	return sanitized
}

func sanitizeKeyPart(value string) string {
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", " ", "_", "|", "_")
	sanitized := replacer.Replace(strings.TrimSpace(value))
	if sanitized == "" {
		return "unknown"
	}
	return sanitized
}
