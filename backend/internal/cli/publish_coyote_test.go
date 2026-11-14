package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
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

func TestReleasePublishCoyote(t *testing.T) {
	version, releaseVersion, status, target, serverDigest := "0.0.41", "0.0.41", "complete", "coyote-server", "a"
	buildStatus := "success"
	artifactBytes := make(map[string][]byte)
	checksums := &strings.Builder{}
	for _, platform := range releases.CoyoteCLIPlatforms {
		contents := []byte("CLI archive " + platform)
		artifactBytes["cli-"+platform] = contents
		extension := ".tar.gz"
		if strings.HasPrefix(platform, "windows-") {
			extension = ".zip"
		}
		fmt.Fprintf(checksums, "%x  coyote_%s%s\n", sha256.Sum256(contents), strings.ReplaceAll(platform, "-", "_"), extension)
	}
	artifactBytes["cli-checksums"] = []byte(checksums.String())
	artifactBytes["cli-metadata"] = []byte(`{"version":"0.0.41","commit":"` + strings.Repeat("e", 40) + `"}`)
	artifactBytes["compatibility"] = []byte(`{"schema_target":"57","upgrade_from":">=0.0.11 <0.0.41","maintenance":"active"}`)
	artifactBytes["schema-target"] = []byte("57\n")
	var chart bytes.Buffer
	compressed := gzip.NewWriter(&chart)
	archive := tar.NewWriter(compressed)
	chartYAML := "name: coyote-ci\nversion: 0.0.41\nappVersion: 0.0.41\n"
	if writeErr := archive.WriteHeader(&tar.Header{Name: "coyote-ci/Chart.yaml", Mode: 0600, Size: int64(len(chartYAML))}); writeErr != nil {
		t.Fatal(writeErr)
	}
	if _, writeErr := archive.Write([]byte(chartYAML)); writeErr != nil {
		t.Fatal(writeErr)
	}
	if closeErr := archive.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if closeErr := compressed.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	artifactBytes["helm-chart"] = chart.Bytes()
	helmdir := t.TempDir()
	chartPath := filepath.Join(helmdir, "chart.tgz")
	if writeErr := os.WriteFile(chartPath, chart.Bytes(), 0600); writeErr != nil {
		t.Fatal(writeErr)
	}
	helmscript := "#!/bin/sh\nif [ \"$COYOTE_TEST_HELM_FAIL\" = true ]; then echo denied >&2; exit 1; fi\ncp \"$COYOTE_TEST_CHART\" \"$6/coyote-ci-$4.tgz\"\n"
	if writeErr := os.WriteFile(filepath.Join(helmdir, "helm"), []byte(helmscript), 0700); writeErr != nil {
		t.Fatal(writeErr)
	}
	t.Setenv("PATH", helmdir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("COYOTE_TEST_CHART", chartPath)
	dockerScript := "#!/bin/sh\ncase \"$1\" in\nrun) printf '57\\n' ;;\nbuildx) printf '{\"digest\":\"sha256:" + strings.Repeat("f", 64) + "\"}\\n' ;;\n*) exit 1 ;;\nesac\n"
	if writeErr := os.WriteFile(filepath.Join(helmdir, "docker"), []byte(dockerScript), 0700); writeErr != nil {
		t.Fatal(writeErr)
	}
	releaseJSON := func(version, status, target string) string {
		images := `{"data":{"id":"release-1","build_id":"build-1","project_id":"project-1","application_version":"` + version + `","status":"` + status + `","outputs":[` +
			`{"key":"image:coyote-server-image","kind":"image","status":"published","target_image_reference":"` + target + `","immutable_reference":"registry/coyote-server@sha256:` + strings.Repeat(serverDigest, 64) + `"},` +
			`{"key":"image:coyote-frontend-image","kind":"image","status":"published","target_image_reference":"coyote-frontend","immutable_reference":"registry/coyote-frontend@sha256:` + strings.Repeat("b", 64) + `"},` +
			`{"key":"image:coyote-worker-image","kind":"image","status":"published","target_image_reference":"coyote-worker","immutable_reference":"registry/coyote-worker@sha256:` + strings.Repeat("c", 64) + `"},` +
			`{"key":"image:coyote-migrate-image","kind":"image","status":"published","target_image_reference":"coyote-migrate","immutable_reference":"registry/coyote-migrate@sha256:` + strings.Repeat("d", 64) + `" },` +
			`{"key":"image:coyote-mcp-image","kind":"image","status":"published","target_image_reference":"coyote-mcp","immutable_reference":"registry/coyote-mcp@sha256:` + strings.Repeat("f", 64) + `" }`
		for name := range artifactBytes {
			images += `,{"key":"artifact:` + name + `","kind":"artifact","status":"published","artifact_id":"` + name + `"}`
		}
		return images + `]}}`
	}
	downloadFails := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/builds/build-1/application-release":
			_, _ = w.Write([]byte(releaseJSON(releaseVersion, status, target)))
		case "/api/builds/build-1":
			_, _ = w.Write([]byte(`{"data":{"id":"build-1","status":"` + buildStatus + `","created_at":"2026-10-13T14:00:00Z","application_version":"` + version + `","source_sha":"` + strings.Repeat("e", 40) + `"}}`))
		case "/api/builds/build-1/artifacts":
			var artifacts []map[string]any
			for name, contents := range artifactBytes {
				artifacts = append(artifacts, map[string]any{"id": name, "build_id": "build-1", "name": name, "path": name, "size_bytes": len(contents), "checksum_sha256": fmt.Sprintf("%x", sha256.Sum256(contents))})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"build_id": "build-1", "artifacts": artifacts}})
		default:
			if strings.HasPrefix(r.URL.Path, "/api/builds/build-1/artifacts/") && !downloadFails {
				name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/builds/build-1/artifacts/"), "/download")
				_, _ = w.Write(artifactBytes[name])
				return
			}
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	configStore := config.NewStore(filepath.Join(t.TempDir(), "config.json"))
	if saveErr := configStore.Save(config.File{CurrentContext: "local", Contexts: map[string]config.Context{
		"local": {Name: "local", ServerURL: server.URL, CredentialRef: "context:local"},
	}}); saveErr != nil {
		t.Fatalf("Save(): %v", saveErr)
	}
	credentialsStore := credentials.NewMemoryStore()
	if setErr := credentialsStore.Set("context:local", "token"); setErr != nil {
		t.Fatalf("Set(): %v", setErr)
	}
	channel := "latest"
	var storeOpener ReleaseStoreOpener
	run := func(source string) (int, string) {
		stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
		code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: credentialsStore,
			OpenReleaseStore: storeOpener,
			Args:             []string{"release", "publish-coyote", "--build", "build-1", "--release-source", source, "--helm-repository", "oci://registry.example/charts/coyote-ci", "--immutable-chart-tags-confirmed", "--channel", channel}})
		return code, stderr.String()
	}

	t.Run("publishes manifest and latest", func(t *testing.T) {
		source := t.TempDir()
		code, stderr := run(source)
		if code != 0 {
			t.Fatalf("exit=%d stderr=%s", code, stderr)
		}
		fileSource, sourceErr := releases.NewFileSource(source)
		if sourceErr != nil {
			t.Fatalf("NewFileSource(): %v", sourceErr)
		}
		manifest, manifestErr := fileSource.FetchManifest(t.Context(), "0.0.41")
		if manifestErr != nil || !strings.Contains(string(manifest.Data), `"release": "0.0.41"`) {
			t.Fatalf("manifest=%s err=%v", manifest.Data, manifestErr)
		}
		index, indexErr := fileSource.FetchChannelIndex(t.Context())
		if indexErr != nil || index.Channels["latest"].Release != "0.0.41" {
			t.Fatalf("channel=%+v err=%v", index, indexErr)
		}
		if retryCode, retryStderr := run(source); retryCode != 0 {
			t.Fatalf("identical retry: %d %s", retryCode, retryStderr)
		}
	})

	t.Run("publishes without channel promotion", func(t *testing.T) {
		channel = ""
		defer func() { channel = "latest" }()
		source := t.TempDir()
		if code, stderr := run(source); code != 0 {
			t.Fatalf("publish: %d %s", code, stderr)
		}
		if _, err := os.Stat(filepath.Join(source, "channels.json")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("unexpected channel: %v", err)
		}
	})

	t.Run("store failures preserve ordering and allow retry", func(t *testing.T) {
		for _, failure := range []string{"manifest", "channel"} {
			t.Run(failure, func(t *testing.T) {
				store, closer, err := releases.OpenStore(t.Context(), t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				if closer != nil {
					defer func() { _ = closer.Close() }()
				}
				injected := &failingCoyoteStore{Store: store, failure: failure}
				storeOpener = func(context.Context, string) (releases.Store, io.Closer, error) { return injected, nil, nil }
				defer func() { storeOpener = nil }()
				if code, _ := run("unused"); code == 0 {
					t.Fatal("accepted failed store operation")
				}
				if failure == "manifest" && injected.promotions != 0 {
					t.Fatal("channel promoted before manifest publication")
				}
				injected.failure = ""
				if code, stderr := run("unused"); code != 0 {
					t.Fatalf("retry failed: %d %s", code, stderr)
				}
			})
		}
	})

	t.Run("rejects version mismatch and invalid outputs", func(t *testing.T) {
		source := t.TempDir()
		releaseVersion = "0.0.42"
		if code, _ := run(source); code != 1 {
			t.Fatalf("version mismatch exit=%d, want 1", code)
		}
		releaseVersion = "0.0.41"
		target = "wrong-target"
		if code, _ := run(source); code != 1 {
			t.Fatalf("invalid output exit=%d, want 1", code)
		}
		target = "coyote-server"
	})

	t.Run("fails closed without publishing a partial release", func(t *testing.T) {
		for _, failure := range []string{"download", "helm", "checksum", "schema", "build", "incomplete"} {
			t.Run(failure, func(t *testing.T) {
				source := t.TempDir()
				downloadFails = failure == "download"
				t.Setenv("COYOTE_TEST_HELM_FAIL", fmt.Sprint(failure == "helm"))
				originalChecksum := artifactBytes["cli-checksums"]
				originalTarget := artifactBytes["schema-target"]
				if failure == "checksum" {
					artifactBytes["cli-checksums"] = []byte("invalid")
				}
				if failure == "schema" {
					artifactBytes["schema-target"] = []byte("58")
				}
				if failure == "build" {
					buildStatus = "failed"
				}
				if failure == "incomplete" {
					status = "pending"
				}
				if code, stderr := run(source); code == 0 {
					t.Fatalf("partial release accepted: %s", stderr)
				}
				entries, readErr := os.ReadDir(source)
				if readErr != nil || len(entries) != 0 {
					t.Fatalf("partial publication: %v %v", entries, readErr)
				}
				downloadFails = false
				buildStatus, status = "success", "complete"
				artifactBytes["cli-checksums"], artifactBytes["schema-target"] = originalChecksum, originalTarget
			})
		}
	})

	t.Run("does not rewind latest and surfaces manifest conflicts", func(t *testing.T) {
		source := t.TempDir()
		publisher, publisherErr := releases.NewFilePublisher(source)
		if publisherErr != nil {
			t.Fatalf("NewFilePublisher(): %v", publisherErr)
		}
		digest := "sha256:" + strings.Repeat("f", 64)
		if _, promoteErr := publisher.PromoteCoyoteLatestIfNewer(t.Context(), "0.0.42", digest); promoteErr != nil {
			t.Fatalf("seed latest: %v", promoteErr)
		}
		if code, stderr := run(source); code != 0 {
			t.Fatalf("older publish exit=%d stderr=%s", code, stderr)
		}
		fileSource, sourceErr := releases.NewFileSource(source)
		if sourceErr != nil {
			t.Fatalf("NewFileSource(): %v", sourceErr)
		}
		index, indexErr := fileSource.FetchChannelIndex(t.Context())
		if indexErr != nil || index.Channels["latest"].Release != "0.0.42" {
			t.Fatalf("latest=%+v err=%v", index, indexErr)
		}
		serverDigest = "e"
		if code, _ := run(source); code != 1 {
			t.Fatalf("conflicting publish exit=%d, want 1", code)
		}
		serverDigest = "a"
	})
}

type failingCoyoteStore struct {
	releases.Store
	failure    string
	promotions int
}

func (store *failingCoyoteStore) PublishManifest(ctx context.Context, manifest releases.Manifest) (string, error) {
	if store.failure == "manifest" {
		return "", errors.New("injected manifest storage failure")
	}
	return store.Store.PublishManifest(ctx, manifest)
}

func (store *failingCoyoteStore) PromoteCoyoteLatestIfNewer(ctx context.Context, version, digest string) (bool, error) {
	store.promotions++
	if store.failure == "channel" {
		return false, errors.New("injected channel failure")
	}
	return store.Store.PromoteCoyoteLatestIfNewer(ctx, version, digest)
}
