package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/apiclient"
	"github.com/radiation/coyote-ci/backend/internal/cli/config"
	"github.com/radiation/coyote-ci/backend/internal/cli/credentials"
	"github.com/radiation/coyote-ci/backend/internal/versioninfo"
)

type errWriter struct{}

func (errWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func TestContextCommandsAndRemoteCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/me":
			if got := r.Header.Get("Authorization"); got != "Bearer stored-token" {
				t.Fatalf("expected bearer token, got %q", got)
			}
			_, _ = w.Write([]byte(`{"data":{"auth_mode":"oidc","auth_method":"api_token","email_verified":true,"user":{"id":"user-1","email":"dev@example.com","global_role":"admin"}}}`))
		case "/api/info":
			_, _ = w.Write([]byte(`{"data":{"version":"1.0.0","commit":"abc123","build_date":"2026-07-03T00:00:00Z","api_version":"0.1"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	configStore := config.NewStore(filepath.Join(t.TempDir(), "config.json"))
	creds := credentials.NewMemoryStore()

	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"context", "add", "local", "--server", server.URL}}); code != 0 {
		t.Fatalf("context add exit code %d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()

	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Getenv: func(key string) string {
		if key == config.EnvToken {
			return "stored-token"
		}
		return ""
	}, Args: []string{"auth", "token", "set"}}); code != 0 {
		t.Fatalf("auth token set exit code %d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()

	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"auth", "status", "--json"}}); code != 0 {
		t.Fatalf("auth status exit code %d stderr=%s", code, stderr.String())
	}
	var status map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if strings.Contains(stdout.String(), "Context:") {
		t.Fatalf("unexpected human prose in auth status JSON: %s", stdout.String())
	}
	contextData, ok := status["context"].(map[string]any)
	if !ok {
		t.Fatalf("expected context object in payload: %+v", status)
	}
	authData, ok := status["auth"].(map[string]any)
	if !ok {
		t.Fatalf("expected auth object in payload: %+v", status)
	}
	if contextData["server_url"] != server.URL || authData["source"] != "credential_store" {
		t.Fatalf("unexpected status payload: %+v", status)
	}
	if authData["authenticated"] != true {
		t.Fatalf("expected authenticated true, got %+v", authData)
	}
	if strings.Contains(stdout.String(), "stored-token") {
		t.Fatalf("auth status JSON leaked token: %s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()

	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"server", "info", "--json"}}); code != 0 {
		t.Fatalf("server info exit code %d stderr=%s", code, stderr.String())
	}
	var info map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &info); err != nil {
		t.Fatalf("decode info: %v", err)
	}
	if strings.Contains(stdout.String(), "Server:") {
		t.Fatalf("unexpected human prose in server info JSON: %s", stdout.String())
	}
	serverData, ok := info["server"].(map[string]any)
	if !ok {
		t.Fatalf("expected server object in payload: %+v", info)
	}
	if serverData["version"] != "1.0.0" {
		t.Fatalf("unexpected server payload: %+v", info)
	}
}

func TestBuildStatusAndLogsCommands(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.String() {
		case "/api/builds/build-1":
			if got := r.Header.Get("Authorization"); got != "Bearer stored-token" {
				t.Fatalf("expected bearer token, got %q", got)
			}
			_, _ = w.Write([]byte(`{"data":{"id":"build-1","build_number":12,"project_id":"project-1","project_name":"Coyote CI","job_id":"job-1","status":"failed","created_at":"2026-07-04T00:00:00Z","queued_at":"2026-07-04T00:00:01Z","started_at":"2026-07-04T00:00:02Z","finished_at":"2026-07-04T00:00:12Z","current_step_index":1,"attempt_number":1,"error_message":null,"source_ref":"refs/heads/main","source_commit_sha":"abcdef1234567890","source_author_name":"Bryan","trigger_type":"manual","trigger_kind":"manual","image":{"source_kind":"external"}}}`))
		case "/api/builds/build-1/steps":
			_, _ = w.Write([]byte(`{"data":{"build_id":"build-1","steps":[{"id":"step-1","build_id":"build-1","step_index":1,"name":"test","command":"go test ./...","status":"failed","image":{"source_kind":"external"},"job":{"id":"job-exec-1","build_id":"build-1","step_id":"step-1","name":"test","step_index":1,"attempt_number":1,"status":"failed","image":"golang:1.24","working_dir":"/workspace","command":["go","test","./..."],"command_preview":"go test ./...","environment":{},"spec_version":1,"created_at":"2026-07-04T00:00:00Z","outputs":[]},"worker_id":null,"started_at":"2026-07-04T00:00:02Z","finished_at":"2026-07-04T00:00:12Z","exit_code":1,"stdout":null,"stderr":null,"error_message":null}]}}`))
		case "/api/builds/build-1/logs?failed=true&tail=1":
			_, _ = w.Write([]byte(`{"data":{"build_id":"build-1","selected_step":{"step_index":1,"name":"test","status":"failed","exit_code":1},"logs":[{"step_index":1,"step_name":"test","timestamp":"2026-07-04T00:00:10Z","stream":"stderr","line":"FAIL\n","message":"FAIL\n"}],"truncated":true}}`))
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
	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"build", "status", "build-1", "--json"}}); code != 0 {
		t.Fatalf("build status exit code %d stderr=%s", code, stderr.String())
	}
	var statusPayload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &statusPayload); err != nil {
		t.Fatalf("decode build status: %v", err)
	}
	buildData, ok := statusPayload["build"].(map[string]any)
	if !ok {
		t.Fatalf("expected build object, got %+v", statusPayload)
	}
	if buildData["status"] != "failed" || buildData["job_name"] != "test" || buildData["duration_ms"] != float64(10000) {
		t.Fatalf("unexpected build status payload: %+v", statusPayload)
	}
	currentSteps, ok := buildData["current_steps"].([]any)
	if !ok || len(currentSteps) != 0 {
		t.Fatalf("expected empty current_steps array, got %+v", buildData["current_steps"])
	}
	failedStep, ok := statusPayload["failed_step"].(map[string]any)
	if !ok || failedStep["index"] != float64(1) || failedStep["name"] != "test" {
		t.Fatalf("unexpected failed step payload: %+v", statusPayload)
	}
	if strings.Contains(stdout.String(), "Build:") {
		t.Fatalf("unexpected human prose in build status JSON: %s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()

	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"build", "logs", "build-1", "--failed", "--tail", "1"}}); code != 0 {
		t.Fatalf("build logs exit code %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "== step 1: test (failed) ==") || !strings.Contains(stdout.String(), "[stderr] FAIL") {
		t.Fatalf("unexpected build logs output: %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "[truncated] Showing the most recent log entries.") {
		t.Fatalf("expected truncation note, got %s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()

	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"build", "logs", "build-1", "--failed", "--tail", "1", "--json"}}); code != 0 {
		t.Fatalf("build logs json exit code %d stderr=%s", code, stderr.String())
	}
	var logsPayload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &logsPayload); err != nil {
		t.Fatalf("decode build logs: %v", err)
	}
	if logsPayload["build_id"] != "build-1" || logsPayload["truncated"] != true {
		t.Fatalf("unexpected build logs payload: %+v", logsPayload)
	}
	selectedStep, ok := logsPayload["selected_step"].(map[string]any)
	if !ok || selectedStep["name"] != "test" {
		t.Fatalf("unexpected selected_step payload: %+v", logsPayload)
	}
	if strings.Contains(stdout.String(), "== step") {
		t.Fatalf("unexpected human prose in build logs JSON: %s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()

	code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"build", "logs", "build-1", "--failed", "--step", "1"}})
	if code != 2 {
		t.Fatalf("expected validation exit code 2, got %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "step and failed cannot be used together") {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected no stdout on validation error, got %q", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()

	code = Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"build", "logs", "build-1", "--tail", "0"}})
	if code != 2 {
		t.Fatalf("expected tail validation exit code 2, got %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "tail must be a positive integer") {
		t.Fatalf("unexpected stderr for tail validation: %s", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected no stdout on tail validation error, got %q", stdout.String())
	}
}

func TestBuildArtifactsCommands(t *testing.T) {
	stepsCalled := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.String() {
		case "/api/builds/build-1/artifacts":
			_, _ = w.Write([]byte(`{"data":{"build_id":"build-1","artifacts":[{"id":"artifact-1","build_id":"build-1","step_id":"step-1","name":"report.xml","path":"reports/report.xml","size_bytes":42,"content_type":"application/xml","storage_provider":"filesystem","download_url_path":"/builds/build-1/artifacts/artifact-1/download","created_at":"2026-07-05T00:00:00Z"}]}}`))
		case "/api/builds/build-1/steps":
			stepsCalled++
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":{"code":"missing_token_scope","message":"api token does not have the required scope: build:read"}}`))
		case "/api/builds/build-1/artifacts/artifact-1/download":
			_, _ = w.Write([]byte("artifact-body"))
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
	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"build", "artifacts", "build-1"}}); code != 0 {
		t.Fatalf("build artifacts exit code %d stderr=%s", code, stderr.String())
	}
	for _, want := range []string{"Artifacts for build build-1", "artifact-1", "reports/report.xml", "coyote build artifacts download build-1 --artifact artifact-1"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("expected %q in human output, got %s", want, stdout.String())
		}
	}
	if stepsCalled != 0 {
		t.Fatalf("expected artifact list to avoid build-step endpoint, got %d calls", stepsCalled)
	}
	stdout.Reset()
	stderr.Reset()

	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"build", "artifacts", "build-1", "--json"}}); code != 0 {
		t.Fatalf("build artifacts json exit code %d stderr=%s", code, stderr.String())
	}
	var listPayload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &listPayload); err != nil {
		t.Fatalf("decode build artifacts json: %v", err)
	}
	artifacts, ok := listPayload["artifacts"].([]any)
	if !ok || len(artifacts) != 1 {
		t.Fatalf("unexpected artifacts payload: %+v", listPayload)
	}
	first, ok := artifacts[0].(map[string]any)
	if !ok || first["id"] != "artifact-1" || first["step_id"] != "step-1" {
		t.Fatalf("unexpected first artifact payload: %+v", first)
	}
	if strings.Contains(stdout.String(), "Artifacts for build") {
		t.Fatalf("unexpected human prose in build artifacts JSON: %s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()

	outputFile := filepath.Join(t.TempDir(), "downloads", "report.xml")
	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"build", "artifacts", "download", "build-1", "--artifact", "artifact-1", "--output", outputFile}}); code != 0 {
		t.Fatalf("build artifacts download exit code %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Downloaded report.xml ->") {
		t.Fatalf("unexpected download human output: %s", stdout.String())
	}
	body, readErr := os.ReadFile(outputFile)
	if readErr != nil {
		t.Fatalf("read downloaded artifact: %v", readErr)
	}
	if string(body) != "artifact-body" {
		t.Fatalf("unexpected artifact file body: %q", string(body))
	}
	stdout.Reset()
	stderr.Reset()

	outputDir := t.TempDir()
	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"build", "artifacts", "download", "build-1", "--artifact", "reports/report.xml", "--output", outputDir, "--json"}}); code != 0 {
		t.Fatalf("build artifacts download json exit code %d stderr=%s", code, stderr.String())
	}
	var downloadPayload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &downloadPayload); err != nil {
		t.Fatalf("decode build artifact download json: %v", err)
	}
	downloaded, ok := downloadPayload["downloaded"].([]any)
	if !ok || len(downloaded) != 1 {
		t.Fatalf("unexpected download payload: %+v", downloadPayload)
	}
	downloadedFirst, ok := downloaded[0].(map[string]any)
	if !ok || downloadedFirst["artifact_id"] != "artifact-1" {
		t.Fatalf("unexpected downloaded entry: %+v", downloadedFirst)
	}
	if strings.Contains(stdout.String(), "Downloaded report.xml") {
		t.Fatalf("unexpected human prose in artifact download JSON: %s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()

	code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"build", "artifacts", "download", "build-1", "--artifact", "missing"}})
	if code != 2 {
		t.Fatalf("expected selector validation exit code 2, got %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "artifact \"missing\" not found") {
		t.Fatalf("unexpected selector stderr: %s", stderr.String())
	}
	stdout.Reset()
	stderr.Reset()

	code = Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"build", "artifacts", "download", "build-1", "--artifact", "artifact-1", "--output", outputFile}})
	if code == 0 {
		t.Fatal("expected overwrite protection to fail")
	}
	if !strings.Contains(stderr.String(), "already exists") {
		t.Fatalf("unexpected overwrite stderr: %s", stderr.String())
	}
}

