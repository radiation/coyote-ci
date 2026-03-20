package logs

import "context"

type NoopSink struct{}

var _ LogSink = (*NoopSink)(nil)

func NewNoopSink() *NoopSink {
	return &NoopSink{}
}

func (s *NoopSink) WriteStepLog(_ context.Context, _ string, _ string, _ string) error {
	return nil
}
