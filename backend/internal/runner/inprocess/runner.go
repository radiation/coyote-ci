package inprocess

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/runner"
)

var _ runner.Runner = (*Runner)(nil)

type Runner struct{}

func New() *Runner {
	return &Runner{}
}

func (r *Runner) RunStep(ctx context.Context, request runner.RunStepRequest) (runner.RunStepResult, error) {
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

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	startedAt := time.Now().UTC()
	err := cmd.Run()
	finishedAt := time.Now().UTC()

	if err == nil {
		return runner.RunStepResult{
			Status:     runner.RunStepStatusSuccess,
			ExitCode:   0,
			Stdout:     stdout.String(),
			Stderr:     stderr.String(),
			StartedAt:  startedAt,
			FinishedAt: finishedAt,
		}, nil
	}

	if errors.Is(execCtx.Err(), context.DeadlineExceeded) {
		reason := timeoutFailureReason(timeout)
		stderrStr := stderr.String()
		if strings.TrimSpace(stderrStr) == "" {
			stderrStr = reason
		} else {
			stderrStr = strings.TrimRight(stderrStr, "\n") + "\n" + reason
		}
		return runner.RunStepResult{
			Status:     runner.RunStepStatusFailed,
			ExitCode:   -1,
			Stdout:     stdout.String(),
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
			Stdout:     stdout.String(),
			Stderr:     stderr.String(),
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
