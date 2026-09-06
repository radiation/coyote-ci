package docker

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

type errorReadCloser struct {
	reads [][]byte
	err   error
	index int
}

func (r *errorReadCloser) Read(p []byte) (int, error) {
	if r.index >= len(r.reads) {
		return 0, r.err
	}
	n := copy(p, r.reads[r.index])
	r.index++
	if r.index >= len(r.reads) {
		return n, r.err
	}
	return n, nil
}

func (r *errorReadCloser) Close() error { return nil }

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

func TestRunner_ValidationBranchesAvoidDockerExecution(t *testing.T) {
	r := New(Options{Workspace: &fakeWorkspace{preparePath: "/tmp/ws/build-1"}, DefaultImage: " alpine:3.20 ", Executor: &fakeExecutor{}})

	if got := r.ResolveStepImage(" golang:1.27.1 "); got != "golang:1.27.1" {
		t.Fatalf("expected step image to win, got %q", got)
	}
	if got := r.ResolveStepImage(" "); got != "alpine:3.20" {
		t.Fatalf("expected trimmed default image, got %q", got)
	}

	if err := r.PrepareBuild(context.Background(), runner.PrepareBuildRequest{BuildID: " "}); err == nil {
		t.Fatal("expected blank build id prepare error")
	}
	if err := New(Options{DefaultImage: "alpine"}).PrepareBuild(context.Background(), runner.PrepareBuildRequest{BuildID: "build-1"}); err == nil {
		t.Fatal("expected missing workspace materializer error")
	}

	if _, err := r.RunStep(context.Background(), runner.RunStepRequest{Command: "echo hi"}); err == nil || !strings.Contains(err.Error(), "build id") {
		t.Fatalf("expected build id validation error, got %v", err)
	}
	if _, err := r.RunStep(context.Background(), runner.RunStepRequest{BuildID: "build-1"}); err == nil || !strings.Contains(err.Error(), "command") {
		t.Fatalf("expected command validation error, got %v", err)
	}
	if _, err := New(Options{Workspace: &fakeWorkspace{preparePath: "/tmp/ws/build-1"}}).RunStep(context.Background(), runner.RunStepRequest{BuildID: "build-1", Command: "echo hi"}); err == nil || !strings.Contains(err.Error(), "no execution image") {
		t.Fatalf("expected missing image validation error, got %v", err)
	}
	if _, err := r.RunStep(context.Background(), runner.RunStepRequest{BuildID: "missing", Command: "echo hi"}); err == nil || !strings.Contains(err.Error(), "workspace not prepared") {
		t.Fatalf("expected missing workspace validation error, got %v", err)
	}
}

