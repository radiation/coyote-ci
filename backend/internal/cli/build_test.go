package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/api"
	"github.com/radiation/coyote-ci/backend/internal/apiclient"
	"github.com/radiation/coyote-ci/backend/internal/cli/config"
	"github.com/radiation/coyote-ci/backend/internal/cli/credentials"
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
		JobName:      stringPtr("coyote-ci"),
		Status:       "failed",
		CreatedAt:    "2026-07-04T00:00:00Z",
		StartedAt:    stringPtr("2026-07-04T00:00:01Z"),
		FinishedAt:   stringPtr("2026-07-04T00:00:03Z"),
		SourceRef:    &ref,
		SourceSHA:    &sha,
		TriggeredBy:  stringPtr("trigger-user"),
		ErrorMessage: &errorMessage,
		PipelineName: &pipeline,
		CurrentSteps: []api.BuildCurrentStepResponse{{ID: "step-0", Index: 0, Name: "lint", Status: "running", StartedAt: stringPtr("2026-07-04T00:00:01Z")}, {ID: "step-2", Index: 2, Name: "test", Status: "running", StartedAt: stringPtr("2026-07-04T00:00:02Z")}},
	}
	steps := []api.BuildStepResponse{{StepIndex: 1, Name: "test", Status: "failed", ExitCode: intPtr(1), Job: &api.ExecutionJobResponse{Name: jobName}}}
	payload := makeBuildStatusPayload("https://example.com/base", build, steps)
	if payload.Build.JobName == nil || *payload.Build.JobName != "coyote-ci" {
		t.Fatalf("expected derived job name, got %+v", payload)
	}
	if len(payload.Build.CurrentSteps) != 2 || payload.Build.CurrentSteps[0].Name != "lint" || payload.Build.CurrentSteps[1].Index != 2 {
		t.Fatalf("unexpected current steps payload: %+v", payload.Build.CurrentSteps)
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
	for _, want := range []string{"Project: Project X", "Job:     coyote-ci", "Commit:  abcdef1", "Failed:  step 1 test exited 1", "Running:", "[0] lint", "[2] test"} {
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
	if strings.Contains(buf.String(), "Running:") {
		t.Fatalf("did not expect running section for minimal payload, got %s", buf.String())
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

func TestBuildWatchEmitterOutputs(t *testing.T) {
	human := &bytes.Buffer{}
	humanEmitter := newBuildWatchEmitter(output.ModeHuman, human)
	stepIndex := 2
	stepName := "test"
	exitCode := 1
	for _, event := range []buildWatchEvent{
		{Type: "build_status", BuildID: "build-1", Timestamp: "2026-07-07T00:00:00Z", Status: "running"},
		{Type: "step_started", BuildID: "build-1", Timestamp: "2026-07-07T00:00:01Z", StepIndex: &stepIndex, StepName: &stepName, Status: "running"},
		{Type: "log_chunk", BuildID: "build-1", Timestamp: "2026-07-07T00:00:02Z", StepIndex: &stepIndex, StepName: &stepName, Stream: "stderr", Text: "boom\n"},
		{Type: "logs_unavailable", BuildID: "build-1", Timestamp: "2026-07-07T00:00:03Z"},
		{Type: "step_finished", BuildID: "build-1", Timestamp: "2026-07-07T00:00:04Z", StepIndex: &stepIndex, StepName: &stepName, Status: "failed", ExitCode: &exitCode},
		{Type: "terminal", BuildID: "build-1", Timestamp: "2026-07-07T00:00:05Z", Status: "failed", ExitCode: &exitCode},
	} {
		if err := humanEmitter.emit(event); err != nil {
			t.Fatalf("emit human event: %v", err)
		}
	}
	out := human.String()
	for _, want := range []string{
		"Build build-1: running",
		"==> step 2: test started",
		"[step 2 test][stderr] boom",
		"Live logs unavailable for this token; continuing with status-only watch.",
		"<== step 2: test failed (exit code 1)",
		"Build build-1 completed with status failed (exit 1)",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in output, got %s", want, out)
		}
	}

	jsonBuf := &bytes.Buffer{}
	jsonEmitter := newBuildWatchEmitter(output.ModeJSON, jsonBuf)
	if err := jsonEmitter.emit(buildWatchEvent{Type: "terminal", BuildID: "build-1", Timestamp: "2026-07-07T00:00:05Z", Status: "success", ExitCode: intPtr(0)}); err != nil {
		t.Fatalf("emit json event: %v", err)
	}
	var decoded buildWatchEvent
	if err := json.Unmarshal(jsonBuf.Bytes(), &decoded); err != nil {
		t.Fatalf("decode json event: %v", err)
	}
	if decoded.Type != "terminal" || decoded.BuildID != "build-1" || decoded.Status != "success" || decoded.ExitCode == nil || *decoded.ExitCode != 0 {
		t.Fatalf("unexpected decoded event: %+v", decoded)
	}

	failingEmitter := newBuildWatchEmitter(output.ModeHuman, failWriter{})
	if err := failingEmitter.emit(buildWatchEvent{Type: "build_status", BuildID: "build-1", Timestamp: "2026-07-07T00:00:00Z", Status: "running"}); err == nil || err.Error() != "write failed" {
		t.Fatalf("expected write failure, got %v", err)
	}
}

func TestBuildArtifactHelpers(t *testing.T) {
	stepID := "step-1"
	contentType := "application/xml"
	artifacts := []api.BuildArtifactResponse{
		{ID: "artifact-2", BuildID: "build-1", Name: "coverage.html", Path: "reports/coverage.html", SizeBytes: 2048, CreatedAt: "2026-07-05T00:00:01Z"},
		{ID: "artifact-1", BuildID: "build-1", StepID: &stepID, Name: "report.xml", Path: "reports/report.xml", SizeBytes: 42, ContentType: &contentType, CreatedAt: "2026-07-05T00:00:02Z"},
	}

	payload := makeBuildArtifactsPayload("build-1", artifacts)
	if payload.BuildID != "build-1" || len(payload.Artifacts) != 2 {
		t.Fatalf("unexpected payload: %+v", payload)
	}
	if payload.Artifacts[0].ID != "artifact-1" || payload.Artifacts[0].StepID == nil || *payload.Artifacts[0].StepID != "step-1" {
		t.Fatalf("expected newest artifact with step metadata first, got %+v", payload.Artifacts[0])
	}

	buf := &bytes.Buffer{}
	if err := writeBuildArtifactsHuman(buf, payload); err != nil {
		t.Fatalf("writeBuildArtifactsHuman failed: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"Artifacts for build build-1", "artifact-1", "reports/report.xml", "2 KB", "coyote build artifacts download build-1 --artifact artifact-1"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in output, got %s", want, out)
		}
	}

	selected, err := selectBuildArtifact(artifacts, "reports/report.xml")
	if err != nil || selected.ID != "artifact-1" {
		t.Fatalf("expected select by path to return artifact-1, got %+v err=%v", selected, err)
	}
	selected, err = selectBuildArtifact(artifacts, "coverage.html")
	if err != nil || selected.ID != "artifact-2" {
		t.Fatalf("expected select by name to return artifact-2, got %+v err=%v", selected, err)
	}
	if _, selectMissingErr := selectBuildArtifact(artifacts, "missing"); selectMissingErr == nil || !strings.Contains(selectMissingErr.Error(), "not found") {
		t.Fatalf("expected not found error, got %v", selectMissingErr)
	}

	duplicateArtifacts := []api.BuildArtifactResponse{
		{ID: "artifact-a", Path: "reports/a/report.xml", Name: "report.xml"},
		{ID: "artifact-b", Path: "reports/b/report.xml", Name: "report.xml"},
	}
	if _, selectAmbiguousErr := selectBuildArtifact(duplicateArtifacts, "report.xml"); selectAmbiguousErr == nil || !strings.Contains(selectAmbiguousErr.Error(), "matched multiple artifact names") {
		t.Fatalf("expected ambiguity error, got %v", selectAmbiguousErr)
	}

	reportArtifact := artifacts[1]
	tempDir := t.TempDir()
	if got, resolveDefaultErr := resolveArtifactOutputPath("", reportArtifact); resolveDefaultErr != nil || got != "report.xml" {
		t.Fatalf("expected default file name, got %q err=%v", got, resolveDefaultErr)
	}
	if got, resolveDirErr := resolveArtifactOutputPath(tempDir, reportArtifact); resolveDirErr != nil || got != filepath.Join(tempDir, "report.xml") {
		t.Fatalf("expected directory output path, got %q err=%v", got, resolveDirErr)
	}
	if got := displayPath("report.xml"); got != "./report.xml" {
		t.Fatalf("unexpected display path: %s", got)
	}
	if got := formatArtifactSize(42); got != "42 B" {
		t.Fatalf("unexpected byte format: %s", got)
	}
	if got := formatArtifactSize(2048); got != "2 KB" {
		t.Fatalf("unexpected kilobyte format: %s", got)
	}
	if _, traversalErr := resolveArtifactOutputPath("", api.BuildArtifactResponse{ID: "artifact-unsafe", Path: "../secrets.txt"}); traversalErr == nil || !strings.Contains(traversalErr.Error(), "not safe") {
		t.Fatalf("expected traversal path rejection, got %v", traversalErr)
	}
	if _, absolutePathErr := resolveArtifactOutputPath(tempDir, api.BuildArtifactResponse{ID: "artifact-unsafe", Path: "/etc/passwd"}); absolutePathErr == nil || !strings.Contains(absolutePathErr.Error(), "not safe") {
		t.Fatalf("expected absolute path rejection, got %v", absolutePathErr)
	}
	if _, drivePathErr := resolveArtifactOutputPath("", api.BuildArtifactResponse{ID: "artifact-unsafe", Path: `C:\temp\evil.txt`}); drivePathErr == nil || !strings.Contains(drivePathErr.Error(), "not safe") {
		t.Fatalf("expected drive path rejection, got %v", drivePathErr)
	}

	downloadPayload := buildArtifactDownloadPayload{BuildID: "build-1", Downloaded: []buildArtifactDownloadView{{ArtifactID: "artifact-1", Name: "report.xml", Path: "./report.xml", SizeBytes: 42}}}
	buf.Reset()
	if writeDownloadErr := writeBuildArtifactDownloadHuman(buf, downloadPayload); writeDownloadErr != nil {
		t.Fatalf("writeBuildArtifactDownloadHuman failed: %v", writeDownloadErr)
	}
	if !strings.Contains(buf.String(), "Downloaded report.xml -> ./report.xml") {
		t.Fatalf("unexpected download human output: %s", buf.String())
	}
	if writeFailErr := writeBuildArtifactDownloadHuman(failWriter{}, downloadPayload); writeFailErr == nil {
		t.Fatal("expected download human writer to surface write errors")
	}

	destination := filepath.Join(tempDir, "nested", "report.xml")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.String() != "/api/builds/build-1/artifacts/artifact-1/download" {
			t.Fatalf("unexpected download path %q", r.URL.String())
		}
		_, _ = w.Write([]byte("artifact-body"))
	}))
	defer server.Close()
	client, err := apiclient.New(server.URL, "test-token", "coyote/dev", nil)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	originalReplace := replaceFileAtomicFunc
	t.Cleanup(func() {
		replaceFileAtomicFunc = originalReplace
	})
	written, err := downloadBuildArtifactToPath(t.Context(), client, "build-1", reportArtifact, destination, false)
	if err != nil {
		t.Fatalf("downloadBuildArtifactToPath failed: %v", err)
	}
	if written != int64(len("artifact-body")) {
		t.Fatalf("unexpected written bytes: %d", written)
	}
	body, readErr := os.ReadFile(destination)
	if readErr != nil {
		t.Fatalf("read downloaded file: %v", readErr)
	}
	if string(body) != "artifact-body" {
		t.Fatalf("unexpected downloaded file body: %q", string(body))
	}
	if _, overwriteErr := downloadBuildArtifactToPath(t.Context(), client, "build-1", reportArtifact, destination, false); overwriteErr == nil || !strings.Contains(overwriteErr.Error(), "already exists") {
		t.Fatalf("expected overwrite protection, got %v", overwriteErr)
	}
	if _, forceOverwriteErr := downloadBuildArtifactToPath(t.Context(), client, "build-1", reportArtifact, destination, true); forceOverwriteErr != nil {
		t.Fatalf("expected force overwrite to succeed, got %v", forceOverwriteErr)
	}
	tempMatches, err := filepath.Glob(filepath.Join(filepath.Dir(destination), ".coyote-artifact-*"))
	if err != nil {
		t.Fatalf("glob temp files: %v", err)
	}
	if len(tempMatches) != 0 {
		t.Fatalf("expected temp files to be cleaned up, got %v", tempMatches)
	}

	originalContent := []byte("old-content")
	if seedWriteErr := os.WriteFile(destination, originalContent, 0o640); seedWriteErr != nil {
		t.Fatalf("seed destination: %v", seedWriteErr)
	}
	failingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":"missing_token_scope","message":"api token does not have the required scope: artifact:read"}}`))
	}))
	defer failingServer.Close()
	failingClient, err := apiclient.New(failingServer.URL, "test-token", "coyote/dev", nil)
	if err != nil {
		t.Fatalf("new failing client: %v", err)
	}
	if _, failingDownloadErr := downloadBuildArtifactToPath(t.Context(), failingClient, "build-1", reportArtifact, destination, true); failingDownloadErr == nil {
		t.Fatal("expected download failure")
	}
	body, readErr = os.ReadFile(destination)
	if readErr != nil {
		t.Fatalf("read preserved destination: %v", readErr)
	}
	if string(body) != string(originalContent) {
		t.Fatalf("expected destination to remain intact after download failure, got %q", string(body))
	}

	replaceFileAtomicFunc = func(source string, destination string) error {
		if filepath.Dir(source) != filepath.Dir(destination) {
			t.Fatalf("expected temp file in destination directory, got %s for %s", source, destination)
		}
		return errors.New("replace failed")
	}
	if _, replaceErr := downloadBuildArtifactToPath(t.Context(), client, "build-1", reportArtifact, destination, true); replaceErr == nil || !strings.Contains(replaceErr.Error(), "replace failed") {
		t.Fatalf("expected replacement failure, got %v", replaceErr)
	}
	body, readErr = os.ReadFile(destination)
	if readErr != nil {
		t.Fatalf("read destination after replace failure: %v", readErr)
	}
	if string(body) != string(originalContent) {
		t.Fatalf("expected destination to remain intact after replace failure, got %q", string(body))
	}
	tempMatches, err = filepath.Glob(filepath.Join(filepath.Dir(destination), ".coyote-artifact-*"))
	if err != nil {
		t.Fatalf("glob temp files after replace failure: %v", err)
	}
	if len(tempMatches) != 0 {
		t.Fatalf("expected temp files to be cleaned up after replace failure, got %v", tempMatches)
	}

	if got := buildArtifactDownloadViewFromArtifact(api.BuildArtifactResponse{ID: "artifact-fallback", Path: "../unsafe"}, "./artifact-fallback", 0); got.Name != "artifact-fallback" || got.SizeBytes != 0 {
		t.Fatalf("expected fallback download view, got %+v", got)
	}

	if err := writeBuildArtifactsHuman(failWriter{}, payload); err == nil {
		t.Fatal("expected writeBuildArtifactsHuman to surface write errors")
	}
	emptyBuf := &bytes.Buffer{}
	if err := writeBuildArtifactsHuman(emptyBuf, buildArtifactsPayload{BuildID: "build-empty"}); err != nil {
		t.Fatalf("writeBuildArtifactsHuman empty failed: %v", err)
	}
	if !strings.Contains(emptyBuf.String(), "No artifacts found") {
		t.Fatalf("unexpected empty artifacts output: %s", emptyBuf.String())
	}
}

