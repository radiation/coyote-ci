package logs

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestPostgresSink_AppendStepLogChunk(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sql mock: %v", err)
	}

	sink := NewPostgresSink(db)
	now := time.Now().UTC()

	mock.ExpectQuery("INSERT INTO build_step_logs").
		WithArgs("build-1", 0, "setup", "stdout", "line-1", now).
		WillReturnRows(sqlmock.NewRows([]string{"sequence_no", "build_id", "step_id", "step_index", "step_name", "stream", "chunk_text", "created_at"}).
			AddRow(1, "build-1", "step-1", 0, "setup", "stdout", "line-1", now))
	mock.ExpectClose()

	chunk, err := sink.AppendStepLogChunk(context.Background(), StepLogChunk{
		BuildID:   "build-1",
		StepIndex: 0,
		StepName:  "setup",
		Stream:    StepLogStreamStdout,
		ChunkText: "line-1",
		CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("append chunk failed: %v", err)
	}
	if chunk.SequenceNo != 1 {
		t.Fatalf("expected sequence 1, got %d", chunk.SequenceNo)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("failed to close db: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestPostgresSink_ListStepLogChunks(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sql mock: %v", err)
	}

	sink := NewPostgresSink(db)
	now := time.Now().UTC()

	mock.ExpectQuery("SELECT sequence_no, build_id, step_id, step_index, step_name, stream, chunk_text, created_at").
		WithArgs("build-1", 0, int64(0), 10).
		WillReturnRows(sqlmock.NewRows([]string{"sequence_no", "build_id", "step_id", "step_index", "step_name", "stream", "chunk_text", "created_at"}).
			AddRow(1, "build-1", "step-1", 0, "setup", "stdout", "line-1", now).
			AddRow(2, "build-1", "step-1", 0, "setup", "stderr", "line-2", now.Add(time.Second)))
	mock.ExpectClose()

	chunks, err := sink.ListStepLogChunks(context.Background(), "build-1", 0, 0, 10)
	if err != nil {
		t.Fatalf("list chunks failed: %v", err)
	}
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	if chunks[0].SequenceNo != 1 || chunks[1].SequenceNo != 2 {
		t.Fatalf("unexpected sequence ordering: %+v", chunks)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("failed to close db: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestPostgresSink_ListStepLogChunksTail(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sql mock: %v", err)
	}

	sink := NewPostgresSink(db)
	now := time.Now().UTC()
	stepIndex := 0

	mock.ExpectQuery("SELECT sequence_no, build_id, step_id, step_index, step_name, stream, chunk_text, created_at").
		WithArgs("build-1", stepIndex, 3).
		WillReturnRows(sqlmock.NewRows([]string{"sequence_no", "build_id", "step_id", "step_index", "step_name", "stream", "chunk_text", "created_at"}).
			AddRow(4, "build-1", "step-1", 0, "setup", "stdout", "line-4", now.Add(3*time.Second)).
			AddRow(3, "build-1", "step-1", 0, "setup", "stdout", "line-3", now.Add(2*time.Second)).
			AddRow(2, "build-1", "step-1", 0, "setup", "stdout", "line-2", now.Add(1*time.Second)))
	mock.ExpectClose()

	chunks, truncated, tailErr := sink.ListStepLogChunksTail(context.Background(), "build-1", &stepIndex, 2)
	if tailErr != nil {
		t.Fatalf("tail list failed: %v", tailErr)
	}
	if !truncated {
		t.Fatal("expected truncated result")
	}
	if len(chunks) != 2 || chunks[0].SequenceNo != 3 || chunks[1].SequenceNo != 4 {
		t.Fatalf("unexpected sequence ordering: %+v", chunks)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("failed to close db: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}
