package domain

import "time"

// BuildArtifact is persisted metadata for a collected build output.
type BuildArtifact struct {
	ID             string
	BuildID        string
	LogicalPath    string
	StorageKey     string
	SizeBytes      int64
	ContentType    *string
	ChecksumSHA256 *string
	CreatedAt      time.Time
}
