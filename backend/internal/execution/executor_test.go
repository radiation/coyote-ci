package execution

import (
	"context"
	"testing"
	"time"
)

func TestLocalExecutor_Execute_Success(t *testing.T) {
	executor := NewLocalExecutor()

	result, err := executor.Execute(context.Background(), CommandRequest{
		Command: "sh",
		Args:    []string{"-c", "echo hello"},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", result.ExitCode)
	}
	if result.Status != CommandStatusSuccess {
		t.Fatalf("expected status %q, got %q", CommandStatusSuccess, result.Status)
	}
	if result.Stdout != "hello\n" {
		t.Fatalf("expected stdout hello, got %q", result.Stdout)
	}
	if result.StartedAt.IsZero() || result.CompletedAt.IsZero() {
		t.Fatal("expected started/completed timestamps to be set")
	}
}

func TestLocalExecutor_Execute_NonZeroExit(t *testing.T) {
	executor := NewLocalExecutor()

	result, err := executor.Execute(context.Background(), CommandRequest{
		Command: "sh",
		Args:    []string{"-c", "echo boom 1>&2; exit 3"},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.ExitCode != 3 {
		t.Fatalf("expected exit code 3, got %d", result.ExitCode)
	}
	if result.Status != CommandStatusFailed {
		t.Fatalf("expected status %q, got %q", CommandStatusFailed, result.Status)
	}
	if result.Stderr != "boom\n" {
		t.Fatalf("expected stderr boom, got %q", result.Stderr)
	}
}

func TestLocalExecutor_Execute_Timeout(t *testing.T) {
	executor := NewLocalExecutor()

	result, err := executor.Execute(context.Background(), CommandRequest{
		Command: "sh",
		Args:    []string{"-c", "sleep 2"},
		Timeout: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.ExitCode != -1 {
		t.Fatalf("expected timeout exit code -1, got %d", result.ExitCode)
	}
	if result.Status != CommandStatusFailed {
		t.Fatalf("expected status %q, got %q", CommandStatusFailed, result.Status)
	}
	if result.Stderr != "command timed out" {
		t.Fatalf("expected timeout stderr, got %q", result.Stderr)
	}
}

func TestLocalExecutor_Execute_CommandValidationError(t *testing.T) {
	executor := NewLocalExecutor()

	result, err := executor.Execute(context.Background(), CommandRequest{})
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if result.Status != CommandStatusError {
		t.Fatalf("expected status %q, got %q", CommandStatusError, result.Status)
	}
	if result.Error == "" {
		t.Fatal("expected error message on result")
	}
	if result.StartedAt.IsZero() || result.CompletedAt.IsZero() {
		t.Fatal("expected started/completed timestamps on validation error")
	}
}

func TestLocalExecutor_Execute_CommandNotFound_ReturnsExecutionError(t *testing.T) {
	executor := NewLocalExecutor()

	result, err := executor.Execute(context.Background(), CommandRequest{
		Command: "definitely-not-a-real-command",
	})
	if err == nil {
		t.Fatal("expected runtime error, got nil")
	}
	if result.Status != CommandStatusError {
		t.Fatalf("expected status %q, got %q", CommandStatusError, result.Status)
	}
	if result.Error == "" {
		t.Fatal("expected result error message to be set")
	}
	if result.Error != err.Error() {
		t.Fatalf("expected result error %q to match returned error %q", result.Error, err.Error())
	}
	if result.StartedAt.IsZero() || result.CompletedAt.IsZero() {
		t.Fatal("expected started/completed timestamps on runtime error")
	}
}
