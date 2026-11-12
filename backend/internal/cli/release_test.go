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

func TestReleasePromoteChannelCommand(t *testing.T) {
	manifestPath := writeCLIManifest(t, t.TempDir())
	source := t.TempDir()
	stderr := &bytes.Buffer{}
	if code := Run(Dependencies{Stdout: &bytes.Buffer{}, Stderr: stderr, Args: []string{"release", "publish", "--manifest", manifestPath, "--release-source", source}}); code != 0 {
		t.Fatalf("publish exit=%d stderr=%s", code, stderr.String())
	}

	stdout := &bytes.Buffer{}
	args := []string{"release", "promote-channel", "--release", "2.5.1", "--channel", "latest", "--release-source", source}
	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, Args: args}); code != 0 {
		t.Fatalf("promote exit=%d stderr=%s", code, stderr.String())
	}
	if output := stdout.String(); !strings.Contains(output, "Previous target: <none>") || !strings.Contains(output, "New target: 2.5.1") || !strings.Contains(output, "Changed: true") {
		t.Fatalf("unexpected promote output: %s", output)
	}

	stdout.Reset()
	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, Args: args}); code != 0 {
		t.Fatalf("repeat promote exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Changed: false") {
		t.Fatalf("repeat promotion was not idempotent: %s", stdout.String())
	}

	channels, readErr := os.ReadFile(filepath.Join(source, "channels.json"))
	if readErr != nil {
		t.Fatalf("read channels: %v", readErr)
	}
	manifestData, manifestReadErr := os.ReadFile(filepath.Join(source, "releases", "2.5.1.json"))
	if manifestReadErr != nil {
		t.Fatalf("read stored manifest: %v", manifestReadErr)
	}
	var index releases.ChannelIndex
	if decodeErr := json.Unmarshal(channels, &index); decodeErr != nil {
		t.Fatalf("decode channels: %v", decodeErr)
	}
	if entry, found := index.Channels["latest"]; !found || entry != (releases.ChannelEntry{Release: "2.5.1", ManifestDigest: releases.Digest(manifestData)}) || index.Channels["stable"] != (releases.ChannelEntry{}) {
		t.Fatalf("unexpected channel state=%+v", index)
	}

	if code := Run(Dependencies{Stdout: &bytes.Buffer{}, Stderr: stderr, Args: []string{"release", "promote-channel", "--release", "2.5.2", "--channel", "latest", "--release-source", source}}); code != releaseFetchNotFoundExitCode {
		t.Fatalf("missing release promote exit=%d want %d", code, releaseFetchNotFoundExitCode)
	}
	if code := Run(Dependencies{Stdout: &bytes.Buffer{}, Stderr: stderr, Args: []string{"release", "promote-channel", "--release", "2.5.1", "--channel", "preview", "--release-source", source}}); code != 2 {
		t.Fatalf("invalid channel promote exit=%d want 2", code)
	}
}

func TestReleasePromoteChannelReportsAmbiguousOutcome(t *testing.T) {
	manifestPath := writeCLIManifest(t, t.TempDir())
	manifestData, readErr := os.ReadFile(manifestPath)
	if readErr != nil {
		t.Fatalf("read manifest: %v", readErr)
	}
	digest := releases.Digest(manifestData)
	store := &releaseStoreFake{
		document:        releases.Document{Data: manifestData, Digest: digest},
		promotionResult: releases.ChannelPromotionResult{Current: releases.ChannelEntry{Release: "2.5.1", ManifestDigest: digest}},
	}
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := Run(Dependencies{
		Stdout: stdout,
		Stderr: stderr,
		OpenReleaseStore: func(context.Context, string) (releases.Store, io.Closer, error) {
			return store, nil, nil
		},
		Args: []string{"release", "promote-channel", "--release", "2.5.1", "--channel", "latest", "--release-source", "gs://release-bucket/releases"},
	})
	if code != 0 {
		t.Fatalf("promote exit=%d stderr=%s", code, stderr.String())
	}
	if output := stdout.String(); !strings.Contains(output, "Previous target: <unknown>") || !strings.Contains(output, "Changed: unknown") {
		t.Fatalf("unexpected ambiguous promotion output: %s", output)
	}
}

