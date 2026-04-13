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
	Compression      string
	Status           CacheEntryStatus
	CreatedByBuildID string
	CreatedByStepID  string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	LastAccessedAt   *time.Time
}
