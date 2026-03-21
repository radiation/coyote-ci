package execution

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
)

type CommandRequest struct {
	BuildID    string
	StepName   string
	Command    string
	Args       []string
	Env        map[string]string
	WorkingDir string
	Timeout    time.Duration
}

type CommandStatus string

const (
	CommandStatusSuccess CommandStatus = "success"
	CommandStatusFailed  CommandStatus = "failed"
	CommandStatusError   CommandStatus = "error"
)

type CommandResult struct {
	Status      CommandStatus
	ExitCode    int
	Stdout      string
	Stderr      string
	Error       string
	StartedAt   time.Time
	CompletedAt time.Time
}

type Executor interface {
	Execute(ctx context.Context, request CommandRequest) (CommandResult, error)
}

type LocalExecutor struct{}

var _ Executor = (*LocalExecutor)(nil)

func NewLocalExecutor() *LocalExecutor {
	return &LocalExecutor{}
}

func (e *LocalExecutor) Execute(ctx context.Context, request CommandRequest) (CommandResult, error) {
	if request.Command == "" {
		err := errors.New("command is required")
		now := time.Now().UTC()
		return CommandResult{
			Status:      CommandStatusError,
			Error:       err.Error(),
			StartedAt:   now,
			CompletedAt: now,
		}, err
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
	completedAt := time.Now().UTC()

	result := CommandResult{
		ExitCode:    0,
		Stdout:      stdout.String(),
		Stderr:      stderr.String(),
		StartedAt:   startedAt,
		CompletedAt: completedAt,
	}

	if err == nil {
		result.Status = CommandStatusSuccess
		return result, nil
	}

	if errors.Is(execCtx.Err(), context.DeadlineExceeded) {
		result.ExitCode = -1
		result.Status = CommandStatusFailed
		reason := timeoutFailureReason(request.Timeout)
		if strings.TrimSpace(result.Stderr) == "" {
			result.Stderr = reason
		} else {
			result.Stderr = strings.TrimRight(result.Stderr, "\n") + "\n" + reason
		}
		return result, nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitCode(exitErr)
		result.Status = CommandStatusFailed
		return result, nil
	}

	result.Status = CommandStatusError
	result.Error = err.Error()
	return result, err
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

func exitCode(err *exec.ExitError) int {
	if err == nil {
		return 0
	}
	return err.ExitCode()
}
