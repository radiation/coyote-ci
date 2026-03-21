package logs

import (
	"context"
	"testing"
)

func TestMemorySink_WriteAndReadBuildLogs(t *testing.T) {
	sink := NewMemorySink()

	if err := sink.WriteStepLog(context.Background(), "build-1", "step-a", "line 1"); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if err := sink.WriteStepLog(context.Background(), "build-1", "step-a", "line 2"); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if err := sink.WriteStepLog(context.Background(), "build-2", "step-b", "other build"); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	logs, err := sink.GetBuildLogs(context.Background(), "build-1")
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if len(logs) != 2 {
		t.Fatalf("expected 2 logs for build-1, got %d", len(logs))
	}
	if logs[0].StepName != "step-a" || logs[0].Message != "line 1" {
		t.Fatalf("unexpected first log: %+v", logs[0])
	}
	if logs[1].Message != "line 2" {
		t.Fatalf("unexpected second log: %+v", logs[1])
	}
}
