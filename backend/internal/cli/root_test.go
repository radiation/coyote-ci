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
	"sync/atomic"
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

func TestBuildStatusCommandJSONIncludesSCMStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.String() {
		case "/api/builds/build-2":
			_, _ = w.Write([]byte(`{"data":{"id":"build-2","build_number":13,"project_id":"project-1","project_name":"Coyote CI","job_id":"job-1","job_name":"test","status":"failed","created_at":"2026-07-04T00:00:00Z","started_at":"2026-07-04T00:00:02Z","finished_at":"2026-07-04T00:00:12Z","current_step_index":1,"attempt_number":2,"source_ref":"refs/heads/main","source_commit_sha":"abcdef1234567890","trigger_type":"manual","trigger_kind":"manual","scm_status":{"reportable":true,"configured":true,"provider":"github","repository_owner":"octo","repository_name":"repo","commit_sha":"abcdef1234567890","context":"coyote/payments/job-1","desired_state":"failure","last_sent_state":"pending","delivery_state":"retry_waiting","attempts":2,"next_attempt_at":"2026-07-17T14:30:00Z","last_error":"GitHub rate limit exceeded"},"image":{"source_kind":"external"}}}`))
		case "/api/builds/build-2/steps":
			_, _ = w.Write([]byte(`{"data":{"build_id":"build-2","steps":[{"id":"step-1","build_id":"build-2","step_index":1,"name":"test","command":"go test ./...","status":"failed","image":{"source_kind":"external"},"job":{"id":"job-exec-1","build_id":"build-2","step_id":"step-1","name":"test","step_index":1,"attempt_number":1,"status":"failed","image":"golang:1.24","working_dir":"/workspace","command":["go","test","./..."],"command_preview":"go test ./...","environment":{},"spec_version":1,"created_at":"2026-07-04T00:00:00Z","outputs":[]},"started_at":"2026-07-04T00:00:02Z","finished_at":"2026-07-04T00:00:12Z","exit_code":1}]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	configStore := config.NewStore(filepath.Join(t.TempDir(), "config.json"))
	if err := configStore.Save(config.File{CurrentContext: "local", Contexts: map[string]config.Context{"local": {Name: "local", ServerURL: server.URL, CredentialRef: "context:local"}}}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	creds := credentials.NewMemoryStore()
	if setErr := creds.Set("context:local", "stored-token"); setErr != nil {
		t.Fatalf("set token: %v", setErr)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"build", "status", "build-2", "--json"}}); code != 0 {
		t.Fatalf("build status exit code %d stderr=%s", code, stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode build status: %v", err)
	}
	buildData, ok := payload["build"].(map[string]any)
	if !ok {
		t.Fatalf("expected build object, got %+v", payload)
	}
	scmData, ok := buildData["scm_status"].(map[string]any)
	if !ok {
		t.Fatalf("expected scm_status object, got %+v", buildData["scm_status"])
	}
	if scmData["provider"] != "github" || scmData["repository_owner"] != "octo" || scmData["delivery_state"] != "retry_waiting" || scmData["last_sent_state"] != "pending" {
		t.Fatalf("unexpected scm status payload: %+v", scmData)
	}
	if scmData["attempts"] != float64(2) || scmData["last_error"] != "GitHub rate limit exceeded" || scmData["next_attempt_at"] != "2026-07-17T14:30:00Z" {
		t.Fatalf("unexpected scm retry payload: %+v", scmData)
	}
	if strings.Contains(stdout.String(), "SCM status") {
		t.Fatalf("unexpected human prose in build status JSON: %s", stdout.String())
	}
}

func TestBuildWatchCommandHuman(t *testing.T) {
	originalPollInterval := buildWatchPollInterval
	buildWatchPollInterval = 5 * time.Millisecond
	t.Cleanup(func() {
		buildWatchPollInterval = originalPollInterval
	})

	var buildCalls int32
	var stepsCalls int32
	var streamHit int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.String() {
		case "/api/builds/build-1":
			call := atomic.AddInt32(&buildCalls, 1)
			if call == 1 || atomic.LoadInt32(&streamHit) == 0 {
				_, _ = w.Write([]byte(`{"data":{"id":"build-1","project_id":"project-1","project_name":"Coyote CI","job_id":"job-1","job_name":"watch-job","status":"running","created_at":"2026-07-07T00:00:00Z","started_at":"2026-07-07T00:00:01Z","finished_at":null,"current_step_index":0,"attempt_number":1,"error_message":null,"trigger_type":"manual","trigger_kind":"manual","image":{"source_kind":"external"},"current_steps":[{"id":"step-1","index":0,"name":"test","status":"running","started_at":"2026-07-07T00:00:01Z"}]}}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":{"id":"build-1","project_id":"project-1","project_name":"Coyote CI","job_id":"job-1","job_name":"watch-job","status":"success","created_at":"2026-07-07T00:00:00Z","started_at":"2026-07-07T00:00:01Z","finished_at":"2026-07-07T00:00:03Z","current_step_index":0,"attempt_number":1,"error_message":null,"trigger_type":"manual","trigger_kind":"manual","image":{"source_kind":"external"},"current_steps":[]}}`))
		case "/api/builds/build-1/steps":
			call := atomic.AddInt32(&stepsCalls, 1)
			if call == 1 || atomic.LoadInt32(&streamHit) == 0 {
				_, _ = w.Write([]byte(`{"data":{"build_id":"build-1","steps":[{"id":"step-1","build_id":"build-1","step_index":0,"name":"test","command":"go test ./...","status":"running","image":{"source_kind":"external"},"job":null,"worker_id":null,"started_at":"2026-07-07T00:00:01Z","finished_at":null,"exit_code":null,"stdout":null,"stderr":null,"error_message":null}]}}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":{"build_id":"build-1","steps":[{"id":"step-1","build_id":"build-1","step_index":0,"name":"test","command":"go test ./...","status":"success","image":{"source_kind":"external"},"job":null,"worker_id":null,"started_at":"2026-07-07T00:00:01Z","finished_at":"2026-07-07T00:00:03Z","exit_code":0,"stdout":null,"stderr":null,"error_message":null}]}}`))
		case "/api/builds/build-1/steps/0/logs/stream":
			atomic.StoreInt32(&streamHit, 1)
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "event: chunk\n")
			_, _ = io.WriteString(w, "data: {\"sequence_no\":1,\"build_id\":\"build-1\",\"step_id\":\"step-1\",\"step_index\":0,\"step_name\":\"test\",\"stream\":\"stdout\",\"chunk_text\":\"ok\\n\",\"created_at\":\"2026-07-07T00:00:02Z\"}\n\n")
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
	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"build", "watch", "build-1"}}); code != 0 {
		t.Fatalf("build watch exit code %d stderr=%s", code, stderr.String())
	}
	for _, want := range []string{
		"Build build-1: running",
		"==> step 0: test started",
		"[step 0 test] ok",
		"<== step 0: test success (exit code 0)",
		"Build build-1 completed with status success (exit 0)",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("expected %q in output, got %s", want, stdout.String())
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
	if atomic.LoadInt32(&buildCalls) < 2 || atomic.LoadInt32(&stepsCalls) < 2 {
		t.Fatalf("expected repeated polling, got buildCalls=%d stepsCalls=%d", atomic.LoadInt32(&buildCalls), atomic.LoadInt32(&stepsCalls))
	}
}

