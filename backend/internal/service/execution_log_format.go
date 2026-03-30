package service

import (
	"fmt"
	"strings"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

func formatBuildStartLines(image string, workspacePath string, totalSteps int) []string {
	resolvedImage := strings.TrimSpace(image)
	if resolvedImage == "" {
		resolvedImage = "unknown"
	}

	return []string{
		"Starting build",
		"Pipeline image: " + resolvedImage,
		"Workspace: " + strings.TrimSpace(workspacePath),
		fmt.Sprintf("Steps: %d", totalSteps),
	}
}

func formatStepStartLines(stepIndex int, totalSteps int, stepName string, image string, workingDir string, renderedCommand string) []string {
	resolvedImage := strings.TrimSpace(image)
	if resolvedImage == "" {
		resolvedImage = "unknown"
	}

	lines := []string{
		fmt.Sprintf("==> Step %d/%d: %s", stepIndex, totalSteps, strings.TrimSpace(stepName)),
		"Image: " + resolvedImage,
		"Working directory: " + strings.TrimSpace(workingDir),
		"Command:",
	}

	commandLines := splitLogLines(renderedCommand)
	if len(commandLines) == 0 {
		lines = append(lines, "<empty>")
		return lines
	}

	return append(lines, commandLines...)
}

func formatStepEndLine(stepIndex int, totalSteps int, stepName string, status string, duration time.Duration, exitCode int) string {
	result := fmt.Sprintf("<== Step %d/%d: %s %s in %s", stepIndex, totalSteps, strings.TrimSpace(stepName), status, formatDurationSeconds(duration))
	if status == "failed" && exitCode >= 0 {
		result += fmt.Sprintf(" (exit code %d)", exitCode)
	}
	return result
}

func formatTimedOutStepEndLine(stepIndex int, totalSteps int, stepName string, duration time.Duration) string {
	return fmt.Sprintf("<== Step %d/%d: %s failed in %s (timed out)", stepIndex, totalSteps, strings.TrimSpace(stepName), formatDurationSeconds(duration))
}

func formatFailureReasonLine(reason string) string {
	trimmed := strings.TrimSpace(reason)
	if trimmed == "" {
		trimmed = "unknown failure"
	}
	return "Failure reason: " + trimmed
}

func formatBuildSummaryLines(status domain.BuildStatus, duration time.Duration, completedSteps int, totalSteps int, artifactPaths []string) []string {
	result := "finished"
	if status == domain.BuildStatusSuccess {
		result = "succeeded"
	}
	if status == domain.BuildStatusFailed {
		result = "failed"
	}

	lines := []string{
		fmt.Sprintf("Build %s in %s", result, formatDurationSeconds(duration)),
		fmt.Sprintf("Completed steps: %d/%d", completedSteps, totalSteps),
		fmt.Sprintf("Artifacts collected: %d", len(artifactPaths)),
	}

	for _, artifactPath := range artifactPaths {
		lines = append(lines, "- "+artifactPath)
	}

	if status == domain.BuildStatusFailed {
		lines = append(lines, "Failure summary: see failed step marker(s) above for exit details")
	}

	return lines
}

func formatDurationSeconds(duration time.Duration) string {
	seconds := duration.Seconds()
	if seconds < 0 {
		seconds = 0
	}
	return fmt.Sprintf("%.1fs", seconds)
}
