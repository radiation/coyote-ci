package execution

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/logs"
	"github.com/radiation/coyote-ci/backend/internal/runner"
	"github.com/radiation/coyote-ci/backend/internal/workspace"
)

type ExecutionLogManager struct {
	logSink          logs.LogSink
	executionContext StepExecutionContext

	mu              sync.Mutex
	visibilityErr   error
	chunkPersistErr error
}

func NewExecutionLogManager(logSink logs.LogSink, executionContext StepExecutionContext) *ExecutionLogManager {
	return &ExecutionLogManager{logSink: logSink, executionContext: executionContext}
}

func (m *ExecutionLogManager) ChunkAppender() logs.StepLogChunkAppender {
	return m.executionContext.ChunkAppender
}

func (m *ExecutionLogManager) HasChunkAppender() bool {
	return m.executionContext.HasChunkAppender
}

func (m *ExecutionLogManager) EmitSystemLine(ctx context.Context, line string) {
	if err := WriteExecutionSystemLogLine(ctx, m.logSink, m.executionContext.ExecutionRequest, m.executionContext.ChunkAppender, line); err != nil {
		m.mu.Lock()
		if m.visibilityErr == nil {
			m.visibilityErr = err
		}
		m.mu.Unlock()
	}
}

func (m *ExecutionLogManager) EmitSystemLines(ctx context.Context, lines []string) {
	for _, line := range lines {
		m.EmitSystemLine(ctx, line)
	}
}

func (m *ExecutionLogManager) EmitExecutionStart(ctx context.Context) {
	if m.executionContext.StepNumber == 1 && (m.executionContext.Build.StartedAt == nil || m.executionContext.Build.StartedAt.IsZero()) {
		m.EmitSystemLines(ctx, formatBuildStartLines(m.executionContext.ExecutionImage, workspace.DefaultContainerRoot, m.executionContext.TotalSteps))
	}
	if m.executionContext.StepNumber == 1 {
		m.EmitSystemLine(ctx, "Executing pipeline steps")
	}
	m.EmitSystemLines(ctx, formatStepStartLines(
		m.executionContext.StepNumber,
		m.executionContext.TotalSteps,
		m.executionContext.ExecutionRequest.StepName,
		m.executionContext.ExecutionImage,
		m.executionContext.StepWorkingDir,
		m.executionContext.StepCommand,
	))
}

func (m *ExecutionLogManager) EmitExecutionEnd(ctx context.Context, result runner.RunStepResult) {
	if result.Status == runner.RunStepStatusFailed {
		failureKind, failureReason := classifyExecutionStepFailure(result)
		m.EmitSystemLine(ctx, formatExecutionFailureStepEndLine(
			m.executionContext.StepNumber,
			m.executionContext.TotalSteps,
			m.executionContext.ExecutionRequest.StepName,
			result.FinishedAt.Sub(result.StartedAt),
			result.ExitCode,
			failureKind,
		))
		m.EmitSystemLine(ctx, formatFailureReasonLine(failureReason))
		return
	}

	m.EmitSystemLine(ctx, formatStepEndLine(
		m.executionContext.StepNumber,
		m.executionContext.TotalSteps,
		m.executionContext.ExecutionRequest.StepName,
		"succeeded",
		result.FinishedAt.Sub(result.StartedAt),
		result.ExitCode,
	))
}

func (m *ExecutionLogManager) PersistRunnerChunk(ctx context.Context, chunk runner.StepOutputChunk) error {
	if !m.executionContext.HasChunkAppender || m.executionContext.ChunkAppender == nil {
		return nil
	}

	text := strings.TrimRight(chunk.ChunkText, "\n")
	if strings.TrimSpace(text) == "" {
		return nil
	}

	stream := logs.StepLogStreamSystem
	switch chunk.Stream {
	case runner.StepOutputStreamStdout:
		stream = logs.StepLogStreamStdout
	case runner.StepOutputStreamStderr:
		stream = logs.StepLogStreamStderr
	case runner.StepOutputStreamSystem:
		stream = logs.StepLogStreamSystem
	}

	_, appendErr := m.executionContext.ChunkAppender.AppendStepLogChunk(ctx, logs.StepLogChunk{
		BuildID:   m.executionContext.ExecutionRequest.BuildID,
		StepID:    m.executionContext.ExecutionRequest.StepID,
		StepIndex: m.executionContext.ExecutionRequest.StepIndex,
		StepName:  m.executionContext.ExecutionRequest.StepName,
		Stream:    stream,
		ChunkText: text,
		CreatedAt: chunk.EmittedAt,
	})
	if appendErr != nil {
		m.mu.Lock()
		if m.chunkPersistErr == nil {
			m.chunkPersistErr = appendErr
		}
		m.mu.Unlock()
	}
	return nil
}

func (m *ExecutionLogManager) BackfillNonStreamingOutput(ctx context.Context, result runner.RunStepResult) {
	if !m.executionContext.HasChunkAppender {
		return
	}
	for _, line := range splitExecutionLogLines(result.Stdout) {
		_ = m.PersistRunnerChunk(ctx, runner.StepOutputChunk{Stream: runner.StepOutputStreamStdout, ChunkText: line, EmittedAt: time.Now().UTC()})
	}
	for _, line := range splitExecutionLogLines(result.Stderr) {
		_ = m.PersistRunnerChunk(ctx, runner.StepOutputChunk{Stream: runner.StepOutputStreamStderr, ChunkText: line, EmittedAt: time.Now().UTC()})
	}
}

func (m *ExecutionLogManager) ApplyBufferedErrors(report *StepCompletionReport) {
	m.mu.Lock()
	capturedChunkErr := m.chunkPersistErr
	capturedVisibilityErr := m.visibilityErr
	m.mu.Unlock()

	if capturedChunkErr != nil {
		report.SideEffectErr = joinSideEffectErrors(report.SideEffectErr, capturedChunkErr)
	}
	if capturedVisibilityErr != nil {
		report.SideEffectErr = joinSideEffectErrors(report.SideEffectErr, capturedVisibilityErr)
	}
}
