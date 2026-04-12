package docker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/runner"
	"github.com/radiation/coyote-ci/backend/internal/source"
)

type fakeWorkspace struct {
	prepareCalls int
	cleanupCalls int
	lastRequest  source.WorkspacePrepareRequest
	preparePath  string
	prepareErr   error
	cleanupErr   error
}

func (f *fakeWorkspace) PrepareWorkspace(_ context.Context, request source.WorkspacePrepareRequest) (string, error) {
	f.prepareCalls++
	f.lastRequest = request
	if f.prepareErr != nil {
		return "", f.prepareErr
	}
	return f.preparePath, nil
}

func (f *fakeWorkspace) CleanupWorkspace(_ context.Context, _ string) error {
	f.cleanupCalls++
	return f.cleanupErr
}

type cmdCall struct {
	name string
	args []string
}

type fakeExecutor struct {
	calls      []cmdCall
	responses  []executorResponse
	defaultErr error
}

type executorResponse struct {
	output []byte
	err    error
}

func (f *fakeExecutor) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, cmdCall{name: name, args: append([]string(nil), args...)})
	if len(f.responses) > 0 {
		resp := f.responses[0]
		f.responses = f.responses[1:]
		return resp.output, resp.err
	}
	if f.defaultErr != nil {
		return nil, f.defaultErr
	}
	return []byte{}, nil
}

func TestRunner_PrepareBuild_UsesCommitSHAAndPreparesWorkspace(t *testing.T) {
	workspace := &fakeWorkspace{preparePath: "/tmp/ws/build-1"}
	exec := &fakeExecutor{}

	r := New(Options{Workspace: workspace, DefaultImage: "alpine:3.20", Executor: exec})
	err := r.PrepareBuild(context.Background(), runner.PrepareBuildRequest{
		BuildID:   "build-1",
		RepoURL:   "https://example.com/repo.git",
		Ref:       "main",
		CommitSHA: "abc123",
		Image:     "golang:1.23-alpine",
	})
	if err != nil {
		t.Fatalf("prepare build failed: %v", err)
	}
	if workspace.prepareCalls != 1 {
		t.Fatalf("expected one workspace prepare call, got %d", workspace.prepareCalls)
	}
	if workspace.lastRequest.CommitSHA != "abc123" {
		t.Fatalf("expected commit sha to be forwarded, got %q", workspace.lastRequest.CommitSHA)
	}
	// PrepareBuild should NOT invoke any docker commands (containers are per-step now)
	if len(exec.calls) != 0 {
		t.Fatalf("expected no docker calls, got %d", len(exec.calls))
	}
}

func TestRunner_PrepareBuild_UsesDefaultImage(t *testing.T) {
	workspace := &fakeWorkspace{preparePath: "/tmp/ws/build-2"}
	exec := &fakeExecutor{}

	r := New(Options{Workspace: workspace, DefaultImage: "alpine:3.20", Executor: exec})
	err := r.PrepareBuild(context.Background(), runner.PrepareBuildRequest{BuildID: "build-2"})
	if err != nil {
		t.Fatalf("prepare build failed: %v", err)
	}
	// No docker calls expected during prepare; image is resolved per-step
	if len(exec.calls) != 0 {
		t.Fatalf("expected no docker calls, got %d", len(exec.calls))
	}
}

func TestRunner_PrepareBuild_UsesCanonicalWorkspaceMountSource(t *testing.T) {
	realRoot := filepath.Join(t.TempDir(), "real-root")
	if err := os.MkdirAll(realRoot, 0o755); err != nil {
		t.Fatalf("mkdir real root: %v", err)
	}
	realWorkspace := filepath.Join(realRoot, "build-2")
	if err := os.MkdirAll(realWorkspace, 0o755); err != nil {
		t.Fatalf("mkdir real workspace: %v", err)
	}

	symlinkRoot := filepath.Join(t.TempDir(), "root-link")
	if err := os.Symlink(realRoot, symlinkRoot); err != nil {
		t.Fatalf("creating symlink root: %v", err)
	}
	symlinkWorkspace := filepath.Join(symlinkRoot, "build-2")

	workspace := &fakeWorkspace{preparePath: symlinkWorkspace}
	exec := &fakeExecutor{}

	r := New(Options{Workspace: workspace, DefaultImage: "alpine:3.20", Executor: exec})
	err := r.PrepareBuild(context.Background(), runner.PrepareBuildRequest{BuildID: "build-2"})
	if err != nil {
		t.Fatalf("prepare build failed: %v", err)
	}

	// Workspace path should be canonical (symlinks resolved)
	path, ok := r.workspacePathForBuild("build-2")
	if !ok {
		t.Fatal("expected workspace path to be stored")
	}
	canonicalWorkspace, canonicalErr := filepath.EvalSymlinks(realWorkspace)
	if canonicalErr != nil {
		t.Fatalf("eval canonical workspace: %v", canonicalErr)
	}
	if path != canonicalWorkspace {
		t.Fatalf("expected canonical workspace path %q, got %q", canonicalWorkspace, path)
	}
}