func TestBuildWatchCommandJSONLogsUnavailable(t *testing.T) {
	originalPollInterval := buildWatchPollInterval
	buildWatchPollInterval = 5 * time.Millisecond
	t.Cleanup(func() {
		buildWatchPollInterval = originalPollInterval
	})

	var buildCalls int32
	var stepsCalls int32
	var streamAttempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.String() {
		case "/api/builds/build-1":
			call := atomic.AddInt32(&buildCalls, 1)
			if call == 1 || atomic.LoadInt32(&streamAttempts) == 0 {
				_, _ = w.Write([]byte(`{"data":{"id":"build-1","project_id":"project-1","project_name":"Coyote CI","job_id":"job-1","job_name":"watch-job","status":"running","created_at":"2026-07-07T00:00:00Z","started_at":"2026-07-07T00:00:01Z","finished_at":null,"current_step_index":0,"attempt_number":1,"error_message":null,"trigger_type":"manual","trigger_kind":"manual","image":{"source_kind":"external"},"current_steps":[{"id":"step-1","index":0,"name":"test","status":"running","started_at":"2026-07-07T00:00:01Z"}]}}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":{"id":"build-1","project_id":"project-1","project_name":"Coyote CI","job_id":"job-1","job_name":"watch-job","status":"failed","created_at":"2026-07-07T00:00:00Z","started_at":"2026-07-07T00:00:01Z","finished_at":"2026-07-07T00:00:03Z","current_step_index":0,"attempt_number":1,"error_message":"boom","trigger_type":"manual","trigger_kind":"manual","image":{"source_kind":"external"},"current_steps":[]}}`))
		case "/api/builds/build-1/steps":
			call := atomic.AddInt32(&stepsCalls, 1)
			if call == 1 || atomic.LoadInt32(&streamAttempts) == 0 {
				_, _ = w.Write([]byte(`{"data":{"build_id":"build-1","steps":[{"id":"step-1","build_id":"build-1","step_index":0,"name":"test","command":"go test ./...","status":"running","image":{"source_kind":"external"},"job":null,"worker_id":null,"started_at":"2026-07-07T00:00:01Z","finished_at":null,"exit_code":null,"stdout":null,"stderr":null,"error_message":null}]}}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":{"build_id":"build-1","steps":[{"id":"step-1","build_id":"build-1","step_index":0,"name":"test","command":"go test ./...","status":"failed","image":{"source_kind":"external"},"job":null,"worker_id":null,"started_at":"2026-07-07T00:00:01Z","finished_at":"2026-07-07T00:00:03Z","exit_code":1,"stdout":null,"stderr":null,"error_message":"boom"}]}}`))
		case "/api/builds/build-1/steps/0/logs/stream":
			atomic.AddInt32(&streamAttempts, 1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":{"code":"missing_token_scope","message":"api token does not have the required scope: build:logs"}}`))
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
	code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"build", "watch", "build-1", "--json"}})
	if code != 1 {
		t.Fatalf("expected failed build watch exit code 1, got %d stderr=%s", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) < 5 {
		t.Fatalf("expected multiple ndjson events, got %q", stdout.String())
	}
	seenTypes := map[string]buildWatchEvent{}
	typeCounts := map[string]int{}
	for _, line := range lines {
		var event buildWatchEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("decode ndjson line %q: %v", line, err)
		}
		if event.Type == "" || event.BuildID != "build-1" || strings.TrimSpace(event.Timestamp) == "" {
			t.Fatalf("event missing required fields: %+v", event)
		}
		typeCounts[event.Type]++
		seenTypes[event.Type] = event
	}
	for _, wantType := range []string{"build_status", "step_started", "logs_unavailable", "step_finished", "terminal"} {
		if _, ok := seenTypes[wantType]; !ok {
			t.Fatalf("expected event type %q, got %+v", wantType, seenTypes)
		}
	}
	if typeCounts["logs_unavailable"] != 1 {
		t.Fatalf("expected logs_unavailable once, got counts=%+v", typeCounts)
	}
	terminal := seenTypes["terminal"]
	if terminal.Status != "failed" || terminal.ExitCode == nil || *terminal.ExitCode != 1 {
		t.Fatalf("unexpected terminal event: %+v", terminal)
	}
	if seenTypes["logs_unavailable"].StepIndex != nil {
		t.Fatalf("logs_unavailable should not be step scoped: %+v", seenTypes["logs_unavailable"])
	}
	stepStarted := seenTypes["step_started"]
	if stepStarted.StepIndex == nil || *stepStarted.StepIndex != 0 || stepStarted.StepName == nil || *stepStarted.StepName != "test" || stepStarted.StepID == nil || *stepStarted.StepID != "step-1" {
		t.Fatalf("unexpected step_started event: %+v", stepStarted)
	}
}

func TestBuildWatchCommandStreamFailureSurfaces(t *testing.T) {
	originalPollInterval := buildWatchPollInterval
	buildWatchPollInterval = 5 * time.Millisecond
	t.Cleanup(func() {
		buildWatchPollInterval = originalPollInterval
	})

	var streamAttempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.String() {
		case "/api/builds/build-1":
			_, _ = w.Write([]byte(`{"data":{"id":"build-1","project_id":"project-1","project_name":"Coyote CI","job_id":"job-1","job_name":"watch-job","status":"running","created_at":"2026-07-07T00:00:00Z","started_at":"2026-07-07T00:00:01Z","finished_at":null,"current_step_index":0,"attempt_number":1,"error_message":null,"trigger_type":"manual","trigger_kind":"manual","image":{"source_kind":"external"},"current_steps":[{"id":"step-1","index":0,"name":"test","status":"running","started_at":"2026-07-07T00:00:01Z"}]}}`))
		case "/api/builds/build-1/steps":
			_, _ = w.Write([]byte(`{"data":{"build_id":"build-1","steps":[{"id":"step-1","build_id":"build-1","step_index":0,"name":"test","command":"go test ./...","status":"running","image":{"source_kind":"external"},"job":null,"worker_id":null,"started_at":"2026-07-07T00:00:01Z","finished_at":null,"exit_code":null,"stdout":null,"stderr":null,"error_message":null}]}}`))
		case "/api/builds/build-1/steps/0/logs/stream":
			atomic.AddInt32(&streamAttempts, 1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"error":{"code":"bad_gateway","message":"upstream stream failed"}}`))
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
	code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"build", "watch", "build-1"}})
	if code != 9 {
		t.Fatalf("expected server error exit code 9, got %d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	if atomic.LoadInt32(&streamAttempts) == 0 {
		t.Fatal("expected log stream request before failure")
	}
	if !strings.Contains(stderr.String(), "upstream stream failed") {
		t.Fatalf("expected surfaced stream error, got %q", stderr.String())
	}
	if strings.Contains(stdout.String(), "Live logs unavailable") {
		t.Fatalf("did not expect logs_unavailable output for non-scope failure: %q", stdout.String())
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
	if downloadedFirst["artifact_path"] != "reports/report.xml" {
		t.Fatalf("expected artifact_path reports/report.xml, got %+v", downloadedFirst)
	}
	if downloadedFirst["step_id"] != "step-1" {
		t.Fatalf("expected step_id step-1, got %+v", downloadedFirst)
	}
	if downloadedFirst["content_type"] != "application/xml" {
		t.Fatalf("expected content_type application/xml, got %+v", downloadedFirst)
	}
	if downloadedFirst["size_bytes"] != float64(42) {
		t.Fatalf("expected size_bytes 42, got %+v", downloadedFirst)
	}
	if downloadedFirst["local_path"] != filepath.Join(outputDir, "report.xml") {
		t.Fatalf("expected local_path to match output file, got %+v", downloadedFirst)
	}
	if downloadedFirst["path"] != filepath.Join(outputDir, "report.xml") {
		t.Fatalf("expected legacy path alias to match local_path, got %+v", downloadedFirst)
	}
	if downloadedFirst["downloaded_bytes"] != float64(len("artifact-body")) {
		t.Fatalf("expected downloaded_bytes %d, got %+v", len("artifact-body"), downloadedFirst)
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

func TestBuildArtifactsBulkDownloadCommand(t *testing.T) {
	stepsCalled := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.String() {
		case "/api/builds/build-1/artifacts":
			_, _ = w.Write([]byte(`{"data":{"build_id":"build-1","artifacts":[{"id":"artifact-1","build_id":"build-1","step_id":"step-1","name":"report.xml","path":"reports/report.xml","size_bytes":42,"content_type":"application/xml","storage_provider":"filesystem","download_url_path":"/builds/build-1/artifacts/artifact-1/download","created_at":"2026-07-05T00:00:00Z"},{"id":"artifact-2","build_id":"build-1","step_id":"step-2","name":"summary.txt","path":"logs/summary.txt","size_bytes":13,"content_type":"text/plain","storage_provider":"filesystem","download_url_path":"/builds/build-1/artifacts/artifact-2/download","created_at":"2026-07-05T00:00:01Z"}]}}`))
		case "/api/builds/build-1/steps":
			stepsCalled++
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":{"code":"missing_token_scope","message":"api token does not have the required scope: build:read"}}`))
		case "/api/builds/build-1/artifacts/artifact-1/download":
			_, _ = w.Write([]byte("artifact-body-1"))
		case "/api/builds/build-1/artifacts/artifact-2/download":
			_, _ = w.Write([]byte("artifact-body-2"))
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
	outputDir := filepath.Join(t.TempDir(), "downloads")
	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"build", "artifacts", "download", "build-1", "--all", "--output", outputDir, "--json"}}); code != 0 {
		t.Fatalf("bulk artifact download json exit code %d stderr=%s", code, stderr.String())
	}
	if stepsCalled != 0 {
		t.Fatalf("expected bulk artifact download to avoid build-step endpoint, got %d calls", stepsCalled)
	}
	firstPath := filepath.Join(outputDir, "reports", "report.xml")
	secondPath := filepath.Join(outputDir, "logs", "summary.txt")
	firstBody, readErr := os.ReadFile(firstPath)
	if readErr != nil {
		t.Fatalf("read first bulk artifact: %v", readErr)
	}
	if string(firstBody) != "artifact-body-1" {
		t.Fatalf("unexpected first bulk artifact body: %q", string(firstBody))
	}
	secondBody, readErr := os.ReadFile(secondPath)
	if readErr != nil {
		t.Fatalf("read second bulk artifact: %v", readErr)
	}
	if string(secondBody) != "artifact-body-2" {
		t.Fatalf("unexpected second bulk artifact body: %q", string(secondBody))
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode bulk download json: %v", err)
	}
	downloaded, ok := payload["downloaded"].([]any)
	if !ok || len(downloaded) != 2 {
		t.Fatalf("unexpected bulk download payload: %+v", payload)
	}
	first, ok := downloaded[0].(map[string]any)
	if !ok || first["artifact_id"] != "artifact-1" || first["local_path"] != firstPath || first["artifact_path"] != "reports/report.xml" {
		t.Fatalf("unexpected first bulk downloaded entry: %+v", first)
	}
	second, ok := downloaded[1].(map[string]any)
	if !ok || second["artifact_id"] != "artifact-2" || second["local_path"] != secondPath || second["artifact_path"] != "logs/summary.txt" {
		t.Fatalf("unexpected second bulk downloaded entry: %+v", second)
	}
	if strings.Contains(stdout.String(), "Downloaded report.xml") {
		t.Fatalf("unexpected human prose in bulk artifact JSON: %s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"build", "artifacts", "download", "build-1", "--all", "--output", outputDir}}); code != 2 {
		t.Fatalf("expected overwrite protection exit code 2, got %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "already exists") {
		t.Fatalf("unexpected bulk overwrite stderr: %s", stderr.String())
	}
}

func TestBuildArtifactsBulkDownloadCommand_NoArtifacts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.String() {
		case "/api/builds/build-1/artifacts":
			_, _ = w.Write([]byte(`{"data":{"build_id":"build-1","artifacts":[]}}`))
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
	outputDir := filepath.Join(t.TempDir(), "downloads")
	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"build", "artifacts", "download", "build-1", "--all", "--output", outputDir}}); code != 0 {
		t.Fatalf("bulk empty artifact download exit code %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "No artifacts found for build build-1") {
		t.Fatalf("unexpected empty bulk human output: %s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"build", "artifacts", "download", "build-1", "--all", "--output", outputDir, "--json"}}); code != 0 {
		t.Fatalf("bulk empty artifact download json exit code %d stderr=%s", code, stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode empty bulk download json: %v", err)
	}
	downloaded, ok := payload["downloaded"].([]any)
	if !ok || len(downloaded) != 0 {
		t.Fatalf("unexpected empty bulk download payload: %+v", payload)
	}
}

func TestBuildArtifactTriggersCommand(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.String() {
		case "/api/builds/build-1/artifact-triggers":
			_, _ = w.Write([]byte(`{"data":{"build_id":"build-1","build_trigger_kind":"manual","recursive_dispatch_blocked":false,"summary":{"delivery_count":2,"queued_count":1,"failed_count":1},"deliveries":[{"delivery_id":"delivery-1","status":"queued","created_at":"2026-07-05T00:00:01Z","updated_at":"2026-07-05T00:00:02Z","producer_build_id":"build-1","producer_project_id":"project-1","producer_job_id":"job-upstream","artifact_id":"artifact-1","artifact_path":"reports/report.xml","artifact_name":"report.xml","artifact_size_bytes":42,"consumer_job_id":"job-deploy","consumer_job_name":"deploy","downstream_build_id":"build-2"},{"delivery_id":"delivery-2","status":"failed","created_at":"2026-07-05T00:00:03Z","updated_at":"2026-07-05T00:00:04Z","producer_build_id":"build-1","producer_project_id":"project-1","producer_job_id":"job-upstream","artifact_id":"artifact-2","artifact_path":"docs/summary.txt","consumer_job_id":"job-docs","error_message":"queue failed"}]}}`))
		case "/api/artifact-trigger-deliveries/delivery-2/retry":
			_, _ = w.Write([]byte(`{"data":{"result":"retried","message":"queued downstream build","delivery":{"delivery_id":"delivery-2","status":"queued","created_at":"2026-07-05T00:00:03Z","updated_at":"2026-07-05T00:00:05Z","producer_build_id":"build-1","producer_project_id":"project-1","producer_job_id":"job-upstream","artifact_id":"artifact-2","artifact_path":"docs/summary.txt","consumer_job_id":"job-docs","downstream_build_id":"build-3"}}}`))
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
	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"build", "artifact-triggers", "build-1"}}); code != 0 {
		t.Fatalf("build artifact-triggers exit code %d stderr=%s", code, stderr.String())
	}
	for _, want := range []string{"Artifact trigger deliveries for build build-1", "Summary: 2 deliveries, 1 queued, 1 failed", "delivery-1", "delivery-2", "report.xml", "deploy", "build-2", "queue failed"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("expected %q in human output, got %s", want, stdout.String())
		}
	}
	stdout.Reset()
	stderr.Reset()

	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"build", "artifact-triggers", "build-1", "--json"}}); code != 0 {
		t.Fatalf("build artifact-triggers json exit code %d stderr=%s", code, stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode build artifact triggers json: %v", err)
	}
	if payload["build_id"] != "build-1" || payload["build_trigger_kind"] != "manual" || payload["recursive_dispatch_blocked"] != false {
		t.Fatalf("unexpected artifact trigger payload: %+v", payload)
	}
	summary, ok := payload["summary"].(map[string]any)
	if !ok || summary["delivery_count"] != float64(2) || summary["queued_count"] != float64(1) || summary["failed_count"] != float64(1) {
		t.Fatalf("unexpected artifact trigger summary: %+v", payload)
	}
	deliveries, ok := payload["deliveries"].([]any)
	if !ok || len(deliveries) != 2 {
		t.Fatalf("unexpected artifact trigger deliveries: %+v", payload)
	}
	first, ok := deliveries[0].(map[string]any)
	if !ok || first["consumer_job_name"] != "deploy" || first["artifact_name"] != "report.xml" {
		t.Fatalf("unexpected first artifact trigger delivery: %+v", first)
	}
	if strings.Contains(stdout.String(), "Artifact trigger deliveries for build") {
		t.Fatalf("unexpected human prose in artifact trigger JSON: %s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()

	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"build", "artifact-triggers", "retry", "delivery-2"}}); code != 0 {
		t.Fatalf("build artifact-triggers retry exit code %d stderr=%s", code, stderr.String())
	}
	if got := stdout.String(); !strings.Contains(got, "Retried artifact-trigger delivery delivery-2 -> build-3") || !strings.Contains(got, "Status: queued") {
		t.Fatalf("unexpected artifact trigger retry output: %s", got)
	}
	stdout.Reset()
	stderr.Reset()

	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"build", "artifact-triggers", "retry", "delivery-2", "--json"}}); code != 0 {
		t.Fatalf("build artifact-triggers retry json exit code %d stderr=%s", code, stderr.String())
	}
	payload = map[string]any{}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode build artifact trigger retry json: %v", err)
	}
	if payload["result"] != "retried" {
		t.Fatalf("unexpected artifact trigger retry payload: %+v", payload)
	}
}