func TestBuildArtifactHelperEdgeCases(t *testing.T) {
	blankStepID := "  "
	blankContentType := "  "
	payload := makeBuildArtifactsPayload(" trimmed-build ", []api.BuildArtifactResponse{{
		ID:          "artifact-1",
		BuildID:     "",
		Name:        " report.xml ",
		Path:        "reports/report.xml",
		StepID:      &blankStepID,
		ContentType: &blankContentType,
		SizeBytes:   512,
		CreatedAt:   "2026-07-05T00:00:00Z",
	}})
	if payload.BuildID != "trimmed-build" {
		t.Fatalf("expected trimmed build id fallback, got %+v", payload)
	}
	if payload.Artifacts[0].StepID != nil || payload.Artifacts[0].ContentType != nil || payload.Artifacts[0].Name != "report.xml" {
		t.Fatalf("expected trimmed metadata in payload, got %+v", payload.Artifacts[0])
	}

	buf := &bytes.Buffer{}
	if err := writeBuildArtifactsHuman(buf, payload); err != nil {
		t.Fatalf("writeBuildArtifactsHuman failed: %v", err)
	}
	if !strings.Contains(buf.String(), "artifact-1") || !strings.Contains(buf.String(), "reports/report.xml") || !strings.Contains(buf.String(), "512 B") {
		t.Fatalf("expected nil step/content type placeholders in output, got %s", buf.String())
	}

	duplicatePaths := []api.BuildArtifactResponse{
		{ID: "artifact-a", Path: "reports/report.xml"},
		{ID: "artifact-b", Path: "reports/report.xml"},
	}
	if _, duplicatePathErr := selectBuildArtifact(duplicatePaths, "reports/report.xml"); duplicatePathErr == nil || !strings.Contains(duplicatePathErr.Error(), "multiple artifact paths") {
		t.Fatalf("expected duplicate path error, got %v", duplicatePathErr)
	}

	if !artifactNameMatches(api.BuildArtifactResponse{Name: "other", Path: "reports/report.xml"}, "report.xml") {
		t.Fatal("expected artifactNameMatches to match path basename")
	}
	if artifactNameMatches(api.BuildArtifactResponse{Name: "other", Path: "reports/report.xml"}, "missing") {
		t.Fatal("expected artifactNameMatches to reject non-matches")
	}

	if got, err := artifactDownloadRelativePath(api.BuildArtifactResponse{Name: "reports/from-name.xml"}); err != nil || got != "reports/from-name.xml" {
		t.Fatalf("expected name fallback path, got %q err=%v", got, err)
	}
	if got, err := artifactDownloadRelativePath(api.BuildArtifactResponse{ID: "artifact-id-only"}); err != nil || got != "artifact-id-only" {
		t.Fatalf("expected id fallback path, got %q err=%v", got, err)
	}
	if _, err := artifactDownloadRelativePath(api.BuildArtifactResponse{}); err == nil || !strings.Contains(err.Error(), "safe local filename") {
		t.Fatalf("expected empty artifact path error, got %v", err)
	}

	if got, err := validateArtifactRelativePath("reports/nested/output.txt"); err != nil || got != "reports/nested/output.txt" {
		t.Fatalf("expected safe nested path, got %q err=%v", got, err)
	}
	if _, err := validateArtifactRelativePath("."); err == nil || !strings.Contains(err.Error(), "not safe") {
		t.Fatalf("expected dot path rejection, got %v", err)
	}
	if _, err := validateArtifactRelativePath("   "); err == nil || !strings.Contains(err.Error(), "safe local filename") {
		t.Fatalf("expected blank path rejection, got %v", err)
	}

	fileOutput := filepath.Join(t.TempDir(), "artifact.out")
	if err := os.WriteFile(fileOutput, []byte("existing"), 0o600); err != nil {
		t.Fatalf("seed file output: %v", err)
	}
	if got, err := resolveArtifactOutputPath(fileOutput, api.BuildArtifactResponse{Path: "reports/report.xml"}); err != nil || got != fileOutput {
		t.Fatalf("expected existing file output path, got %q err=%v", got, err)
	}
	missingDirOutput := filepath.Join(t.TempDir(), "missing-dir") + string(os.PathSeparator)
	if got, err := resolveArtifactOutputPath(missingDirOutput, api.BuildArtifactResponse{Path: "reports/report.xml"}); err != nil || got != filepath.Join(missingDirOutput, "report.xml") {
		t.Fatalf("expected trailing separator directory output path, got %q err=%v", got, err)
	}

	if got, err := artifactDownloadName(api.BuildArtifactResponse{Name: "report.xml"}); err != nil || got != "report.xml" {
		t.Fatalf("expected artifactDownloadName name fallback, got %q err=%v", got, err)
	}
	if got := displayPath("./report.xml"); got != "./report.xml" {
		t.Fatalf("expected dotted display path to remain unchanged, got %s", got)
	}
	if got := displayPath(filepath.Join(string(os.PathSeparator), "tmp", "report.xml")); got != filepath.Join(string(os.PathSeparator), "tmp", "report.xml") {
		t.Fatalf("expected absolute display path to remain unchanged, got %s", got)
	}
	if hasWindowsDrivePrefix("artifact.txt") {
		t.Fatal("expected non-drive path to be false")
	}

	unsorted := []api.BuildArtifactResponse{
		{ID: "b", Path: "zeta", CreatedAt: "2026-07-05T00:00:00Z"},
		{ID: "a", Path: "alpha", CreatedAt: "2026-07-05T00:00:00Z"},
		{ID: "c", Path: "latest", CreatedAt: "2026-07-05T00:00:01Z"},
		{ID: "d", Path: "alpha", CreatedAt: "2026-07-05T00:00:00Z"},
	}
	sorted := sortBuildArtifacts(unsorted)
	if sorted[0].ID != "c" || sorted[1].ID != "a" || sorted[2].ID != "d" || sorted[3].ID != "b" {
		t.Fatalf("unexpected artifact sort order: %+v", sorted)
	}
}

