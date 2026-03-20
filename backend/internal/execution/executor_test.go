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
	if result.Stdout != "hello\n" {
		t.Fatalf("expected stdout hello, got %q", result.Stdout)
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
	if result.Stderr != "command timed out" {
		t.Fatalf("expected timeout stderr, got %q", result.Stderr)
	}
}
