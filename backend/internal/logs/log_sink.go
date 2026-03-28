package logs

import (
	"context"
	"time"
)

type StepLogStream string

const (
	StepLogStreamStdout StepLogStream = "stdout"
	StepLogStreamStderr StepLogStream = "stderr"
	StepLogStreamSystem StepLogStream = "system"
)

// StepLogChunk is an append-only persisted chunk for a step output stream.
type StepLogChunk struct {
	SequenceNo int64
	BuildID    string
	StepID     string
	StepIndex  int
	StepName   string
	Stream     StepLogStream
	ChunkText  string
	CreatedAt  time.Time
}

// StepLogChunkAppender appends a single chunk and returns the persisted chunk including sequence.
type StepLogChunkAppender interface {
	AppendStepLogChunk(ctx context.Context, chunk StepLogChunk) (StepLogChunk, error)
}

// StepLogChunkReader lists persisted chunks by sequence for replay/resume.
type StepLogChunkReader interface {
	ListStepLogChunks(ctx context.Context, buildID string, stepIndex int, afterSequence int64, limit int) ([]StepLogChunk, error)
}

type LogSink interface {
	WriteStepLog(ctx context.Context, buildID string, stepName string, line string) error
}

// BuildLogLine represents a single log line captured during step execution.
type BuildLogLine struct {
	StepName  string
	Timestamp time.Time
	Message   string
}

type LogReader interface {
	GetBuildLogs(ctx context.Context, buildID string) ([]BuildLogLine, error)
}