func TestReleasePromoteChannelCommandFailures(t *testing.T) {
	manifestPath := writeCLIManifest(t, t.TempDir())
	manifestData, readErr := os.ReadFile(manifestPath)
	if readErr != nil {
		t.Fatalf("read manifest: %v", readErr)
	}
	mismatchedManifest := bytes.Replace(manifestData, []byte(`"2.5.1"`), []byte(`"2.5.2"`), 1)
	validDigest := releases.Digest(manifestData)

	tests := []struct {
		name    string
		args    []string
		store   *releaseStoreFake
		openErr error
		want    int
	}{
		{name: "missing required flags", args: []string{"release", "promote-channel"}, want: 2},
		{name: "invalid version", args: []string{"release", "promote-channel", "--release", "1.2", "--channel", "latest", "--release-source", "test"}, want: 2},
		{name: "store error", args: []string{"release", "promote-channel", "--release", "2.5.1", "--channel", "latest", "--release-source", "test"}, openErr: errors.New("store unavailable"), want: 1},
		{name: "fetch error", args: []string{"release", "promote-channel", "--release", "2.5.1", "--channel", "latest", "--release-source", "test"}, store: &releaseStoreFake{fetchErr: errors.New("permission denied")}, want: 1},
		{name: "invalid manifest", args: []string{"release", "promote-channel", "--release", "2.5.1", "--channel", "latest", "--release-source", "test"}, store: &releaseStoreFake{document: releases.Document{Data: []byte("{")}}, want: 1},
		{name: "mismatched manifest", args: []string{"release", "promote-channel", "--release", "2.5.1", "--channel", "latest", "--release-source", "test"}, store: &releaseStoreFake{document: releases.Document{Data: mismatchedManifest}}, want: 1},
		{name: "promotion error", args: []string{"release", "promote-channel", "--release", "2.5.1", "--channel", "latest", "--release-source", "test"}, store: &releaseStoreFake{document: releases.Document{Data: manifestData, Digest: validDigest}, promoteErr: errors.New("promotion failed")}, want: 1},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			code := Run(Dependencies{
				Stdout: &bytes.Buffer{},
				Stderr: &bytes.Buffer{},
				OpenReleaseStore: func(context.Context, string) (releases.Store, io.Closer, error) {
					return testCase.store, nil, testCase.openErr
				},
				Args: testCase.args,
			})
			if code != testCase.want {
				t.Fatalf("promote exit=%d want %d", code, testCase.want)
			}
		})
	}
}

func TestReleasePromoteChannelReportsPreviousTarget(t *testing.T) {
	manifestPath := writeCLIManifest(t, t.TempDir())
	manifestData, readErr := os.ReadFile(manifestPath)
	if readErr != nil {
		t.Fatalf("read manifest: %v", readErr)
	}
	digest := releases.Digest(manifestData)
	previous := releases.ChannelEntry{Release: "2.5.0", ManifestDigest: "sha256:" + strings.Repeat("a", 64)}
	store := &releaseStoreFake{
		document: releases.Document{Data: manifestData, Digest: digest},
		promotionResult: releases.ChannelPromotionResult{
			Previous: &previous, PreviousKnown: true,
			Current: releases.ChannelEntry{Release: "2.5.1", ManifestDigest: digest},
			Changed: true, ChangedKnown: true,
		},
	}
	stdout := &bytes.Buffer{}
	code := Run(Dependencies{
		Stdout: stdout,
		Stderr: &bytes.Buffer{},
		OpenReleaseStore: func(context.Context, string) (releases.Store, io.Closer, error) {
			return store, nil, nil
		},
		Args: []string{"release", "promote-channel", "--release", "2.5.1", "--channel", "latest", "--release-source", "test"},
	})
	if code != 0 {
		t.Fatalf("promote exit=%d", code)
	}
	if output := stdout.String(); !strings.Contains(output, "Previous target: 2.5.0 ("+previous.ManifestDigest+")") || !strings.Contains(output, "Changed: true") {
		t.Fatalf("unexpected promote output: %s", output)
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

func TestReleaseFetchRejectsInvalidVersionBeforeOpeningStore(t *testing.T) {
	for _, version := range []string{"../channels", "1.2", "1.2.3/../channels", " 1.2.3"} {
		t.Run(version, func(t *testing.T) {
			opened := false
			code := Run(Dependencies{
				Stdout: &bytes.Buffer{},
				Stderr: &bytes.Buffer{},
				OpenReleaseStore: func(context.Context, string) (releases.Store, io.Closer, error) {
					opened = true
					return nil, nil, errors.New("release store should not open")
				},
				Args: []string{"release", "fetch", "--version", version, "--release-source", "gs://release-bucket", "--output", "-"},
			})
			if code != 2 {
				t.Fatalf("fetch exit=%d want 2", code)
			}
			if opened {
				t.Fatal("invalid version opened release store")
			}
		})
	}
}

func TestReleaseFetchClassifiesStoreOpeningFailures(t *testing.T) {
	tests := []struct {
		name string
		err  error
		code int
	}{
		{name: "invalid location", err: fmt.Errorf("%w: malformed GCS URL", releases.ErrInvalidLocation), code: 2},
		{name: "unsupported backend", err: fmt.Errorf("%w: s3", releases.ErrUnsupportedBackend), code: 2},
		{name: "application default credentials", err: errors.New("could not find default credentials"), code: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code := Run(Dependencies{
				Stdout: &bytes.Buffer{},
				Stderr: &bytes.Buffer{},
				OpenReleaseStore: func(context.Context, string) (releases.Store, io.Closer, error) {
					return nil, nil, tc.err
				},
				Args: []string{"release", "fetch", "--version", "2.5.1", "--release-source", "gs://release-bucket", "--output", "-"},
			})
			if code != tc.code {
				t.Fatalf("fetch exit=%d want %d", code, tc.code)
			}
		})
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
	document        releases.Document
	fetchErr        error
	fetchContext    context.Context
	promotionResult releases.ChannelPromotionResult
	promoteErr      error
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

func (s *releaseStoreFake) PromoteChannel(context.Context, string, string, string) (releases.ChannelPromotionResult, error) {
	return s.promotionResult, s.promoteErr
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
