package domain

import (
	"path/filepath"
	"strings"
)

// InferArtifactType derives an artifact type from persisted instance metadata.
// This is the single source of truth for heuristic detection when a pipeline
// declaration did not specify the type explicitly.
func InferArtifactType(logicalPath string, contentType *string) ArtifactType {
	lowerPath := strings.ToLower(strings.TrimSpace(logicalPath))
	lowerContentType := ""
	if contentType != nil {
		lowerContentType = strings.ToLower(strings.TrimSpace(*contentType))
	}

	switch {
	case strings.Contains(lowerContentType, "docker") || strings.Contains(lowerContentType, "oci"):
		return ArtifactTypeDockerImage
	case strings.HasSuffix(lowerPath, ".oci"):
		return ArtifactTypeDockerImage
	case strings.HasSuffix(lowerPath, ".tar") && (strings.Contains(lowerPath, "docker") || strings.Contains(lowerPath, "image") || strings.Contains(lowerPath, "container")):
		return ArtifactTypeDockerImage
	case strings.HasSuffix(lowerPath, ".tgz"):
		base := filepath.Base(lowerPath)
		if strings.Contains(base, "-") || strings.HasPrefix(base, "@") {
			return ArtifactTypeNPMPackage
		}
		return ArtifactTypeGeneric
	case lowerContentType == "" && filepath.Ext(lowerPath) == "":
		return ArtifactTypeUnknown
	case lowerPath == "" && lowerContentType == "":
		return ArtifactTypeUnknown
	default:
		return ArtifactTypeGeneric
	}
}

// ResolveArtifactType returns the persisted type when present and otherwise
// falls back to the shared detection heuristic.
func ResolveArtifactType(artifact BuildArtifact) ArtifactType {
	if artifact.ArtifactType != "" {
		return artifact.ArtifactType
	}
	return InferArtifactType(artifact.LogicalPath, artifact.ContentType)
}