func TestBuildArtifactsCommands_ArtifactReadOnlyScope(t *testing.T) {
	stepsCalled := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.String() {
		case "/api/builds/build-1/artifacts":
			_, _ = w.Write([]byte(`{"data":{"build_id":"build-1","artifacts":[{"id":"artifact-1","build_id":"build-1","step_id":"step-1","name":"report.xml","path":"reports/report.xml","size_bytes":42,"content_type":"application/xml","storage_provider":"filesystem","download_url_path":"/builds/build-1/artifacts/artifact-1/download","created_at":"2026-07-05T00:00:00Z"}]}}`))
		case "/api/builds/build-1/artifacts/artifact-1/download":
			_, _ = w.Write([]byte("artifact-body"))
		case "/api/builds/build-1":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":{"code":"missing_token_scope","message":"api token does not have the required scope: build:read"}}`))
		case "/api/builds/build-1/steps":
			stepsCalled++
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":{"code":"missing_token_scope","message":"api token does not have the required scope: build:read"}}`))
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
	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"build", "artifacts", "build-1", "--json"}}); code != 0 {
		t.Fatalf("artifact-read-only list exit code %d stderr=%s", code, stderr.String())
	}
	if stepsCalled != 0 {
		t.Fatalf("expected no build-step requests for artifact list, got %d", stepsCalled)
	}
	stdout.Reset()
	stderr.Reset()

	outputFile := filepath.Join(t.TempDir(), "artifact.txt")
	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"build", "artifacts", "download", "build-1", "--artifact", "artifact-1", "--output", outputFile, "--json"}}); code != 0 {
		t.Fatalf("artifact-read-only download exit code %d stderr=%s", code, stderr.String())
	}
	body, readErr := os.ReadFile(outputFile)
	if readErr != nil {
		t.Fatalf("read downloaded artifact: %v", readErr)
	}
	if string(body) != "artifact-body" {
		t.Fatalf("unexpected artifact body: %q", string(body))
	}
	stdout.Reset()
	stderr.Reset()

	code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"build", "status", "build-1", "--json"}})
	if code == 0 {
		t.Fatal("expected build status to fail without build:read")
	}
	if !strings.Contains(stderr.String(), "api token does not have the required scope: build:read") {
		t.Fatalf("unexpected status stderr: %s", stderr.String())
	}
}

