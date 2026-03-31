package docker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
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

func TestRunner_PrepareBuild_UsesCommitSHAAndImageParameter(t *testing.T) {
	workspace := &fakeWorkspace{preparePath: "/tmp/ws/build-1"}
	exec := &fakeExecutor{
		responses: []executorResponse{
			{output: []byte(""), err: errors.New("No such container: coyote-build-build-1")},
			{output: []byte("container-id"), err: nil},
		},
	}

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
	if len(exec.calls) != 2 {
		t.Fatalf("expected inspect + run calls, got %d", len(exec.calls))
	}

	runCall := exec.calls[1]
	expectedPrefix := []string{"run", "-d", "--name", "coyote-build-build-1", "-w", "/workspace", "-v", "/tmp/ws/build-1:/workspace", "golang:1.23-alpine", "sh", "-c"}
	if len(runCall.args) < len(expectedPrefix) {
		t.Fatalf("unexpected docker run args length: %+v", runCall.args)
	}
	if !reflect.DeepEqual(runCall.args[:len(expectedPrefix)], expectedPrefix) {
		t.Fatalf("unexpected docker run args prefix: %+v", runCall.args)
	}
}

func TestRunner_PrepareBuild_UsesDefaultImage(t *testing.T) {
	workspace := &fakeWorkspace{preparePath: "/tmp/ws/build-2"}
	exec := &fakeExecutor{responses: []executorResponse{{err: errors.New("No such container: coyote-build-build-2")}, {output: []byte("ok")}}}

	r := New(Options{Workspace: workspace, DefaultImage: "alpine:3.20", Executor: exec})
	err := r.PrepareBuild(context.Background(), runner.PrepareBuildRequest{BuildID: "build-2"})
	if err != nil {
		t.Fatalf("prepare build failed: %v", err)
	}
	if len(exec.calls) < 2 {
		t.Fatalf("expected docker run to be called")
	}
	if got := exec.calls[1].args[8]; got != "alpine:3.20" {
		t.Fatalf("expected default image alpine:3.20, got %q", got)
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
	exec := &fakeExecutor{responses: []executorResponse{{err: errors.New("No such container: coyote-build-build-2")}, {output: []byte("ok")}}}

	r := New(Options{Workspace: workspace, DefaultImage: "alpine:3.20", Executor: exec})
	err := r.PrepareBuild(context.Background(), runner.PrepareBuildRequest{BuildID: "build-2"})
	if err != nil {
		t.Fatalf("prepare build failed: %v", err)
	}
	if len(exec.calls) < 2 {
		t.Fatalf("expected docker run to be called")
	}

	canonicalWorkspace, canonicalErr := filepath.EvalSymlinks(realWorkspace)
	if canonicalErr != nil {
		t.Fatalf("eval canonical workspace: %v", canonicalErr)
	}
	expectedMount := canonicalWorkspace + ":/workspace"
	if got := exec.calls[1].args[7]; got != expectedMount {
		t.Fatalf("expected canonical mount source %q, got %q", expectedMount, got)
	}
}

func TestRunner_PrepareBuild_InspectFailureIncludesOutput(t *testing.T) {
	workspace := &fakeWorkspace{preparePath: "/tmp/ws/build-inspect-fail"}
	exec := &fakeExecutor{responses: []executorResponse{{output: []byte("permission denied to access Docker daemon"), err: errors.New("exit status 1")}}}

	r := New(Options{Workspace: workspace, DefaultImage: "alpine:3.20", Executor: exec})
	err := r.PrepareBuild(context.Background(), runner.PrepareBuildRequest{BuildID: "build-inspect-fail"})
	if err == nil {
		t.Fatal("expected prepare build to fail")
	}
	if !strings.Contains(err.Error(), "inspecting build container") {
		t.Fatalf("expected inspect context in error, got %v", err)
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("expected docker output in error, got %v", err)
	}
}

func TestRunner_PrepareBuild_IdempotentWhenContainerAlreadyRunning(t *testing.T) {
	workspace := &fakeWorkspace{preparePath: "/tmp/ws/build-3"}
	exec := &fakeExecutor{
		responses: []executorResponse{
			{err: errors.New("No such container: coyote-build-build-3")},
			{output: []byte("created"), err: nil},
			{output: []byte("true"), err: nil},
		},
	}

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
	if len(exec.calls) != 3 {
		t.Fatalf("expected inspect+run then inspect, got %d calls", len(exec.calls))
	}
	if !reflect.DeepEqual(exec.calls[2].args, []string{"inspect", "-f", "{{.State.Running}}", "coyote-build-build-3"}) {
		t.Fatalf("expected second prepare to only inspect container, got %+v", exec.calls[2].args)
	}
}

func TestRunner_PrepareBuild_StartsStoppedContainer(t *testing.T) {
	workspace := &fakeWorkspace{preparePath: "/tmp/ws/build-4"}
	exec := &fakeExecutor{
		responses: []executorResponse{
			{output: []byte("false"), err: nil},
			{output: []byte("started"), err: nil},
		},
	}

	r := New(Options{Workspace: workspace, DefaultImage: "alpine:3.20", Executor: exec})
	if err := r.PrepareBuild(context.Background(), runner.PrepareBuildRequest{BuildID: "build-4"}); err != nil {
		t.Fatalf("prepare failed: %v", err)
	}

	if len(exec.calls) != 2 {
		t.Fatalf("expected inspect + start calls, got %d", len(exec.calls))
	}
	if !reflect.DeepEqual(exec.calls[1].args, []string{"start", "coyote-build-build-4"}) {
		t.Fatalf("expected stopped container to be started, got %+v", exec.calls[1].args)
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

func TestDockerExecArgs_UsesWorkspaceDefaultAndSafeSubdirectory(t *testing.T) {
	defaultArgs := dockerExecArgs(runner.RunStepRequest{BuildID: "build-5", Command: "sh", Args: []string{"-c", "pwd"}}, "/workspace")
	if len(defaultArgs) < 3 {
		t.Fatalf("expected docker exec args, got %+v", defaultArgs)
	}
	if defaultArgs[0] != "exec" || defaultArgs[1] != "-w" || defaultArgs[2] != "/workspace" {
		t.Fatalf("expected default working dir /workspace, got %+v", defaultArgs[:3])
	}

	escapedArgs := dockerExecArgs(runner.RunStepRequest{BuildID: "build-5", WorkingDir: "../../etc", Command: "sh", Args: []string{"-c", "pwd"}}, "/workspace")
	if escapedArgs[2] != "/workspace" {
		t.Fatalf("expected traversal to fall back to /workspace, got %q", escapedArgs[2])
	}

	subdirArgs := dockerExecArgs(runner.RunStepRequest{BuildID: "build-5", WorkingDir: "backend", Command: "sh", Args: []string{"-c", "pwd"}}, "/workspace/backend")
	if subdirArgs[2] != "/workspace/backend" {
		t.Fatalf("expected safe subdirectory under workspace, got %q", subdirArgs[2])
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

func TestRunner_CleanupBuild_InvokesContainerAndWorkspaceCleanup(t *testing.T) {
	workspace := &fakeWorkspace{}
	exec := &fakeExecutor{}
	r := New(Options{Workspace: workspace, DefaultImage: "alpine:3.20", Executor: exec})

	if err := r.CleanupBuild(context.Background(), "build-9"); err != nil {
		t.Fatalf("cleanup failed: %v", err)
	}
	if workspace.cleanupCalls != 1 {
		t.Fatalf("expected one workspace cleanup call, got %d", workspace.cleanupCalls)
	}
	if len(exec.calls) != 1 {
		t.Fatalf("expected one docker rm call, got %d", len(exec.calls))
	}
	if !reflect.DeepEqual(exec.calls[0].args, []string{"rm", "-f", "coyote-build-build-9"}) {
		t.Fatalf("unexpected cleanup args: %+v", exec.calls[0].args)
	}
}
