package docker

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/runner"
	"github.com/radiation/coyote-ci/backend/internal/source"
	"github.com/radiation/coyote-ci/backend/internal/workspace"
)

const (
	workspaceMountPath = workspace.DefaultContainerRoot
)

var validNameChars = regexp.MustCompile(`[^a-zA-Z0-9_.-]+`)

type commandExecutor interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type osCommandExecutor struct{}

func (e *osCommandExecutor) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.CombinedOutput()
}

type Options struct {
	Workspace         source.WorkspaceMaterializer
	DefaultImage      string
	MountDockerSocket bool
	Executor          commandExecutor
}

type Runner struct {
	workspace         source.WorkspaceMaterializer
	defaultImage      string
	mountDockerSocket bool
	executor          commandExecutor

	workspaceMu    sync.RWMutex
	workspacePaths map[string]string
}

var _ runner.BuildScopedRunner = (*Runner)(nil)

func New(opts Options) *Runner {
	execImpl := opts.Executor
	if execImpl == nil {
		execImpl = &osCommandExecutor{}
	}

	return &Runner{
		workspace:         opts.Workspace,
		defaultImage:      strings.TrimSpace(opts.DefaultImage),
		mountDockerSocket: opts.MountDockerSocket,
		executor:          execImpl,
		workspacePaths:    map[string]string{},
	}
}

// PrepareBuild creates a shared host workspace for the build.
// No container is created here; containers are ephemeral and created per-step.
func (r *Runner) PrepareBuild(ctx context.Context, request runner.PrepareBuildRequest) error {
	buildID := strings.TrimSpace(request.BuildID)
	if buildID == "" {
		return errors.New("build id is required")
	}

	workspacePath, err := r.ensureBuildWorkspaceReady(ctx, request)
	if err != nil {
		return err
	}

	r.setWorkspacePath(buildID, workspacePath)
	return nil
}

func (r *Runner) resolveExecutionImage(candidate string) string {
	image := strings.TrimSpace(candidate)
	if image != "" {
		return image
	}
	return r.defaultImage
}

// ResolveStepImage resolves the image for a step, preferring step-level, then default.
func (r *Runner) ResolveStepImage(stepImage string) string {
	return r.resolveExecutionImage(stepImage)
}

func (r *Runner) ensureBuildWorkspaceReady(ctx context.Context, request runner.PrepareBuildRequest) (string, error) {
	if r.workspace == nil {
		return "", errors.New("workspace materializer is required")
	}

	workspacePath, err := r.workspace.PrepareWorkspace(ctx, source.WorkspacePrepareRequest{
		BuildID:   strings.TrimSpace(request.BuildID),
		RepoURL:   strings.TrimSpace(request.RepoURL),
		Ref:       strings.TrimSpace(request.Ref),
		CommitSHA: strings.TrimSpace(request.CommitSHA),
	})
	if err != nil {
		return "", err
	}

	return canonicalizeHostPath(workspacePath), nil
}

func (r *Runner) RunStep(ctx context.Context, request runner.RunStepRequest) (runner.RunStepResult, error) {
	return r.RunStepStream(ctx, request, nil)
}

