package logs

import (
	"context"
	"sync"
	"time"

	"github.com/radiation/coyote-ci/backend/pkg/contracts"
)

type memoryLogEntry struct {
	buildID   string
	stepName  string
	message   string
	timestamp time.Time
}

type MemorySink struct {
	mu      sync.RWMutex
	entries []memoryLogEntry
}

var _ LogSink = (*MemorySink)(nil)
var _ LogReader = (*MemorySink)(nil)

func NewMemorySink() *MemorySink {
	return &MemorySink{entries: make([]memoryLogEntry, 0)}
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

func (s *MemorySink) GetBuildLogs(_ context.Context, buildID string) ([]contracts.BuildLogLine, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	logs := make([]contracts.BuildLogLine, 0)
	for _, entry := range s.entries {
		if entry.buildID != buildID {
			continue
		}
		logs = append(logs, contracts.BuildLogLine{
			StepName:  entry.stepName,
			Message:   entry.message,
			Timestamp: entry.timestamp,
		})
	}

	return logs, nil
}
