package domain

import (
	"sort"
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

// ArtifactRecord is the read-model source row for repository browsing.
// It combines one artifact instance with the build and optional step that
// produced it.
type ArtifactRecord struct {
	Artifact BuildArtifact
	Build    Build
	Step     *BuildStep
}

// ArtifactVersion is one visible version entry under a browsed Artifact.
// VersionTag remains the immutable assignment record attached to the
// produced artifact instance.
type ArtifactVersion struct {
	Artifact BuildArtifact
	Build    Build
	Step     *BuildStep
}

// Artifact is the synthesized repository identity users browse today.
// It is derived from persisted artifact instances; this PR does not add a
// first-class artifact identity table.
type Artifact struct {
	Key             string
	Name            string
	Path            string
	ProjectID       string
	JobID           *string
	ArtifactType    ArtifactType
	LatestCreatedAt time.Time
	Versions        []ArtifactVersion
}

type ArtifactBrowseRecord = ArtifactRecord

type ArtifactBrowseVersion = ArtifactVersion

type ArtifactBrowseItem = Artifact

// ArtifactIdentityKey returns the synthesized repository identity for a
// produced artifact instance. Until a future feature requires a dedicated
// artifact identity table, job+logical-path is the canonical browse key.
func ArtifactIdentityKey(build Build, artifact BuildArtifact) string {
	if build.JobID != nil && strings.TrimSpace(*build.JobID) != "" {
		return strings.TrimSpace(*build.JobID) + "::" + artifact.LogicalPath
	}
	return build.ID + "::" + artifact.LogicalPath
}

func ArtifactIdentityKeyFromRecord(record ArtifactRecord) string {
	return ArtifactIdentityKey(record.Build, record.Artifact)
}

func GroupArtifacts(records []ArtifactRecord) []Artifact {
	grouped := make(map[string]*Artifact, len(records))
	order := make([]string, 0, len(records))
	for _, record := range records {
		key := ArtifactIdentityKeyFromRecord(record)
		item, ok := grouped[key]
		if !ok {
			item = &Artifact{
				Key:             key,
				Name:            record.Artifact.Name,
				Path:            record.Artifact.LogicalPath,
				ProjectID:       record.Build.ProjectID,
				JobID:           record.Build.JobID,
				ArtifactType:    ResolveArtifactType(record.Artifact),
				LatestCreatedAt: record.Artifact.CreatedAt,
				Versions:        make([]ArtifactVersion, 0, 1),
			}
			grouped[key] = item
			order = append(order, key)
		}

		item.Versions = append(item.Versions, ArtifactVersion(record))
		if record.Artifact.CreatedAt.After(item.LatestCreatedAt) {
			item.LatestCreatedAt = record.Artifact.CreatedAt
			item.Name = record.Artifact.Name
			item.ArtifactType = ResolveArtifactType(record.Artifact)
		}
	}

	items := make([]Artifact, 0, len(order))
	for _, key := range order {
		item := grouped[key]
		sort.SliceStable(item.Versions, func(i, j int) bool {
			left := item.Versions[i]
			right := item.Versions[j]
			if !left.Artifact.CreatedAt.Equal(right.Artifact.CreatedAt) {
				return left.Artifact.CreatedAt.After(right.Artifact.CreatedAt)
			}
			return left.Build.BuildNumber > right.Build.BuildNumber
		})
		items = append(items, *item)
	}

	sort.SliceStable(items, func(i, j int) bool {
		if !items[i].LatestCreatedAt.Equal(items[j].LatestCreatedAt) {
			return items[i].LatestCreatedAt.After(items[j].LatestCreatedAt)
		}
		if items[i].Path != items[j].Path {
			return items[i].Path < items[j].Path
		}
		return items[i].Key < items[j].Key
	})

	return items
}
