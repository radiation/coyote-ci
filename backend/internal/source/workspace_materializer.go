package source

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const defaultWorkspaceDirName = "coyote-builds"

// WorkspacePrepareRequest contains source metadata used to prepare a build workspace.
type WorkspacePrepareRequest struct {
	BuildID   string
	RepoURL   string
	Ref       string
	CommitSHA string
}

// WorkspaceMaterializer prepares host workspaces for build execution.
type WorkspaceMaterializer interface {
	PrepareWorkspace(ctx context.Context, request WorkspacePrepareRequest) (string, error)
	CleanupWorkspace(ctx context.Context, buildID string) error
}

// HostWorkspaceMaterializer prepares build workspaces on the host filesystem.
type HostWorkspaceMaterializer struct {
	root string

	mu sync.Mutex
}

func NewHostWorkspaceMaterializer(root string) *HostWorkspaceMaterializer {
	trimmedRoot := strings.TrimSpace(root)
	if trimmedRoot == "" {
		trimmedRoot = filepath.Join(os.TempDir(), defaultWorkspaceDirName)
	}
	trimmedRoot = normalizeWorkspaceRootPath(trimmedRoot)

	return &HostWorkspaceMaterializer{
		root: trimmedRoot,
	}
}

func (m *HostWorkspaceMaterializer) PrepareWorkspace(ctx context.Context, request WorkspacePrepareRequest) (string, error) {
	buildID := strings.TrimSpace(request.BuildID)
	if buildID == "" {
		return "", errors.New("build id is required")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.root = canonicalizeExistingPath(m.root)
	workspacePath := filepath.Join(m.root, buildID)

	if m.isWorkspacePrepared(workspacePath) {
		return canonicalizeExistingPath(workspacePath), nil
	}

	if err := m.ensureWorkspaceRootExists(); err != nil {
		return "", fmt.Errorf("creating workspace root: %w", err)
	}

	if err := m.prepareEmptyWorkspace(workspacePath); err != nil {
		return "", err
	}

	return canonicalizeExistingPath(workspacePath), nil
}

func (m *HostWorkspaceMaterializer) ensureWorkspaceRootExists() error {
	return os.MkdirAll(m.root, 0o755)
}

func (m *HostWorkspaceMaterializer) prepareEmptyWorkspace(workspacePath string) error {
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		return fmt.Errorf("creating empty workspace: %w", err)
	}
	return nil
}

func (m *HostWorkspaceMaterializer) isWorkspacePrepared(workspacePath string) bool {
	info, err := os.Stat(workspacePath)
	if err != nil || !info.IsDir() {
		return false
	}
	return true
}

func (m *HostWorkspaceMaterializer) CleanupWorkspace(_ context.Context, buildID string) error {
	trimmedBuildID := strings.TrimSpace(buildID)
	if trimmedBuildID == "" {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	workspacePath := filepath.Join(m.root, trimmedBuildID)
	if err := os.RemoveAll(workspacePath); err != nil {
		return fmt.Errorf("removing workspace: %w", err)
	}
	return nil
}

func (m *HostWorkspaceMaterializer) WorkspaceRoot() string {
	return m.root
}

func normalizeWorkspaceRootPath(root string) string {
	cleaned := filepath.Clean(root)
	absPath, err := filepath.Abs(cleaned)
	if err == nil {
		cleaned = absPath
	}

	return canonicalizeExistingPath(cleaned)
}

func canonicalizeExistingPath(path string) string {
	cleaned := filepath.Clean(strings.TrimSpace(path))
	if cleaned == "" {
		return ""
	}

	resolved, err := filepath.EvalSymlinks(cleaned)
	if err == nil {
		return filepath.Clean(resolved)
	}

	return cleaned
}
