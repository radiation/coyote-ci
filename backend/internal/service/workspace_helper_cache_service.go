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
var ErrWorkspaceHelperCachePublishClaimInvalid = errors.New("invalid or expired workspace helper cache publish claim")

const (
	defaultWorkspaceHelperCacheMaxUncompressedBytes int64 = 4 * 1024 * 1024 * 1024
	defaultWorkspaceHelperCacheMaxArchiveEntries          = 100000
	defaultWorkspaceHelperCachePublishLease               = 10 * time.Minute
)

type WorkspaceHelperCachePayload struct {
	Archive     io.ReadCloser
	Publication domain.WorkspaceRevisionPublication
	Preset      string
	CacheKey    string
}

type WorkspaceHelperCachePublishClaim struct {
	Acquired   bool
	ClaimToken string
	Reclaimed  bool
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
	started := time.Now()
	metrics := cacheTransferMetrics{operation: "restore", preset: preset, cacheKey: cacheKey}
	defer func() { logCacheTransferMetrics(metrics, time.Since(started)) }()
	lookupStarted := time.Now()
	entry, found, err := s.entries.FindReadyByKey(ctx, cacheJobID(build), preset, cacheKey)
	metrics.metadataDuration = time.Since(lookupStarted)
	if err != nil {
		metrics.outcome = "metadata_error"
		return WorkspaceHelperCachePayload{}, false, err
	}
	if !found {
		metrics.outcome = "miss"
		return WorkspaceHelperCachePayload{}, false, nil
	}
	metrics.outcome = "hit"
	metrics.compressedBytes = entry.SizeBytes
	if archiveStore, ok := s.store.(cachepkg.ArchiveStore); ok {
		effectiveDigest, direct := directCacheArchiveDigest(entry)
		if direct {
			metrics.transferMode = "direct"
			openStarted := time.Now()
			archive, result, openErr := archiveStore.Open(ctx, entry.ObjectKey)
			metrics.remoteOpenDuration = time.Since(openStarted)
			if openErr != nil {
				metrics.outcome = "restore_error"
				return WorkspaceHelperCachePayload{}, false, openErr
			}
			if !result.Hit {
				metrics.outcome = "miss"
				return WorkspaceHelperCachePayload{}, false, nil
			}
			if result.SizeBytes != entry.SizeBytes {
				_ = archive.Close()
				metrics.outcome = "restore_error"
				return WorkspaceHelperCachePayload{}, false, workspace.ErrWorkspaceRevisionDigestMismatch
			}
			if markErr := s.entries.MarkAccessed(ctx, entry.ID, s.now()); markErr != nil && !errors.Is(markErr, repository.ErrCacheEntryNotFound) {
				_ = archive.Close()
				metrics.outcome = "metadata_error"
				return WorkspaceHelperCachePayload{}, false, markErr
			}
			return WorkspaceHelperCachePayload{Archive: archive, Publication: domain.WorkspaceRevisionPublication{StorageKey: "cache/transport.tar.gz", ContentDigest: effectiveDigest, StorageProvider: domain.StorageProviderFilesystem, SizeBytes: &entry.SizeBytes}, Preset: preset, CacheKey: cacheKey}, true, nil
		}
	}
	metrics.transferMode = "legacy_materialized"
	directory, directoryErr := os.MkdirTemp("", "coyote-cache-restore-*")
	if directoryErr != nil {
		metrics.outcome = "restore_error"
		return WorkspaceHelperCachePayload{}, false, directoryErr
	}
	defer func() { _ = os.RemoveAll(directory) }()
	result, restoreErr := s.store.Restore(ctx, entry.ObjectKey, directory)
	if restoreErr != nil {
		metrics.outcome = "restore_error"
		return WorkspaceHelperCachePayload{}, false, restoreErr
	}
	if !result.Hit {
		metrics.outcome = "miss"
		return WorkspaceHelperCachePayload{}, false, nil
	}
	if markErr := s.entries.MarkAccessed(ctx, entry.ID, s.now()); markErr != nil && !errors.Is(markErr, repository.ErrCacheEntryNotFound) {
		metrics.outcome = "metadata_error"
		return WorkspaceHelperCachePayload{}, false, markErr
	}
	archive, publication, archiveErr := workspace.ArchiveDirectory(ctx, directory)
	if archiveErr != nil {
		metrics.outcome = "restore_error"
		return WorkspaceHelperCachePayload{}, false, archiveErr
	}
	return WorkspaceHelperCachePayload{Archive: archive, Publication: publication, Preset: preset, CacheKey: cacheKey}, true, nil
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
	started := time.Now()
	metrics := cacheTransferMetrics{operation: "save", preset: preset, cacheKey: cacheKey, compressedBytes: valueOrZero(publication.SizeBytes)}
	defer func() { logCacheTransferMetrics(metrics, time.Since(started)) }()
	lookupStarted := time.Now()
	ready := s.cacheContentIsReady(ctx, jobID, preset, cacheKey, publication)
	metrics.metadataDuration += time.Since(lookupStarted)
	if ready {
		metrics.outcome = "skipped_publish"
		log.Printf("INFO cache publish skipped unchanged job_id=%s preset=%s key=%s", jobID, preset, cacheKey)
		return nil
	}
	claimStarted := time.Now()
	claim, acquired, claimErr := s.entries.TryAcquirePublishClaim(ctx, jobID, preset, cacheKey, executionJobID, s.now(), defaultWorkspaceHelperCachePublishLease)
	metrics.claimDuration = time.Since(claimStarted)
	if claimErr != nil {
		metrics.outcome = "claim_error"
		return claimErr
	}
	if !acquired {
		metrics.outcome = "deduplicated_publish"
		log.Printf("INFO cache publish skipped writer_busy job_id=%s preset=%s key=%s", jobID, preset, cacheKey)
		return nil
	}
	return s.saveWithClaim(ctx, build, step, preset, cacheKey, archive, publication, claim, &metrics)
}

