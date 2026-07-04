package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/cli/config"
	"github.com/radiation/coyote-ci/backend/internal/cli/credentials"
	"github.com/radiation/coyote-ci/backend/internal/versioninfo"
)

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
		if !errors.Is(context.Canceled, context.Canceled) {
			t.Fatal("unexpected cancellation check")
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