func TestBuildArtifactTriggersCommand_EmptyStates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.String() {
		case "/api/builds/build-blocked/artifact-triggers":
			_, _ = w.Write([]byte(`{"data":{"build_id":"build-blocked","build_trigger_kind":"artifact","recursive_dispatch_blocked":true,"summary":{"delivery_count":0,"queued_count":0,"failed_count":0},"deliveries":[]}}`))
		case "/api/builds/build-empty/artifact-triggers":
			_, _ = w.Write([]byte(`{"data":{"build_id":"build-empty","build_trigger_kind":"manual","recursive_dispatch_blocked":false,"summary":{"delivery_count":0,"queued_count":0,"failed_count":0},"deliveries":[]}}`))
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
	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"build", "artifact-triggers", "build-blocked"}}); code != 0 {
		t.Fatalf("build artifact-triggers blocked exit code %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Recursive artifact-trigger dispatch is blocked for artifact-triggered builds.") {
		t.Fatalf("unexpected blocked output: %s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()

	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"build", "artifact-triggers", "build-empty"}}); code != 0 {
		t.Fatalf("build artifact-triggers empty exit code %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "No artifact-trigger deliveries were recorded for this build.") {
		t.Fatalf("unexpected empty output: %s", stdout.String())
	}
}

func TestBuildArtifactTriggersCommand_MissingScope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.String() {
		case "/api/builds/build-1/artifact-triggers":
			w.Header().Set("Content-Type", "application/json")
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
	code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"build", "artifact-triggers", "build-1"}})
	if code != 5 {
		t.Fatalf("expected missing scope exit code 5, got %d stderr=%s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected no stdout on missing scope, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "build:read") {
		t.Fatalf("expected build:read scope error in stderr, got %s", stderr.String())
	}
}