func (s *WorkspaceHelperCacheService) ClaimPublish(ctx context.Context, capabilityToken string, executionJobID string, podUID string, preset string, cacheKey string) (WorkspaceHelperCachePublishClaim, error) {
	build, _, err := s.authorizeAndValidate(ctx, capabilityToken, executionJobID, podUID, preset, cacheKey, domain.WorkspaceHelperRoleCacheSave)
	if err != nil {
		return WorkspaceHelperCachePublishClaim{}, err
	}
	started := time.Now()
	jobID := cacheJobID(build)
	claim, acquired, claimErr := s.entries.TryAcquirePublishClaim(ctx, jobID, preset, cacheKey, executionJobID, s.now(), defaultWorkspaceHelperCachePublishLease)
	outcome := "not_acquired"
	if claimErr != nil {
		outcome = "claim_error"
	} else if acquired {
		outcome = "acquired"
	}
	claimLifecycle := "busy"
	if claimErr != nil {
		claimLifecycle = "error"
	} else if acquired {
		claimLifecycle = "issued"
	}
	log.Printf("INFO cache_transfer operation=preclaim outcome=%s preset=%s cache_key=%s preclaim_ms=%d archive_skipped=%t claim_lifecycle=%s claim_reclaimed=%t", outcome, preset, cacheKey, time.Since(started).Milliseconds(), !acquired, claimLifecycle, acquired && claim.Reclaimed)
	if claimErr != nil {
		return WorkspaceHelperCachePublishClaim{}, claimErr
	}
	if !acquired {
		return WorkspaceHelperCachePublishClaim{}, nil
	}
	return WorkspaceHelperCachePublishClaim{Acquired: true, ClaimToken: claim.ClaimToken, Reclaimed: claim.Reclaimed}, nil
}

