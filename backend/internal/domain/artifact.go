package domain

import "time"

// StorageProvider identifies the backend that holds artifact content.
type StorageProvider string

const (
	StorageProviderFilesystem StorageProvider = "filesystem"
	StorageProviderGCS        StorageProvider = "gcs"
)

// BuildArtifact is persisted metadata for a collected build output.
type BuildArtifact struct {
	ID              string
	BuildID         string
	StepID          *string // nullable; set when artifact came from a specific step
	LogicalPath     string
	StorageKey      string
	StorageProvider StorageProvider
	SizeBytes       int64
	ContentType     *string
	ChecksumSHA256  *string
	VersionTags     []VersionTag
	CreatedAt       time.Time
}