func TestBuildArtifactsBulkDownloadMidFailureDoesNotEmitPartialJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.String() {
		case "/api/builds/build-1/artifacts":
			_, _ = w.Write([]byte(`{"data":{"build_id":"build-1","artifacts":[{"id":"artifact-1","build_id":"build-1","name":"report.xml","path":"reports/report.xml","size_bytes":42,"storage_provider":"filesystem","download_url_path":"/builds/build-1/artifacts/artifact-1/download","created_at":"2026-07-05T00:00:00Z"},{"id":"artifact-2","build_id":"build-1","name":"summary.txt","path":"logs/summary.txt","size_bytes":13,"storage_provider":"filesystem","download_url_path":"/builds/build-1/artifacts/artifact-2/download","created_at":"2026-07-05T00:00:01Z"}]}}`))
		case "/api/builds/build-1/artifacts/artifact-1/download":
			_, _ = w.Write([]byte("artifact-body-1"))
		case "/api/builds/build-1/artifacts/artifact-2/download":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":{"code":"missing_token_scope","message":"api token does not have the required scope: artifact:read"}}`))
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
	outputDir := filepath.Join(t.TempDir(), "downloads")
	code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"build", "artifacts", "download", "build-1", "--all", "--output", outputDir, "--json"}})
	if code != 5 {
		t.Fatalf("expected mid-download authorization exit code 5, got %d stderr=%s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected no stdout for mid-download failure, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "artifact:read") {
		t.Fatalf("unexpected mid-download stderr: %s", stderr.String())
	}
	firstPath := filepath.Join(outputDir, "reports", "report.xml")
	firstBody, readErr := os.ReadFile(firstPath)
	if readErr != nil {
		t.Fatalf("read first bulk artifact after failure: %v", readErr)
	}
	if string(firstBody) != "artifact-body-1" {
		t.Fatalf("unexpected first bulk artifact body after failure: %q", string(firstBody))
	}
	secondPath := filepath.Join(outputDir, "logs", "summary.txt")
	if _, statErr := os.Stat(secondPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected second bulk artifact to be absent, got %v", statErr)
	}
}