// RunStepStream creates an ephemeral container for the step, mounts the shared workspace,
// runs the command, streams output, and removes the container afterward.
func (r *Runner) RunStepStream(ctx context.Context, request runner.RunStepRequest, onOutput runner.StepOutputCallback) (runner.RunStepResult, error) {
	if strings.TrimSpace(request.BuildID) == "" {
		return runner.RunStepResult{}, errors.New("build id is required")
	}
	if strings.TrimSpace(request.Command) == "" {
		return runner.RunStepResult{}, errors.New("command is required")
	}

	image := r.resolveExecutionImage(request.Image)
	if image == "" {
		return runner.RunStepResult{}, errors.New("no execution image available: set step-level image, pipeline-level image, or default image")
	}

	workspacePath, ok := r.workspacePathForBuild(request.BuildID)
	if !ok {
		return runner.RunStepResult{}, fmt.Errorf("workspace not prepared for build %s", request.BuildID)
	}

	containerName := containerNameForStep(request.BuildID, request.StepIndex)
	containerWorkingDir := r.resolveContainerWorkingDirForStep(request)
	validatedCacheMounts, validateErr := validateCacheMounts(request.CacheMounts)
	if validateErr != nil {
		return runner.RunStepResult{}, validateErr
	}
	request.CacheMounts = validatedCacheMounts

	// Build docker run args for an ephemeral step container.
	buildWorkspace := workspace.New(request.BuildID, workspacePath)
	mountBinding := buildWorkspace.HostRoot + ":" + buildWorkspace.ContainerRoot

	args := stepContainerRunArgs(containerName, image, mountBinding, containerWorkingDir, r.mountDockerSocket, request)
	logCommand := dockerCommandString(redactDockerArgsForLogging(args))
	log.Printf("starting step container: image=%s container=%s command=%s working_dir=%s mounts=%s",
		image, containerName, logCommand, containerWorkingDir, mountBinding)

	execCtx := ctx
	cancel := func() {}
	timeout := time.Duration(request.TimeoutSeconds) * time.Second
	if timeout > 0 {
		execCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	// Ensure container is removed on completion, failure, or cancellation.
	defer r.removeContainer(context.Background(), containerName)

	cmd := exec.CommandContext(execCtx, "docker", args...)
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return runner.RunStepResult{}, err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return runner.RunStepResult{}, err
	}

	startedAt := time.Now().UTC()
	if err := cmd.Start(); err != nil {
		log.Printf("docker command failed: command=%s error=%v output=omitted", logCommand, err)
		return runner.RunStepResult{}, fmt.Errorf("docker command failed: %w", err)
	}

	stdout, stderr, streamErr := streamOutput(stdoutPipe, stderrPipe, onOutput)
	waitErr := cmd.Wait()
	finishedAt := time.Now().UTC()

	if streamErr != nil {
		return runner.RunStepResult{}, streamErr
	}

	if waitErr == nil {
		return runner.RunStepResult{
			Status:     runner.RunStepStatusSuccess,
			ExitCode:   0,
			Stdout:     stdout,
			Stderr:     stderr,
			StartedAt:  startedAt,
			FinishedAt: finishedAt,
		}, nil
	}

	if errors.Is(execCtx.Err(), context.DeadlineExceeded) {
		// On timeout, stop the container to ensure the process terminates.
		r.stopContainer(context.Background(), containerName)
		reason := timeoutFailureReason(timeout)
		if strings.TrimSpace(stderr) == "" {
			stderr = reason
		} else {
			stderr = strings.TrimRight(stderr, "\n") + "\n" + reason
		}
		if onOutput != nil {
			if err := onOutput(runner.StepOutputChunk{Stream: runner.StepOutputStreamSystem, ChunkText: reason, EmittedAt: time.Now().UTC()}); err != nil {
				return runner.RunStepResult{}, err
			}
		}
		log.Printf("docker command timed out: command=%s error=%v stdout_bytes=%d stderr_bytes=%d",
			logCommand, waitErr, len(stdout), len(stderr))
		return runner.RunStepResult{
			Status:     runner.RunStepStatusFailed,
			ExitCode:   -1,
			Stdout:     stdout,
			Stderr:     stderr,
			StartedAt:  startedAt,
			FinishedAt: finishedAt,
		}, nil
	}

	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) {
		log.Printf("step container exited with error: command=%s exit_code=%d stdout_bytes=%d stderr_bytes=%d",
			logCommand, exitErr.ExitCode(), len(stdout), len(stderr))
		return runner.RunStepResult{
			Status:     runner.RunStepStatusFailed,
			ExitCode:   exitErr.ExitCode(),
			Stdout:     stdout,
			Stderr:     stderr,
			StartedAt:  startedAt,
			FinishedAt: finishedAt,
		}, nil
	}

	log.Printf("docker command failed: command=%s error=%v stdout_bytes=%d stderr_bytes=%d",
		logCommand, waitErr, len(stdout), len(stderr))
	return runner.RunStepResult{}, fmt.Errorf("docker command failed: %w: %s", waitErr, strings.TrimSpace(stdout+stderr))
}

// CleanupBuild removes the shared workspace. Step containers are ephemeral and already removed.
func (r *Runner) CleanupBuild(ctx context.Context, buildID string) error {
	trimmedBuildID := strings.TrimSpace(buildID)
	if trimmedBuildID == "" {
		return nil
	}
	r.clearWorkspacePath(trimmedBuildID)

	if r.workspace == nil {
		return nil
	}

	if err := r.workspace.CleanupWorkspace(ctx, trimmedBuildID); err != nil {
		return fmt.Errorf("cleaning up workspace: %w", err)
	}
	return nil
}

