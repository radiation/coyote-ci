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

func NewHostWorkspaceMaterializer(_ RepoFetcher, root string) *HostWorkspaceMaterializer {
	trimmedRoot := strings.TrimSpace(root)
	if trimmedRoot == "" {
		trimmedRoot = filepath.Join(os.TempDir(), defaultWorkspaceDirName)
	}

	return &HostWorkspaceMaterializer{
		root: trimmedRoot,
	}
}

func (m *HostWorkspaceMaterializer) PrepareWorkspace(ctx context.Context, request WorkspacePrepareRequest) (string, error) {
	buildID := strings.TrimSpace(request.BuildID)
	if buildID == "" {
		return "", errors.New("build id is required")
	}

	workspacePath := filepath.Join(m.root, buildID)

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.isWorkspacePrepared(workspacePath) {
		return workspacePath, nil
	}

	if err := m.ensureWorkspaceRootExists(); err != nil {
		return "", fmt.Errorf("creating workspace root: %w", err)
	}

	return workspacePath, m.prepareEmptyWorkspace(workspacePath)
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