func TestRunner_RunStepTimeoutSetsTypedResult(t *testing.T) {
	dockerBinDir := t.TempDir()
	dockerPath := filepath.Join(dockerBinDir, "docker")
	script := "#!/bin/sh\nif [ \"$1\" = \"run\" ]; then\n  while :; do :; done\nfi\nexit 0\n"
	if writeErr := os.WriteFile(dockerPath, []byte(script), 0o755); writeErr != nil {
		t.Fatalf("write fake docker executable: %v", writeErr)
	}
	t.Setenv("PATH", dockerBinDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	workspacePath := t.TempDir()
	r := New(Options{
		Workspace:    &fakeWorkspace{preparePath: workspacePath},
		DefaultImage: "alpine:3.20",
		Executor:     &fakeExecutor{},
	})
	if prepareErr := r.PrepareBuild(context.Background(), runner.PrepareBuildRequest{BuildID: "build-timeout"}); prepareErr != nil {
		t.Fatalf("prepare build: %v", prepareErr)
	}

	result, runErr := r.RunStep(context.Background(), runner.RunStepRequest{
		BuildID:        "build-timeout",
		Command:        "sh",
		TimeoutSeconds: 1,
	})
	if runErr != nil {
		t.Fatalf("run timed-out step: %v", runErr)
	}
	if result.Status != runner.RunStepStatusFailed || result.ExitCode != -1 {
		t.Fatalf("expected failed timeout result, got status=%q exit_code=%d", result.Status, result.ExitCode)
	}
	if !result.TimedOut {
		t.Fatal("expected typed timeout result")
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

func TestMergeStepEnvironment_DerivesTriggerArtifactVisiblePaths(t *testing.T) {
	envEntries := mergeStepEnvironment(runner.RunStepRequest{
		BuildID: "build-1",
		StepID:  "step-1",
		Env: map[string]string{
			runner.EnvTriggerArtifactLocalRelative: ".coyote/trigger-artifacts/dist/app.tar.gz",
		},
	})

	envMap := map[string]string{}
	for _, entry := range envEntries {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) != 2 {
			continue
		}
		envMap[parts[0]] = parts[1]
	}

	if envMap[runner.EnvWorkspace] != "/workspace" {
		t.Fatalf("expected docker workspace env /workspace, got %q", envMap[runner.EnvWorkspace])
	}
	if envMap[runner.EnvTriggerArtifactLocalDir] != "/workspace/.coyote/trigger-artifacts" {
		t.Fatalf("expected trigger artifact local dir under docker workspace, got %q", envMap[runner.EnvTriggerArtifactLocalDir])
	}
	if envMap[runner.EnvTriggerArtifactLocalPath] != "/workspace/.coyote/trigger-artifacts/dist/app.tar.gz" {
		t.Fatalf("expected trigger artifact local path under docker workspace, got %q", envMap[runner.EnvTriggerArtifactLocalPath])
	}
}

func TestRunner_StepVisibleWorkspaceRoot(t *testing.T) {
	r := New(Options{})
	if got, ok := r.StepVisibleWorkspaceRoot("build-1"); !ok || got != "/workspace" {
		t.Fatalf("expected docker-visible workspace root, got %q ok=%v", got, ok)
	}
	if got, ok := r.StepVisibleWorkspaceRoot(" "); ok || got != "" {
		t.Fatalf("expected blank build id to fail, got %q ok=%v", got, ok)
	}
}

func TestAugmentTriggerArtifactEnvironment_NoRelativePathNoop(t *testing.T) {
	env := map[string]string{}
	augmentTriggerArtifactEnvironment(env, "/workspace")
	if len(env) != 0 {
		t.Fatalf("expected noop env augmentation, got %#v", env)
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

func TestRunner_CleanupBuild_HandlesBlankAndNilWorkspace(t *testing.T) {
	workspace := &fakeWorkspace{}
	r := New(Options{Workspace: workspace, DefaultImage: "alpine"})
	r.setWorkspacePath("build-blank", "/tmp/ws/build-blank")

	if err := r.CleanupBuild(context.Background(), " "); err != nil {
		t.Fatalf("blank cleanup should be ignored: %v", err)
	}
	if workspace.cleanupCalls != 0 {
		t.Fatalf("expected no cleanup call for blank build id, got %d", workspace.cleanupCalls)
	}

	withoutWorkspace := New(Options{DefaultImage: "alpine"})
	withoutWorkspace.setWorkspacePath("build-1", "/tmp/ws/build-1")
	if err := withoutWorkspace.CleanupBuild(context.Background(), " build-1 "); err != nil {
		t.Fatalf("nil workspace cleanup should be ignored: %v", err)
	}
	if _, ok := withoutWorkspace.workspacePathForBuild("build-1"); ok {
		t.Fatal("expected cleanup to clear stored workspace path")
	}
}

func TestValidateCacheMounts_RejectsForbiddenContainerPath(t *testing.T) {
	_, err := validateCacheMounts([]runner.CacheMount{{HostPath: t.TempDir(), ContainerPath: "/etc"}})
	if err == nil {
		t.Fatal("expected forbidden cache mount path to be rejected")
	}
	if !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("expected clear not-allowed error, got %v", err)
	}
}

func TestValidateCacheMounts_HostPathMustBeDirectory(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "cache-file")
	if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
		t.Fatalf("write fixture file: %v", err)
	}

	_, err := validateCacheMounts([]runner.CacheMount{{HostPath: filePath, ContainerPath: "/go/pkg/mod"}})
	if err == nil {
		t.Fatal("expected file host path to be rejected")
	}
	if !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("expected not-a-directory error, got %v", err)
	}
}