func TestRunner_PrepareBuild_WorkspaceFailurePropagatesError(t *testing.T) {
	workspace := &fakeWorkspace{prepareErr: errors.New("disk full")}
	exec := &fakeExecutor{}

	r := New(Options{Workspace: workspace, DefaultImage: "alpine:3.20", Executor: exec})
	err := r.PrepareBuild(context.Background(), runner.PrepareBuildRequest{BuildID: "build-fail"})
	if err == nil {
		t.Fatal("expected prepare build to fail")
	}
	if !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("expected workspace error in message, got %v", err)
	}
}

func TestRunner_PrepareBuild_IdempotentWorkspace(t *testing.T) {
	workspace := &fakeWorkspace{preparePath: "/tmp/ws/build-3"}
	exec := &fakeExecutor{}

	r := New(Options{Workspace: workspace, DefaultImage: "alpine:3.20", Executor: exec})
	req := runner.PrepareBuildRequest{BuildID: "build-3", Image: "alpine:3.20"}
	if err := r.PrepareBuild(context.Background(), req); err != nil {
		t.Fatalf("first prepare failed: %v", err)
	}
	if err := r.PrepareBuild(context.Background(), req); err != nil {
		t.Fatalf("second prepare failed: %v", err)
	}

	if workspace.prepareCalls != 2 {
		t.Fatalf("expected workspace convergence check on each call, got %d", workspace.prepareCalls)
	}
	// No docker commands in prepare (containers are per-step)
	if len(exec.calls) != 0 {
		t.Fatalf("expected no docker calls, got %d", len(exec.calls))
	}
}

func TestResolveContainerWorkingDir(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "empty defaults to workspace", input: "", expected: "/workspace"},
		{name: "dot defaults to workspace", input: ".", expected: "/workspace"},
		{name: "relative with dots stays under workspace", input: "a/../backend", expected: "/workspace/backend"},
		{name: "relative under workspace", input: "backend", expected: "/workspace/backend"},
		{name: "attempt escape blocked", input: "../../etc", expected: "/workspace"},
		{name: "absolute under workspace allowed", input: "/workspace/sub", expected: "/workspace/sub"},
		{name: "absolute outside blocked", input: "/etc", expected: "/workspace"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveContainerWorkingDir(tc.input); got != tc.expected {
				t.Fatalf("expected %q, got %q", tc.expected, got)
			}
		})
	}
}

