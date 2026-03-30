package docker

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os/exec"
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
	containerIdleCmd   = "while true; do sleep 3600; done"
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
	Workspace    source.WorkspaceMaterializer
	DefaultImage string
	Executor     commandExecutor
}

type Runner struct {
	workspace    source.WorkspaceMaterializer
	defaultImage string
	executor     commandExecutor
}

var _ runner.BuildScopedRunner = (*Runner)(nil)

func New(opts Options) *Runner {
	execImpl := opts.Executor
	if execImpl == nil {
		execImpl = &osCommandExecutor{}
	}

	return &Runner{
		workspace:    opts.Workspace,
		defaultImage: strings.TrimSpace(opts.DefaultImage),
		executor:     execImpl,
	}
}

func (r *Runner) PrepareBuild(ctx context.Context, request runner.PrepareBuildRequest) error {
	buildID := strings.TrimSpace(request.BuildID)
	if buildID == "" {
		return errors.New("build id is required")
	}

	workspacePath, err := r.ensureBuildWorkspaceReady(ctx, request)
	if err != nil {
		return err
	}

	image := r.resolveExecutionImage(request.Image)
	if image == "" {
		return errors.New("execution image is required")
	}

	containerName := containerNameForBuild(buildID)
	if err := r.ensureBuildContainerReady(ctx, buildID, containerName, image, workspacePath); err != nil {
		return err
	}

	return nil
}

func (r *Runner) resolveExecutionImage(candidate string) string {
	image := strings.TrimSpace(candidate)
	if image != "" {
		return image
	}
	return r.defaultImage
}

func (r *Runner) ensureBuildWorkspaceReady(ctx context.Context, request runner.PrepareBuildRequest) (string, error) {
	if r.workspace == nil {
		return "", errors.New("workspace materializer is required")
	}

	return r.workspace.PrepareWorkspace(ctx, source.WorkspacePrepareRequest{
		BuildID:   strings.TrimSpace(request.BuildID),
		RepoURL:   strings.TrimSpace(request.RepoURL),
		Ref:       strings.TrimSpace(request.Ref),
		CommitSHA: strings.TrimSpace(request.CommitSHA),
	})
}

func (r *Runner) ensureBuildContainerReady(ctx context.Context, buildID string, containerName string, image string, workspacePath string) error {
	exists, running, err := r.inspectContainerState(ctx, containerName)
	if err != nil {
		return err
	}

	if exists && running {
		return nil
	}
	if exists {
		if _, err := r.runDockerCommand(ctx, "start", containerName); err != nil {
			return fmt.Errorf("starting build container: %w", err)
		}
		return nil
	}

	return r.createBuildContainer(ctx, buildID, containerName, image, workspacePath)
}

func (r *Runner) inspectContainerState(ctx context.Context, containerName string) (exists bool, running bool, err error) {
	inspectArgs := []string{"inspect", "-f", "{{.State.Running}}", containerName}
	stateOut, inspectErr := r.executor.Run(ctx, "docker", inspectArgs...)
	if inspectErr == nil {
		return true, strings.EqualFold(strings.TrimSpace(string(stateOut)), "true"), nil
	}
	if isContainerNotFound(inspectErr, stateOut) {
		return false, false, nil
	}
	logDockerCommandFailure(inspectArgs, inspectErr, stateOut)
	return false, false, fmt.Errorf("inspecting build container: docker command failed: %w: %s", inspectErr, strings.TrimSpace(string(stateOut)))
}

func isContainerNotFound(err error, output []byte) bool {
	combined := strings.ToLower(strings.TrimSpace(err.Error() + " " + string(output)))
	return strings.Contains(combined, "no such container") || strings.Contains(combined, "no such object")
}

func (r *Runner) createBuildContainer(ctx context.Context, buildID string, containerName string, image string, workspacePath string) error {
	buildWorkspace := workspace.New(buildID, workspacePath)
	mountBinding := buildWorkspace.HostRoot + ":" + buildWorkspace.ContainerRoot
	workingDir := buildWorkspace.ContainerWorkingDir(".")

	args := []string{
		"run",
		"-d",
		"--name", containerName,
		"-w", workingDir,
		"-v", mountBinding,
		image,
		"sh",
		"-c",
		containerIdleCmd,
	}
	log.Printf("starting container: image=%s command=%s working_dir=%s mounts=%s", image, dockerCommandString(args), workingDir, mountBinding)
	if _, err := r.runDockerCommand(ctx, args...); err != nil {
		return fmt.Errorf("creating build container: %w", err)
	}
	return nil
}

func (r *Runner) RunStep(ctx context.Context, request runner.RunStepRequest) (runner.RunStepResult, error) {
	return r.RunStepStream(ctx, request, nil)
}

