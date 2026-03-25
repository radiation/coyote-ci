package logs

import (
	"context"
	"time"
)

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
