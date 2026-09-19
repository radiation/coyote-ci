package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
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
	defaultWorkspaceHelperCachePublishLease               = 10 * time.Minute
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
	if archiveStore, ok := s.store.(cachepkg.ArchiveStore); ok && directCacheArchiveCompatible(entry) {
		archive, result, openErr := archiveStore.Open(ctx, entry.ObjectKey)
		if openErr != nil || !result.Hit {
			return WorkspaceHelperCachePayload{}, result.Hit, openErr
		}
		if result.SizeBytes != entry.SizeBytes {
			_ = archive.Close()
			return WorkspaceHelperCachePayload{}, false, workspace.ErrWorkspaceRevisionDigestMismatch
		}
		if markErr := s.entries.MarkAccessed(ctx, entry.ID, s.now()); markErr != nil && !errors.Is(markErr, repository.ErrCacheEntryNotFound) {
			_ = archive.Close()
			return WorkspaceHelperCachePayload{}, false, markErr
		}
		return WorkspaceHelperCachePayload{Archive: archive, Publication: domain.WorkspaceRevisionPublication{StorageKey: "cache/transport.tar.gz", ContentDigest: entry.ContentDigest, SizeBytes: &entry.SizeBytes}}, true, nil
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
	jobID := cacheJobID(build)
	if s.cacheContentIsReady(ctx, jobID, preset, cacheKey, publication) {
		log.Printf("INFO cache publish skipped unchanged job_id=%s preset=%s key=%s", jobID, preset, cacheKey)
		return nil
	}
	claim, acquired, claimErr := s.entries.TryAcquirePublishClaim(ctx, jobID, preset, cacheKey, executionJobID, s.now(), defaultWorkspaceHelperCachePublishLease)
	if claimErr != nil {
		return claimErr
	}
	if !acquired {
		log.Printf("INFO cache publish skipped writer_busy job_id=%s preset=%s key=%s", jobID, preset, cacheKey)
		return nil
	}
	if claim.Reclaimed {
		log.Printf("INFO cache publish claim reclaimed job_id=%s preset=%s key=%s", jobID, preset, cacheKey)
	} else {
		log.Printf("INFO cache publish claim acquired job_id=%s preset=%s key=%s", jobID, preset, cacheKey)
	}
	defer func() {
		if releaseErr := s.entries.ReleasePublishClaim(ctx, claim); releaseErr != nil {
			log.Printf("WARN cache publish claim release failed job_id=%s preset=%s key=%s err=%v", jobID, preset, cacheKey, releaseErr)
		}
	}()
	if s.cacheContentIsReady(ctx, jobID, preset, cacheKey, publication) {
		log.Printf("INFO cache publish skipped unchanged_after_claim job_id=%s preset=%s key=%s", jobID, preset, cacheKey)
		return nil
	}
	previous, hadPrevious, previousErr := s.entries.FindReadyByKey(ctx, jobID, preset, cacheKey)
	if previousErr != nil {
		return previousErr
	}
	limits := workspace.WorkspaceRevisionRestoreLimits{MaxUncompressedBytes: s.maxUncompressedBytes, MaxEntries: s.maxArchiveEntries}
	objectKey := cacheObjectKey(jobID, preset, cacheKey, publication.ContentDigest)
	saved, saveErr := s.saveArchive(ctx, objectKey, claim.ClaimToken, archive, publication, limits)
	if saveErr != nil {
		log.Printf("WARN cache publish failed job_id=%s preset=%s key=%s err=%v", jobID, preset, cacheKey, saveErr)
		return saveErr
	}
	entry, upsertErr := s.entries.CompletePublishClaim(ctx, claim, repository.CacheEntryUpsertInput{JobID: jobID, Preset: preset, CacheKey: cacheKey, StorageProvider: s.store.Provider(), ObjectKey: objectKey, SizeBytes: saved.SizeBytes, Checksum: saved.Checksum, ContentDigest: publication.ContentDigest, Compression: saved.Compression, Status: domain.CacheEntryStatusReady, CreatedByBuildID: build.ID, CreatedByStepID: step.ID}, s.now())
	if errors.Is(upsertErr, repository.ErrCachePublishClaimStale) {
		log.Printf("INFO cache publish skipped stale_claim job_id=%s preset=%s key=%s", jobID, preset, cacheKey)
		return nil
	}
	if upsertErr != nil {
		return upsertErr
	}
	if hadPrevious && strings.TrimSpace(previous.ObjectKey) != "" && previous.StorageProvider == entry.StorageProvider && previous.ObjectKey != entry.ObjectKey {
		if archiveStore, ok := s.store.(cachepkg.ArchiveStore); ok {
			if deleteErr := archiveStore.DeleteArchive(ctx, previous.ObjectKey); deleteErr != nil {
				log.Printf("WARN cache archive replacement cleanup failed key=%s err=%v", previous.ObjectKey, deleteErr)
			}
		}
	}
	log.Printf("INFO cache publish succeeded job_id=%s preset=%s key=%s", jobID, preset, cacheKey)
	return nil
}

