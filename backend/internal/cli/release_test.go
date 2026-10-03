package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