// removeContainer force-removes a container, ignoring "not found" errors.
func (r *Runner) removeContainer(ctx context.Context, containerName string) {
	rmArgs := []string{"rm", "-f", containerName}
	rmOut, err := r.executor.Run(ctx, "docker", rmArgs...)
	if err != nil && !isContainerNotFound(err, rmOut) {
		logDockerCommandFailure(rmArgs, err, rmOut)
	}
}

// stopContainer sends a stop signal to a running container with short timeout.
func (r *Runner) stopContainer(ctx context.Context, containerName string) {
	stopCtx, stopCancel := context.WithTimeout(ctx, 10*time.Second)
	defer stopCancel()
	stopArgs := []string{"stop", "-t", "5", containerName}
	if _, err := r.executor.Run(stopCtx, "docker", stopArgs...); err != nil {
		log.Printf("failed to stop container %s: %v", containerName, err)
	}
}

func isContainerNotFound(err error, output []byte) bool {
	combined := strings.ToLower(strings.TrimSpace(err.Error() + " " + string(output)))
	return strings.Contains(combined, "no such container") || strings.Contains(combined, "no such object")
}

// stepContainerRunArgs builds docker run arguments for an ephemeral step container.
func stepContainerRunArgs(containerName, image, mountBinding, workingDir string, mountDockerSocket bool, request runner.RunStepRequest) []string {
	if workingDir == "" {
		workingDir = workspaceMountPath
	}

	args := []string{
		"run",
		"--name", containerName,
		"-v", mountBinding,
		"-w", workingDir,
	}

	for _, cacheMount := range request.CacheMounts {
		hostPath := strings.TrimSpace(cacheMount.HostPath)
		containerPath := strings.TrimSpace(cacheMount.ContainerPath)
		if hostPath == "" || containerPath == "" {
			continue
		}
		args = append(args, "-v", hostPath+":"+containerPath)
	}

	if mountDockerSocket {
		args = append(args, "-v", "/var/run/docker.sock:/var/run/docker.sock")
	}

	for _, envEntry := range mergeStepEnvironment(request) {
		args = append(args, "-e", envEntry)
	}

	args = append(args, image, request.Command)
	args = append(args, request.Args...)
	return args
}

var forbiddenCacheMountPaths = []string{
	"/",
	"/bin",
	"/dev",
	"/etc",
	"/lib",
	"/lib64",
	"/proc",
	"/sbin",
	"/sys",
	"/usr",
	"/var/run",
	workspaceMountPath,
}

func validateCacheMounts(mounts []runner.CacheMount) ([]runner.CacheMount, error) {
	validated := make([]runner.CacheMount, 0, len(mounts))
	for idx, mount := range mounts {
		hostPath := strings.TrimSpace(mount.HostPath)
		if hostPath == "" {
			return nil, fmt.Errorf("invalid cache mount[%d]: host path is required", idx)
		}
		hostPath = filepath.Clean(hostPath)
		if !filepath.IsAbs(hostPath) {
			return nil, fmt.Errorf("invalid cache mount[%d]: host path must be absolute", idx)
		}

		containerPath := path.Clean(strings.TrimSpace(strings.ReplaceAll(mount.ContainerPath, "\\", "/")))
		if containerPath == "." || !path.IsAbs(containerPath) {
			return nil, fmt.Errorf("invalid cache mount[%d]: container path must be absolute", idx)
		}
		for _, forbidden := range forbiddenCacheMountPaths {
			if containerPath == forbidden || strings.HasPrefix(containerPath, forbidden+"/") {
				return nil, fmt.Errorf("invalid cache mount[%d]: container path %s is not allowed", idx, containerPath)
			}
		}

		if mkdirErr := os.MkdirAll(hostPath, 0o755); mkdirErr != nil {
			return nil, fmt.Errorf("invalid cache mount[%d]: ensuring host path %s: %w", idx, hostPath, mkdirErr)
		}
		info, statErr := os.Stat(hostPath)
		if statErr != nil {
			return nil, fmt.Errorf("invalid cache mount[%d]: stat host path %s: %w", idx, hostPath, statErr)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("invalid cache mount[%d]: host path %s is not a directory", idx, hostPath)
		}
		if _, readErr := os.ReadDir(hostPath); readErr != nil {
			return nil, fmt.Errorf("invalid cache mount[%d]: host path %s not accessible: %w", idx, hostPath, readErr)
		}

		validated = append(validated, runner.CacheMount{HostPath: hostPath, ContainerPath: containerPath})
	}
	return validated, nil
}

