package service

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/logs"
	"github.com/radiation/coyote-ci/backend/internal/runner"
)

func (s *BuildService) writeSystemExecutionLogLine(ctx context.Context, request runner.RunStepRequest, appender logs.StepLogChunkAppender, line string) error {
	text := strings.TrimRight(line, "\n")
	if strings.TrimSpace(text) == "" {
		return nil
	}

	if appender != nil {
		_, err := appender.AppendStepLogChunk(ctx, logs.StepLogChunk{
			BuildID:   request.BuildID,
			StepID:    request.StepID,
			StepIndex: request.StepIndex,
			StepName:  request.StepName,
			Stream:    logs.StepLogStreamSystem,
			ChunkText: text,
			CreatedAt: time.Now().UTC(),
		})
		return err
	}

	return s.logSink.WriteStepLog(ctx, request.BuildID, request.StepName, text)
}

func classifyPrepareFailure(err error) (marker string, reason string) {
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	if strings.Contains(message, "creating build container") || strings.Contains(message, "docker create") || strings.Contains(message, "docker run") {
		return "Failed to start build container", "docker create failed"
	}
	if strings.Contains(message, "starting build container") || strings.Contains(message, "docker start") {
		return "Failed to start build container", "docker start failed"
	}
	if strings.Contains(message, "workspace") {
		return "Failed to prepare workspace", "workspace preparation failed"
	}
	return "Failed to prepare build execution", "prepare build failed"
}

func classifyStepFailure(result runner.RunStepResult) (stepFailureKind, string) {
	if result.Status != runner.RunStepStatusFailed {
		return stepFailureKindNone, ""
	}

	stderr := strings.ToLower(strings.TrimSpace(result.Stderr))
	if strings.Contains(stderr, "timed out") {
		trimmed := strings.TrimSpace(result.Stderr)
		if trimmed == "" {
			return stepFailureKindTimeout, "step timed out"
		}
		return stepFailureKindTimeout, trimmed
	}

	if result.ExitCode >= 0 {
		return stepFailureKindExitCode, fmt.Sprintf("command exited with code %d", result.ExitCode)
	}

	return stepFailureKindInternal, "internal execution error"
}

func formatFailureStepEndLine(stepNumber int, totalSteps int, stepName string, duration time.Duration, exitCode int, failureKind stepFailureKind) string {
	if failureKind == stepFailureKindTimeout {
		return formatTimedOutStepEndLine(stepNumber, totalSteps, stepName, duration)
	}
	return formatStepEndLine(stepNumber, totalSteps, stepName, "failed", duration, exitCode)
}

func writeOutputLogs(ctx context.Context, sink logs.LogSink, buildID string, stepName string, output string) error {
	for _, line := range splitLogLines(output) {
		if err := sink.WriteStepLog(ctx, buildID, stepName, line); err != nil {
			return err
		}
	}

	return nil
}

var lineBreakSplitter = regexp.MustCompile(`\r?\n`)

func splitLogLines(output string) []string {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return nil
	}

	return lineBreakSplitter.Split(trimmed, -1)
}
