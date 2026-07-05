package cli

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/api"
	"github.com/radiation/coyote-ci/backend/internal/cli/output"
)

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

type failReader struct{}

func (failReader) Read([]byte) (int, error) {
	return 0, errors.New("read failed")
}

func TestParseBuildLogsOptions(t *testing.T) {
	tests := []struct {
		name       string
		stepRaw    string
		failed     bool
		tail       int
		tailSet    bool
		wantStep   *int
		wantFailed bool
		wantTail   int
		wantErr    string
	}{
		{name: "empty step uses defaults", tail: 0, wantFailed: false, wantTail: 0},
		{name: "explicit zero tail rejected", tail: 0, tailSet: true, wantErr: "tail must be a positive integer"},
		{name: "step and failed conflict", stepRaw: "2", failed: true, wantErr: "step and failed cannot be used together"},
		{name: "negative step", stepRaw: "-1", wantErr: "step must be a non-negative integer"},
		{name: "negative tail", tail: -1, wantErr: "tail must be a positive integer"},
		{name: "step and tail", stepRaw: " 3 ", tail: 5, tailSet: true, wantStep: intPtr(3), wantTail: 5},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseBuildLogsOptions(tc.stepRaw, tc.failed, tc.tail, tc.tailSet)
			if tc.wantErr != "" {
				if err == nil || err.Error() != tc.wantErr {
					t.Fatalf("expected error %q, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantStep == nil {
				if got.Step != nil {
					t.Fatalf("expected nil step, got %v", *got.Step)
				}
			} else if got.Step == nil || *got.Step != *tc.wantStep {
				t.Fatalf("expected step %v, got %+v", *tc.wantStep, got.Step)
			}
			if got.Failed != tc.wantFailed || got.Tail != tc.wantTail {
				t.Fatalf("unexpected options: %+v", got)
			}
		})
	}
}

