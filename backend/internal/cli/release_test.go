package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
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

type releaseContextKey struct{}

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

func TestReleaseRawImportAndFetchFilesystem(t *testing.T) {
	manifestPath := writeCLIManifest(t, t.TempDir())
	raw, readErr := os.ReadFile(manifestPath)
	if readErr != nil {
		t.Fatalf("read manifest: %v", readErr)
	}
	raw = append([]byte("\n"), raw...)
	rawPath := filepath.Join(t.TempDir(), "historical.json")
	if writeErr := os.WriteFile(rawPath, raw, 0o600); writeErr != nil {
		t.Fatalf("write raw manifest: %v", writeErr)
	}
	source := t.TempDir()
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	args := []string{"release", "import", "--file", rawPath, "--release-source", source, "--expected-digest", releases.Digest(raw)}
	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, Args: args}); code != 0 {
		t.Fatalf("import exit=%d stderr=%s", code, stderr.String())
	}
	if _, channelErr := os.Stat(filepath.Join(source, "channels.json")); !os.IsNotExist(channelErr) {
		t.Fatalf("import created channels.json: %v", channelErr)
	}

	fetchedPath := filepath.Join(t.TempDir(), "fetched.json")
	if code := Run(Dependencies{Stdout: &bytes.Buffer{}, Stderr: stderr, Args: []string{"release", "fetch", "--version", "2.5.1", "--release-source", "file://" + source, "--output", fetchedPath}}); code != 0 {
		t.Fatalf("fetch exit=%d stderr=%s", code, stderr.String())
	}
	fetched, fetchReadErr := os.ReadFile(fetchedPath)
	if fetchReadErr != nil {
		t.Fatalf("read fetched manifest: %v", fetchReadErr)
	}
	if !bytes.Equal(fetched, raw) {
		t.Fatalf("fetched bytes changed\nwant=%q\n got=%q", raw, fetched)
	}

	stdout.Reset()
	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, Args: []string{"release", "fetch", "--version", "2.5.1", "--release-source", source, "--output", "-"}}); code != 0 {
		t.Fatalf("stdout fetch exit=%d stderr=%s", code, stderr.String())
	}
	if !bytes.Equal(stdout.Bytes(), raw) {
		t.Fatalf("stdout bytes changed\nwant=%q\n got=%q", raw, stdout.Bytes())
	}

	if code := Run(Dependencies{Stdout: &bytes.Buffer{}, Stderr: stderr, Args: args}); code != 0 {
		t.Fatalf("idempotent import exit=%d stderr=%s", code, stderr.String())
	}
	conflicting := bytes.Replace(raw, []byte("2026-10-03"), []byte("2026-10-04"), 1)
	if writeErr := os.WriteFile(rawPath, conflicting, 0o600); writeErr != nil {
		t.Fatalf("write conflicting manifest: %v", writeErr)
	}
	conflictArgs := []string{"release", "import", "--file", rawPath, "--release-source", source, "--expected-digest", releases.Digest(conflicting)}
	if code := Run(Dependencies{Stdout: &bytes.Buffer{}, Stderr: stderr, Args: conflictArgs}); code != 1 {
		t.Fatalf("conflicting import exit=%d want 1 stderr=%s", code, stderr.String())
	}
	stored, storedErr := os.ReadFile(filepath.Join(source, "releases", "2.5.1.json"))
	if storedErr != nil || !bytes.Equal(stored, raw) {
		t.Fatalf("stored manifest changed data=%q err=%v", stored, storedErr)
	}
}

func TestReleaseRawImportAndFetchFailures(t *testing.T) {
	source := t.TempDir()
	invalidPath := filepath.Join(t.TempDir(), "invalid.json")
	if writeErr := os.WriteFile(invalidPath, []byte("{"), 0o600); writeErr != nil {
		t.Fatalf("write invalid manifest: %v", writeErr)
	}
	if code := Run(Dependencies{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Args: []string{"release", "import", "--file", invalidPath, "--release-source", source, "--expected-digest", "sha256:" + strings.Repeat("a", 64)}}); code != 2 {
		t.Fatalf("invalid import exit=%d want 2", code)
	}

	manifestPath := writeCLIManifest(t, t.TempDir())
	if code := Run(Dependencies{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Args: []string{"release", "import", "--file", manifestPath, "--release-source", source, "--expected-digest", "sha256:" + strings.Repeat("a", 64)}}); code != 2 {
		t.Fatalf("digest mismatch exit=%d want 2", code)
	}

	outputPath := filepath.Join(t.TempDir(), "preserve.json")
	if writeErr := os.WriteFile(outputPath, []byte("preserve"), 0o600); writeErr != nil {
		t.Fatalf("write output: %v", writeErr)
	}
	if code := Run(Dependencies{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Args: []string{"release", "fetch", "--version", "2.5.1", "--release-source", source, "--output", outputPath}}); code != releaseFetchNotFoundExitCode {
		t.Fatalf("missing fetch exit=%d want %d", code, releaseFetchNotFoundExitCode)
	}
	output, outputErr := os.ReadFile(outputPath)
	if outputErr != nil || string(output) != "preserve" {
		t.Fatalf("failed fetch changed output=%q err=%v", output, outputErr)
	}
}

