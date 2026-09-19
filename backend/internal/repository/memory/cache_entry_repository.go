package memory

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type CacheEntryRepository struct {
	mu      sync.RWMutex
	entries map[string]domain.CacheEntry
	index   map[string]string
	claims  map[string]domain.CachePublishClaim
}

func NewCacheEntryRepository() *CacheEntryRepository {
	return &CacheEntryRepository{
		entries: make(map[string]domain.CacheEntry),
		index:   make(map[string]string),
		claims:  make(map[string]domain.CachePublishClaim),
	}
}

func (r *CacheEntryRepository) TryAcquirePublishClaim(_ context.Context, jobID, preset, cacheKey, claimant string, now time.Time, lease time.Duration) (domain.CachePublishClaim, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := composeCacheKey(jobID, preset, cacheKey)
	if existing, found := r.claims[key]; found && existing.ExpiresAt.After(now.UTC()) {
		return existing, false, nil
	}
	claim := domain.CachePublishClaim{JobID: strings.TrimSpace(jobID), Preset: strings.TrimSpace(strings.ToLower(preset)), CacheKey: strings.TrimSpace(cacheKey), ClaimToken: uuid.NewString(), ClaimedBy: strings.TrimSpace(claimant), ClaimedAt: now.UTC(), ExpiresAt: now.UTC().Add(lease)}
	if _, found := r.claims[key]; found {
		claim.Reclaimed = true
	}
	r.claims[key] = claim
	return claim, true, nil
}

func (r *CacheEntryRepository) ReleasePublishClaim(_ context.Context, claim domain.CachePublishClaim) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := composeCacheKey(claim.JobID, claim.Preset, claim.CacheKey)
	existing, found := r.claims[key]
	if !found || existing.ClaimToken != claim.ClaimToken {
		return nil
	}
	delete(r.claims, key)
	return nil
}

func (r *CacheEntryRepository) ValidatePublishClaim(_ context.Context, claim domain.CachePublishClaim, claimant string, now time.Time) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	existing, found := r.claims[composeCacheKey(claim.JobID, claim.Preset, claim.CacheKey)]
	if !found ||
		existing.ClaimToken != strings.TrimSpace(claim.ClaimToken) ||
		existing.ClaimedBy != strings.TrimSpace(claimant) ||
		!existing.ExpiresAt.After(now.UTC()) {
		return repository.ErrCachePublishClaimStale
	}
	return nil
}

func (r *CacheEntryRepository) ValidatePublishClaimOwnership(_ context.Context, claim domain.CachePublishClaim, claimant string) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	existing, found := r.claims[composeCacheKey(claim.JobID, claim.Preset, claim.CacheKey)]
	if !found ||
		existing.ClaimToken != strings.TrimSpace(claim.ClaimToken) ||
		existing.ClaimedBy != strings.TrimSpace(claimant) {
		return repository.ErrCachePublishClaimStale
	}
	return nil
}

func (r *CacheEntryRepository) CompletePublishClaim(_ context.Context, claim domain.CachePublishClaim, input repository.CacheEntryUpsertInput, now time.Time) (domain.CacheEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := composeCacheKey(claim.JobID, claim.Preset, claim.CacheKey)
	existing, found := r.claims[key]
	if !found {
		return domain.CacheEntry{}, repository.ErrCachePublishClaimStale
	}
	if existing.ClaimToken != claim.ClaimToken {
		return domain.CacheEntry{}, repository.ErrCachePublishClaimReplaced
	}
	entry := r.upsertLocked(input, now.UTC())
	delete(r.claims, key)
	return entry, nil
}

func (r *CacheEntryRepository) FindReadyByKey(_ context.Context, jobID string, preset string, cacheKey string) (domain.CacheEntry, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	id, ok := r.index[composeCacheKey(jobID, preset, cacheKey)]
	if !ok {
		return domain.CacheEntry{}, false, nil
	}
	entry, ok := r.entries[id]
	if !ok || entry.Status != domain.CacheEntryStatusReady {
		return domain.CacheEntry{}, false, nil
	}
	return entry, true, nil
}

func (r *CacheEntryRepository) Upsert(_ context.Context, input repository.CacheEntryUpsertInput) (domain.CacheEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.upsertLocked(input, time.Now().UTC()), nil
}

func (r *CacheEntryRepository) upsertLocked(input repository.CacheEntryUpsertInput, now time.Time) domain.CacheEntry {
	indexKey := composeCacheKey(input.JobID, input.Preset, input.CacheKey)
	id, found := r.index[indexKey]
	entry := domain.CacheEntry{}
	if found {
		entry = r.entries[id]
	} else {
		entry.ID = uuid.NewString()
		entry.CreatedAt = now
	}

	entry.JobID = strings.TrimSpace(input.JobID)
	entry.Preset = strings.TrimSpace(strings.ToLower(input.Preset))
	entry.CacheKey = strings.TrimSpace(input.CacheKey)
	entry.StorageProvider = input.StorageProvider
	entry.ObjectKey = strings.TrimSpace(input.ObjectKey)
	entry.SizeBytes = input.SizeBytes
	entry.Checksum = strings.TrimSpace(input.Checksum)
	entry.ContentDigest = strings.TrimSpace(input.ContentDigest)
	entry.Compression = strings.TrimSpace(input.Compression)
	entry.Status = input.Status
	entry.CreatedByBuildID = strings.TrimSpace(input.CreatedByBuildID)
	entry.CreatedByStepID = strings.TrimSpace(input.CreatedByStepID)
	entry.UpdatedAt = now
	if !found {
		entry.LastAccessedAt = nil
	}

	r.entries[entry.ID] = entry
	r.index[indexKey] = entry.ID
	return entry
}

func (r *CacheEntryRepository) MarkAccessed(_ context.Context, id string, accessedAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, ok := r.entries[id]
	if !ok {
		return repository.ErrCacheEntryNotFound
	}
	at := accessedAt.UTC()
	entry.LastAccessedAt = &at
	entry.UpdatedAt = time.Now().UTC()
	r.entries[id] = entry
	return nil
}

func composeCacheKey(jobID string, preset string, cacheKey string) string {
	return strings.TrimSpace(jobID) + "|" + strings.TrimSpace(strings.ToLower(preset)) + "|" + strings.TrimSpace(cacheKey)
}
