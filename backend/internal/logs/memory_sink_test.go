package logs

import (
	"context"
	"testing"
	"time"
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

func TestMemorySink_AppendAndListStepLogChunks(t *testing.T) {
	sink := NewMemorySink()

	first, err := sink.AppendStepLogChunk(context.Background(), StepLogChunk{
		BuildID:   "build-1",
		StepID:    "step-1",
		StepIndex: 0,
		StepName:  "setup",
		Stream:    StepLogStreamStdout,
		ChunkText: "line-1",
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("append first chunk failed: %v", err)
	}
	if first.SequenceNo != 1 {
		t.Fatalf("expected first sequence 1, got %d", first.SequenceNo)
	}

	second, err := sink.AppendStepLogChunk(context.Background(), StepLogChunk{
		BuildID:   "build-1",
		StepID:    "step-1",
		StepIndex: 0,
		StepName:  "setup",
		Stream:    StepLogStreamStderr,
		ChunkText: "line-2",
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("append second chunk failed: %v", err)
	}
	if second.SequenceNo != 2 {
		t.Fatalf("expected second sequence 2, got %d", second.SequenceNo)
	}

	chunks, err := sink.ListStepLogChunks(context.Background(), "build-1", 0, 0, 10)
	if err != nil {
		t.Fatalf("list chunks failed: %v", err)
	}
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	if chunks[0].ChunkText != "line-1" || chunks[1].ChunkText != "line-2" {
		t.Fatalf("unexpected chunk ordering: %+v", chunks)
	}

	afterOne, err := sink.ListStepLogChunks(context.Background(), "build-1", 0, 1, 10)
	if err != nil {
		t.Fatalf("list chunks after cursor failed: %v", err)
	}
	if len(afterOne) != 1 || afterOne[0].SequenceNo != 2 {
		t.Fatalf("expected one chunk after cursor with sequence 2, got %+v", afterOne)
	}
}

func TestMemorySink_ListStepLogChunks_LimitIsCapped(t *testing.T) {
	sink := NewMemorySink()

	for i := range 2500 {
		_, err := sink.AppendStepLogChunk(context.Background(), StepLogChunk{
			BuildID:   "build-1",
			StepID:    "step-1",
			StepIndex: 0,
			StepName:  "setup",
			Stream:    StepLogStreamStdout,
			ChunkText: "line",
			CreatedAt: time.Now().UTC(),
		})
		if err != nil {
			t.Fatalf("append chunk failed at index %d: %v", i, err)
		}
	}

	chunks, err := sink.ListStepLogChunks(context.Background(), "build-1", 0, 0, 1000000)
	if err != nil {
		t.Fatalf("list chunks failed: %v", err)
	}
	if len(chunks) != 2000 {
		t.Fatalf("expected capped result size 2000, got %d", len(chunks))
	}
}
