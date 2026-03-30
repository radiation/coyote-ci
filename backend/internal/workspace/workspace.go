package workspace

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
)

const DefaultContainerRoot = "/workspace"

// Workspace defines the host/container path contract for one build workspace.
type Workspace struct {
	BuildID       string
	HostRoot      string
	ContainerRoot string
}

func New(buildID string, hostRoot string) Workspace {
	return Workspace{
		BuildID:       strings.TrimSpace(buildID),
		HostRoot:      filepath.Clean(strings.TrimSpace(hostRoot)),
		ContainerRoot: DefaultContainerRoot,
	}
}

func (w Workspace) ContainerWorkingDir(requested string) string {
	containerRoot := strings.TrimSpace(w.ContainerRoot)
	if containerRoot == "" {
		containerRoot = DefaultContainerRoot
	}

	trimmed := strings.TrimSpace(requested)
	if trimmed == "" || trimmed == "." {
		return containerRoot
	}

	if strings.HasPrefix(trimmed, "/") {
		cleanAbs := path.Clean(trimmed)
		if cleanAbs == containerRoot || strings.HasPrefix(cleanAbs, containerRoot+"/") {
			return cleanAbs
		}
		return containerRoot
	}

	cleanRel := path.Clean(strings.ReplaceAll(trimmed, "\\", "/"))
	if cleanRel == "." || cleanRel == ".." || strings.HasPrefix(cleanRel, "../") {
		return containerRoot
	}

	resolved := path.Clean(path.Join(containerRoot, cleanRel))
	if resolved == containerRoot || strings.HasPrefix(resolved, containerRoot+"/") {
		return resolved
	}

	return containerRoot
}

func (w Workspace) ValidateArtifactPath(rel string) error {
	trimmed := strings.TrimSpace(rel)
	if trimmed == "" {
		return fmt.Errorf("artifact path is required")
	}

	normalized := strings.ReplaceAll(trimmed, "\\", "/")
	if path.IsAbs(normalized) || filepath.IsAbs(trimmed) {
		return fmt.Errorf("artifact path must be workspace-relative")
	}

	cleanRel := path.Clean(normalized)
	if cleanRel == "." || cleanRel == ".." || strings.HasPrefix(cleanRel, "../") {
		return fmt.Errorf("artifact path escapes workspace")
	}

	return nil
}

func (w Workspace) ResolveRelativePath(rel string) (string, error) {
	hostRoot := strings.TrimSpace(w.HostRoot)
	if hostRoot == "" {
		return "", fmt.Errorf("workspace host root is required")
	}

	trimmed := strings.TrimSpace(rel)
	if trimmed == "" || trimmed == "." {
		return filepath.Clean(hostRoot), nil
	}

	if err := w.ValidateArtifactPath(trimmed); err != nil {
		return "", err
	}

	cleanRel := path.Clean(strings.ReplaceAll(trimmed, "\\", "/"))
	resolved := filepath.Clean(filepath.Join(hostRoot, filepath.FromSlash(cleanRel)))

	relCheck, err := filepath.Rel(filepath.Clean(hostRoot), resolved)
	if err != nil {
		return "", err
	}
	if relCheck == ".." || strings.HasPrefix(relCheck, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes workspace")
	}

	return resolved, nil
}