func TestBuildArtifactsBulkDownloadLocalFailureDoesNotEmitPartialJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.String() {
		case "/api/builds/build-1/artifacts":
			_, _ = w.Write([]byte(`{"data":{"build_id":"build-1","artifacts":[{"id":"artifact-1","build_id":"build-1","name":"report.xml","path":"reports/report.xml","size_bytes":42,"storage_provider":"filesystem","download_url_path":"/builds/build-1/artifacts/artifact-1/download","created_at":"2026-07-05T00:00:00Z"},{"id":"artifact-2","build_id":"build-1","name":"summary.txt","path":"logs/summary.txt","size_bytes":13,"storage_provider":"filesystem","download_url_path":"/builds/build-1/artifacts/artifact-2/download","created_at":"2026-07-05T00:00:01Z"}]}}`))
		case "/api/builds/build-1/artifacts/artifact-1/download":
			_, _ = w.Write([]byte("artifact-body-1"))
		case "/api/builds/build-1/artifacts/artifact-2/download":
			_, _ = w.Write([]byte("artifact-body-2"))
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

	originalReplace := replaceFileAtomicFunc
	replaceCalls := 0
	t.Cleanup(func() {
		replaceFileAtomicFunc = originalReplace
	})
	replaceFileAtomicFunc = func(source string, destination string) error {
		replaceCalls++
		if replaceCalls == 2 {
			return errors.New("replace failed")
		}
		return originalReplace(source, destination)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	outputDir := filepath.Join(t.TempDir(), "downloads")
	code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"build", "artifacts", "download", "build-1", "--all", "--output", outputDir, "--json"}})
	if code != 1 {
		t.Fatalf("expected local mid-download failure exit code 1, got %d stderr=%s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected no stdout for local mid-download failure, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "replace failed") {
		t.Fatalf("unexpected local mid-download stderr: %s", stderr.String())
	}
	firstPath := filepath.Join(outputDir, "reports", "report.xml")
	firstBody, readErr := os.ReadFile(firstPath)
	if readErr != nil {
		t.Fatalf("read first bulk artifact after local failure: %v", readErr)
	}
	if string(firstBody) != "artifact-body-1" {
		t.Fatalf("unexpected first bulk artifact body after local failure: %q", string(firstBody))
	}
	secondPath := filepath.Join(outputDir, "logs", "summary.txt")
	if _, statErr := os.Stat(secondPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected second bulk artifact to be absent after local failure, got %v", statErr)
	}
}

