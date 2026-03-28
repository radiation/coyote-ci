package logs

import "context"

type NoopSink struct{}

var _ LogSink = (*NoopSink)(nil)
var _ StepLogChunkAppender = (*NoopSink)(nil)
var _ StepLogChunkReader = (*NoopSink)(nil)

func NewNoopSink() *NoopSink {
	return &NoopSink{}
}

func (s *NoopSink) WriteStepLog(_ context.Context, _ string, _ string, _ string) error {
	return nil
}

func (s *NoopSink) AppendStepLogChunk(_ context.Context, chunk StepLogChunk) (StepLogChunk, error) {
	return chunk, nil
}

func (s *NoopSink) ListStepLogChunks(_ context.Context, _ string, _ int, _ int64, _ int) ([]StepLogChunk, error) {
	return []StepLogChunk{}, nil
}
