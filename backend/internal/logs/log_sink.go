package logs

import "context"

type LogSink interface {
	WriteStepLog(ctx context.Context, buildID string, stepName string, line string) error
}