func TestBuildRetryCommand(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.String() {
		case "/api/builds/build-1/rerun":
			if r.Method != http.MethodPost {
				t.Fatalf("expected POST, got %s", r.Method)
			}
			_, _ = w.Write([]byte(`{"data":{"id":"build-2","build_number":13,"project_id":"project-1","project_name":"Coyote CI","job_id":"job-1","status":"queued","created_at":"2026-07-04T00:01:00Z","queued_at":"2026-07-04T00:01:00Z","started_at":null,"finished_at":null,"current_step_index":0,"attempt_number":2,"rerun_of_build_id":"build-1","error_message":null,"trigger_type":"rerun","trigger_kind":"manual","image":{"source_kind":"external"}}}`))
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
	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, Stdin: strings.NewReader("ignored"), ConfigStore: configStore, Credentials: creds, Args: []string{"build", "retry", "build-1", "--yes"}}); code != 0 {
		t.Fatalf("build retry exit code %d stderr=%s", code, stderr.String())
	}
	if got := stdout.String(); !strings.Contains(got, "Retried build build-1 -> build-2") || !strings.Contains(got, "Status: queued") || !strings.Contains(got, "/builds/build-2") {
		t.Fatalf("unexpected retry output: %s", got)
	}
	stdout.Reset()
	stderr.Reset()

	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, Stdin: strings.NewReader("ignored"), ConfigStore: configStore, Credentials: creds, Args: []string{"build", "rerun", "build-1", "--yes", "--json"}}); code != 0 {
		t.Fatalf("build rerun json exit code %d stderr=%s", code, stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode build retry json: %v", err)
	}
	retried, ok := payload["retried"].(map[string]any)
	if !ok {
		t.Fatalf("expected retried object, got %+v", payload)
	}
	if retried["source_build_id"] != "build-1" || retried["build_id"] != "build-2" || retried["status"] != "queued" {
		t.Fatalf("unexpected retry payload: %+v", payload)
	}
	if strings.Contains(stdout.String(), "Retried build") {
		t.Fatalf("unexpected human prose in build retry JSON: %s", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
}

func TestBuildRetryCommandRequiresExplicitYesWhenNonInteractive(t *testing.T) {
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
	code := Run(Dependencies{Stdout: stdout, Stderr: stderr, Stdin: strings.NewReader(""), ConfigStore: configStore, Credentials: creds, Args: []string{"build", "retry", "build-1"}})
	if code != 2 {
		t.Fatalf("expected exit code 2, got %d stderr=%s", code, stderr.String())
	}
	if called {
		t.Fatal("expected retry command to stop before making an HTTP request")
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected no stdout, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "build retry requires --yes when stdin is not interactive") {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
	stdout.Reset()
	stderr.Reset()

	code = Run(Dependencies{Stdout: stdout, Stderr: stderr, Stdin: strings.NewReader(""), ConfigStore: configStore, Credentials: creds, Args: []string{"build", "retry", "build-1", "--json"}})
	if code != 2 {
		t.Fatalf("expected json retry exit code 2, got %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "build retry with --json requires --yes") {
		t.Fatalf("unexpected json stderr: %s", stderr.String())
	}
}

func TestBuildCommands_MissingTokenScopeErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		message := "api token does not have the required scope: build:read"
		if strings.Contains(r.URL.Path, "/logs") {
			message = "api token does not have the required scope: build:logs"
		} else if strings.Contains(r.URL.Path, "/artifacts") {
			message = "api token does not have the required scope: artifact:read"
		} else if strings.Contains(r.URL.Path, "/rerun") {
			message = "api token does not have the required scope: build:run"
		}
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":"missing_token_scope","message":"` + message + `"}}`))
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

	tests := []struct {
		name         string
		args         []string
		wantContains string
	}{
		{name: "status missing scope", args: []string{"build", "status", "build-1", "--json"}, wantContains: "api token does not have the required scope: build:read"},
		{name: "logs missing scope", args: []string{"build", "logs", "build-1", "--json"}, wantContains: "api token does not have the required scope: build:logs"},
		{name: "artifacts missing scope", args: []string{"build", "artifacts", "build-1", "--json"}, wantContains: "api token does not have the required scope: artifact:read"},
		{name: "artifact download missing scope", args: []string{"build", "artifacts", "download", "build-1", "--artifact", "artifact-1", "--json"}, wantContains: "api token does not have the required scope: artifact:read"},
		{name: "retry missing scope", args: []string{"build", "retry", "build-1", "--yes", "--json"}, wantContains: "api token does not have the required scope: build:run"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stdout := &bytes.Buffer{}
			stderr := &bytes.Buffer{}
			code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: tc.args})
			if code == 0 {
				t.Fatal("expected nonzero exit code")
			}
			if stdout.Len() != 0 {
				t.Fatalf("expected no stdout on scope failure, got %q", stdout.String())
			}
			if !strings.Contains(stderr.String(), tc.wantContains) {
				t.Fatalf("unexpected stderr: %s", stderr.String())
			}
			if strings.Contains(stderr.String(), "stored-token") {
				t.Fatalf("stderr leaked token: %s", stderr.String())
			}
		})
	}
}