// containerNameForStep generates a unique container name for a build step.
func containerNameForStep(buildID string, stepIndex int) string {
	trimmed := strings.TrimSpace(buildID)
	if trimmed == "" {
		return fmt.Sprintf("coyote-step-unknown-%d", stepIndex)
	}
	normalized := strings.ToLower(trimmed)
	normalized = validNameChars.ReplaceAllString(normalized, "-")
	normalized = strings.Trim(normalized, "-._")
	if normalized == "" {
		normalized = "unknown"
	}
	if len(normalized) > 48 {
		normalized = normalized[:48]
	}
	return fmt.Sprintf("coyote-step-%s-%d", normalized, stepIndex)
}

// containerNameForBuild kept for backward compatibility in tests.
func containerNameForBuild(buildID string) string {
	trimmed := strings.TrimSpace(buildID)
	if trimmed == "" {
		return "coyote-build-unknown"
	}
	normalized := strings.ToLower(trimmed)
	normalized = validNameChars.ReplaceAllString(normalized, "-")
	normalized = strings.Trim(normalized, "-._")
	if normalized == "" {
		normalized = "unknown"
	}
	if len(normalized) > 48 {
		normalized = normalized[:48]
	}
	return "coyote-build-" + normalized
}

func resolveContainerWorkingDir(requested string) string {
	return workspace.New("", "").ContainerWorkingDir(requested)
}

func (r *Runner) setWorkspacePath(buildID string, workspacePath string) {
	trimmedBuildID := strings.TrimSpace(buildID)
	trimmedWorkspacePath := strings.TrimSpace(workspacePath)
	if trimmedBuildID == "" || trimmedWorkspacePath == "" {
		return
	}

	r.workspaceMu.Lock()
	r.workspacePaths[trimmedBuildID] = trimmedWorkspacePath
	r.workspaceMu.Unlock()
}

func (r *Runner) clearWorkspacePath(buildID string) {
	trimmedBuildID := strings.TrimSpace(buildID)
	if trimmedBuildID == "" {
		return
	}

	r.workspaceMu.Lock()
	delete(r.workspacePaths, trimmedBuildID)
	r.workspaceMu.Unlock()
}

func (r *Runner) workspacePathForBuild(buildID string) (string, bool) {
	trimmedBuildID := strings.TrimSpace(buildID)
	if trimmedBuildID == "" {
		return "", false
	}

	r.workspaceMu.RLock()
	workspacePath, ok := r.workspacePaths[trimmedBuildID]
	r.workspaceMu.RUnlock()
	if !ok || strings.TrimSpace(workspacePath) == "" {
		return "", false
	}

	return workspacePath, true
}

func (r *Runner) resolveContainerWorkingDirForStep(request runner.RunStepRequest) string {
	resolved := resolveContainerWorkingDir(request.WorkingDir)
	workspacePath, ok := r.workspacePathForBuild(request.BuildID)
	if !ok {
		return resolved
	}

	return constrainContainerWorkingDirToWorkspace(resolved, workspacePath)
}

func constrainContainerWorkingDirToWorkspace(containerWorkingDir string, workspacePath string) string {
	trimmedDir := strings.TrimSpace(containerWorkingDir)
	if trimmedDir == "" {
		return workspaceMountPath
	}
	if trimmedDir == workspaceMountPath {
		return trimmedDir
	}

	prefix := workspaceMountPath + "/"
	if !strings.HasPrefix(trimmedDir, prefix) {
		return workspaceMountPath
	}

	rel := strings.TrimPrefix(trimmedDir, prefix)
	if rel == "" {
		return workspaceMountPath
	}

	workspaceRoot := strings.TrimSpace(workspacePath)
	if workspaceRoot == "" {
		return trimmedDir
	}
	workspaceRoot = canonicalizeHostPath(workspaceRoot)

	hostCandidate := filepath.Join(workspaceRoot, filepath.FromSlash(rel))
	resolvedCandidate, err := filepath.EvalSymlinks(hostCandidate)
	if err != nil {
		return trimmedDir
	}

	relToRoot, err := filepath.Rel(workspaceRoot, resolvedCandidate)
	if err != nil {
		return workspaceMountPath
	}
	if relToRoot == ".." || strings.HasPrefix(relToRoot, ".."+string(filepath.Separator)) {
		log.Printf("working directory escaped workspace via symlink; falling back to workspace root: requested=%s workspace=%s resolved=%s", trimmedDir, workspaceRoot, resolvedCandidate)
		return workspaceMountPath
	}

	return trimmedDir
}

