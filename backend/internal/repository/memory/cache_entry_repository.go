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
}

func NewCacheEntryRepository() *CacheEntryRepository {
	return &CacheEntryRepository{
		entries: make(map[string]domain.CacheEntry),
		index:   make(map[string]string),
	}
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

	now := time.Now().UTC()
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
	return entry, nil
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
