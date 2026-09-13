package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	cachepkg "github.com/radiation/coyote-ci/backend/internal/cache"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/workspace"
)

var ErrWorkspaceHelperCacheInvalidInput = errors.New("invalid workspace helper cache input")

const (
	defaultWorkspaceHelperCacheMaxUncompressedBytes int64 = 4 * 1024 * 1024 * 1024
	defaultWorkspaceHelperCacheMaxArchiveEntries          = 100000
)

type WorkspaceHelperCachePayload struct {
	Archive     io.ReadCloser
	Publication domain.WorkspaceRevisionPublication
}

type workspaceHelperCacheBuildRepository interface {
	GetByID(context.Context, string) (domain.Build, error)
	GetStepsByBuildID(context.Context, string) ([]domain.BuildStep, error)
}

type WorkspaceHelperCacheServiceConfig struct {
	CapabilityAuthorizer WorkspacePrepareCapabilityAuthorizer
	ExecutionJobs        workspacePrepareExecutionJobRepository
	Builds               workspaceHelperCacheBuildRepository
	Entries              repository.CacheEntryRepository
	Store                cachepkg.Store
	MaxUncompressedBytes int64
	MaxArchiveEntries    int
}

type WorkspaceHelperCacheService struct {
	capabilities         WorkspacePrepareCapabilityAuthorizer
	executionJobs        workspacePrepareExecutionJobRepository
	builds               workspaceHelperCacheBuildRepository
	entries              repository.CacheEntryRepository
	store                cachepkg.Store
	maxUncompressedBytes int64
	maxArchiveEntries    int
	now                  func() time.Time
}

