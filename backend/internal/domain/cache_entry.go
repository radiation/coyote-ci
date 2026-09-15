package domain

import "time"

type CacheEntryStatus string

const (
	CacheEntryStatusWriting CacheEntryStatus = "writing"
	CacheEntryStatusReady   CacheEntryStatus = "ready"
	CacheEntryStatusFailed  CacheEntryStatus = "failed"
	CacheEntryStatusDeleted CacheEntryStatus = "deleted"
)

type CacheEntry struct {
	ID               string
	JobID            string
	Preset           string
	CacheKey         string
	StorageProvider  StorageProvider
	ObjectKey        string
	SizeBytes        int64
	Checksum         string
	ContentDigest    string
	Compression      string
	Status           CacheEntryStatus
	CreatedByBuildID string
	CreatedByStepID  string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	LastAccessedAt   *time.Time
}

type CachePublishClaim struct {
	JobID      string
	Preset     string
	CacheKey   string
	ClaimToken string
	ClaimedBy  string
	ClaimedAt  time.Time
	ExpiresAt  time.Time
	Reclaimed  bool
}