func TestCanceledCommandContextCancelsHTTPRequests(t *testing.T) {
	requestSeen := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestSeen <- struct{}{}
		<-r.Context().Done()
	}))
	defer server.Close()

	store := config.NewStore(filepath.Join(t.TempDir(), "config.json"))
	if err := store.Save(config.File{
		CurrentContext: "local",
		Contexts: map[string]config.Context{
			"local": {Name: "local", ServerURL: server.URL},
		},
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	done := make(chan int, 1)
	go func() {
		done <- Run(Dependencies{Context: ctx, Stdout: stdout, Stderr: stderr, ConfigStore: store, Args: []string{"server", "info"}})
	}()

	select {
	case <-requestSeen:
	case <-time.After(2 * time.Second):
		t.Fatal("request was not started")
	}
	cancel()

	select {
	case code := <-done:
		if code != 130 {
			t.Fatalf("expected exit code 130, got %d stderr=%s", code, stderr.String())
		}
		if !strings.Contains(stderr.String(), "context canceled") {
			t.Fatalf("expected cancellation message in stderr, got %q", stderr.String())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("command did not return promptly after cancellation")
	}
}

func TestVersionJSONAndContextPrecedence(t *testing.T) {
	originalVersion := versioninfo.Version
	originalCommit := versioninfo.Commit
	originalBuildDate := versioninfo.BuildDate
	t.Cleanup(func() {
		versioninfo.Version = originalVersion
		versioninfo.Commit = originalCommit
		versioninfo.BuildDate = originalBuildDate
	})
	versioninfo.Version = "2.0.0"
	versioninfo.Commit = "deadbeef"
	versioninfo.BuildDate = "2026-07-03"

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	store := config.NewStore(filepath.Join(t.TempDir(), "config.json"))
	if err := store.Save(config.File{
		CurrentContext: "default",
		Contexts: map[string]config.Context{
			"default": {Name: "default", ServerURL: "http://localhost:8080"},
			"other":   {Name: "other", ServerURL: "https://example.com"},
		},
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: store, Args: []string{"version", "--json"}}); code != 0 {
		t.Fatalf("version exit code %d stderr=%s", code, stderr.String())
	}
	var versionPayload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &versionPayload); err != nil {
		t.Fatalf("decode version: %v", err)
	}
	cliData, ok := versionPayload["cli"].(map[string]any)
	if !ok || cliData["version"] != "2.0.0" {
		t.Fatalf("unexpected version output: %s", stdout.String())
	}
	if strings.Contains(stdout.String(), "coyote 2.0.0") {
		t.Fatalf("unexpected human prose in version JSON: %s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()

	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: store, Getenv: func(key string) string {
		if key == config.EnvContext {
			return "other"
		}
		return ""
	}, Args: []string{"context", "current", "--json"}}); code != 0 {
		t.Fatalf("context current exit code %d stderr=%s", code, stderr.String())
	}
	var currentPayload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &currentPayload); err != nil {
		t.Fatalf("decode context current: %v", err)
	}
	currentContext, ok := currentPayload["context"].(map[string]any)
	if !ok || currentContext["name"] != "other" {
		t.Fatalf("unexpected context current output: %s", stdout.String())
	}
	if strings.Contains(stdout.String(), " -> ") {
		t.Fatalf("unexpected human prose in context current JSON: %s", stdout.String())
	}
}

