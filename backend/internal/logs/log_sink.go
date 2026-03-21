package logs

import (
	"context"

	"github.com/radiation/coyote-ci/backend/pkg/contracts"
)

type LogSink interface {
	WriteStepLog(ctx context.Context, buildID string, stepName string, line string) error
}

type LogReader interface {
	GetBuildLogs(ctx context.Context, buildID string) ([]contracts.BuildLogLine, error)
}
