package inprocess

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/execution"
	"github.com/radiation/coyote-ci/backend/pkg/contracts"
)

type fakeExecutor struct {
	result      execution.CommandResult
	err         error
	called      bool
	lastRequest execution.CommandRequest
}

func (e *fakeExecutor) Execute(_ context.Context, request execution.CommandRequest) (execution.CommandResult, error) {
	e.called = true
	e.lastRequest = request
	if e.err != nil {
		return execution.CommandResult{}, e.err
	}
	return e.result, nil
}

func TestRunner_RunStep_Success(t *testing.T) {
	exec := &fakeExecutor{result: execution.CommandResult{ExitCode: 0, Stdout: "ok", StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC()}}
	r := New(exec)

	res, err := r.RunStep(context.Background(), contracts.RunStepRequest{Command: "echo", Args: []string{"ok"}, TimeoutSeconds: 5})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !exec.called {
		t.Fatal("expected executor to be called")
	}
	if exec.lastRequest.Command != "echo" {
		t.Fatalf("expected command echo, got %q", exec.lastRequest.Command)
	}
	if exec.lastRequest.Timeout != 5*time.Second {
		t.Fatalf("expected 5s timeout, got %v", exec.lastRequest.Timeout)
	}
	if res.Status != contracts.RunStepStatusSuccess {
		t.Fatalf("expected success status, got %q", res.Status)
	}
}

func TestRunner_RunStep_FailedExitCode(t *testing.T) {
	exec := &fakeExecutor{result: execution.CommandResult{ExitCode: 2}}
	r := New(exec)

	res, err := r.RunStep(context.Background(), contracts.RunStepRequest{Command: "false"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if res.Status != contracts.RunStepStatusFailed {
		t.Fatalf("expected failed status, got %q", res.Status)
	}
}

func TestRunner_RunStep_ExecutorError(t *testing.T) {
	exec := &fakeExecutor{err: errors.New("exec failed")}
	r := New(exec)

	_, err := r.RunStep(context.Background(), contracts.RunStepRequest{Command: "echo"})
	if err == nil || err.Error() != "exec failed" {
		t.Fatalf("expected exec error, got %v", err)
	}
}
