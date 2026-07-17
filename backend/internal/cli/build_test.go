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
		SCMStatus:    &api.BuildSCMStatusResponse{Reportable: true, Configured: true, Provider: "github", RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: stringPtr("abcdef1234567890"), Context: stringPtr("coyote/project-1/job-1"), DesiredState: stringPtr("failure"), DeliveryState: stringPtr("retry_waiting"), Attempts: intPtr(2), NextAttemptAt: stringPtr("2026-07-17T14:30:00Z"), LastError: stringPtr("GitHub rate limit exceeded")},
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
	for _, want := range []string{"Project: Project X", "Job:     coyote-ci", "Commit:  abcdef1", "Failed:  step 1 test exited 1", "SCM status", "Provider:       github", "Repository:     octo/repo", "Context:        coyote/project-1/job-1", "Desired state:  failure", "Delivery state: retry_waiting", "Attempts:       2", "Next retry:     2026-07-17T14:30:00Z", "Last error:     GitHub rate limit exceeded", "Running:", "[0] lint", "[2] test"} {
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

	artifactTriggersPayload := makeBuildArtifactTriggerDeliveriesPayload("build-1", api.BuildArtifactTriggerDeliveriesResponse{
		BuildID:                  "build-1",
		BuildTriggerKind:         "manual",
		RecursiveDispatchBlocked: false,
		Summary:                  api.BuildArtifactTriggerDeliverySummaryResponse{DeliveryCount: 2, QueuedCount: 1, FailedCount: 1},
		Deliveries: []api.BuildArtifactTriggerDeliveryResponse{
			{DeliveryID: "delivery-1", Status: "queued", ArtifactID: "artifact-1", ArtifactName: stringPtr("report.xml"), ArtifactPath: "reports/report.xml", ConsumerJobID: "job-1", ConsumerJobName: stringPtr("deploy"), DownstreamBuildID: stringPtr("build-2")},
			{DeliveryID: "delivery-2", Status: "failed", ArtifactID: "artifact-2", ArtifactPath: "docs/summary.txt", ConsumerJobID: "job-2", ErrorMessage: stringPtr("queue failed")},
		},
	})
	if artifactTriggersPayload.Summary.DeliveryCount != 2 || len(artifactTriggersPayload.Deliveries) != 2 || artifactTriggersPayload.Deliveries[0].ConsumerJobName == nil || *artifactTriggersPayload.Deliveries[0].ConsumerJobName != "deploy" {
		t.Fatalf("unexpected artifact trigger payload: %+v", artifactTriggersPayload)
	}
	retryTriggerPayload := buildArtifactTriggerRetryPayload{Result: "retried", Message: "queued downstream build", Delivery: buildArtifactTriggerDeliveryView{DeliveryID: "delivery-1", Status: "queued", DownstreamBuildID: stringPtr("build-2")}}
	trimmedTriggerPayload := makeBuildArtifactTriggerDeliveriesPayload(" build-fallback ", api.BuildArtifactTriggerDeliveriesResponse{
		BuildID:                  "",
		BuildTriggerKind:         "  ",
		RecursiveDispatchBlocked: false,
		Summary:                  api.BuildArtifactTriggerDeliverySummaryResponse{},
		Deliveries:               []api.BuildArtifactTriggerDeliveryResponse{{ArtifactID: "artifact-1", ArtifactPath: "  path/from/api.tgz  ", ArtifactName: stringPtr("  "), ConsumerJobID: "job-1", ConsumerJobName: stringPtr("  "), DownstreamBuildID: stringPtr("  "), ErrorMessage: stringPtr("  ")}},
	})
	if trimmedTriggerPayload.BuildID != "build-fallback" || trimmedTriggerPayload.BuildTriggerKind != "" {
		t.Fatalf("unexpected fallback trigger payload metadata: %+v", trimmedTriggerPayload)
	}
	if trimmedTriggerPayload.Deliveries[0].ArtifactName != nil || trimmedTriggerPayload.Deliveries[0].ConsumerJobName != nil || trimmedTriggerPayload.Deliveries[0].DownstreamBuildID != nil || trimmedTriggerPayload.Deliveries[0].ErrorMessage != nil {
		t.Fatalf("expected blank optional fields to be trimmed away, got %+v", trimmedTriggerPayload.Deliveries[0])
	}
	if got := displayArtifactTriggerArtifact(buildArtifactTriggerDeliveryView{ArtifactName: stringPtr("artifact-name"), ArtifactPath: "path", ArtifactID: "id"}); got != "artifact-name" {
		t.Fatalf("unexpected artifact display from name: %s", got)
	}
	if got := displayArtifactTriggerArtifact(buildArtifactTriggerDeliveryView{ArtifactPath: " path/from/delivery.tgz ", ArtifactID: "id"}); got != "path/from/delivery.tgz" {
		t.Fatalf("unexpected artifact display from path: %s", got)
	}
	if got := displayArtifactTriggerArtifact(buildArtifactTriggerDeliveryView{ArtifactID: "artifact-id"}); got != "artifact-id" {
		t.Fatalf("unexpected artifact display from id fallback: %s", got)
	}
	if got := displayArtifactTriggerConsumerJob(buildArtifactTriggerDeliveryView{ConsumerJobName: stringPtr("deploy"), ConsumerJobID: "job-1"}); got != "deploy" {
		t.Fatalf("unexpected consumer job display from name: %s", got)
	}
	if got := displayArtifactTriggerConsumerJob(buildArtifactTriggerDeliveryView{ConsumerJobID: "job-1"}); got != "job-1" {
		t.Fatalf("unexpected consumer job display fallback: %s", got)
	}
	if got := displayTriggerKind("  "); got != "unknown" {
		t.Fatalf("unexpected trigger kind fallback: %s", got)
	}
	if got := displayTriggerKind(" manual "); got != "manual" {
		t.Fatalf("unexpected trigger kind trim: %s", got)
	}
	buf.Reset()
	if err := writeBuildArtifactTriggersHuman(buf, artifactTriggersPayload); err != nil {
		t.Fatalf("writeBuildArtifactTriggersHuman failed: %v", err)
	}
	artifactTriggersOut := buf.String()
	for _, want := range []string{"Artifact trigger deliveries for build build-1", "Summary: 2 deliveries, 1 queued, 1 failed", "delivery-1", "delivery-2", "queued", "report.xml", "deploy", "build-2", "queue failed"} {
		if !strings.Contains(artifactTriggersOut, want) {
			t.Fatalf("expected %q in artifact trigger output, got %s", want, artifactTriggersOut)
		}
	}
	buf.Reset()
	if err := writeBuildArtifactTriggerRetryHuman(buf, retryTriggerPayload); err != nil {
		t.Fatalf("writeBuildArtifactTriggerRetryHuman failed: %v", err)
	}
	if got := buf.String(); !strings.Contains(got, "Retried artifact-trigger delivery delivery-1 -> build-2") || !strings.Contains(got, "Status: queued") {
		t.Fatalf("unexpected artifact trigger retry output: %s", got)
	}
	buf.Reset()
	if err := writeBuildArtifactTriggerRetryHuman(buf, buildArtifactTriggerRetryPayload{Result: "retried", Delivery: buildArtifactTriggerDeliveryView{DeliveryID: "delivery-3", Status: "pending"}}); err != nil {
		t.Fatalf("writeBuildArtifactTriggerRetryHuman no-downstream failed: %v", err)
	}
	if got := buf.String(); !strings.Contains(got, "Retried artifact-trigger delivery delivery-3\n") || !strings.Contains(got, "Status: pending") || strings.Contains(got, "Message:") {
		t.Fatalf("unexpected no-downstream retry output: %s", got)
	}
	buf.Reset()
	if err := writeBuildArtifactTriggerRetryHuman(buf, buildArtifactTriggerRetryPayload{Result: "already_satisfied", Message: "artifact trigger delivery already points at a downstream build", Delivery: buildArtifactTriggerDeliveryView{DeliveryID: "delivery-2", Status: "queued", DownstreamBuildID: stringPtr("build-9")}}); err != nil {
		t.Fatalf("writeBuildArtifactTriggerRetryHuman already satisfied failed: %v", err)
	}
	if !strings.Contains(buf.String(), "already points at downstream build build-9") {
		t.Fatalf("unexpected already satisfied retry output: %s", buf.String())
	}
	if err := writeBuildArtifactTriggerRetryHuman(failWriter{}, retryTriggerPayload); err == nil {
		t.Fatal("expected writeBuildArtifactTriggerRetryHuman to surface write errors")
	}
	buf.Reset()
	if err := writeBuildArtifactTriggersHuman(buf, buildArtifactTriggerDeliveriesPayload{BuildID: "build-1", BuildTriggerKind: "artifact", RecursiveDispatchBlocked: true, Summary: buildArtifactTriggerSummaryView{}}); err != nil {
		t.Fatalf("writeBuildArtifactTriggersHuman blocked-empty failed: %v", err)
	}
	if !strings.Contains(buf.String(), "Recursive artifact-trigger dispatch is blocked for artifact-triggered builds.") {
		t.Fatalf("unexpected blocked-empty output: %s", buf.String())
	}
	buf.Reset()
	if err := writeBuildArtifactTriggersHuman(buf, buildArtifactTriggerDeliveriesPayload{BuildID: "build-1", BuildTriggerKind: "manual", Summary: buildArtifactTriggerSummaryView{}}); err != nil {
		t.Fatalf("writeBuildArtifactTriggersHuman empty failed: %v", err)
	}
	if !strings.Contains(buf.String(), "No artifact-trigger deliveries were recorded for this build.") {
		t.Fatalf("unexpected empty output: %s", buf.String())
	}
	if err := writeBuildArtifactTriggersHuman(failWriter{}, artifactTriggersPayload); err == nil {
		t.Fatal("expected writeBuildArtifactTriggersHuman to surface write errors")
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

func TestBuildWatchHelpers(t *testing.T) {
	t.Run("log chunk event trims step metadata", func(t *testing.T) {
		event := buildWatchLogChunkEvent("build-1", api.StepLogChunkResponse{
			StepIndex: 3,
			StepName:  "  test-step  ",
			StepID:    "  step-3  ",
			Stream:    " stderr ",
			ChunkText: "boom\n",
			CreatedAt: "2026-07-07T00:00:00Z",
		})
		if event.StepIndex == nil || *event.StepIndex != 3 || event.StepName == nil || *event.StepName != "test-step" || event.StepID == nil || *event.StepID != "step-3" || event.Stream != "stderr" {
			t.Fatalf("unexpected log chunk event: %+v", event)
		}
	})

	t.Run("step event uses finished timestamp for finished events", func(t *testing.T) {
		step := api.BuildStepResponse{ID: " step-1 ", StepIndex: 1, Name: " test ", Status: "failed", StartedAt: stringPtr("2026-07-07T00:00:01Z"), FinishedAt: stringPtr("2026-07-07T00:00:02Z")}
		started := buildWatchStepEvent("step_started", "build-1", step, nil)
		finished := buildWatchStepEvent("step_finished", "build-1", step, intPtr(1))
		if started.Timestamp != "2026-07-07T00:00:01Z" || finished.Timestamp != "2026-07-07T00:00:02Z" {
			t.Fatalf("unexpected step timestamps: started=%+v finished=%+v", started, finished)
		}
	})

	t.Run("watch helper fallbacks", func(t *testing.T) {
		if got := watchTimestamp(nil); strings.TrimSpace(got) == "" {
			t.Fatal("expected fallback timestamp")
		}
		steps := sortBuildSteps([]api.BuildStepResponse{{ID: "b", StepIndex: 2}, {ID: "a", StepIndex: 2}, {ID: "c", StepIndex: 1}})
		if steps[0].ID != "c" || steps[1].ID != "a" || steps[2].ID != "b" {
			t.Fatalf("unexpected step ordering: %+v", steps)
		}
		if got := maxInt64(1, 5); got != 5 {
			t.Fatalf("expected maxInt64 to pick 5, got %d", got)
		}
		if got := maxInt64(7, 2); got != 7 {
			t.Fatalf("expected maxInt64 to keep 7, got %d", got)
		}
		if got := valueOrZero(nil); got != 0 {
			t.Fatalf("expected zero fallback, got %d", got)
		}
		if got := valueOrZero(intPtr(4)); got != 4 {
			t.Fatalf("expected value fallback 4, got %d", got)
		}
		if got := valueOrUnknownPtr(nil); got != "unknown" {
			t.Fatalf("expected unknown fallback, got %q", got)
		}
		if got := valueOrUnknownPtr(stringPtr("  named-step  ")); got != "named-step" {
			t.Fatalf("expected trimmed value, got %q", got)
		}
	})
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

	downloadPayload := buildArtifactDownloadPayload{BuildID: "build-1", Downloaded: []buildArtifactDownloadView{{ArtifactID: "artifact-1", Name: "report.xml", ArtifactPath: "reports/report.xml", StepID: &stepID, ContentType: &contentType, SizeBytes: 42, Path: "./report.xml", LocalPath: "./report.xml", DownloadedBytes: 13}}}
	buf.Reset()
	if writeDownloadErr := writeBuildArtifactDownloadHuman(buf, downloadPayload); writeDownloadErr != nil {
		t.Fatalf("writeBuildArtifactDownloadHuman failed: %v", writeDownloadErr)
	}
	if !strings.Contains(buf.String(), "Downloaded report.xml -> ./report.xml") {
		t.Fatalf("unexpected download human output: %s", buf.String())
	}
	buf.Reset()
	if writeDownloadErr := writeBuildArtifactDownloadHuman(buf, buildArtifactDownloadPayload{BuildID: "build-empty"}); writeDownloadErr != nil {
		t.Fatalf("writeBuildArtifactDownloadHuman empty failed: %v", writeDownloadErr)
	}
	if !strings.Contains(buf.String(), "No artifacts found for build build-empty") {
		t.Fatalf("unexpected empty download output: %s", buf.String())
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

	if got := buildArtifactDownloadViewFromArtifact(api.BuildArtifactResponse{ID: "artifact-fallback", Path: "../unsafe"}, "./artifact-fallback", 0); got.Name != "artifact-fallback" || got.SizeBytes != 0 || got.DownloadedBytes != 0 || got.LocalPath != "./artifact-fallback" || got.Path != "./artifact-fallback" {
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

	bulkOutputDir := filepath.Join(t.TempDir(), "bulk-artifacts")
	plannedBulk, planErr := planBulkArtifactDownloads([]api.BuildArtifactResponse{{ID: "artifact-1", Path: "reports/report.xml"}, {ID: "artifact-2", Path: "logs/summary.txt"}}, bulkOutputDir, false)
	if planErr != nil {
		t.Fatalf("planBulkArtifactDownloads failed: %v", planErr)
	}
	if len(plannedBulk) != 2 || plannedBulk[0].DestinationPath != filepath.Join(bulkOutputDir, "reports", "report.xml") || plannedBulk[1].DestinationPath != filepath.Join(bulkOutputDir, "logs", "summary.txt") {
		t.Fatalf("unexpected bulk plan: %+v", plannedBulk)
	}
	if got, err := resolveArtifactBulkOutputDir(bulkOutputDir); err != nil || got != bulkOutputDir {
		t.Fatalf("expected missing bulk output directory to be accepted, got %q err=%v", got, err)
	}
	existingBulkDir := t.TempDir()
	if got, err := resolveArtifactBulkOutputDir(existingBulkDir); err != nil || got != existingBulkDir {
		t.Fatalf("expected existing bulk output directory, got %q err=%v", got, err)
	}
	existingBulkFile := filepath.Join(t.TempDir(), "bulk.out")
	if err := os.WriteFile(existingBulkFile, []byte("existing"), 0o600); err != nil {
		t.Fatalf("seed bulk output file: %v", err)
	}
	if _, err := resolveArtifactBulkOutputDir(existingBulkFile); err == nil || !strings.Contains(err.Error(), "must be a directory") {
		t.Fatalf("expected bulk output file rejection, got %v", err)
	}
	bulkParentFile := filepath.Join(t.TempDir(), "bulk-parent-file")
	if err := os.WriteFile(bulkParentFile, []byte("existing"), 0o600); err != nil {
		t.Fatalf("seed bulk parent file: %v", err)
	}
	if _, err := resolveArtifactBulkOutputDir(filepath.Join(bulkParentFile, "child")); err == nil {
		t.Fatal("expected bulk output stat failure when parent is a file")
	}
	if _, err := resolveArtifactBulkOutputDir("   "); err == nil || !strings.Contains(err.Error(), "requires --output") {
		t.Fatalf("expected missing bulk output rejection, got %v", err)
	}
	if _, err := planBulkArtifactDownloads([]api.BuildArtifactResponse{{ID: "artifact-1", Path: "reports/report.xml"}, {ID: "artifact-2", Path: "reports/report.xml"}}, bulkOutputDir, false); err == nil || !strings.Contains(err.Error(), "map to the same output path") {
		t.Fatalf("expected duplicate bulk path rejection, got %v", err)
	}
	if _, err := planBulkArtifactDownloads([]api.BuildArtifactResponse{{ID: "artifact-1", Path: "reports/report.xml"}}, filepath.Join(bulkParentFile, "child"), false); err == nil {
		t.Fatal("expected bulk plan stat failure when output parent is a file")
	}
	existingDestination := filepath.Join(existingBulkDir, "reports", "report.xml")
	if err := os.MkdirAll(filepath.Dir(existingDestination), 0o755); err != nil {
		t.Fatalf("mkdir existing destination parent: %v", err)
	}
	if err := os.WriteFile(existingDestination, []byte("existing"), 0o600); err != nil {
		t.Fatalf("seed existing destination: %v", err)
	}
	if _, err := planBulkArtifactDownloads([]api.BuildArtifactResponse{{ID: "artifact-1", Path: "reports/report.xml"}}, existingBulkDir, false); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected existing bulk file rejection, got %v", err)
	}
	if _, err := planBulkArtifactDownloads([]api.BuildArtifactResponse{{ID: "artifact-1", Path: "reports/report.xml"}}, existingBulkDir, true); err != nil {
		t.Fatalf("expected force bulk overwrite preflight to succeed, got %v", err)
	}
	existingDestinationDir := filepath.Join(existingBulkDir, "logs", "summary.txt")
	if err := os.MkdirAll(existingDestinationDir, 0o755); err != nil {
		t.Fatalf("seed directory destination: %v", err)
	}
	if _, err := planBulkArtifactDownloads([]api.BuildArtifactResponse{{ID: "artifact-2", Path: "logs/summary.txt"}}, existingBulkDir, true); err == nil || !strings.Contains(err.Error(), "exists as a directory") {
		t.Fatalf("expected directory collision rejection, got %v", err)
	}
	directoryDestination := filepath.Join(existingBulkDir, "reports-dir")
	if err := os.MkdirAll(directoryDestination, 0o755); err != nil {
		t.Fatalf("seed directory output path: %v", err)
	}
	directoryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.String() != "/api/builds/build-1/artifacts/artifact-1/download" {
			t.Fatalf("unexpected download path %q", r.URL.String())
		}
		_, _ = w.Write([]byte("artifact-body"))
	}))
	defer directoryServer.Close()
	directoryClient, err := apiclient.New(directoryServer.URL, "test-token", "coyote/dev", nil)
	if err != nil {
		t.Fatalf("new directory client: %v", err)
	}
	reportArtifactForDirectory := api.BuildArtifactResponse{ID: "artifact-1", Path: "reports/report.xml"}
	if _, err := downloadBuildArtifactToPath(t.Context(), directoryClient, "build-1", reportArtifactForDirectory, directoryDestination, true); err == nil || !strings.Contains(err.Error(), "exists as a directory") {
		t.Fatalf("expected directory output rejection, got %v", err)
	}

	stepID := " step-7 "
	contentType := " application/xml "
	view := buildArtifactDownloadViewFromArtifact(api.BuildArtifactResponse{
		ID:          "artifact-7",
		Name:        "report.xml",
		Path:        "reports/report.xml",
		StepID:      &stepID,
		ContentType: &contentType,
		SizeBytes:   42,
	}, "./downloads/report.xml", 13)
	if view.ArtifactID != "artifact-7" || view.Name != "report.xml" || view.ArtifactPath != "reports/report.xml" || view.StepID == nil || *view.StepID != "step-7" || view.ContentType == nil || *view.ContentType != "application/xml" || view.SizeBytes != 42 || view.Path != "./downloads/report.xml" || view.LocalPath != "./downloads/report.xml" || view.DownloadedBytes != 13 {
		t.Fatalf("unexpected download view: %+v", view)
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
	t.Run("download help describes single and bulk modes", func(t *testing.T) {
		stdout := &bytes.Buffer{}
		stderr := &bytes.Buffer{}
		cmd := NewRootCommand(Dependencies{Stdout: stdout, Stderr: stderr})
		cmd.SetOut(stdout)
		cmd.SetErr(stderr)
		cmd.SetArgs([]string{"build", "artifacts", "download", "--help"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("expected help to succeed, got %v stderr=%s", err, stderr.String())
		}
		out := stdout.String()
		for _, want := range []string{
			"Download one artifact by selector, or download all artifacts into a directory.",
			"Use --artifact to select one artifact by ID, exact artifact path, name, or basename.",
			"Use --all with --output <dir> to download every artifact while preserving safe artifact paths.",
			"--artifact and --all are mutually exclusive.",
			"coyote build artifacts download <build-id> --artifact report.xml",
			"coyote build artifacts download <build-id> --all --output ./artifacts/",
		} {
			if !strings.Contains(out, want) {
				t.Fatalf("expected %q in help output, got %s", want, out)
			}
		}
		if stderr.Len() != 0 {
			t.Fatalf("expected no stderr for help, got %q", stderr.String())
		}
	})

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
		if !strings.Contains(stderr.String(), "one of --artifact or --all is required") {
			t.Fatalf("unexpected stderr: %s", stderr.String())
		}
	})

	t.Run("download rejects mutually exclusive artifact and all before network", func(t *testing.T) {
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
		code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"build", "artifacts", "download", "build-1", "--artifact", "artifact-1", "--all", "--output", t.TempDir()}})
		if code != 2 {
			t.Fatalf("expected exit code 2, got %d stderr=%s", code, stderr.String())
		}
		if called {
			t.Fatal("expected mutually exclusive validation to stop before HTTP requests")
		}
		if !strings.Contains(stderr.String(), "mutually exclusive") {
			t.Fatalf("unexpected stderr: %s", stderr.String())
		}
	})

	t.Run("bulk download requires output directory before network", func(t *testing.T) {
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
		code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"build", "artifacts", "download", "build-1", "--all"}})
		if code != 2 {
			t.Fatalf("expected exit code 2, got %d stderr=%s", code, stderr.String())
		}
		if called {
			t.Fatal("expected bulk output validation to stop before HTTP requests")
		}
		if !strings.Contains(stderr.String(), "requires --output") {
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
