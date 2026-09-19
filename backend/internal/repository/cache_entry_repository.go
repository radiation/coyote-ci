package repository

import (
	"context"
	"errors"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

var ErrCacheEntryNotFound = errors.New("cache entry not found")
var ErrCachePublishClaimStale = errors.New("cache publish claim is stale")
var ErrCachePublishClaimReplaced = errors.New("cache publish claim was replaced")

type CacheEntryUpsertInput struct {
	JobID            string
	Preset           string
	CacheKey         string
	StorageProvider  domain.StorageProvider
	ObjectKey        string
	SizeBytes        int64
	Checksum         string
	ContentDigest    string
	Compression      string
	Status           domain.CacheEntryStatus
	CreatedByBuildID string
	CreatedByStepID  string
}

type CacheEntryRepository interface {
	FindReadyByKey(ctx context.Context, jobID string, preset string, cacheKey string) (domain.CacheEntry, bool, error)
	Upsert(ctx context.Context, input CacheEntryUpsertInput) (domain.CacheEntry, error)
	MarkAccessed(ctx context.Context, id string, accessedAt time.Time) error
	TryAcquirePublishClaim(ctx context.Context, jobID, preset, cacheKey, claimant string, now time.Time, lease time.Duration) (domain.CachePublishClaim, bool, error)
	ValidatePublishClaim(ctx context.Context, claim domain.CachePublishClaim, claimant string, now time.Time) error
	ValidatePublishClaimOwnership(ctx context.Context, claim domain.CachePublishClaim, claimant string) error
	CompletePublishClaim(ctx context.Context, claim domain.CachePublishClaim, input CacheEntryUpsertInput, now time.Time) (domain.CacheEntry, error)
	ReleasePublishClaim(ctx context.Context, claim domain.CachePublishClaim) error
}
