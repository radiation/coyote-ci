package domain

import "time"

type VersionTagTargetType string

type VersionTagKind string

const (
	VersionTagTargetArtifact            VersionTagTargetType = "artifact"
	VersionTagTargetManagedImageVersion VersionTagTargetType = "managed_image_version"
	VersionTagKindVersion               VersionTagKind       = "version"
	VersionTagKindChannel               VersionTagKind       = "channel"
)

// VersionTag is the compatibility read model returned by HTTP APIs. Artifact
// labels are stored in package/version/channel tables, while managed image
// version labels remain backed by version_tags.
type VersionTag struct {
	ID                    string
	JobID                 string
	Kind                  VersionTagKind
	Version               string
	TargetType            VersionTagTargetType
	ArtifactID            *string
	ManagedImageVersionID *string
	CreatedAt             time.Time
}