func (s *WorkspaceHelperCacheService) SaveClaimed(ctx context.Context, capabilityToken string, executionJobID string, podUID string, preset string, cacheKey string, claimToken string, archive io.Reader, publication domain.WorkspaceRevisionPublication) error {
	build, step, err := s.authorizeAndValidate(ctx, capabilityToken, executionJobID, podUID, preset, cacheKey, domain.WorkspaceHelperRoleCacheSave)
	if err != nil {
		return err
	}
	if archive == nil || publication.Validate() != nil || strings.TrimSpace(claimToken) == "" {
		return ErrWorkspaceHelperCacheInvalidInput
	}
	claim := domain.CachePublishClaim{JobID: cacheJobID(build), Preset: strings.TrimSpace(strings.ToLower(preset)), CacheKey: strings.TrimSpace(cacheKey), ClaimToken: strings.TrimSpace(claimToken)}
	if validateErr := s.entries.ValidatePublishClaimOwnership(ctx, claim, executionJobID); validateErr != nil {
		if errors.Is(validateErr, repository.ErrCachePublishClaimStale) {
			return ErrWorkspaceHelperCachePublishClaimInvalid
		}
		return validateErr
	}
	metrics := cacheTransferMetrics{operation: "save", preset: preset, cacheKey: cacheKey, compressedBytes: valueOrZero(publication.SizeBytes), claimLifecycle: "validated"}
	started := time.Now()
	defer func() { logCacheTransferMetrics(metrics, time.Since(started)) }()
	return s.saveWithClaim(ctx, build, step, preset, cacheKey, archive, publication, claim, &metrics)
}

func (s *WorkspaceHelperCacheService) ReleasePublishClaim(ctx context.Context, capabilityToken string, executionJobID string, podUID string, preset string, cacheKey string, claimToken string) error {
	build, _, err := s.authorizeAndValidate(ctx, capabilityToken, executionJobID, podUID, preset, cacheKey, domain.WorkspaceHelperRoleCacheSave)
	if err != nil {
		return err
	}
	if strings.TrimSpace(claimToken) == "" {
		return ErrWorkspaceHelperCacheInvalidInput
	}
	claim := domain.CachePublishClaim{
		JobID:      cacheJobID(build),
		Preset:     strings.TrimSpace(strings.ToLower(preset)),
		CacheKey:   strings.TrimSpace(cacheKey),
		ClaimToken: strings.TrimSpace(claimToken),
	}
	if validateErr := s.entries.ValidatePublishClaim(ctx, claim, executionJobID, s.now()); validateErr != nil {
		if errors.Is(validateErr, repository.ErrCachePublishClaimStale) {
			return nil
		}
		return validateErr
	}
	return s.entries.ReleasePublishClaim(ctx, claim)
}

func (s *WorkspaceHelperCacheService) saveWithClaim(ctx context.Context, build domain.Build, step domain.BuildStep, preset string, cacheKey string, archive io.Reader, publication domain.WorkspaceRevisionPublication, claim domain.CachePublishClaim, metrics *cacheTransferMetrics) error {
	jobID := cacheJobID(build)
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
	lookupStarted := time.Now()
	ready := s.cacheContentIsReady(ctx, jobID, preset, cacheKey, publication)
	metrics.metadataDuration += time.Since(lookupStarted)
	if ready {
		metrics.outcome = "skipped_publish"
		metrics.claimLifecycle = "released_unchanged"
		log.Printf("INFO cache publish skipped unchanged_after_claim job_id=%s preset=%s key=%s", jobID, preset, cacheKey)
		return nil
	}
	lookupStarted = time.Now()
	previous, hadPrevious, previousErr := s.entries.FindReadyByKey(ctx, jobID, preset, cacheKey)
	metrics.metadataDuration += time.Since(lookupStarted)
	if previousErr != nil {
		metrics.outcome = "metadata_error"
		return previousErr
	}
	limits := workspace.WorkspaceRevisionRestoreLimits{MaxUncompressedBytes: s.maxUncompressedBytes, MaxEntries: s.maxArchiveEntries}
	objectKey := cacheObjectKey(jobID, preset, cacheKey, publication.ContentDigest)
	saved, archiveMetrics, saveErr := s.saveArchive(ctx, objectKey, claim.ClaimToken, archive, publication, limits)
	metrics.archive = archiveMetrics
	if saveErr != nil {
		metrics.outcome = "publish_error"
		metrics.claimLifecycle = "released_after_failure"
		log.Printf("WARN cache publish failed job_id=%s preset=%s key=%s err=%v", jobID, preset, cacheKey, saveErr)
		return saveErr
	}
	finalizationStarted := time.Now()
	entry, upsertErr := s.entries.CompletePublishClaim(ctx, claim, repository.CacheEntryUpsertInput{JobID: jobID, Preset: preset, CacheKey: cacheKey, StorageProvider: s.store.Provider(), ObjectKey: objectKey, SizeBytes: saved.SizeBytes, Checksum: saved.Checksum, ContentDigest: publication.ContentDigest, Compression: saved.Compression, Status: domain.CacheEntryStatusReady, CreatedByBuildID: build.ID, CreatedByStepID: step.ID}, s.now())
	metrics.finalizationDuration = time.Since(finalizationStarted)
	if errors.Is(upsertErr, repository.ErrCachePublishClaimReplaced) {
		metrics.outcome = "deduplicated_publish"
		metrics.claimLifecycle = "replaced_before_completion"
		log.Printf("INFO cache publish skipped replaced_claim job_id=%s preset=%s key=%s", jobID, preset, cacheKey)
		return nil
	}
	if upsertErr != nil {
		metrics.outcome = "publish_error"
		if errors.Is(upsertErr, repository.ErrCachePublishClaimStale) {
			metrics.claimLifecycle = "missing_before_completion"
		}
		return upsertErr
	}
	if hadPrevious && strings.TrimSpace(previous.ObjectKey) != "" && previous.StorageProvider == entry.StorageProvider && previous.ObjectKey != entry.ObjectKey {
		if archiveStore, ok := s.store.(cachepkg.ArchiveStore); ok {
			if deleteErr := archiveStore.DeleteArchive(ctx, previous.ObjectKey); deleteErr != nil {
				log.Printf("WARN cache archive replacement cleanup failed key=%s err=%v", previous.ObjectKey, deleteErr)
			}
		}
	}
	metrics.outcome = "publish"
	metrics.claimLifecycle = "completed"
	metrics.compressedBytes = saved.SizeBytes
	log.Printf("INFO cache publish succeeded job_id=%s preset=%s key=%s", jobID, preset, cacheKey)
	return nil
}