func TestReleaseGCSStoreSelectionContextAndCleanup(t *testing.T) {
	raw := []byte("{\"raw\":true}\n")
	store := &releaseStoreFake{document: releases.Document{Data: raw, Digest: releases.Digest(raw)}}
	closer := &releaseStoreCloserFake{}
	contextKey := releaseContextKey{}
	ctx := context.WithValue(context.Background(), contextKey, "release-gcs")
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := Run(Dependencies{
		Context: ctx,
		Stdout:  stdout,
		Stderr:  stderr,
		OpenReleaseStore: func(openContext context.Context, location string) (releases.Store, io.Closer, error) {
			if location != "gs://release-bucket/releases" {
				t.Fatalf("release source=%q", location)
			}
			if openContext.Value(contextKey) != "release-gcs" {
				t.Fatal("opener did not receive command context")
			}
			return store, closer, nil
		},
		Args: []string{"release", "fetch", "--version", "2.5.1", "--release-source", "gs://release-bucket/releases", "--output", "-"},
	})
	if code != 0 {
		t.Fatalf("GCS fetch exit=%d stderr=%s", code, stderr.String())
	}
	if !bytes.Equal(stdout.Bytes(), raw) || store.fetchContext.Value(contextKey) != "release-gcs" {
		t.Fatalf("unexpected fetch bytes=%q context=%v", stdout.Bytes(), store.fetchContext)
	}
	if closer.closed != 1 {
		t.Fatalf("GCS client close calls=%d want 1", closer.closed)
	}

	store.fetchErr = releases.ErrNotFound
	outputPath := filepath.Join(t.TempDir(), "preserve.json")
	if writeErr := os.WriteFile(outputPath, []byte("preserve"), 0o600); writeErr != nil {
		t.Fatalf("write output: %v", writeErr)
	}
	if code = Run(Dependencies{Stdout: &bytes.Buffer{}, Stderr: stderr, OpenReleaseStore: func(context.Context, string) (releases.Store, io.Closer, error) {
		return store, closer, nil
	}, Args: []string{"release", "fetch", "--version", "2.5.1", "--release-source", "gs://release-bucket/releases", "--output", outputPath}}); code != releaseFetchNotFoundExitCode {
		t.Fatalf("failed GCS fetch exit=%d want %d", code, releaseFetchNotFoundExitCode)
	}
	output, outputErr := os.ReadFile(outputPath)
	if outputErr != nil || string(output) != "preserve" {
		t.Fatalf("failed GCS fetch changed output=%q err=%v", output, outputErr)
	}
	if closer.closed != 2 {
		t.Fatalf("GCS client close calls=%d want 2", closer.closed)
	}

	store.fetchErr = errors.New("permission denied: release document not found in policy detail")
	if code = Run(Dependencies{Stdout: &bytes.Buffer{}, Stderr: stderr, OpenReleaseStore: func(context.Context, string) (releases.Store, io.Closer, error) {
		return store, closer, nil
	}, Args: []string{"release", "fetch", "--version", "2.5.1", "--release-source", "gs://release-bucket/releases", "--output", outputPath}}); code != 1 {
		t.Fatalf("permission fetch exit=%d want 1", code)
	}
	if closer.closed != 3 {
		t.Fatalf("GCS client close calls=%d want 3", closer.closed)
	}

	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()
	store.fetchErr = nil
	if code = Run(Dependencies{Context: canceledContext, Stdout: &bytes.Buffer{}, Stderr: stderr, OpenReleaseStore: func(context.Context, string) (releases.Store, io.Closer, error) {
		return store, closer, nil
	}, Args: []string{"release", "fetch", "--version", "2.5.1", "--release-source", "gs://release-bucket/releases", "--output", "-"}}); code != 1 {
		t.Fatalf("canceled GCS fetch exit=%d want 1", code)
	}
	if !errors.Is(store.fetchContext.Err(), context.Canceled) {
		t.Fatalf("fetch context error=%v", store.fetchContext.Err())
	}
	if closer.closed != 4 {
		t.Fatalf("GCS client close calls=%d want 4", closer.closed)
	}
}

type releaseStoreFake struct {
	document     releases.Document
	fetchErr     error
	fetchContext context.Context
}

func (s *releaseStoreFake) FetchManifest(ctx context.Context, _ string) (releases.Document, error) {
	s.fetchContext = ctx
	if contextErr := ctx.Err(); contextErr != nil {
		return releases.Document{}, contextErr
	}
	if s.fetchErr != nil {
		return releases.Document{}, s.fetchErr
	}
	return s.document, nil
}

func (s *releaseStoreFake) FetchChannelIndex(context.Context) (releases.ChannelIndex, error) {
	return releases.ChannelIndex{}, errors.New("not implemented")
}

func (s *releaseStoreFake) PublishManifest(context.Context, releases.Manifest) (string, error) {
	return "", errors.New("not implemented")
}

func (s *releaseStoreFake) ImportManifest(context.Context, []byte) (releases.Manifest, string, error) {
	return releases.Manifest{}, "", errors.New("not implemented")
}

func (s *releaseStoreFake) PromoteChannel(context.Context, string, string, string) error {
	return errors.New("not implemented")
}

func (s *releaseStoreFake) PromoteCoyoteLatestIfNewer(context.Context, string, string) (bool, error) {
	return false, errors.New("not implemented")
}

type releaseStoreCloserFake struct{ closed int }

func (c *releaseStoreCloserFake) Close() error {
	c.closed++
	return nil
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