func (r *Runner) RunStepStream(ctx context.Context, request runner.RunStepRequest, onOutput runner.StepOutputCallback) (runner.RunStepResult, error) {
	if strings.TrimSpace(request.BuildID) == "" {
		return runner.RunStepResult{}, errors.New("build id is required")
	}
	if strings.TrimSpace(request.Command) == "" {
		return runner.RunStepResult{}, errors.New("command is required")
	}

	execCtx := ctx
	cancel := func() {}
	timeout := time.Duration(request.TimeoutSeconds) * time.Second
	if timeout > 0 {
		execCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	args := dockerExecArgs(request)
	logCommand := dockerCommandString(redactDockerArgsForLogging(args))
	containerName := containerNameForBuild(request.BuildID)
	containerImage := r.inspectContainerImage(ctx, containerName)
	if containerImage == "" {
		containerImage = "unknown"
	}
	log.Printf("starting container step execution: image=%s command=%s working_dir=%s mounts=%s", containerImage, logCommand, resolveContainerWorkingDir(request.WorkingDir), workspaceMountPath)

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
				if err := onOutput(runner.StepOutputChunk{Stream: stream, ChunkText: line, EmittedAt: time.Now().UTC()}); err != nil {
					streamMu.Lock()
					if streamErr == nil {
						streamErr = err
					}
					streamMu.Unlock()
				}
			}
		}
		if err := scanner.Err(); err != nil {
			streamMu.Lock()
			if streamErr == nil {
				streamErr = err
			}
			streamMu.Unlock()
		}
	}

	wg.Add(2)
	go consume(stdoutPipe, runner.StepOutputStreamStdout, &stdoutBuilder)
	go consume(stderrPipe, runner.StepOutputStreamStderr, &stderrBuilder)

	wg.Wait()
	waitErr := cmd.Wait()
	finishedAt := time.Now().UTC()

	streamMu.Lock()
	emitErr := streamErr
	streamMu.Unlock()
	if emitErr != nil {
		return runner.RunStepResult{}, emitErr
	}

	stdout := stdoutBuilder.String()
	stderr := stderrBuilder.String()
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
		log.Printf("docker command failed: command=%s error=%v stdout_bytes=%d stderr_bytes=%d output=omitted", logCommand, waitErr, len(stdout), len(stderr))
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
		log.Printf("docker command failed: command=%s error=%v stdout_bytes=%d stderr_bytes=%d output=omitted", logCommand, waitErr, len(stdout), len(stderr))
		return runner.RunStepResult{
			Status:     runner.RunStepStatusFailed,
			ExitCode:   exitErr.ExitCode(),
			Stdout:     stdout,
			Stderr:     stderr,
			StartedAt:  startedAt,
			FinishedAt: finishedAt,
		}, nil
	}

	log.Printf("docker command failed: command=%s error=%v stdout_bytes=%d stderr_bytes=%d output=omitted", logCommand, waitErr, len(stdout), len(stderr))
	return runner.RunStepResult{}, fmt.Errorf("docker command failed: %w: %s", waitErr, strings.TrimSpace(stdout+stderr))
}

func (r *Runner) CleanupBuild(ctx context.Context, buildID string) error {
	trimmedBuildID := strings.TrimSpace(buildID)
	if trimmedBuildID == "" {
		return nil
	}

	containerName := containerNameForBuild(trimmedBuildID)
	var rmErr error
	rmArgs := []string{"rm", "-f", containerName}
	rmOut, err := r.executor.Run(ctx, "docker", rmArgs...)
	if err != nil && !isContainerNotFound(err, rmOut) {
		logDockerCommandFailure(rmArgs, err, rmOut)
		rmErr = fmt.Errorf("removing build container: docker command failed: %w: %s", err, strings.TrimSpace(string(rmOut)))
	}

	if r.workspace == nil {
		return rmErr
	}

	wsErr := r.workspace.CleanupWorkspace(ctx, trimmedBuildID)
	if wsErr != nil {
		wsErr = fmt.Errorf("cleaning up workspace: %w", wsErr)
	}

	return errors.Join(rmErr, wsErr)
}
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

func dockerExecArgs(request runner.RunStepRequest) []string {
	args := []string{"exec", "-w", resolveContainerWorkingDir(request.WorkingDir)}

	for _, envEntry := range mergeStepEnvironment(request) {
		args = append(args, "-e", envEntry)
	}

	args = append(args, containerNameForBuild(request.BuildID), request.Command)
	args = append(args, request.Args...)
	return args
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

func (r *Runner) inspectContainerImage(ctx context.Context, containerName string) string {
	out, err := r.executor.Run(ctx, "docker", "inspect", "-f", "{{.Config.Image}}", containerName)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
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
