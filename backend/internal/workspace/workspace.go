package workspace

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
)

const DefaultContainerRoot = "/workspace"
const TriggerArtifactsRelativeRoot = ".coyote/trigger-artifacts"

// Workspace defines the host/container path contract for one build workspace.
type Workspace struct {
	BuildID       string
	HostRoot      string
	ContainerRoot string
}

func New(buildID string, hostRoot string) Workspace {
	trimmedHostRoot := strings.TrimSpace(hostRoot)
	if trimmedHostRoot != "" {
		trimmedHostRoot = filepath.Clean(trimmedHostRoot)
	}

	return Workspace{
		BuildID:       strings.TrimSpace(buildID),
		HostRoot:      trimmedHostRoot,
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

func TriggerArtifactRelativePath(logicalPath string) (string, error) {
	trimmed := strings.TrimSpace(logicalPath)
	if trimmed == "" {
		return "", fmt.Errorf("trigger artifact path is required")
	}
	if looksLikeWindowsDrivePath(trimmed) {
		return "", fmt.Errorf("trigger artifact path must be workspace-relative")
	}

	validator := New("", "")
	if err := validator.ValidateArtifactPath(trimmed); err != nil {
		return "", err
	}

	cleanRel := path.Clean(strings.ReplaceAll(trimmed, "\\", "/"))
	return path.Clean(path.Join(TriggerArtifactsRelativeRoot, cleanRel)), nil
}

func ResolveVisiblePath(workspaceRoot string, relativePath string) string {
	trimmedRoot := strings.TrimSpace(workspaceRoot)
	if trimmedRoot == "" {
		return ""
	}
	cleanRoot := filepath.Clean(trimmedRoot)
	containerRoot := path.Clean(trimmedRoot)
	trimmedRel := strings.TrimSpace(relativePath)
	if trimmedRel == "" || trimmedRel == "." {
		if isContainerWorkspaceRoot(trimmedRoot) {
			return containerRoot
		}
		return cleanRoot
	}

	if err := (Workspace{}).ValidateArtifactPath(trimmedRel); err != nil {
		if isContainerWorkspaceRoot(trimmedRoot) {
			return containerRoot
		}
		return cleanRoot
	}

	cleanRel := path.Clean(strings.ReplaceAll(trimmedRel, "\\", "/"))
	if isContainerWorkspaceRoot(trimmedRoot) {
		return path.Join(trimmedRoot, cleanRel)
	}
	return filepath.Join(cleanRoot, filepath.FromSlash(cleanRel))
}

func ResolveVisibleWorkingDir(workspaceRoot string, requested string) string {
	trimmedRoot := strings.TrimSpace(workspaceRoot)
	if trimmedRoot == "" {
		trimmedRoot = DefaultContainerRoot
	}
	if isContainerWorkspaceRoot(trimmedRoot) {
		ws := Workspace{ContainerRoot: trimmedRoot}
		return ws.ContainerWorkingDir(requested)
	}

	hostRoot := filepath.Clean(trimmedRoot)
	ws := Workspace{HostRoot: hostRoot}
	trimmedRequested := strings.TrimSpace(requested)
	if trimmedRequested == "" || trimmedRequested == "." {
		return hostRoot
	}
	if filepath.IsAbs(trimmedRequested) || looksLikeWindowsDrivePath(trimmedRequested) {
		resolved := filepath.Clean(trimmedRequested)
		relCheck, err := filepath.Rel(hostRoot, resolved)
		if err == nil && relCheck != ".." && !strings.HasPrefix(relCheck, ".."+string(filepath.Separator)) {
			return resolved
		}
		return hostRoot
	}

	resolved, err := ws.ResolveRelativePath(trimmedRequested)
	if err != nil {
		return hostRoot
	}
	return resolved
}

func (w Workspace) ValidateArtifactPath(rel string) error {
	trimmed := strings.TrimSpace(rel)
	if trimmed == "" {
		return fmt.Errorf("artifact path is required")
	}
	if looksLikeWindowsDrivePath(trimmed) {
		return fmt.Errorf("artifact path must be workspace-relative")
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

func looksLikeWindowsDrivePath(value string) bool {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) < 2 || trimmed[1] != ':' {
		return false
	}
	first := trimmed[0]
	return (first >= 'a' && first <= 'z') || (first >= 'A' && first <= 'Z')
}

func isContainerWorkspaceRoot(root string) bool {
	return path.Clean(strings.ReplaceAll(strings.TrimSpace(root), "\\", "/")) == DefaultContainerRoot
}