func (s *WorkspaceHelperCacheService) saveArchive(ctx context.Context, objectKey string, claimToken string, archive io.Reader, publication domain.WorkspaceRevisionPublication, limits workspace.WorkspaceRevisionRestoreLimits) (cachepkg.SaveResult, cacheArchiveMetrics, error) {
	archiveStore, ok := s.store.(cachepkg.ArchiveStore)
	if !ok {
		directory, directoryErr := os.MkdirTemp("", "coyote-cache-save-*")
		if directoryErr != nil {
			return cachepkg.SaveResult{}, cacheArchiveMetrics{}, directoryErr
		}
		defer func() { _ = os.RemoveAll(directory) }()
		payloadRoot := filepath.Join(directory, "payload")
		if restoreErr := workspace.RestoreArchiveWithLimits(ctx, archive, publication, payloadRoot, limits); restoreErr != nil {
			return cachepkg.SaveResult{}, cacheArchiveMetrics{}, fmt.Errorf("%w: cache archive: %w", ErrWorkspaceHelperCacheInvalidInput, restoreErr)
		}
		saved, saveErr := s.store.Save(ctx, objectKey, payloadRoot)
		return saved, cacheArchiveMetrics{}, saveErr
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
	validation := make(chan cacheArchiveValidationResult, 1)
	go func() {
		validationStarted := time.Now()
		validationMetrics, validateErr := workspace.ValidateArchiveWithMetrics(ctx, io.TeeReader(archive, writer), publication, limits)
		_ = writer.CloseWithError(validateErr)
		validation <- cacheArchiveValidationResult{metrics: validationMetrics, duration: time.Since(validationStarted), err: validateErr}
	}()
	uploadStarted := time.Now()
	saved, saveErr := archiveStore.SaveArchive(ctx, stagingKey, reader)
	metrics := cacheArchiveMetrics{uploadDuration: time.Since(uploadStarted)}
	if saveErr != nil {
		_ = reader.CloseWithError(saveErr)
	}
	validationResult := <-validation
	metrics.validationDuration = validationResult.duration
	metrics.uncompressedBytes = validationResult.metrics.UncompressedBytes
	metrics.entries = validationResult.metrics.Entries
	if validationResult.err != nil {
		return cachepkg.SaveResult{}, metrics, fmt.Errorf("%w: cache archive: %w", ErrWorkspaceHelperCacheInvalidInput, validationResult.err)
	}
	if saveErr != nil {
		return cachepkg.SaveResult{}, metrics, saveErr
	}
	if strings.TrimSpace(saved.Checksum) != strings.TrimPrefix(strings.TrimSpace(publication.ContentDigest), "sha256:") || (publication.SizeBytes != nil && saved.SizeBytes != *publication.SizeBytes) {
		return cachepkg.SaveResult{}, metrics, fmt.Errorf("%w: cache archive storage result does not match validated archive", ErrWorkspaceHelperCacheInvalidInput)
	}
	promotionStarted := time.Now()
	if promoteErr := archiveStore.PromoteArchive(ctx, stagingKey, objectKey); promoteErr != nil {
		return cachepkg.SaveResult{}, metrics, promoteErr
	}
	metrics.promotionDuration = time.Since(promotionStarted)
	promoted = true
	return saved, metrics, nil
}

type cacheArchiveValidationResult struct {
	metrics  workspace.ArchiveValidationMetrics
	duration time.Duration
	err      error
}

type cacheArchiveMetrics struct {
	uploadDuration     time.Duration
	validationDuration time.Duration
	promotionDuration  time.Duration
	uncompressedBytes  int64
	entries            int
}

type cacheTransferMetrics struct {
	operation            string
	preset               string
	cacheKey             string
	outcome              string
	transferMode         string
	compressedBytes      int64
	metadataDuration     time.Duration
	remoteOpenDuration   time.Duration
	claimDuration        time.Duration
	finalizationDuration time.Duration
	archive              cacheArchiveMetrics
	claimLifecycle       string
}

func logCacheTransferMetrics(metrics cacheTransferMetrics, total time.Duration) {
	log.Printf("INFO cache_transfer operation=%s outcome=%s transfer_mode=%s preset=%s cache_key=%s compressed_bytes=%d uncompressed_bytes=%d archive_entries=%d metadata_resolution_ms=%d remote_open_ms=%d claim_acquisition_ms=%d remote_upload_ms=%d gzip_tar_validation_ms=%d promotion_ms=%d finalization_ms=%d claim_lifecycle=%s total_ms=%d", metrics.operation, metrics.outcome, metrics.transferMode, metrics.preset, metrics.cacheKey, metrics.compressedBytes, metrics.archive.uncompressedBytes, metrics.archive.entries, metrics.metadataDuration.Milliseconds(), metrics.remoteOpenDuration.Milliseconds(), metrics.claimDuration.Milliseconds(), metrics.archive.uploadDuration.Milliseconds(), metrics.archive.validationDuration.Milliseconds(), metrics.archive.promotionDuration.Milliseconds(), metrics.finalizationDuration.Milliseconds(), metrics.claimLifecycle, total.Milliseconds())
}

func valueOrZero(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func directCacheArchiveDigest(entry domain.CacheEntry) (string, bool) {
	if entry.Compression != "tar.gz" {
		return "", false
	}
	checksum := strings.TrimSpace(entry.Checksum)
	contentDigest := strings.TrimSpace(entry.ContentDigest)
	if contentDigest == "sha256:"+checksum {
		return contentDigest, true
	}
	if contentDigest != "" || len(checksum) != sha256.Size*2 {
		return "", false
	}
	if _, err := hex.DecodeString(checksum); err != nil {
		return "", false
	}
	return "sha256:" + checksum, true
}

func (s *WorkspaceHelperCacheService) cacheContentIsReady(ctx context.Context, jobID, preset, cacheKey string, publication domain.WorkspaceRevisionPublication) bool {
	entry, found, err := s.entries.FindReadyByKey(ctx, jobID, preset, cacheKey)
	if err != nil || !found || publication.SizeBytes == nil {
		return false
	}
	contentDigest := strings.TrimSpace(publication.ContentDigest)
	return entry.Status == domain.CacheEntryStatusReady &&
		entry.ContentDigest == contentDigest &&
		entry.Compression == "tar.gz" &&
		entry.Checksum == strings.TrimPrefix(contentDigest, "sha256:") &&
		entry.SizeBytes == *publication.SizeBytes &&
		entry.ObjectKey == cacheObjectKey(jobID, preset, cacheKey, contentDigest) &&
		entry.StorageProvider == s.store.Provider()
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