func TestContextListJSON(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	store := config.NewStore(filepath.Join(t.TempDir(), "config.json"))
	if err := store.Save(config.File{
		CurrentContext: "local",
		Contexts: map[string]config.Context{
			"local": {Name: "local", ServerURL: "http://localhost:8080", CredentialRef: "context:local"},
			"prod":  {Name: "prod", ServerURL: "https://example.com", CredentialRef: "context:prod"},
		},
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: store, Args: []string{"context", "list", "--json"}}); code != 0 {
		t.Fatalf("context list exit code %d stderr=%s", code, stderr.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode context list: %v", err)
	}
	if payload["current_context"] != "local" {
		t.Fatalf("unexpected current_context: %+v", payload)
	}
	contexts, ok := payload["contexts"].([]any)
	if !ok || len(contexts) != 2 {
		t.Fatalf("unexpected contexts payload: %+v", payload)
	}
	if strings.Contains(stdout.String(), "No contexts configured") || strings.Contains(stdout.String(), " -> ") {
		t.Fatalf("unexpected human prose in context list JSON: %s", stdout.String())
	}
}

func TestJSONErrorsStillGoToStderr(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	store := config.NewStore(filepath.Join(t.TempDir(), "config.json"))
	code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: store, Args: []string{"context", "current", "--json"}})
	if code != 3 {
		t.Fatalf("expected exit code 3, got %d", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected no stdout on error, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "no context is selected") {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
}

func TestAuthStatusJSONFailureReturnsNonzeroWithoutSuccessPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeHeader := http.StatusUnauthorized
		w.WriteHeader(writeHeader)
		_, _ = w.Write([]byte(`{"error":{"code":"unauthorized","message":"authentication required"}}`))
	}))
	defer server.Close()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	store := config.NewStore(filepath.Join(t.TempDir(), "config.json"))
	if err := store.Save(config.File{
		CurrentContext: "local",
		Contexts: map[string]config.Context{
			"local": {Name: "local", ServerURL: server.URL},
		},
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: store, Args: []string{"auth", "status", "--json"}})
	if code == 0 {
		t.Fatal("expected nonzero exit code")
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected no stdout on auth failure, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "authentication required") {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
}

