package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseResolveCommand(t *testing.T) {
	root := t.TempDir()
	images := map[string]string{}
	for component, character := range map[string]string{"server": "a", "frontend": "b", "worker": "c", "migrate": "d"} {
		images[component] = "registry.example/coyote-" + component + "@sha256:" + strings.Repeat(character, 64)
	}
	manifest := map[string]any{"schema_version": 1, "release": "2.5.1", "source": map[string]string{"commit": strings.Repeat("a", 40), "created_at": "2026-10-03T14:00:00Z"}, "images": images}
	manifestData, _ := json.Marshal(manifest)
	if mkdirErr := os.MkdirAll(filepath.Join(root, "releases"), 0o755); mkdirErr != nil {
		t.Fatalf("mkdir: %v", mkdirErr)
	}
	if writeErr := os.WriteFile(filepath.Join(root, "releases", "2.5.1.json"), manifestData, 0o600); writeErr != nil {
		t.Fatalf("manifest: %v", writeErr)
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
	if !strings.Contains(string(data), "server: "+images["server"]) || !strings.Contains(string(data), "worker: "+images["worker"]) {
		t.Fatalf("unexpected output: %s", data)
	}
	publishSource := t.TempDir()
	manifestPath := filepath.Join(t.TempDir(), "manifest.json")
	if manifestWriteErr := os.WriteFile(manifestPath, manifestData, 0o600); manifestWriteErr != nil {
		t.Fatalf("write publish manifest: %v", manifestWriteErr)
	}
	if code := Run(Dependencies{Stdout: &bytes.Buffer{}, Stderr: stderr, Args: []string{"release", "publish", "--manifest", manifestPath, "--release-source", publishSource, "--channel", "stable"}}); code != 0 {
		t.Fatalf("publish exit %d stderr=%s", code, stderr.String())
	}
	channelOutput := filepath.Join(t.TempDir(), "stable.yaml")
	if code := Run(Dependencies{Stdout: &bytes.Buffer{}, Stderr: stderr, Args: []string{"release", "resolve", "--channel", "stable", "--release-source", publishSource, "--output", channelOutput}}); code != 0 {
		t.Fatalf("channel resolve exit %d stderr=%s", code, stderr.String())
	}
	for _, args := range [][]string{{"release", "resolve", "--release-source", root, "--output", output}, {"release", "resolve", "--release", "2.5.1", "--channel", "stable", "--release-source", root, "--output", output}} {
		if code := Run(Dependencies{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Args: args}); code != 2 {
			t.Fatalf("expected usage error %d for %v, got %d", 2, args, code)
		}
	}
}
