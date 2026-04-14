package build

import (
	"strings"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

func TestFormatBuildStartLines(t *testing.T) {
	lines := formatBuildStartLines("golang:1.26", "/workspace", 3)
	if len(lines) != 4 {
		t.Fatalf("expected 4 header lines, got %d", len(lines))
	}
	if lines[0] != "Starting build" {
		t.Fatalf("unexpected first line: %q", lines[0])
	}
	if lines[1] != "Pipeline image: golang:1.26" {
		t.Fatalf("unexpected image line: %q", lines[1])
	}
	if lines[2] != "Workspace: /workspace" {
		t.Fatalf("unexpected workspace line: %q", lines[2])
	}
	if lines[3] != "Steps: 3" {
		t.Fatalf("unexpected step count line: %q", lines[3])
	}
}

func TestFormatStepStartAndEndLines(t *testing.T) {
	start := formatStepStartLines(1, 3, "setup", "golang:1.26", "/workspace", "echo \"hello\"")
	if start[0] != "==> Step 1/3: setup" {
		t.Fatalf("unexpected step start marker: %q", start[0])
	}
	if start[1] != "Image: golang:1.26" {
		t.Fatalf("unexpected image line: %q", start[1])
	}
	if start[2] != "Working directory: /workspace" {
		t.Fatalf("unexpected working directory line: %q", start[2])
	}
	if start[3] != "Command:" {
		t.Fatalf("unexpected command label: %q", start[3])
	}
	if start[4] != "echo \"hello\"" {
		t.Fatalf("unexpected command body: %q", start[4])
	}

	success := formatStepEndLine(1, 3, "setup", "succeeded", 800*time.Millisecond, 0)
	if success != "<== Step 1/3: setup succeeded in 0.8s" {
		t.Fatalf("unexpected success line: %q", success)
	}

	failure := formatStepEndLine(2, 3, "test", "failed", 4200*time.Millisecond, 1)
	if failure != "<== Step 2/3: test failed in 4.2s (exit code 1)" {
		t.Fatalf("unexpected failure line: %q", failure)
	}

	timeoutFailure := formatTimedOutStepEndLine(2, 3, "test", 600*time.Second)
	if timeoutFailure != "<== Step 2/3: test failed in 600.0s (timed out)" {
		t.Fatalf("unexpected timeout failure line: %q", timeoutFailure)
	}

	reason := formatFailureReasonLine("command exited with code 1")
	if reason != "Failure reason: command exited with code 1" {
		t.Fatalf("unexpected failure reason line: %q", reason)
	}
}

func TestFormatBuildSummaryLines(t *testing.T) {
	lines := formatBuildSummaryLines(domain.BuildStatusFailed, 5400*time.Millisecond, 2, 3, []string{"coverage.out", "junit.xml"})
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "Build failed in 5.4s") {
		t.Fatalf("expected failed summary line, got %q", joined)
	}
	if !strings.Contains(joined, "Artifacts collected: 2") {
		t.Fatalf("expected artifact count line, got %q", joined)
	}
	if !strings.Contains(joined, "- coverage.out") || !strings.Contains(joined, "- junit.xml") {
		t.Fatalf("expected artifact list, got %q", joined)
	}
	if !strings.Contains(joined, "Failure summary:") {
		t.Fatalf("expected failure summary marker, got %q", joined)
	}
}