func TestValidateCacheMounts_NormalizesAndRejectsMalformedPaths(t *testing.T) {
	hostPath := filepath.Join(t.TempDir(), "cache", "mod")
	validated, err := validateCacheMounts([]runner.CacheMount{{HostPath: hostPath, ContainerPath: `\go\pkg\mod`}})
	if err != nil {
		t.Fatalf("expected valid cache mount, got %v", err)
	}
	if len(validated) != 1 || validated[0].HostPath != hostPath || validated[0].ContainerPath != "/go/pkg/mod" {
		t.Fatalf("unexpected validated mounts: %#v", validated)
	}
	if info, statErr := os.Stat(hostPath); statErr != nil || !info.IsDir() {
		t.Fatalf("expected host path to be created as directory: info=%v err=%v", info, statErr)
	}

	tests := []struct {
		name  string
		mount runner.CacheMount
		want  string
	}{
		{name: "blank host", mount: runner.CacheMount{HostPath: " ", ContainerPath: "/cache"}, want: "host path is required"},
		{name: "relative host", mount: runner.CacheMount{HostPath: "relative", ContainerPath: "/cache"}, want: "host path must be absolute"},
		{name: "relative container", mount: runner.CacheMount{HostPath: t.TempDir(), ContainerPath: "cache"}, want: "container path must be absolute"},
		{name: "workspace container", mount: runner.CacheMount{HostPath: t.TempDir(), ContainerPath: "/workspace/cache"}, want: "not allowed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, validateErr := validateCacheMounts([]runner.CacheMount{tc.mount})
			if validateErr == nil || !strings.Contains(validateErr.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %v", tc.want, validateErr)
			}
		})
	}
}

func TestRunner_WorkspacePathFallbackUsesMaterializerRoot(t *testing.T) {
	root := t.TempDir()
	workspacePath := filepath.Join(root, "build-1")
	if mkdirErr := os.MkdirAll(workspacePath, 0o755); mkdirErr != nil {
		t.Fatalf("mkdir workspace: %v", mkdirErr)
	}

	r := New(Options{Workspace: &fakeWorkspaceWithRoot{fakeWorkspace: fakeWorkspace{}, root: root}, DefaultImage: "alpine"})
	got, ok := r.workspacePathForBuild(" build-1 ")
	if !ok {
		t.Fatal("expected fallback workspace path")
	}
	if got != canonicalizeHostPath(workspacePath) {
		t.Fatalf("expected %q, got %q", canonicalizeHostPath(workspacePath), got)
	}
	if _, ok := r.workspacePathForBuild("missing"); ok {
		t.Fatal("expected missing fallback workspace to be absent")
	}
	if _, ok := r.workspacePathForBuild(" "); ok {
		t.Fatal("expected blank build id to be absent")
	}
}

type fakeWorkspaceWithRoot struct {
	fakeWorkspace
	root string
}

func (f *fakeWorkspaceWithRoot) WorkspaceRoot() string { return f.root }

func TestRunner_DockerHelpers(t *testing.T) {
	longID := strings.Repeat("a", 80)
	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "step blank", got: containerNameForStep(" ", 3), want: "coyote-step-unknown-3"},
		{name: "step normalized", got: containerNameForStep(" Build ID/Feature ", 4), want: "coyote-step-build-id-feature-4"},
		{name: "step punctuation", got: containerNameForStep("***", 5), want: "coyote-step-unknown-5"},
		{name: "build blank", got: containerNameForBuild(" "), want: "coyote-build-unknown"},
		{name: "build normalized", got: containerNameForBuild(" Build ID/Feature "), want: "coyote-build-build-id-feature"},
		{name: "build punctuation", got: containerNameForBuild("***"), want: "coyote-build-unknown"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, tc.got)
			}
		})
	}
	if got := containerNameForStep(longID, 1); len(strings.TrimPrefix(strings.TrimSuffix(got, "-1"), "coyote-step-")) != 48 {
		t.Fatalf("expected normalized step id to be truncated to 48 chars, got %q", got)
	}

	if !isContainerNotFound(errors.New("No such container: coyote"), nil) {
		t.Fatal("expected no-such-container error to be recognized")
	}
	if !isContainerNotFound(errors.New("docker failed"), []byte("No such object: coyote")) {
		t.Fatal("expected no-such-object output to be recognized")
	}
	if isContainerNotFound(errors.New("permission denied"), nil) {
		t.Fatal("expected unrelated error to be preserved")
	}

	if got := timeoutFailureReason(0); got != "step execution timed out" {
		t.Fatalf("unexpected zero timeout reason: %q", got)
	}
	if got := timeoutFailureReason(2 * time.Second); got != "step execution timed out after 2s" {
		t.Fatalf("unexpected timeout reason: %q", got)
	}

	redacted := redactDockerArgsForLogging([]string{"run", "-e", "TOKEN=secret", "--env=PASSWORD=hunter2", "--env", "NO_EQUALS", "alpine"})
	want := []string{"run", "-e", "TOKEN=<redacted>", "--env=PASSWORD=<redacted>", "--env", "NO_EQUALS", "alpine"}
	for idx := range want {
		if redacted[idx] != want[idx] {
			t.Fatalf("redacted[%d]: expected %q, got %q", idx, want[idx], redacted[idx])
		}
	}
	if got := dockerCommandString([]string{"run", "alpine", "echo hi"}); got != `docker "run" "alpine" "echo hi"` {
		t.Fatalf("unexpected command string: %q", got)
	}
}