func canonicalizeHostPath(hostPath string) string {
	cleaned := filepath.Clean(strings.TrimSpace(hostPath))
	if cleaned == "" {
		return ""
	}

	resolved, err := filepath.EvalSymlinks(cleaned)
	if err == nil {
		return filepath.Clean(resolved)
	}

	return cleaned
}

// streamOutput consumes stdout and stderr pipes, forwarding chunks to the callback
// while accumulating full output strings.
func streamOutput(stdoutPipe, stderrPipe io.ReadCloser, onOutput runner.StepOutputCallback) (stdout, stderr string, err error) {
	var stdoutBuilder strings.Builder
	var stderrBuilder strings.Builder
	var wg sync.WaitGroup
	var streamErr error
	var streamMu sync.Mutex

	consume := func(pipe io.ReadCloser, stream runner.StepOutputStream, target *strings.Builder) {
		defer wg.Done()
		scanner := bufio.NewScanner(pipe)
		buffer := make([]byte, 0, 64*1024)
		scanner.Buffer(buffer, 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			target.WriteString(line)
			target.WriteString("\n")
			if onOutput != nil {
				if cbErr := onOutput(runner.StepOutputChunk{Stream: stream, ChunkText: line, EmittedAt: time.Now().UTC()}); cbErr != nil {
					streamMu.Lock()
					if streamErr == nil {
						streamErr = cbErr
					}
					streamMu.Unlock()
				}
			}
		}
		if scanErr := scanner.Err(); scanErr != nil {
			streamMu.Lock()
			if streamErr == nil {
				streamErr = scanErr
			}
			streamMu.Unlock()
		}
	}

	wg.Add(2)
	go consume(stdoutPipe, runner.StepOutputStreamStdout, &stdoutBuilder)
	go consume(stderrPipe, runner.StepOutputStreamStderr, &stderrBuilder)
	wg.Wait()

	streamMu.Lock()
	emitErr := streamErr
	streamMu.Unlock()

	return stdoutBuilder.String(), stderrBuilder.String(), emitErr
}

func mergeStepEnvironment(request runner.RunStepRequest) []string {
	merged := map[string]string{}
	for k, v := range request.Env {
		merged[k] = v
	}
	merged["CI"] = "true"
	merged["COYOTE_BUILD_ID"] = request.BuildID
	merged["COYOTE_STEP_ID"] = request.StepID
	merged["COYOTE_WORKSPACE"] = workspaceMountPath

	keys := make([]string, 0, len(merged))
	for k := range merged {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+merged[k])
	}
	return out
}

func timeoutFailureReason(timeout time.Duration) string {
	if timeout > 0 {
		return fmt.Sprintf("step execution timed out after %s", timeout)
	}
	return "step execution timed out"
}

func (r *Runner) runDockerCommand(ctx context.Context, args ...string) ([]byte, error) {
	output, err := r.executor.Run(ctx, "docker", args...)
	if err != nil {
		logDockerCommandFailure(args, err, output)
		return output, fmt.Errorf("docker command failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

func logDockerCommandFailure(args []string, err error, output []byte) {
	log.Printf("docker command failed: command=%s error=%v output_bytes=%d output=omitted", dockerCommandString(redactDockerArgsForLogging(args)), err, len(output))
}

func redactDockerArgsForLogging(args []string) []string {
	redacted := append([]string(nil), args...)
	for idx := 0; idx < len(redacted); idx++ {
		arg := redacted[idx]
		switch {
		case arg == "-e" || arg == "--env":
			if idx+1 < len(redacted) {
				redacted[idx+1] = redactEnvAssignment(redacted[idx+1])
				idx++
			}
		case strings.HasPrefix(arg, "--env="):
			redacted[idx] = "--env=" + redactEnvAssignment(strings.TrimPrefix(arg, "--env="))
		}
	}
	return redacted
}

func redactEnvAssignment(value string) string {
	equalsAt := strings.Index(value, "=")
	if equalsAt <= 0 {
		return value
	}
	return value[:equalsAt] + "=<redacted>"
}

func dockerCommandString(args []string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, "docker")
	for _, arg := range args {
		parts = append(parts, strconv.Quote(arg))
	}
	return strings.Join(parts, " ")
}
