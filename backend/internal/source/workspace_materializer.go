package source

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

const defaultWorkspaceDirName = "coyote-builds"

var ErrWorkspaceFanOutUnsupported = errors.New("workspace fan-out materialization is not supported")
var ErrWorkspaceFanInUnsupported = errors.New("workspace fan-in materialization is not supported")
var ErrWorkspaceInputUnsupported = errors.New("workspace input plan is not supported")

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

// MaterializeWorkspaceRequest describes the logical workspace baseline needed
// by one execution. It intentionally contains no storage-provider details.
type MaterializeWorkspaceRequest struct {
	BuildID string
	Input   domain.WorkspaceInputPlan
}

// MaterializedWorkspace is the writable filesystem supplied to execution.
// Later implementations may associate it with durable revision state without
// exposing that storage detail to runners.
type MaterializedWorkspace struct {
	BuildID string
	Path    string
	Input   domain.WorkspaceInputPlan
}

// ExecutionWorkspaceMaterializer bridges a logical workspace input to a
// writable execution filesystem and records its successful logical advance.
type ExecutionWorkspaceMaterializer interface {
	Materialize(ctx context.Context, request MaterializeWorkspaceRequest) (MaterializedWorkspace, error)
	Commit(ctx context.Context, workspace MaterializedWorkspace, claimToken string) error
	Release(ctx context.Context, workspace MaterializedWorkspace) error
}

// HostWorkspaceMaterializer prepares build workspaces on the host filesystem.
type HostWorkspaceMaterializer struct {
	root string

	mu sync.Mutex
}

var _ ExecutionWorkspaceMaterializer = (*HostWorkspaceMaterializer)(nil)

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

// Materialize returns the existing build-scoped directory for every supported
// lineage plan. This preserves the legacy local runner behavior while physical
// fan-out isolation and fan-in restoration are not implemented. Implementations
// intended for portable execution must reject unsupported plans rather than
// sharing this directory.
func (m *HostWorkspaceMaterializer) Materialize(ctx context.Context, request MaterializeWorkspaceRequest) (MaterializedWorkspace, error) {
	input := normalizeWorkspaceInput(request.Input)
	if input.Mode != domain.WorkspaceInputModeSource && input.Mode != domain.WorkspaceInputModePredecessor && input.Mode != domain.WorkspaceInputModeFanIn {
		return MaterializedWorkspace{}, ErrWorkspaceInputUnsupported
	}

	workspacePath, err := m.PrepareWorkspace(ctx, WorkspacePrepareRequest{BuildID: request.BuildID})
	if err != nil {
		return MaterializedWorkspace{}, err
	}

	return MaterializedWorkspace{
		BuildID: strings.TrimSpace(request.BuildID),
		Path:    workspacePath,
		Input:   input,
	}, nil
}

// Commit advances only the logical revision in the local implementation. The
// build directory remains in place for a same-worker linear successor.
func (m *HostWorkspaceMaterializer) Commit(_ context.Context, workspace MaterializedWorkspace, _ string) error {
	if strings.TrimSpace(workspace.BuildID) == "" || strings.TrimSpace(workspace.Path) == "" {
		return errors.New("materialized workspace is required")
	}
	return nil
}

// Release removes a terminal build's local workspace. It is idempotent so it
// can run after runner cleanup has detached the filesystem.
func (m *HostWorkspaceMaterializer) Release(ctx context.Context, workspace MaterializedWorkspace) error {
	return m.CleanupWorkspace(ctx, workspace.BuildID)
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

func normalizeWorkspaceInput(input domain.WorkspaceInputPlan) domain.WorkspaceInputPlan {
	if input.Mode == "" {
		input.Mode = domain.WorkspaceInputModeSource
	}
	return input
}
