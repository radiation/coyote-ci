package inprocess

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/runner"
)

var _ runner.Runner = (*Runner)(nil)
var _ runner.StreamingRunner = (*Runner)(nil)

type Runner struct{}

func New() *Runner {
	return &Runner{}
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
	if request.WorkingDir != "" {
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

func mergeEnvironment(extra map[string]string) []string {
	base := os.Environ()
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
