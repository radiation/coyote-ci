package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/cli/config"
	"github.com/radiation/coyote-ci/backend/internal/cli/credentials"
	releases "github.com/radiation/coyote-ci/backend/internal/release"
)

func TestReleasePublishCoyote(t *testing.T) {
	version, releaseVersion, status, target, serverDigest := "0.0.41", "0.0.41", "complete", "coyote-server", "a"
	releaseJSON := func(version, status, target string) string {
		return `{"data":{"id":"release-1","build_id":"build-1","project_id":"project-1","application_version":"` + version + `","status":"` + status + `","outputs":[` +
			`{"key":"image:coyote-server-image","kind":"image","status":"published","target_image_reference":"` + target + `","immutable_reference":"registry/coyote-server@sha256:` + strings.Repeat(serverDigest, 64) + `"},` +
			`{"key":"image:coyote-frontend-image","kind":"image","status":"published","target_image_reference":"coyote-frontend","immutable_reference":"registry/coyote-frontend@sha256:` + strings.Repeat("b", 64) + `"},` +
			`{"key":"image:coyote-worker-image","kind":"image","status":"published","target_image_reference":"coyote-worker","immutable_reference":"registry/coyote-worker@sha256:` + strings.Repeat("c", 64) + `"},` +
			`{"key":"image:coyote-migrate-image","kind":"image","status":"published","target_image_reference":"coyote-migrate","immutable_reference":"registry/coyote-migrate@sha256:` + strings.Repeat("d", 64) + `" }]}}`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/builds/build-1/application-release":
			_, _ = w.Write([]byte(releaseJSON(releaseVersion, status, target)))
		case "/api/builds/build-1":
			_, _ = w.Write([]byte(`{"data":{"id":"build-1","created_at":"2026-10-13T14:00:00Z","application_version":"` + version + `","source_sha":"` + strings.Repeat("e", 40) + `"}}`))
		default:
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
	run := func(source string) (int, string) {
		stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
		code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: credentialsStore,
			Args: []string{"release", "publish-coyote", "--build", "build-1", "--release-source", source}})
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
