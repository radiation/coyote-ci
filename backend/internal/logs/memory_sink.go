package logs

import (
	"context"
	"sort"
	"sync"
	"time"
)

type memoryLogEntry struct {
	buildID   string
	stepName  string
	message   string
	timestamp time.Time
}

type MemorySink struct {
	mu           sync.RWMutex
	entries      []memoryLogEntry
	stepChunks   []StepLogChunk
	nextSequence int64
}

var _ LogSink = (*MemorySink)(nil)
var _ LogReader = (*MemorySink)(nil)
var _ StepLogChunkAppender = (*MemorySink)(nil)
var _ StepLogChunkReader = (*MemorySink)(nil)

func NewMemorySink() *MemorySink {
	return &MemorySink{entries: make([]memoryLogEntry, 0), stepChunks: make([]StepLogChunk, 0), nextSequence: 1}
}

func (s *MemorySink) WriteStepLog(_ context.Context, buildID string, stepName string, line string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.entries = append(s.entries, memoryLogEntry{
		buildID:   buildID,
		stepName:  stepName,
		message:   line,
		timestamp: time.Now().UTC(),
	})
	return nil
}

func (s *MemorySink) AppendStepLogChunk(_ context.Context, chunk StepLogChunk) (StepLogChunk, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	persisted := chunk
	if persisted.SequenceNo == 0 {
		persisted.SequenceNo = s.nextSequence
		s.nextSequence++
	}
	if persisted.CreatedAt.IsZero() {
		persisted.CreatedAt = time.Now().UTC()
	}

	s.stepChunks = append(s.stepChunks, persisted)
	s.entries = append(s.entries, memoryLogEntry{
		buildID:   persisted.BuildID,
		stepName:  persisted.StepName,
		message:   persisted.ChunkText,
		timestamp: persisted.CreatedAt,
	})
	return persisted, nil
}

func (s *MemorySink) ListStepLogChunks(_ context.Context, buildID string, stepIndex int, afterSequence int64, limit int) ([]StepLogChunk, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 {
		limit = 200
	}
	if limit > 2000 {
		limit = 2000
	}

	out := make([]StepLogChunk, 0, limit)
	for _, chunk := range s.stepChunks {
		if chunk.BuildID != buildID || chunk.StepIndex != stepIndex {
			continue
		}
		if chunk.SequenceNo <= afterSequence {
			continue
		}
		out = append(out, chunk)
		if len(out) >= limit {
			break
		}
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].SequenceNo < out[j].SequenceNo
	})

	return out, nil
}

func (s *MemorySink) GetBuildLogs(_ context.Context, buildID string) ([]BuildLogLine, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	logs := make([]BuildLogLine, 0)
	for _, entry := range s.entries {
		if entry.buildID != buildID {
			continue
		}
		logs = append(logs, BuildLogLine{
			StepName:  entry.stepName,
			Message:   entry.message,
			Timestamp: entry.timestamp,
		})
	}

	return logs, nil
}
