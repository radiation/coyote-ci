package logs

import (
	"context"
	"testing"
)

func TestNoopSink_WriteStepLog(t *testing.T) {
	tests := []struct {
		name     string
		buildID  string
		stepName string
		line     string
	}{
		{name: "empty inputs", buildID: "", stepName: "", line: ""},
		{name: "normal line", buildID: "build-1", stepName: "step-1", line: "hello"},
	}

	sink := NewNoopSink()
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := sink.WriteStepLog(context.Background(), tc.buildID, tc.stepName, tc.line); err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}
		})
	}
}

func TestNoopSink_WriteStepLog_CanceledContext(t *testing.T) {
	sink := NewNoopSink()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := sink.WriteStepLog(ctx, "build-1", "step-1", "line"); err != nil {
		t.Fatalf("expected nil error for canceled context, got %v", err)
	}
}
