package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/cli/config"
	"github.com/radiation/coyote-ci/backend/internal/cli/credentials"
	releases "github.com/radiation/coyote-ci/backend/internal/release"
)

func TestReleaseResolveCommand(t *testing.T) {
	root := t.TempDir()
	manifestPath := writeCLIManifest(t, root)
	manifestData, readErr := os.ReadFile(manifestPath)
	if readErr != nil {
		t.Fatalf("read manifest: %v", readErr)
	}
	if mkdirErr := os.MkdirAll(filepath.Join(root, "releases"), 0o755); mkdirErr != nil {
		t.Fatalf("mkdir: %v", mkdirErr)
	}
	if writeErr := os.WriteFile(filepath.Join(root, "releases", "2.5.1.json"), manifestData, 0o600); writeErr != nil {
		t.Fatalf("write source manifest: %v", writeErr)
	}

	output := filepath.Join(t.TempDir(), "resolved.yaml")
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, Args: []string{"release", "resolve", "--release", "2.5.1", "--release-source", root, "--output", output}}); code != 0 {
		t.Fatalf("resolve exit %d stderr=%s", code, stderr.String())
	}
	data, readErr := os.ReadFile(output)
	if readErr != nil {
		t.Fatalf("read output: %v", readErr)
	}
	if !strings.Contains(string(data), "server: registry.example/coyote-server@sha256:") || !strings.Contains(string(data), "worker: registry.example/coyote-worker@sha256:") {
		t.Fatalf("unexpected output: %s", data)
	}

	publishSource := t.TempDir()
	if code := Run(Dependencies{Stdout: &bytes.Buffer{}, Stderr: stderr, Args: []string{"release", "publish", "--manifest", manifestPath, "--release-source", publishSource, "--channel", "stable"}}); code != 0 {
		t.Fatalf("publish exit %d stderr=%s", code, stderr.String())
	}
	channelOutput := filepath.Join(t.TempDir(), "stable.yaml")
	if code := Run(Dependencies{Stdout: &bytes.Buffer{}, Stderr: stderr, Args: []string{"release", "resolve", "--channel", "stable", "--release-source", publishSource, "--output", channelOutput}}); code != 0 {
		t.Fatalf("channel resolve exit %d stderr=%s", code, stderr.String())
	}
}

func TestReleaseCommandFailures(t *testing.T) {
	root := t.TempDir()
	output := filepath.Join(t.TempDir(), "resolved.yaml")
	tests := []struct {
		name string
		args []string
		code int
	}{
		{"resolve missing flags", []string{"release", "resolve", "--release-source", root, "--output", output}, 2},
		{"resolve missing source", []string{"release", "resolve", "--release", "2.5.1", "--output", output}, 2},
		{"resolve missing manifest", []string{"release", "resolve", "--release", "2.5.1", "--release-source", root, "--output", output}, 1},
		{"publish missing flags", []string{"release", "publish"}, 2},
		{"publish missing manifest", []string{"release", "publish", "--manifest", filepath.Join(root, "missing.json"), "--release-source", root}, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if code := Run(Dependencies{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Args: tc.args}); code != tc.code {
				t.Fatalf("expected exit code %d, got %d", tc.code, code)
			}
		})
	}

	invalidManifestPath := filepath.Join(root, "invalid.json")
	if writeErr := os.WriteFile(invalidManifestPath, []byte("{"), 0o600); writeErr != nil {
		t.Fatalf("write invalid manifest: %v", writeErr)
	}
	if code := Run(Dependencies{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Args: []string{"release", "publish", "--manifest", invalidManifestPath, "--release-source", root}}); code != 2 {
		t.Fatalf("expected invalid manifest exit code 2, got %d", code)
	}

	validManifestPath := writeCLIManifest(t, root)
	if code := Run(Dependencies{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Args: []string{"release", "publish", "--manifest", validManifestPath, "--release-source", root, "--channel", "preview"}}); code != 2 {
		t.Fatalf("expected invalid channel exit code 2, got %d", code)
	}
	if _, releaseDirErr := os.Stat(filepath.Join(root, "releases")); !os.IsNotExist(releaseDirErr) {
		t.Fatalf("invalid channel created a release directory: %v", releaseDirErr)
	}

	blockedSource := filepath.Join(t.TempDir(), "blocked")
	if writeErr := os.WriteFile(blockedSource, []byte("not a directory"), 0o600); writeErr != nil {
		t.Fatalf("write blocked source: %v", writeErr)
	}
	if code := Run(Dependencies{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Args: []string{"release", "publish", "--manifest", validManifestPath, "--release-source", blockedSource}}); code != 1 {
		t.Fatalf("expected blocked source exit code 1, got %d", code)
	}
}