func TestBuildHelperFormattingAndFallbacks(t *testing.T) {
	projectName := "Project X"
	jobID := "job-12345678"
	jobName := "unit"
	author := "Alice"
	sha := "abcdef123456"
	ref := "refs/heads/main"
	errorMessage := "boom"
	pipeline := "default"
	build := api.BuildResponse{
		ID:           "build-1",
		ProjectID:    "project-1",
		ProjectName:  &projectName,
		JobID:        &jobID,
		Status:       "failed",
		CreatedAt:    "2026-07-04T00:00:00Z",
		StartedAt:    stringPtr("2026-07-04T00:00:01Z"),
		FinishedAt:   stringPtr("2026-07-04T00:00:03Z"),
		SourceRef:    &ref,
		SourceSHA:    &sha,
		TriggeredBy:  stringPtr("trigger-user"),
		ErrorMessage: &errorMessage,
		PipelineName: &pipeline,
	}
	steps := []api.BuildStepResponse{{StepIndex: 1, Name: "test", Status: "failed", ExitCode: intPtr(1), Job: &api.ExecutionJobResponse{Name: jobName}}}
	payload := makeBuildStatusPayload("https://example.com/base", build, steps)
	if payload.Build.JobName == nil || *payload.Build.JobName != jobName {
		t.Fatalf("expected derived job name, got %+v", payload)
	}
	if payload.Build.Author == nil || *payload.Build.Author != "trigger-user" {
		t.Fatalf("expected author fallback from trigger, got %+v", payload.Build.Author)
	}
	if payload.Build.DurationMS == nil || *payload.Build.DurationMS != 2000 {
		t.Fatalf("expected duration 2000ms, got %+v", payload.Build.DurationMS)
	}
	if !strings.Contains(payload.Build.WebURL, "/base/builds/build-1?step=1") {
		t.Fatalf("unexpected web url: %s", payload.Build.WebURL)
	}

	buf := &bytes.Buffer{}
	if err := writeBuildStatusHuman(buf, payload); err != nil {
		t.Fatalf("writeBuildStatusHuman failed: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"Project: Project X", "Job:     unit", "Commit:  abcdef1", "Failed:  step 1 test exited 1"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in output, got %s", want, out)
		}
	}

	minimal := buildStatusPayload{Build: buildStatusView{ID: "build-2", ProjectID: "project-2", Status: "running", CreatedAt: "2026-07-04T00:00:00Z"}}
	buf.Reset()
	if err := writeBuildStatusHuman(buf, minimal); err != nil {
		t.Fatalf("writeBuildStatusHuman minimal failed: %v", err)
	}
	if !strings.Contains(buf.String(), "Job:     manual") {
		t.Fatalf("expected manual job fallback, got %s", buf.String())
	}
	if err := writeBuildStatusHuman(failWriter{}, payload); err == nil {
		t.Fatal("expected writeBuildStatusHuman to surface write errors")
	}

	logsBuf := &bytes.Buffer{}
	logsResponse := api.BuildLogsResponse{
		Logs: []api.BuildLogResponse{
			{StepIndex: 0, StepName: "setup", Stream: "stdout", Line: "alpha"},
			{StepIndex: 1, StepName: "test", Stream: "stderr", Message: "beta"},
		},
		Truncated: true,
	}
	if err := writeBuildLogsHuman(logsBuf, logsResponse); err != nil {
		t.Fatalf("writeBuildLogsHuman failed: %v", err)
	}
	logsOut := logsBuf.String()
	for _, want := range []string{"== step 0: setup ==", "== step 1: test ==", "[stderr] beta", "[truncated] Showing the most recent log entries."} {
		if !strings.Contains(logsOut, want) {
			t.Fatalf("expected %q in output, got %s", want, logsOut)
		}
	}

	if err := writeBuildLogsHuman(failWriter{}, api.BuildLogsResponse{Logs: []api.BuildLogResponse{{StepIndex: 0, StepName: "setup", Line: "alpha"}}}); err == nil {
		t.Fatal("expected writeBuildLogsHuman to surface write errors")
	}
	logsBuf.Reset()
	if err := writeBuildLogsHuman(logsBuf, api.BuildLogsResponse{}); err != nil {
		t.Fatalf("expected no logs response to render, got %v", err)
	}
	if strings.TrimSpace(logsBuf.String()) != "No logs found" {
		t.Fatalf("unexpected no logs output: %q", logsBuf.String())
	}

	if got := logHeader(&api.BuildLogSelectedStepResponse{StepIndex: 2, Name: "verify", Status: "failed"}, api.BuildLogResponse{}); got != "step 2: verify (failed)" {
		t.Fatalf("unexpected selected header: %s", got)
	}
	if got := logHeader(nil, api.BuildLogResponse{StepIndex: 3, StepName: "build"}); got != "step 3: build" {
		t.Fatalf("unexpected entry header: %s", got)
	}
	if got := displayProjectLabel(nil, "project-3"); got != "project-3" {
		t.Fatalf("unexpected project label fallback: %s", got)
	}
	if got := displayJobLabel(nil, &jobID); got != jobID {
		t.Fatalf("unexpected job label fallback: %s", got)
	}
	if got := displayJobLabel(nil, nil); got != "manual" {
		t.Fatalf("unexpected manual job label: %s", got)
	}
	if got := shortSHA("abc123"); got != "abc123" {
		t.Fatalf("unexpected short sha short path: %s", got)
	}
	if got := formatDurationMS(1500); got != "1.5s" {
		t.Fatalf("unexpected duration string: %s", got)
	}
	if buildDurationMS(nil, stringPtr("2026-07-04T00:00:01Z")) != nil {
		t.Fatal("expected nil duration for missing start")
	}
	if buildDurationMS(stringPtr("bad"), stringPtr("2026-07-04T00:00:01Z")) != nil {
		t.Fatal("expected nil duration for bad timestamp")
	}
	if buildDurationMS(stringPtr("2026-07-04T00:00:02Z"), stringPtr("2026-07-04T00:00:01Z")) != nil {
		t.Fatal("expected nil duration for reversed timestamps")
	}
	if got := buildWebURL("://bad", "build-1", nil); got != "" {
		t.Fatalf("expected empty web url for invalid server url, got %s", got)
	}
	if got := firstJobName([]api.BuildStepResponse{{Job: &api.ExecutionJobResponse{Name: "  "}}}); got != nil {
		t.Fatalf("expected nil job name when blank, got %v", *got)
	}
	if got := firstFailedStep([]api.BuildStepResponse{{StepIndex: 0, Name: "ok", Status: "success"}}); got != nil {
		t.Fatalf("expected nil failed step, got %+v", got)
	}
	if got := firstNonEmptyPtr(nil, stringPtr("  "), &author); got == nil || *got != author {
		t.Fatalf("unexpected first non-empty ptr result: %+v", got)
	}

	retryPayload := makeBuildRetryPayload("https://example.com/base", "build-1", api.BuildResponse{ID: "build-2", Status: "queued"})
	if retryPayload.Retried.SourceBuildID != "build-1" || retryPayload.Retried.BuildID != "build-2" || !strings.Contains(retryPayload.Retried.WebURL, "/base/builds/build-2") {
		t.Fatalf("unexpected retry payload: %+v", retryPayload)
	}
	buf.Reset()
	if err := writeBuildRetryHuman(buf, retryPayload); err != nil {
		t.Fatalf("writeBuildRetryHuman failed: %v", err)
	}
	if got := buf.String(); !strings.Contains(got, "Retried build build-1 -> build-2") || !strings.Contains(got, "Status: queued") {
		t.Fatalf("unexpected retry output: %s", got)
	}
	buf.Reset()
	if err := writeBuildRetryHuman(buf, buildRetryPayload{Retried: buildRetryView{SourceBuildID: "build-1", BuildID: "build-1", Status: "queued"}}); err != nil {
		t.Fatalf("writeBuildRetryHuman same-id failed: %v", err)
	}
	if !strings.Contains(buf.String(), "Retried build build-1\n") {
		t.Fatalf("expected same-id retry output, got %q", buf.String())
	}
	if err := writeBuildRetryHuman(failWriter{}, retryPayload); err == nil {
		t.Fatal("expected writeBuildRetryHuman to surface write errors")
	}
	if isInteractiveInput(strings.NewReader("y\n")) {
		t.Fatal("expected strings reader to be treated as non-interactive")
	}
}

