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

	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"auth", "status", "--output", "json"}}); code != 0 {
		t.Fatalf("auth status exit code %d stderr=%s", code, stderr.String())
	}
	var status map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if status["server_url"] != server.URL || status["auth_source"] != "credential_store" {
		t.Fatalf("unexpected status payload: %+v", status)
	}
	stdout.Reset()
	stderr.Reset()

	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"server", "info", "--output", "json"}}); code != 0 {
		t.Fatalf("server info exit code %d stderr=%s", code, stderr.String())
	}
	var info map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &info); err != nil {
		t.Fatalf("decode info: %v", err)
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
	if !strings.Contains(stdout.String(), `"version":"2.0.0"`) {
		t.Fatalf("unexpected version output: %s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()

	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: store, Getenv: func(key string) string {
		if key == config.EnvContext {
			return "other"
		}
		return ""
	}, Args: []string{"context", "current", "--output", "json"}}); code != 0 {
		t.Fatalf("context current exit code %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"name":"other"`) {
		t.Fatalf("unexpected context current output: %s", stdout.String())
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
