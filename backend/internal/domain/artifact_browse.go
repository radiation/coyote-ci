package domain

import (
	"strings"
	"time"
)

type ArtifactType string

const (
	ArtifactTypeDockerImage ArtifactType = "docker_image"
	ArtifactTypeNPMPackage  ArtifactType = "npm_package"
	ArtifactTypeGeneric     ArtifactType = "generic"
	ArtifactTypeUnknown     ArtifactType = "unknown"
)

func ParseArtifactType(value string) (ArtifactType, bool) {
	switch ArtifactType(strings.TrimSpace(value)) {
	case ArtifactTypeDockerImage:
		return ArtifactTypeDockerImage, true
	case ArtifactTypeNPMPackage:
		return ArtifactTypeNPMPackage, true
	case ArtifactTypeGeneric:
		return ArtifactTypeGeneric, true
	case ArtifactTypeUnknown:
		return ArtifactTypeUnknown, true
	default:
		return "", false
	}
}

type ArtifactBrowseRecord struct {
	Artifact BuildArtifact
	Build    Build
	Step     *BuildStep
}

type ArtifactBrowseVersion struct {
	Artifact BuildArtifact
	Build    Build
	Step     *BuildStep
}

type ArtifactBrowseItem struct {
	GroupKey        string
	Name            string
	Path            string
	ProjectID       string
	JobID           *string
	ArtifactType    ArtifactType
	LatestCreatedAt time.Time
	Versions        []ArtifactBrowseVersion
}