func TestBuildArtifactsDownloadCommandRejectsAmbiguousNameAtCommandLevel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.String() {
		case "/api/builds/build-1/artifacts":
			_, _ = w.Write([]byte(`{"data":{"build_id":"build-1","artifacts":[{"id":"artifact-1","build_id":"build-1","name":"report.xml","path":"reports/a/report.xml","size_bytes":42,"storage_provider":"filesystem","download_url_path":"/builds/build-1/artifacts/artifact-1/download","created_at":"2026-07-05T00:00:00Z"},{"id":"artifact-2","build_id":"build-1","name":"report.xml","path":"reports/b/report.xml","size_bytes":84,"storage_provider":"filesystem","download_url_path":"/builds/build-1/artifacts/artifact-2/download","created_at":"2026-07-05T00:00:01Z"}]}}`))
		case "/api/builds/build-1/artifacts/artifact-1/download", "/api/builds/build-1/artifacts/artifact-2/download":
			t.Fatalf("download endpoint should not be called for ambiguous selector")
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
	code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"build", "artifacts", "download", "build-1", "--artifact", "report.xml", "--json"}})
	if code != 2 {
		t.Fatalf("expected ambiguity exit code 2, got %d stderr=%s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected no stdout on ambiguity error, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "matched multiple artifact names") {
		t.Fatalf("unexpected ambiguity stderr: %s", stderr.String())
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