func NewWorkspaceHelperCacheService(config WorkspaceHelperCacheServiceConfig) (*WorkspaceHelperCacheService, error) {
	if config.CapabilityAuthorizer == nil || config.ExecutionJobs == nil || config.Builds == nil || config.Entries == nil || config.Store == nil {
		return nil, errors.New("workspace helper cache service requires capability, build, cache entry, and store dependencies")
	}
	maxUncompressedBytes := config.MaxUncompressedBytes
	if maxUncompressedBytes <= 0 {
		maxUncompressedBytes = defaultWorkspaceHelperCacheMaxUncompressedBytes
	}
	maxArchiveEntries := config.MaxArchiveEntries
	if maxArchiveEntries <= 0 {
		maxArchiveEntries = defaultWorkspaceHelperCacheMaxArchiveEntries
	}
	return &WorkspaceHelperCacheService{capabilities: config.CapabilityAuthorizer, executionJobs: config.ExecutionJobs, builds: config.Builds, entries: config.Entries, store: config.Store, maxUncompressedBytes: maxUncompressedBytes, maxArchiveEntries: maxArchiveEntries, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (s *WorkspaceHelperCacheService) Restore(ctx context.Context, capabilityToken string, executionJobID string, podUID string, preset string, cacheKey string) (WorkspaceHelperCachePayload, bool, error) {
	build, _, err := s.authorizeAndValidate(ctx, capabilityToken, executionJobID, podUID, preset, cacheKey, domain.WorkspaceHelperRoleCacheRestore)
	if err != nil {
		return WorkspaceHelperCachePayload{}, false, err
	}
	entry, found, err := s.entries.FindReadyByKey(ctx, cacheJobID(build), preset, cacheKey)
	if err != nil || !found {
		return WorkspaceHelperCachePayload{}, found, err
	}
	directory, directoryErr := os.MkdirTemp("", "coyote-cache-restore-*")
	if directoryErr != nil {
		return WorkspaceHelperCachePayload{}, false, directoryErr
	}
	defer func() { _ = os.RemoveAll(directory) }()
	result, restoreErr := s.store.Restore(ctx, entry.ObjectKey, directory)
	if restoreErr != nil || !result.Hit {
		return WorkspaceHelperCachePayload{}, result.Hit, restoreErr
	}
	if markErr := s.entries.MarkAccessed(ctx, entry.ID, s.now()); markErr != nil && !errors.Is(markErr, repository.ErrCacheEntryNotFound) {
		return WorkspaceHelperCachePayload{}, false, markErr
	}
	archive, publication, archiveErr := workspace.ArchiveDirectory(ctx, directory)
	if archiveErr != nil {
		return WorkspaceHelperCachePayload{}, false, archiveErr
	}
	return WorkspaceHelperCachePayload{Archive: archive, Publication: publication}, true, nil
}

func (s *WorkspaceHelperCacheService) Save(ctx context.Context, capabilityToken string, executionJobID string, podUID string, preset string, cacheKey string, archive io.Reader, publication domain.WorkspaceRevisionPublication) error {
	build, step, err := s.authorizeAndValidate(ctx, capabilityToken, executionJobID, podUID, preset, cacheKey, domain.WorkspaceHelperRoleCacheSave)
	if err != nil {
		return err
	}
	if archive == nil || publication.Validate() != nil {
		return ErrWorkspaceHelperCacheInvalidInput
	}
	directory, directoryErr := os.MkdirTemp("", "coyote-cache-save-*")
	if directoryErr != nil {
		return directoryErr
	}
	defer func() { _ = os.RemoveAll(directory) }()
	payloadRoot := filepath.Join(directory, "payload")
	limits := workspace.WorkspaceRevisionRestoreLimits{MaxUncompressedBytes: s.maxUncompressedBytes, MaxEntries: s.maxArchiveEntries}
	if restoreErr := workspace.RestoreArchiveWithLimits(ctx, archive, publication, payloadRoot, limits); restoreErr != nil {
		return fmt.Errorf("%w: cache archive: %w", ErrWorkspaceHelperCacheInvalidInput, restoreErr)
	}
	objectKey := cacheObjectKey(cacheJobID(build), preset, cacheKey)
	saved, saveErr := s.store.Save(ctx, objectKey, payloadRoot)
	if saveErr != nil {
		return saveErr
	}
	_, upsertErr := s.entries.Upsert(ctx, repository.CacheEntryUpsertInput{JobID: cacheJobID(build), Preset: preset, CacheKey: cacheKey, StorageProvider: s.store.Provider(), ObjectKey: objectKey, SizeBytes: saved.SizeBytes, Checksum: saved.Checksum, Compression: saved.Compression, Status: domain.CacheEntryStatusReady, CreatedByBuildID: build.ID, CreatedByStepID: step.ID})
	return upsertErr
}

func (s *WorkspaceHelperCacheService) authorizeAndValidate(ctx context.Context, token string, executionJobID string, podUID string, preset string, cacheKey string, role domain.WorkspaceHelperRole) (domain.Build, domain.BuildStep, error) {
	if _, err := s.capabilities.Authorize(ctx, token, strings.TrimSpace(executionJobID), strings.TrimSpace(podUID), role); err != nil {
		return domain.Build{}, domain.BuildStep{}, err
	}
	job, err := s.executionJobs.GetJobByID(ctx, strings.TrimSpace(executionJobID))
	if err != nil {
		return domain.Build{}, domain.BuildStep{}, err
	}
	build, err := s.builds.GetByID(ctx, job.BuildID)
	if err != nil {
		return domain.Build{}, domain.BuildStep{}, err
	}
	steps, err := s.builds.GetStepsByBuildID(ctx, build.ID)
	if err != nil {
		return domain.Build{}, domain.BuildStep{}, err
	}
	for _, step := range steps {
		if step.ID != job.StepID || step.Cache == nil {
			continue
		}
		resolved, resolveErr := cachepkg.ResolvePreset(step.Cache.Preset, step.WorkingDir)
		if resolveErr == nil && resolved.Name == strings.TrimSpace(preset) && validCacheKey(resolved.Name, cacheKey) {
			return build, step, nil
		}
	}
	return domain.Build{}, domain.BuildStep{}, ErrWorkspaceHelperCacheInvalidInput
}

func validCacheKey(preset string, cacheKey string) bool {
	prefix := strings.TrimSpace(preset) + ":"
	trimmed := strings.TrimSpace(cacheKey)
	fingerprint := strings.TrimPrefix(trimmed, prefix)
	if !strings.HasPrefix(trimmed, prefix) || len(fingerprint) != 64 {
		return false
	}
	for _, character := range fingerprint {
		if character < '0' || (character > '9' && character < 'a') || character > 'f' {
			return false
		}
	}
	return true
}

func cacheJobID(build domain.Build) string {
	if build.JobID != nil && strings.TrimSpace(*build.JobID) != "" {
		return strings.TrimSpace(*build.JobID)
	}
	return "build:" + strings.TrimSpace(build.ID)
}

func cacheObjectKey(jobID string, preset string, cacheKey string) string {
	hash := sha256.Sum256([]byte(strings.TrimSpace(cacheKey)))
	return fmt.Sprintf("v1/jobs/%s/%s/%s", sanitizeCacheKeyPart(jobID), sanitizeCacheKeyPart(preset), hex.EncodeToString(hash[:]))
}

func sanitizeCacheKeyPart(value string) string {
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", " ", "_", "|", "_")
	sanitized := replacer.Replace(strings.TrimSpace(value))
	if sanitized == "" {
		return "unknown"
	}
	return sanitized
}