func TestReleaseOutputHelper(t *testing.T) {
	blockedParent := filepath.Join(t.TempDir(), "blocked")
	if writeErr := os.WriteFile(blockedParent, []byte("not a directory"), 0o600); writeErr != nil {
		t.Fatalf("write blocked parent: %v", writeErr)
	}
	if overlayErr := writeReleaseOverlay(filepath.Join(blockedParent, "resolved.yaml"), releases.Resolved{}); overlayErr == nil {
		t.Fatal("expected blocked output error")
	}
}

func TestApplicationReleaseCommands(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/application-releases/release-1":
			if r.Method != http.MethodGet {
				t.Fatalf("show method=%s, want GET", r.Method)
			}
			_, _ = w.Write([]byte(`{"data":{"id":"release-1","build_id":"build-1","project_id":"project-1","application_version":"1.2.3","status":"complete","outputs":[]}}`))
		case "/api/application-releases/release-1/channels/stable":
			if r.Method != http.MethodPost {
				t.Fatalf("promote method=%s, want POST", r.Method)
			}
			_, _ = w.Write([]byte(`{"data":{"id":"release-1","build_id":"build-1","project_id":"project-1","application_version":"1.2.3","status":"complete","outputs":[]}}`))
		case "/api/application-releases/pending/channels/stable":
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":{"code":"release_incomplete","message":"only complete releases can be promoted"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	configStore := config.NewStore(filepath.Join(t.TempDir(), "config.json"))
	if saveErr := configStore.Save(config.File{CurrentContext: "local", Contexts: map[string]config.Context{
		"local": {Name: "local", ServerURL: server.URL, CredentialRef: "context:local"},
	}}); saveErr != nil {
		t.Fatalf("save config: %v", saveErr)
	}
	credentialsStore := credentials.NewMemoryStore()
	if setErr := credentialsStore.Set("context:local", "token"); setErr != nil {
		t.Fatalf("set token: %v", setErr)
	}
	dependencies := Dependencies{ConfigStore: configStore, Credentials: credentialsStore}

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	dependencies.Stdout, dependencies.Stderr, dependencies.Args = stdout, stderr, []string{"release", "show", "release-1"}
	if code := Run(dependencies); code != 0 {
		t.Fatalf("release show exit=%d stderr=%s", code, stderr.String())
	}
	if got := stdout.String(); !strings.Contains(got, "Release: release-1") || !strings.Contains(got, "Version: 1.2.3") {
		t.Fatalf("unexpected show output: %s", got)
	}

	stdout.Reset()
	stderr.Reset()
	dependencies.Stdout, dependencies.Stderr, dependencies.Args = stdout, stderr, []string{"release", "show", "release-1", "--json"}
	if code := Run(dependencies); code != 0 {
		t.Fatalf("release show JSON exit=%d stderr=%s", code, stderr.String())
	}
	var shown map[string]any
	if decodeErr := json.Unmarshal(stdout.Bytes(), &shown); decodeErr != nil {
		t.Fatalf("decode show JSON: %v", decodeErr)
	}
	if shown["id"] != "release-1" || shown["status"] != "complete" {
		t.Fatalf("unexpected show JSON: %+v", shown)
	}

	stdout.Reset()
	stderr.Reset()
	dependencies.Stdout, dependencies.Stderr, dependencies.Args = stdout, stderr, []string{"release", "promote", "release-1", "--channel", "stable"}
	if code := Run(dependencies); code != 0 {
		t.Fatalf("release promote exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Promoted release release-1 to stable") {
		t.Fatalf("unexpected promote output: %s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	dependencies.Stdout, dependencies.Stderr, dependencies.Args = stdout, stderr, []string{"release", "promote", "pending", "--channel", "stable"}
	if code := Run(dependencies); code != 2 {
		t.Fatalf("pending promotion exit=%d, want 2 stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "only complete releases can be promoted") {
		t.Fatalf("missing server error: %s", stderr.String())
	}
}

func writeCLIManifest(t *testing.T, root string) string {
	t.Helper()
	images := map[string]string{}
	for component, character := range map[string]string{"server": "a", "frontend": "b", "worker": "c", "migrate": "d"} {
		images[component] = "registry.example/coyote-" + component + "@sha256:" + strings.Repeat(character, 64)
	}
	manifest := map[string]any{"schema_version": 1, "release": "2.5.1", "source": map[string]string{"commit": strings.Repeat("a", 40), "created_at": "2026-10-03T14:00:00Z"}, "images": images}
	data, marshalErr := json.Marshal(manifest)
	if marshalErr != nil {
		t.Fatalf("marshal manifest: %v", marshalErr)
	}
	path := filepath.Join(root, "publish.json")
	if writeErr := os.WriteFile(path, data, 0o600); writeErr != nil {
		t.Fatalf("write manifest: %v", writeErr)
	}
	return path
}
