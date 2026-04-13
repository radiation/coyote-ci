package repository

import (
	"context"
	"errors"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

var ErrCacheEntryNotFound = errors.New("cache entry not found")

type CacheEntryUpsertInput struct {
	JobID            string
	Preset           string
	CacheKey         string
	StorageProvider  domain.StorageProvider
	ObjectKey        string
	SizeBytes        int64
	Checksum         string
	Compression      string
	Status           domain.CacheEntryStatus
	CreatedByBuildID string
	CreatedByStepID  string
}

type CacheEntryRepository interface {
	FindReadyByKey(ctx context.Context, jobID string, preset string, cacheKey string) (domain.CacheEntry, bool, error)
	Upsert(ctx context.Context, input CacheEntryUpsertInput) (domain.CacheEntry, error)
	MarkAccessed(ctx context.Context, id string, accessedAt time.Time) error
}
