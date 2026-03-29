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
const workspacePreparedMarker = ".coyote-workspace-prepared"

var ErrWorkspaceRepoFetcherNotConfigured = errors.New("repo fetcher not configured")
var ErrWorkspaceRefRequired = errors.New("ref is required")

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
	fetcher RepoFetcher
	root    string

	mu sync.Mutex
}

func NewHostWorkspaceMaterializer(fetcher RepoFetcher, root string) *HostWorkspaceMaterializer {
	trimmedRoot := strings.TrimSpace(root)
	if trimmedRoot == "" {
		trimmedRoot = filepath.Join(os.TempDir(), defaultWorkspaceDirName)
	}

	return &HostWorkspaceMaterializer{
		fetcher: fetcher,
		root:    trimmedRoot,
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

	requiresRepo := strings.TrimSpace(request.RepoURL) != ""
	if m.isWorkspacePrepared(workspacePath, requiresRepo) {
		return workspacePath, nil
	}

	if err := m.ensureWorkspaceRootExists(workspacePath); err != nil {
		return "", fmt.Errorf("creating workspace root: %w", err)
	}

	repoURL := strings.TrimSpace(request.RepoURL)
	if repoURL == "" {
		return workspacePath, m.prepareEmptyWorkspace(workspacePath)
	}

	if m.fetcher == nil {
		return "", ErrWorkspaceRepoFetcherNotConfigured
	}

	ref := strings.TrimSpace(request.CommitSHA)
	if ref == "" {
		ref = strings.TrimSpace(request.Ref)
	}
	if ref == "" {
		return "", ErrWorkspaceRefRequired
	}

	tmpPath, _, err := m.fetcher.Fetch(ctx, repoURL, ref)
	if err != nil {
		return "", fmt.Errorf("fetching repo workspace: %w", err)
	}

	moved := false
	defer func() {
		if !moved && tmpPath != "" {
			_ = os.RemoveAll(tmpPath)
		}
	}()

	if err := m.replaceWorkspaceFromSource(tmpPath, workspacePath); err != nil {
		return "", err
	}
	moved = true

	if err := m.markWorkspacePrepared(workspacePath); err != nil {
		return "", err
	}

	return workspacePath, nil
}

func (m *HostWorkspaceMaterializer) ensureWorkspaceRootExists(workspacePath string) error {
	return os.MkdirAll(filepath.Dir(workspacePath), 0o755)
}

func (m *HostWorkspaceMaterializer) prepareEmptyWorkspace(workspacePath string) error {
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		return fmt.Errorf("creating empty workspace: %w", err)
	}
	if err := m.markWorkspacePrepared(workspacePath); err != nil {
		return err
	}
	return nil
}

func (m *HostWorkspaceMaterializer) replaceWorkspaceFromSource(tmpPath string, workspacePath string) error {
	if err := os.RemoveAll(workspacePath); err != nil {
		return fmt.Errorf("resetting workspace path: %w", err)
	}
	if err := os.Rename(tmpPath, workspacePath); err != nil {
		return fmt.Errorf("materializing workspace path: %w", err)
	}
	return nil
}

func (m *HostWorkspaceMaterializer) markerPath(workspacePath string) string {
	return filepath.Join(workspacePath, workspacePreparedMarker)
}

func (m *HostWorkspaceMaterializer) markWorkspacePrepared(workspacePath string) error {
	markerPath := m.markerPath(workspacePath)
	if err := os.WriteFile(markerPath, []byte("ready\n"), 0o644); err != nil {
		return fmt.Errorf("marking workspace prepared: %w", err)
	}
	return nil
}

func (m *HostWorkspaceMaterializer) isWorkspacePrepared(workspacePath string, requiresRepo bool) bool {
	info, err := os.Stat(workspacePath)
	if err != nil || !info.IsDir() {
		return false
	}

	if _, markerErr := os.Stat(m.markerPath(workspacePath)); markerErr != nil {
		return false
	}

	if !requiresRepo {
		return true
	}

	gitPath := filepath.Join(workspacePath, ".git")
	gitInfo, gitErr := os.Stat(gitPath)
	if gitErr != nil || !gitInfo.IsDir() {
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