func TestUnknownContextReturnsConfigExitCode(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	store := config.NewStore(filepath.Join(t.TempDir(), "config.json"))
	code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: store, Args: []string{"context", "use", "missing"}})
	if code != 3 {
		t.Fatalf("expected exit code 3, got %d", code)
	}
	if !strings.Contains(stderr.String(), `unknown context "missing"`) {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
}

func TestContextHumanCommandsAndAuthTokenSetFromStdin(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	creds := credentials.NewMemoryStore()
	store := config.NewStore(filepath.Join(t.TempDir(), "config.json"))
	if err := store.Save(config.File{
		CurrentContext: "local",
		Contexts: map[string]config.Context{
			"local": {Name: "local", ServerURL: "http://localhost:8080", CredentialRef: "context:local"},
			"prod":  {Name: "prod", ServerURL: "https://example.com", CredentialRef: "context:prod"},
		},
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: store, Args: []string{"context", "list"}}); code != 0 {
		t.Fatalf("context list exit code %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "* local -> http://localhost:8080") {
		t.Fatalf("unexpected context list output: %s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()

	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, Stdin: strings.NewReader("stdin-token\n"), ConfigStore: store, Credentials: creds, Args: []string{"auth", "token", "set", "--stdin"}}); code != 0 {
		t.Fatalf("auth token set exit code %d stderr=%s", code, stderr.String())
	}
	storedToken, getErr := creds.Get("context:local")
	if getErr != nil {
		t.Fatalf("get stored token: %v", getErr)
	}
	if storedToken != "stdin-token" {
		t.Fatalf("unexpected stored token: %q", storedToken)
	}
	if !strings.Contains(stdout.String(), "Stored token for context local") {
		t.Fatalf("unexpected auth token set output: %s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()

	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: store, Args: []string{"context", "use", "prod"}}); code != 0 {
		t.Fatalf("context use exit code %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Current context: prod") {
		t.Fatalf("unexpected context use output: %s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()

	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: store, Args: []string{"context", "current"}}); code != 0 {
		t.Fatalf("context current exit code %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "prod -> https://example.com") {
		t.Fatalf("unexpected context current output: %s", stdout.String())
	}
}

func TestContextListEmptyAndVersionHumanOutput(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	store := config.NewStore(filepath.Join(t.TempDir(), "config.json"))

	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: store, Args: []string{"context", "list"}}); code != 0 {
		t.Fatalf("context list exit code %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "No contexts configured") {
		t.Fatalf("unexpected empty context output: %s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()

	originalBuildDate := versioninfo.BuildDate
	t.Cleanup(func() {
		versioninfo.BuildDate = originalBuildDate
	})
	versioninfo.BuildDate = ""

	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, Args: []string{"version"}}); code != 0 {
		t.Fatalf("version exit code %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "build date: unknown") {
		t.Fatalf("unexpected version output: %s", stdout.String())
	}
}

func TestReadTokenForSetSourcesAndPromptFallback(t *testing.T) {
	t.Run("stdin", func(t *testing.T) {
		application := &app{stdin: strings.NewReader(" stdin-token \n")}
		token, source, err := application.readTokenForSet(true)
		if err != nil {
			t.Fatalf("read token from stdin: %v", err)
		}
		if token != "stdin-token" || source != "stdin" {
			t.Fatalf("unexpected stdin token result: %q %q", token, source)
		}
	})

	t.Run("environment", func(t *testing.T) {
		application := &app{getenv: func(key string) string {
			if key == config.EnvToken {
				return " env-token "
			}
			return ""
		}}
		token, source, err := application.readTokenForSet(false)
		if err != nil {
			t.Fatalf("read token from env: %v", err)
		}
		if token != "env-token" || source != "environment" {
			t.Fatalf("unexpected env token result: %q %q", token, source)
		}
	})

	t.Run("prompt", func(t *testing.T) {
		application := &app{getenv: func(string) string { return "" }, promptSecret: func(prompt string) (string, error) {
			if prompt != "API token: " {
				t.Fatalf("unexpected prompt: %q", prompt)
			}
			return " prompt-token \n", nil
		}}
		token, source, err := application.readTokenForSet(false)
		if err != nil {
			t.Fatalf("read token from prompt: %v", err)
		}
		if token != "prompt-token" || source != "prompt" {
			t.Fatalf("unexpected prompt token result: %q %q", token, source)
		}
	})

	t.Run("blank prompt token", func(t *testing.T) {
		application := &app{getenv: func(string) string { return "" }, promptSecret: func(string) (string, error) {
			return "   ", nil
		}}
		_, _, err := application.readTokenForSet(false)
		var exitErr *ExitError
		if !errors.As(err, &exitErr) || exitErr.Code != 2 {
			t.Fatalf("expected exit error code 2, got %v", err)
		}
	})
}

func TestDefaultPromptSecretAndOutputModeHelpers(t *testing.T) {
	stderr := &bytes.Buffer{}
	application := &app{stdin: strings.NewReader("secret\n"), stderr: stderr}
	secret, err := application.defaultPromptSecret("API token: ")
	if err != nil {
		t.Fatalf("defaultPromptSecret: %v", err)
	}
	if secret != "secret\n" {
		t.Fatalf("unexpected secret value: %q", secret)
	}
	if stderr.String() != "API token: " {
		t.Fatalf("unexpected prompt output: %q", stderr.String())
	}

	application.flagJSON = true
	mode, modeErr := application.resolveOutputMode(config.Context{DefaultOutput: "human"}, false)
	if modeErr != nil {
		t.Fatalf("resolve output mode with json flag: %v", modeErr)
	}
	if mode != "json" {
		t.Fatalf("expected json output mode, got %q", mode)
	}

	application.flagJSON = false
	application.flagOutput = "bad"
	_, modeErr = application.resolveOutputMode(config.Context{}, false)
	var exitErr *ExitError
	if !errors.As(modeErr, &exitErr) || exitErr.Code != 2 {
		t.Fatalf("expected invalid output exit error, got %v", modeErr)
	}

	if emptyOr("", "fallback") != "fallback" || valueOrUnknown("") != "unknown" {
		t.Fatal("unexpected helper fallback values")
	}
	if derefContext(nil).Name != "" {
		t.Fatal("expected zero value context from nil dereference")
	}
}

func TestMapCommandErrorExitCodes(t *testing.T) {
	tests := []struct {
		name string
		err  error
		code int
	}{
		{name: "context deadline", err: context.DeadlineExceeded, code: 130},
		{name: "authentication", err: &apiclient.Error{Kind: apiclient.ErrorKindAuthentication}, code: 4},
		{name: "authorization", err: &apiclient.Error{Kind: apiclient.ErrorKindAuthorization}, code: 5},
		{name: "not found", err: &apiclient.Error{Kind: apiclient.ErrorKindNotFound}, code: 6},
		{name: "conflict", err: &apiclient.Error{Kind: apiclient.ErrorKindConflict}, code: 2},
		{name: "validation", err: &apiclient.Error{Kind: apiclient.ErrorKindValidation}, code: 2},
		{name: "transport", err: &apiclient.Error{Kind: apiclient.ErrorKindTransport}, code: 8},
		{name: "server", err: &apiclient.Error{Kind: apiclient.ErrorKindServer}, code: 9},
		{name: "unexpected api", err: &apiclient.Error{Kind: apiclient.ErrorKindUnexpected}, code: 1},
		{name: "generic", err: errors.New("boom"), code: 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mapped := mapCommandError(tc.err)
			var exitErr *ExitError
			if !errors.As(mapped, &exitErr) {
				t.Fatalf("expected exit error, got %T", mapped)
			}
			if exitErr.Code != tc.code {
				t.Fatalf("expected code %d, got %d", tc.code, exitErr.Code)
			}
		})
	}

	wrapped := mapCommandError(fmt.Errorf("wrapped: %w", &apiclient.Error{Kind: apiclient.ErrorKindAuthentication}))
	var exitErr *ExitError
	if !errors.As(wrapped, &exitErr) || exitErr.Code != 4 {
		t.Fatalf("expected wrapped authentication code 4, got %v", wrapped)
	}
}

func TestRunUsesDefaultStderrAndExitErrorFallbacks(t *testing.T) {
	code := Run(Dependencies{Stdout: io.Discard, ConfigStore: config.NewStore(filepath.Join(t.TempDir(), "config.json")), Args: []string{"context", "current"}})
	if code != 3 {
		t.Fatalf("expected exit code 3, got %d", code)
	}

	if (&ExitError{}).Error() != "" {
		t.Fatal("expected empty error string for nil inner error")
	}
	if (&ExitError{}).Unwrap() != nil {
		t.Fatal("expected nil unwrap for nil inner error")
	}
	inner := errors.New("boom")
	if !errors.Is((&ExitError{Code: 1, Err: inner}).Unwrap(), inner) {
		t.Fatal("expected unwrap to expose inner error")
	}
}

func TestAuthStatusAndServerInfoHumanOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/me":
			_, _ = w.Write([]byte(`{"data":{"auth_mode":"header","auth_method":"","email_verified":true,"user":{"id":"user-2","email":"person@example.com","global_role":"user"}}}`))
		case "/api/info":
			_, _ = w.Write([]byte(`{"data":{"version":"1.2.3","commit":"","build_date":"","api_version":"0.2"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	store := config.NewStore(filepath.Join(t.TempDir(), "config.json"))
	if err := store.Save(config.File{
		CurrentContext: "local",
		Contexts: map[string]config.Context{
			"local": {Name: "local", ServerURL: server.URL, DefaultOutput: "human"},
		},
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: store, Args: []string{"auth", "status"}}); code != 0 {
		t.Fatalf("auth status exit code %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Context: local") || !strings.Contains(stdout.String(), "Auth source: none") || !strings.Contains(stdout.String(), "Auth method: none") {
		t.Fatalf("unexpected auth status output: %s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()

	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: store, Args: []string{"server", "info"}}); code != 0 {
		t.Fatalf("server info exit code %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Server: "+server.URL) || !strings.Contains(stdout.String(), "Commit: unknown") || !strings.Contains(stdout.String(), "Build date: unknown") {
		t.Fatalf("unexpected server info output: %s", stdout.String())
	}
}

func TestResolveTargetAndDefaultPromptSecretEdges(t *testing.T) {
	creds := credentials.NewMemoryStore()
	store := config.NewStore(filepath.Join(t.TempDir(), "config.json"))
	if err := store.Save(config.File{
		CurrentContext: "local",
		Contexts: map[string]config.Context{
			"local": {Name: "local", ServerURL: "https://stored.example.com", CredentialRef: "context:local", DefaultOutput: "json"},
		},
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	if setErr := creds.Set("context:local", "stored-token"); setErr != nil {
		t.Fatalf("set credential: %v", setErr)
	}

	application := &app{
		configStore: store,
		credentials: creds,
		getenv: func(key string) string {
			switch key {
			case config.EnvServer:
				return " https://override.example.com/api/ "
			case config.EnvToken:
				return " env-token "
			default:
				return ""
			}
		},
	}
	resolved, err := application.resolveTarget()
	if err != nil {
		t.Fatalf("resolve target: %v", err)
	}
	if resolved.ServerURL != "https://override.example.com/api" || resolved.Token != "env-token" || resolved.AuthSource != "environment" || resolved.OutputMode != "json" {
		t.Fatalf("unexpected resolved target: %+v", resolved)
	}

	missingApplication := &app{configStore: store, credentials: creds, getenv: func(string) string { return "" }, flagContext: "missing"}
	_, err = missingApplication.resolveTarget()
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 3 {
		t.Fatalf("expected missing context exit error, got %v", err)
	}

	promptApp := &app{stdin: strings.NewReader("secret-without-newline"), stderr: &bytes.Buffer{}}
	secret, promptErr := promptApp.defaultPromptSecret("API token: ")
	if promptErr != nil {
		t.Fatalf("defaultPromptSecret with EOF: %v", promptErr)
	}
	if secret != "secret-without-newline" {
		t.Fatalf("unexpected EOF prompt secret: %q", secret)
	}

	writeFailApp := &app{stdin: strings.NewReader("ignored"), stderr: errWriter{}}
	_, promptErr = writeFailApp.defaultPromptSecret("API token: ")
	if promptErr == nil || !strings.Contains(promptErr.Error(), "write failed") {
		t.Fatalf("expected prompt write failure, got %v", promptErr)
	}

	if (*ExitError)(nil).Unwrap() != nil {
		t.Fatal("expected nil unwrap for nil receiver")
	}
}

func TestAuthTokenSetAddsDefaultCredentialRefAndUsesPrompt(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	creds := credentials.NewMemoryStore()
	store := config.NewStore(filepath.Join(t.TempDir(), "config.json"))
	if err := store.Save(config.File{
		CurrentContext: "local",
		Contexts: map[string]config.Context{
			"local": {Name: "local", ServerURL: "https://example.com"},
		},
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	if code := Run(Dependencies{
		Stdout:      stdout,
		Stderr:      stderr,
		ConfigStore: store,
		Credentials: creds,
		Getenv:      func(string) string { return "" },
		PromptSecret: func(prompt string) (string, error) {
			if prompt != "API token: " {
				t.Fatalf("unexpected prompt: %q", prompt)
			}
			return "prompted-token", nil
		},
		Args: []string{"auth", "token", "set"},
	}); code != 0 {
		t.Fatalf("auth token set exit code %d stderr=%s", code, stderr.String())
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if loaded.Contexts["local"].CredentialRef != "context:local" {
		t.Fatalf("expected default credential ref, got %+v", loaded.Contexts["local"])
	}
	storedToken, getErr := creds.Get("context:local")
	if getErr != nil {
		t.Fatalf("get stored token: %v", getErr)
	}
	if storedToken != "prompted-token" {
		t.Fatalf("unexpected stored token: %q", storedToken)
	}
}
