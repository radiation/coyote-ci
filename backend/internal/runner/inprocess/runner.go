package inprocess

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/runner"
	"github.com/radiation/coyote-ci/backend/internal/source"
)

var _ runner.Runner = (*Runner)(nil)
var _ runner.StreamingRunner = (*Runner)(nil)
var _ runner.BuildScopedRunner = (*Runner)(nil)

type Runner struct {
	workspace source.WorkspaceMaterializer

	mu         sync.RWMutex
	workspaces map[string]string
}

func New() *Runner {
	return NewWithWorkspaceRoot("")
}

func NewWithWorkspaceRoot(root string) *Runner {
	materializer := source.NewHostWorkspaceMaterializer(root)
	return NewWithWorkspaceMaterializer(materializer)
}

func NewWithWorkspaceMaterializer(workspace source.WorkspaceMaterializer) *Runner {
	return &Runner{
		workspace:  workspace,
		workspaces: map[string]string{},
	}
}

func (r *Runner) PrepareBuild(ctx context.Context, request runner.PrepareBuildRequest) error {
	if r.workspace == nil {
		return errors.New("workspace materializer is required")
	}

	buildID := strings.TrimSpace(request.BuildID)
	if buildID == "" {
		return errors.New("build id is required")
	}

	workspacePath, err := r.workspace.PrepareWorkspace(ctx, source.WorkspacePrepareRequest{
		BuildID:   buildID,
		RepoURL:   strings.TrimSpace(request.RepoURL),
		Ref:       strings.TrimSpace(request.Ref),
		CommitSHA: strings.TrimSpace(request.CommitSHA),
	})
	if err != nil {
		return err
	}

	r.mu.Lock()
	r.workspaces[buildID] = workspacePath
	r.mu.Unlock()

	log.Printf("inprocess prepare build: build_id=%s workspace_path=%s", buildID, workspacePath)
	return nil
}

func (r *Runner) CleanupBuild(ctx context.Context, buildID string) error {
	trimmedBuildID := strings.TrimSpace(buildID)
	if trimmedBuildID == "" {
		return nil
	}

	if r.workspace != nil {
		if err := r.workspace.CleanupWorkspace(ctx, trimmedBuildID); err != nil {
			return err
		}
	}

	r.mu.Lock()
	delete(r.workspaces, trimmedBuildID)
	r.mu.Unlock()

	log.Printf("inprocess cleanup build: build_id=%s", trimmedBuildID)
	return nil
}

func (r *Runner) RunStep(ctx context.Context, request runner.RunStepRequest) (runner.RunStepResult, error) {
	return r.RunStepStream(ctx, request, nil)
}

