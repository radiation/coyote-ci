package execution

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"sort"
	"time"
)

type CommandRequest struct {
	Command    string
	Args       []string
	Env        map[string]string
	WorkingDir string
	Timeout    time.Duration
}

type CommandResult struct {
	ExitCode   int
	Stdout     string
	Stderr     string
	StartedAt  time.Time
	FinishedAt time.Time
}

type Executor interface {
	Execute(ctx context.Context, request CommandRequest) (CommandResult, error)
}

type LocalExecutor struct{}

func NewLocalExecutor() *LocalExecutor {
	return &LocalExecutor{}
}

func (e *LocalExecutor) Execute(ctx context.Context, request CommandRequest) (CommandResult, error) {
	if request.Command == "" {
		return CommandResult{}, errors.New("command is required")
	}

	execCtx := ctx
	cancel := func() {}
	if request.Timeout > 0 {
		execCtx, cancel = context.WithTimeout(ctx, request.Timeout)
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

	result := CommandResult{
		ExitCode:   0,
		Stdout:     stdout.String(),
		Stderr:     stderr.String(),
		StartedAt:  startedAt,
		FinishedAt: finishedAt,
	}

	if err == nil {
		return result, nil
	}

	if errors.Is(execCtx.Err(), context.DeadlineExceeded) {
		result.ExitCode = -1
		result.Stderr = "command timed out"
		return result, nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitCode(exitErr)
		return result, nil
	}

	return CommandResult{}, err
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

func exitCode(err *exec.ExitError) int {
	if err == nil {
		return 0
	}
	return err.ExitCode()
}