func TestConfirmBuildRetry(t *testing.T) {
	originalInteractiveCheck := isInteractiveInputFunc
	t.Cleanup(func() {
		isInteractiveInputFunc = originalInteractiveCheck
	})

	t.Run("assume yes skips prompt", func(t *testing.T) {
		app := &app{stdin: strings.NewReader("ignored"), stderr: &bytes.Buffer{}}
		if err := app.confirmBuildRetry("build-1", output.ModeHuman, true); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
	})

	t.Run("json requires yes", func(t *testing.T) {
		app := &app{stdin: strings.NewReader("ignored"), stderr: &bytes.Buffer{}}
		err := app.confirmBuildRetry("build-1", output.ModeJSON, false)
		var exitErr *ExitError
		if !errors.As(err, &exitErr) || exitErr.Code != 2 || !strings.Contains(err.Error(), "requires --yes") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("noninteractive requires yes", func(t *testing.T) {
		isInteractiveInputFunc = func(io.Reader) bool { return false }
		app := &app{stdin: strings.NewReader("ignored"), stderr: &bytes.Buffer{}}
		err := app.confirmBuildRetry("build-1", output.ModeHuman, false)
		var exitErr *ExitError
		if !errors.As(err, &exitErr) || exitErr.Code != 2 || !strings.Contains(err.Error(), "stdin is not interactive") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("accepts confirmation", func(t *testing.T) {
		isInteractiveInputFunc = func(io.Reader) bool { return true }
		stderr := &bytes.Buffer{}
		app := &app{stdin: strings.NewReader("yes\n"), stderr: stderr}
		if err := app.confirmBuildRetry("build-1", output.ModeHuman, false); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if !strings.Contains(stderr.String(), "Retry build build-1?") {
			t.Fatalf("expected prompt in stderr, got %q", stderr.String())
		}
	})

	t.Run("declines confirmation", func(t *testing.T) {
		isInteractiveInputFunc = func(io.Reader) bool { return true }
		app := &app{stdin: strings.NewReader("n\n"), stderr: &bytes.Buffer{}}
		err := app.confirmBuildRetry("build-1", output.ModeHuman, false)
		var exitErr *ExitError
		if !errors.As(err, &exitErr) || exitErr.Code != 2 || !strings.Contains(err.Error(), "canceled") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("stderr write failure is returned", func(t *testing.T) {
		isInteractiveInputFunc = func(io.Reader) bool { return true }
		app := &app{stdin: strings.NewReader("yes\n"), stderr: failWriter{}}
		if err := app.confirmBuildRetry("build-1", output.ModeHuman, false); err == nil || err.Error() != "write failed" {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("stdin read failure is returned", func(t *testing.T) {
		isInteractiveInputFunc = func(io.Reader) bool { return true }
		app := &app{stdin: failReader{}, stderr: &bytes.Buffer{}}
		if err := app.confirmBuildRetry("build-1", output.ModeHuman, false); err == nil || err.Error() != "read failed" {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("eof without yes cancels", func(t *testing.T) {
		isInteractiveInputFunc = func(io.Reader) bool { return true }
		app := &app{stdin: strings.NewReader(""), stderr: &bytes.Buffer{}}
		err := app.confirmBuildRetry("build-1", output.ModeHuman, false)
		var exitErr *ExitError
		if !errors.As(err, &exitErr) || exitErr.Code != 2 || !strings.Contains(err.Error(), "canceled") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func intPtr(value int) *int { return &value }

func stringPtr(value string) *string { return &value }