func TestBuildArtifactsCommandValidationAndErrorPaths(t *testing.T) {
	t.Run("download requires artifact selector before network", func(t *testing.T) {
		called := false
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			http.NotFound(w, r)
		}))
		defer server.Close()

		configStore := config.NewStore(filepath.Join(t.TempDir(), "config.json"))
		if err := configStore.Save(config.File{
			CurrentContext: "local",
			Contexts: map[string]config.Context{
				"local": {Name: "local", ServerURL: server.URL, CredentialRef: "context:local"},
			},
		}); err != nil {
			t.Fatalf("save config: %v", err)
		}
		creds := credentials.NewMemoryStore()
		if setErr := creds.Set("context:local", "stored-token"); setErr != nil {
			t.Fatalf("set token: %v", setErr)
		}

		stdout := &bytes.Buffer{}
		stderr := &bytes.Buffer{}
		code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"build", "artifacts", "download", "build-1"}})
		if code != 2 {
			t.Fatalf("expected exit code 2, got %d stderr=%s", code, stderr.String())
		}
		if called {
			t.Fatal("expected selector validation to stop before HTTP requests")
		}
		if !strings.Contains(stderr.String(), "artifact selector is required") {
			t.Fatalf("unexpected stderr: %s", stderr.String())
		}
	})

	t.Run("download rejects unsafe artifact metadata", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.String() {
			case "/api/builds/build-1/artifacts":
				_, _ = w.Write([]byte(`{"data":{"build_id":"build-1","artifacts":[{"id":"artifact-1","build_id":"build-1","name":"report.xml","path":"../secrets.txt","size_bytes":42,"storage_provider":"filesystem","download_url_path":"/builds/build-1/artifacts/artifact-1/download","created_at":"2026-07-05T00:00:00Z"}]}}`))
			default:
				http.NotFound(w, r)
			}
		}))
		defer server.Close()

		configStore := config.NewStore(filepath.Join(t.TempDir(), "config.json"))
		if err := configStore.Save(config.File{
			CurrentContext: "local",
			Contexts: map[string]config.Context{
				"local": {Name: "local", ServerURL: server.URL, CredentialRef: "context:local"},
			},
		}); err != nil {
			t.Fatalf("save config: %v", err)
		}
		creds := credentials.NewMemoryStore()
		if setErr := creds.Set("context:local", "stored-token"); setErr != nil {
			t.Fatalf("set token: %v", setErr)
		}

		stdout := &bytes.Buffer{}
		stderr := &bytes.Buffer{}
		code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"build", "artifacts", "download", "build-1", "--artifact", "artifact-1"}})
		if code != 2 {
			t.Fatalf("expected exit code 2, got %d stderr=%s", code, stderr.String())
		}
		if !strings.Contains(stderr.String(), "not safe for local output") {
			t.Fatalf("unexpected stderr: %s", stderr.String())
		}
		if strings.Contains(stderr.String(), "stored-token") {
			t.Fatalf("stderr leaked token: %s", stderr.String())
		}
		if stdout.Len() != 0 {
			t.Fatalf("expected no stdout, got %q", stdout.String())
		}
	})

	t.Run("download returns exit 1 for local write errors", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.String() {
			case "/api/builds/build-1/artifacts":
				_, _ = w.Write([]byte(`{"data":{"build_id":"build-1","artifacts":[{"id":"artifact-1","build_id":"build-1","name":"report.xml","path":"reports/report.xml","size_bytes":42,"storage_provider":"filesystem","download_url_path":"/builds/build-1/artifacts/artifact-1/download","created_at":"2026-07-05T00:00:00Z"}]}}`))
			default:
				http.NotFound(w, r)
			}
		}))
		defer server.Close()

		configStore := config.NewStore(filepath.Join(t.TempDir(), "config.json"))
		if err := configStore.Save(config.File{
			CurrentContext: "local",
			Contexts: map[string]config.Context{
				"local": {Name: "local", ServerURL: server.URL, CredentialRef: "context:local"},
			},
		}); err != nil {
			t.Fatalf("save config: %v", err)
		}
		creds := credentials.NewMemoryStore()
		if setErr := creds.Set("context:local", "stored-token"); setErr != nil {
			t.Fatalf("set token: %v", setErr)
		}

		existingOutput := filepath.Join(t.TempDir(), "report.xml")
		if err := os.WriteFile(existingOutput, []byte("existing"), 0o600); err != nil {
			t.Fatalf("seed existing output: %v", err)
		}
		stdout := &bytes.Buffer{}
		stderr := &bytes.Buffer{}
		code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"build", "artifacts", "download", "build-1", "--artifact", "artifact-1", "--output", existingOutput}})
		if code != 1 {
			t.Fatalf("expected exit code 1, got %d stderr=%s", code, stderr.String())
		}
		if !strings.Contains(stderr.String(), "already exists") {
			t.Fatalf("unexpected stderr: %s", stderr.String())
		}
		if stdout.Len() != 0 {
			t.Fatalf("expected no stdout on local write error, got %q", stdout.String())
		}
	})
}

func intPtr(value int) *int { return &value }

func stringPtr(value string) *string { return &value }