func TestStreamOutput_CollectsEmitsAndReturnsCallbackError(t *testing.T) {
	chunks := make([]runner.StepOutputChunk, 0)
	stdout, stderr, err := streamOutput(
		io.NopCloser(strings.NewReader("out-1\nout-2\n")),
		io.NopCloser(strings.NewReader("err-1\n")),
		func(chunk runner.StepOutputChunk) error {
			chunks = append(chunks, chunk)
			return nil
		},
	)
	if err != nil {
		t.Fatalf("stream output failed: %v", err)
	}
	if stdout != "out-1\nout-2\n" || stderr != "err-1\n" {
		t.Fatalf("unexpected stdout/stderr: %q %q", stdout, stderr)
	}
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(chunks))
	}

	callbackErr := errors.New("sink closed")
	_, _, err = streamOutput(
		io.NopCloser(strings.NewReader("out\n")),
		io.NopCloser(strings.NewReader("")),
		func(runner.StepOutputChunk) error { return callbackErr },
	)
	if !errors.Is(err, callbackErr) {
		t.Fatalf("expected callback error, got %v", err)
	}
}

func TestStreamOutput_ReturnsScannerError(t *testing.T) {
	scanErr := errors.New("read failed")
	stdoutPipe := &errorReadCloser{reads: [][]byte{[]byte("out\n")}, err: scanErr}
	stderrPipe := io.NopCloser(strings.NewReader(""))

	stdout, stderr, err := streamOutput(stdoutPipe, stderrPipe, nil)
	if !errors.Is(err, scanErr) {
		t.Fatalf("expected scanner error, got %v", err)
	}
	if stdout != "out\n" {
		t.Fatalf("unexpected stdout: %q", stdout)
	}
	if stderr != "" {
		t.Fatalf("unexpected stderr: %q", stderr)
	}
}

func TestRunner_DockerCommandHelpersUseExecutor(t *testing.T) {
	exec := &fakeExecutor{responses: []executorResponse{
		{output: []byte("No such container"), err: errors.New("docker rm failed")},
		{output: []byte("boom"), err: errors.New("denied")},
		{output: []byte("ok")},
	}}
	r := New(Options{Executor: exec})

	r.removeContainer(context.Background(), "missing-container")
	if len(exec.calls) != 1 || exec.calls[0].name != "docker" || strings.Join(exec.calls[0].args, " ") != "rm -f missing-container" {
		t.Fatalf("unexpected remove call: %#v", exec.calls)
	}

	if _, err := r.runDockerCommand(context.Background(), "pull", "private/image"); err == nil || !strings.Contains(err.Error(), "docker command failed") {
		t.Fatalf("expected run docker command error, got %v", err)
	}
	output, err := r.runDockerCommand(context.Background(), "version")
	if err != nil {
		t.Fatalf("expected successful docker command, got %v", err)
	}
	if string(output) != "ok" {
		t.Fatalf("expected ok output, got %q", string(output))
	}
}
