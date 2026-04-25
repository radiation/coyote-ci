package domain

import "time"

type VersionTagTargetType string

const (
	VersionTagTargetArtifact            VersionTagTargetType = "artifact"
	VersionTagTargetManagedImageVersion VersionTagTargetType = "managed_image_version"
)

// VersionTag is an immutable job-scoped version label attached to one target.
type VersionTag struct {
	ID                    string
	JobID                 string
	Version               string
	TargetType            VersionTagTargetType
	ArtifactID            *string
	ManagedImageVersionID *string
	CreatedAt             time.Time
}