func TestStepContainerRunArgs_BuildsCorrectArgs(t *testing.T) {
	args := stepContainerRunArgs(
		"coyote-step-build-5-0",
		"golang:1.23",
		"/tmp/ws/build-5:/workspace",
		"/workspace",
		false,
		runner.RunStepRequest{
			BuildID: "build-5",
			Command: "sh",
			Args:    []string{"-c", "pwd"},
			CacheMounts: []runner.CacheMount{
				{HostPath: "/tmp/cache/mod", ContainerPath: "/go/pkg/mod"},
			},
		},
	)
	// Verify key structural elements
	if args[0] != "run" {
		t.Fatalf("expected run command, got %q", args[0])
	}
	if args[1] != "--name" || args[2] != "coyote-step-build-5-0" {
		t.Fatalf("expected --name coyote-step-build-5-0, got %+v", args[:4])
	}

	// Verify volume mount and working dir are present
	foundMount := false
	foundWorkdir := false
	for i, a := range args {
		if a == "-v" && i+1 < len(args) && args[i+1] == "/tmp/ws/build-5:/workspace" {
			foundMount = true
		}
		if a == "-w" && i+1 < len(args) && args[i+1] == "/workspace" {
			foundWorkdir = true
		}
	}
	if !foundMount {
		t.Fatalf("expected volume mount, got %+v", args)
	}
	if !foundWorkdir {
		t.Fatalf("expected working directory, got %+v", args)
	}

	foundCacheMount := false
	for i, a := range args {
		if a == "-v" && i+1 < len(args) && args[i+1] == "/tmp/cache/mod:/go/pkg/mod" {
			foundCacheMount = true
		}
	}
	if !foundCacheMount {
		t.Fatalf("expected cache mount, got %+v", args)
	}

	// Image and command should be at the end
	imgIdx := -1
	for i, a := range args {
		if a == "golang:1.23" {
			imgIdx = i
			break
		}
	}
	if imgIdx < 0 {
		t.Fatalf("expected image in args, got %+v", args)
	}
	if args[imgIdx+1] != "sh" || args[imgIdx+2] != "-c" || args[imgIdx+3] != "pwd" {
		t.Fatalf("expected command after image, got %+v", args[imgIdx:])
	}

	// Verify CI env vars are injected
	foundCI := false
	for i, a := range args {
		if a == "-e" && i+1 < len(args) && args[i+1] == "CI=true" {
			foundCI = true
		}
	}
	if !foundCI {
		t.Fatalf("expected CI=true env var, got %+v", args)
	}

	// With user env vars
	argsWithEnv := stepContainerRunArgs(
		"coyote-step-build-5-1",
		"golang:1.23",
		"/tmp/ws/build-5:/workspace",
		"/workspace/backend",
		false,
		runner.RunStepRequest{BuildID: "build-5", Command: "make", Env: map[string]string{"GOOS": "linux"}},
	)
	foundEnv := false
	for i, a := range argsWithEnv {
		if a == "-e" && i+1 < len(argsWithEnv) && argsWithEnv[i+1] == "GOOS=linux" {
			foundEnv = true
		}
	}
	if !foundEnv {
		t.Fatalf("expected -e GOOS=linux in args: %+v", argsWithEnv)
	}

	// With Docker socket mount
	argsWithSocket := stepContainerRunArgs(
		"coyote-step-build-5-2",
		"docker:27",
		"/tmp/ws/build-5:/workspace",
		"/workspace",
		true,
		runner.RunStepRequest{BuildID: "build-5", Command: "docker", Args: []string{"build", "."}},
	)
	foundSocket := false
	for i, a := range argsWithSocket {
		if a == "-v" && i+1 < len(argsWithSocket) && argsWithSocket[i+1] == "/var/run/docker.sock:/var/run/docker.sock" {
			foundSocket = true
		}
	}
	if !foundSocket {
		t.Fatalf("expected docker socket mount in args: %+v", argsWithSocket)
	}
}

func TestResolveContainerWorkingDirForStep_SymlinkEscapeFallsBackToWorkspaceRoot(t *testing.T) {
	workspaceRoot := t.TempDir()
	outsideRoot := t.TempDir()

	escapingLink := filepath.Join(workspaceRoot, "linked-out")
	if err := os.Symlink(outsideRoot, escapingLink); err != nil {
		t.Fatalf("failed creating symlink fixture: %v", err)
	}

	safeDir := filepath.Join(workspaceRoot, "backend")
	if err := os.MkdirAll(safeDir, 0o755); err != nil {
		t.Fatalf("failed creating safe directory fixture: %v", err)
	}

	r := New(Options{Workspace: &fakeWorkspace{}, DefaultImage: "alpine:3.20", Executor: &fakeExecutor{}})
	r.setWorkspacePath("build-1", workspaceRoot)

	escaped := r.resolveContainerWorkingDirForStep(runner.RunStepRequest{BuildID: "build-1", WorkingDir: "linked-out"})
	if escaped != "/workspace" {
		t.Fatalf("expected symlink escape to fall back to /workspace, got %q", escaped)
	}

	safe := r.resolveContainerWorkingDirForStep(runner.RunStepRequest{BuildID: "build-1", WorkingDir: "backend"})
	if safe != "/workspace/backend" {
		t.Fatalf("expected safe directory to remain under /workspace, got %q", safe)
	}
}

func TestRunner_CleanupBuild_InvokesWorkspaceCleanup(t *testing.T) {
	workspace := &fakeWorkspace{}
	exec := &fakeExecutor{}
	r := New(Options{Workspace: workspace, DefaultImage: "alpine:3.20", Executor: exec})

	if err := r.CleanupBuild(context.Background(), "build-9"); err != nil {
		t.Fatalf("cleanup failed: %v", err)
	}
	if workspace.cleanupCalls != 1 {
		t.Fatalf("expected one workspace cleanup call, got %d", workspace.cleanupCalls)
	}
	// No docker rm call — containers are per-step and cleaned up after each step
	if len(exec.calls) != 0 {
		t.Fatalf("expected no docker calls, got %d", len(exec.calls))
	}
}