func (r *Runner) RunStepStream(ctx context.Context, request runner.RunStepRequest, onOutput runner.StepOutputCallback) (runner.RunStepResult, error) {
	if request.Command == "" {
		return runner.RunStepResult{}, errors.New("command is required")
	}

	execCtx := ctx
	cancel := func() {}
	timeout := time.Duration(request.TimeoutSeconds) * time.Second
	if timeout > 0 {
		execCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	cmd := exec.CommandContext(execCtx, request.Command, request.Args...)
	if workspacePath, ok := r.lookupWorkspacePath(request.BuildID); ok {
		resolvedDir, resolveErr := resolveWorkingDir(workspacePath, request.WorkingDir)
		if resolveErr != nil {
			return runner.RunStepResult{}, resolveErr
		}
		cmd.Dir = resolvedDir
		log.Printf("inprocess run step: build_id=%s workspace_path=%s working_dir=%s resolved_dir=%s", strings.TrimSpace(request.BuildID), workspacePath, strings.TrimSpace(request.WorkingDir), resolvedDir)
	} else if request.WorkingDir != "" {
		cmd.Dir = request.WorkingDir
	}
	cmd.Env = mergeEnvironment(request.Env)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return runner.RunStepResult{}, err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return runner.RunStepResult{}, err
	}

	startedAt := time.Now().UTC()
	err = cmd.Start()
	if err != nil {
		return runner.RunStepResult{}, err
	}

	var stdoutBuilder strings.Builder
	var stderrBuilder strings.Builder
	var wg sync.WaitGroup
	var streamMu sync.Mutex
	var streamErr error

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
				if emitErr := onOutput(runner.StepOutputChunk{Stream: stream, ChunkText: line, EmittedAt: time.Now().UTC()}); emitErr != nil {
					streamMu.Lock()
					if streamErr == nil {
						streamErr = emitErr
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
	err = cmd.Wait()
	finishedAt := time.Now().UTC()

	streamMu.Lock()
	emitErr := streamErr
	streamMu.Unlock()
	if emitErr != nil {
		return runner.RunStepResult{}, emitErr
	}

	stdoutStr := stdoutBuilder.String()
	stderrStr := stderrBuilder.String()

	if err == nil {
		return runner.RunStepResult{
			Status:     runner.RunStepStatusSuccess,
			ExitCode:   0,
			Stdout:     stdoutStr,
			Stderr:     stderrStr,
			StartedAt:  startedAt,
			FinishedAt: finishedAt,
		}, nil
	}

	if errors.Is(execCtx.Err(), context.DeadlineExceeded) {
		reason := timeoutFailureReason(timeout)
		if strings.TrimSpace(stderrStr) == "" {
			stderrStr = reason
		} else {
			stderrStr = strings.TrimRight(stderrStr, "\n") + "\n" + reason
		}
		if onOutput != nil {
			if callbackErr := onOutput(runner.StepOutputChunk{Stream: runner.StepOutputStreamSystem, ChunkText: reason, EmittedAt: time.Now().UTC()}); callbackErr != nil {
				return runner.RunStepResult{}, callbackErr
			}
		}
		return runner.RunStepResult{
			Status:     runner.RunStepStatusFailed,
			ExitCode:   -1,
			Stdout:     stdoutStr,
			Stderr:     stderrStr,
			StartedAt:  startedAt,
			FinishedAt: finishedAt,
		}, nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return runner.RunStepResult{
			Status:     runner.RunStepStatusFailed,
			ExitCode:   exitErr.ExitCode(),
			Stdout:     stdoutStr,
			Stderr:     stderrStr,
			StartedAt:  startedAt,
			FinishedAt: finishedAt,
		}, nil
	}

	return runner.RunStepResult{}, err
}

func timeoutFailureReason(timeout time.Duration) string {
	if timeout > 0 {
		return fmt.Sprintf("step execution timed out after %s", timeout)
	}
	return "step execution timed out"
}

// safeEnvKeys lists host environment variables that are safe to propagate into
// build step processes. Everything else from the host is excluded to avoid
// leaking secrets (DB passwords, webhook secrets, etc.).
var safeEnvKeys = []string{
	"PATH",
	"HOME",
	"USER",
	"LANG",
	"TERM",
	"TMPDIR",
	"TZ",
	"SHELL",
}

func mergeEnvironment(extra map[string]string) []string {
	base := make([]string, 0, len(safeEnvKeys)+len(extra))
	for _, key := range safeEnvKeys {
		if val, ok := os.LookupEnv(key); ok {
			base = append(base, key+"="+val)
		}
	}

	if len(extra) == 0 {
		return base
	}

	keys := make([]string, 0, len(extra))
	for key := range extra {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		base = append(base, key+"="+extra[key])
	}

	return base
}

func (r *Runner) lookupWorkspacePath(buildID string) (string, bool) {
	trimmedBuildID := strings.TrimSpace(buildID)
	if trimmedBuildID == "" {
		return "", false
	}

	r.mu.RLock()
	workspacePath, ok := r.workspaces[trimmedBuildID]
	r.mu.RUnlock()
	if !ok || strings.TrimSpace(workspacePath) == "" {
		rootProvider, providerOK := r.workspace.(interface{ WorkspaceRoot() string })
		if !providerOK {
			return "", false
		}
		candidate := filepath.Join(strings.TrimSpace(rootProvider.WorkspaceRoot()), trimmedBuildID)
		if info, statErr := os.Stat(candidate); statErr != nil || !info.IsDir() {
			return "", false
		}
		return candidate, true
	}

	return workspacePath, true
}

func resolveWorkingDir(workspacePath string, workingDir string) (string, error) {
	trimmedWorkspacePath := strings.TrimSpace(workspacePath)
	if trimmedWorkspacePath == "" {
		return "", errors.New("workspace path is required")
	}

	trimmedWorkingDir := strings.TrimSpace(workingDir)
	if trimmedWorkingDir == "" || trimmedWorkingDir == "." {
		return trimmedWorkspacePath, nil
	}

	if filepath.IsAbs(trimmedWorkingDir) {
		return trimmedWorkingDir, nil
	}

	resolved := filepath.Clean(filepath.Join(trimmedWorkspacePath, trimmedWorkingDir))
	rel, err := filepath.Rel(trimmedWorkspacePath, resolved)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("working directory %q escapes build workspace", workingDir)
	}

	return resolved, nil
}