func (s *WorkspaceHelperCacheService) saveArchive(ctx context.Context, objectKey string, claimToken string, archive io.Reader, publication domain.WorkspaceRevisionPublication, limits workspace.WorkspaceRevisionRestoreLimits) (cachepkg.SaveResult, error) {
	archiveStore, ok := s.store.(cachepkg.ArchiveStore)
	if !ok {
		directory, directoryErr := os.MkdirTemp("", "coyote-cache-save-*")
		if directoryErr != nil {
			return cachepkg.SaveResult{}, directoryErr
		}
		defer func() { _ = os.RemoveAll(directory) }()
		payloadRoot := filepath.Join(directory, "payload")
		if restoreErr := workspace.RestoreArchiveWithLimits(ctx, archive, publication, payloadRoot, limits); restoreErr != nil {
			return cachepkg.SaveResult{}, fmt.Errorf("%w: cache archive: %w", ErrWorkspaceHelperCacheInvalidInput, restoreErr)
		}
		return s.store.Save(ctx, objectKey, payloadRoot)
	}
	stagingKey := cacheArchiveStagingKey(objectKey, claimToken)
	promoted := false
	defer func() {
		if !promoted {
			if deleteErr := archiveStore.DeleteArchive(context.Background(), stagingKey); deleteErr != nil {
				log.Printf("WARN cache archive staging cleanup failed key=%s err=%v", stagingKey, deleteErr)
			}
		}
	}()
	reader, writer := io.Pipe()
	validation := make(chan error, 1)
	go func() {
		validateErr := workspace.ValidateArchiveWithLimits(ctx, io.TeeReader(archive, writer), publication, limits)
		_ = writer.CloseWithError(validateErr)
		validation <- validateErr
	}()
	saved, saveErr := archiveStore.SaveArchive(ctx, stagingKey, reader)
	if saveErr != nil {
		_ = reader.CloseWithError(saveErr)
	}
	validationErr := <-validation
	if validationErr != nil {
		return cachepkg.SaveResult{}, fmt.Errorf("%w: cache archive: %w", ErrWorkspaceHelperCacheInvalidInput, validationErr)
	}
	if saveErr != nil {
		return cachepkg.SaveResult{}, saveErr
	}
	if strings.TrimSpace(saved.Checksum) != strings.TrimPrefix(strings.TrimSpace(publication.ContentDigest), "sha256:") || (publication.SizeBytes != nil && saved.SizeBytes != *publication.SizeBytes) {
		return cachepkg.SaveResult{}, fmt.Errorf("%w: cache archive storage result does not match validated archive", ErrWorkspaceHelperCacheInvalidInput)
	}
	if promoteErr := archiveStore.PromoteArchive(ctx, stagingKey, objectKey); promoteErr != nil {
		return cachepkg.SaveResult{}, promoteErr
	}
	promoted = true
	return saved, nil
}

func directCacheArchiveCompatible(entry domain.CacheEntry) bool {
	return entry.Compression == "tar.gz" && strings.TrimSpace(entry.ContentDigest) == "sha256:"+strings.TrimSpace(entry.Checksum)
}

func (s *WorkspaceHelperCacheService) cacheContentIsReady(ctx context.Context, jobID, preset, cacheKey string, publication domain.WorkspaceRevisionPublication) bool {
	entry, found, err := s.entries.FindReadyByKey(ctx, jobID, preset, cacheKey)
	if err != nil || !found {
		return false
	}
	return cacheEntryMatchesPublication(entry, s.store.Provider(), jobID, preset, cacheKey, publication)
}

func cacheEntryMatchesPublication(entry domain.CacheEntry, provider domain.StorageProvider, jobID, preset, cacheKey string, publication domain.WorkspaceRevisionPublication) bool {
	if entry.Status != domain.CacheEntryStatusReady {
		return false
	}
	contentDigest := strings.TrimSpace(publication.ContentDigest)
	if strings.TrimSpace(entry.ContentDigest) != contentDigest {
		return false
	}
	if strings.TrimSpace(entry.Compression) != "tar.gz" {
		return false
	}
	if strings.TrimSpace(entry.Checksum) != strings.TrimPrefix(contentDigest, "sha256:") {
		return false
	}
	if publication.SizeBytes == nil || entry.SizeBytes != *publication.SizeBytes {
		return false
	}
	if strings.TrimSpace(entry.ObjectKey) != cacheObjectKey(jobID, preset, cacheKey, publication.ContentDigest) {
		return false
	}
	if entry.StorageProvider != provider {
		return false
	}
	return true
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
		components, resolveErr := cachepkg.ResolvePresetComponents(step.Cache.Preset, step.WorkingDir)
		if resolveErr != nil {
			continue
		}
		for _, component := range components {
			if component.Name == strings.TrimSpace(preset) && validCacheKey(component.Name, cacheKey) {
				return build, step, nil
			}
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

func cacheObjectKey(jobID string, preset string, cacheKey string, contentDigest string) string {
	hash := sha256.Sum256([]byte(strings.TrimSpace(cacheKey)))
	return fmt.Sprintf("v1/jobs/%s/%s/%s/%s", sanitizeCacheKeyPart(jobID), sanitizeCacheKeyPart(preset), hex.EncodeToString(hash[:]), strings.TrimSpace(contentDigest))
}

func cacheArchiveStagingKey(objectKey string, claimToken string) string {
	tokenHash := sha256.Sum256([]byte(strings.TrimSpace(claimToken)))
	return fmt.Sprintf("%s.staging.%x", objectKey, tokenHash[:])
}

func sanitizeCacheKeyPart(value string) string {
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", " ", "_", "|", "_")
	sanitized := replacer.Replace(strings.TrimSpace(value))
	if sanitized == "" {
		return "unknown"
	}
	return sanitized
}
