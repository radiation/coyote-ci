package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	workspacepkg "github.com/radiation/coyote-ci/backend/internal/workspace"
)

func TestEnsureCacheMountPathsCreatesAllPresetPaths(t *testing.T) {
	root := t.TempDir()
	if err := ensureCacheMountPaths(root, "go", "."); err != nil {
		t.Fatalf("ensure cache mount paths: %v", err)
	}
	for _, path := range []string{"paths/000", "paths/001"} {
		info, err := os.Stat(filepath.Join(root, path))
		if err != nil || !info.IsDir() {
			t.Fatalf("cache path %s: info=%v err=%v", path, info, err)
		}
	}
}

func TestEnsureCacheMountPathsRejectsUnsupportedPreset(t *testing.T) {
	if err := ensureCacheMountPaths(t.TempDir(), "unknown", "."); err == nil {
		t.Fatal("expected unsupported cache preset error")
	}
}

func TestRunCacheSaveAfterBuildTreatsObservationFailuresAsSideEffects(t *testing.T) {
	t.Setenv(workspaceHelperPodName, "pod")
	t.Setenv(workspaceHelperNamespace, "ci")
	t.Setenv(workspaceHelperPodUID, "pod-uid")
	originalClient := newWorkspacePublishPodClient
	t.Cleanup(func() { newWorkspacePublishPodClient = originalClient })
	newWorkspacePublishPodClient = func() (workspacePublishPodClient, error) {
		return nil, errors.New("Kubernetes API unavailable")
	}
	if err := runCacheSaveAfterBuild(context.Background()); err != nil {
		t.Fatalf("cache save observation error must be non-fatal: %v", err)
	}
}

func TestRunCacheSaveAfterBuildTreatsMissingPodIdentityAsSideEffect(t *testing.T) {
	t.Setenv(workspaceHelperPodName, "")
	t.Setenv(workspaceHelperNamespace, "")
	t.Setenv(workspaceHelperPodUID, "")
	if err := runCacheSaveAfterBuild(context.Background()); err != nil {
		t.Fatalf("missing cache save identity must be non-fatal: %v", err)
	}
}

func TestRunCacheSaveAfterBuildSavesAfterSuccessfulBuild(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/internal/workspace-helper/capabilities":
			_, _ = w.Write([]byte(`{"data":{"capability":"save-capability"}}`))
		case "/api/internal/workspace-helper/cache/save":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	root := configureCacheHelperForTest(t, server.URL, domain.CachePolicyPush)
	if mkdirErr := os.MkdirAll(filepath.Join(root, "paths", "000"), 0o755); mkdirErr != nil {
		t.Fatalf("create cache path: %v", mkdirErr)
	}
	t.Setenv(workspaceHelperPodName, "pod")
	t.Setenv(workspaceHelperNamespace, "ci")
	originalClient := newWorkspacePublishPodClient
	t.Cleanup(func() { newWorkspacePublishPodClient = originalClient })
	newWorkspacePublishPodClient = func() (workspacePublishPodClient, error) {
		return fakeWorkspacePublishPodClient{pod: workspacePublishTestPod(0)}, nil
	}
	if saveErr := runCacheSaveAfterBuild(context.Background()); saveErr != nil {
		t.Fatalf("save after successful build: %v", saveErr)
	}
}

func TestRunCacheRestoreExchangesCapabilityAndRestoresCache(t *testing.T) {
	source := t.TempDir()
	if mkdirErr := os.MkdirAll(filepath.Join(source, "paths", "000"), 0o755); mkdirErr != nil {
		t.Fatalf("create source cache path: %v", mkdirErr)
	}
	if writeErr := os.WriteFile(filepath.Join(source, "paths", "000", "module"), []byte("cached"), 0o644); writeErr != nil {
		t.Fatalf("write source: %v", writeErr)
	}
	archive, publication, archiveErr := workspacepkg.ArchiveDirectory(context.Background(), source)
	if archiveErr != nil {
		t.Fatalf("archive source: %v", archiveErr)
	}
	defer func() { _ = archive.Close() }()
	payload, readErr := io.ReadAll(archive)
	if readErr != nil {
		t.Fatalf("read archive: %v", readErr)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/internal/workspace-helper/capabilities":
			_, _ = w.Write([]byte(`{"data":{"capability":"restore-capability"}}`))
		case "/api/internal/workspace-helper/cache/restore":
			if r.Header.Get("Authorization") != "Bearer restore-capability" {
				t.Fatalf("restore authorization=%q", r.Header.Get("Authorization"))
			}
			w.Header().Set("Content-Digest", publication.ContentDigest)
			w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
			_, _ = w.Write(payload)
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	root := configureCacheHelperForTest(t, server.URL, domain.CachePolicyPullPush)
	if restoreErr := runCacheRestore(context.Background()); restoreErr != nil {
		t.Fatalf("restore cache: %v", restoreErr)
	}
	contents, readErr := os.ReadFile(filepath.Join(root, "paths", "000", "module"))
	if readErr != nil || string(contents) != "cached" {
		t.Fatalf("restored content=%q err=%v", contents, readErr)
	}
}

func TestRunCacheSaveExchangesCapabilityAndUploadsArchive(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/internal/workspace-helper/capabilities":
			_, _ = w.Write([]byte(`{"data":{"capability":"save-capability"}}`))
		case "/api/internal/workspace-helper/cache/save":
			if r.Header.Get("Authorization") != "Bearer save-capability" || r.Header.Get("Coyote-Cache-Preset") != "go" {
				t.Fatalf("save headers=%v", r.Header)
			}
			payload, readErr := io.ReadAll(r.Body)
			if readErr != nil || len(payload) == 0 || r.ContentLength != int64(len(payload)) || !strings.HasPrefix(r.Header.Get("Content-Digest"), "sha256:") {
				t.Fatalf("save content length=%d payload=%d err=%v headers=%v", r.ContentLength, len(payload), readErr, r.Header)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	root := configureCacheHelperForTest(t, server.URL, domain.CachePolicyPullPush)
	if mkdirErr := os.MkdirAll(filepath.Join(root, "paths", "000"), 0o755); mkdirErr != nil {
		t.Fatalf("create cache path: %v", mkdirErr)
	}
	if writeErr := os.WriteFile(filepath.Join(root, "paths", "000", "module"), []byte("cached"), 0o644); writeErr != nil {
		t.Fatalf("write cache: %v", writeErr)
	}
	if saveErr := runCacheSave(context.Background()); saveErr != nil {
		t.Fatalf("save cache: %v", saveErr)
	}
}

func TestRunCacheSaveUsesKeyPreparedBeforeBuild(t *testing.T) {
	var preparedKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/internal/workspace-helper/capabilities":
			_, _ = w.Write([]byte(`{"data":{"capability":"cache-capability"}}`))
		case "/api/internal/workspace-helper/cache/restore":
			var request struct {
				CacheKey string `json:"cache_key"`
			}
			if decodeErr := json.NewDecoder(r.Body).Decode(&request); decodeErr != nil {
				t.Fatalf("decode restore request: %v", decodeErr)
			}
			preparedKey = request.CacheKey
			w.WriteHeader(http.StatusNoContent)
		case "/api/internal/workspace-helper/cache/save":
			if savedKey := r.Header.Get("Coyote-Cache-Key"); savedKey != preparedKey {
				t.Fatalf("saved key=%q, prepared key=%q", savedKey, preparedKey)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	root := configureCacheHelperForTest(t, server.URL, domain.CachePolicyPullPush)
	if restoreErr := runCacheRestore(context.Background()); restoreErr != nil {
		t.Fatalf("restore cache: %v", restoreErr)
	}
	if writeErr := os.WriteFile(filepath.Join(os.Getenv(workspaceHelperWorkspacePath), "go.sum"), []byte("example.test/dependency v2.0.0 h1:changed\n"), 0o644); writeErr != nil {
		t.Fatalf("change go.sum: %v", writeErr)
	}
	if mkdirErr := os.MkdirAll(filepath.Join(root, "paths", "000"), 0o755); mkdirErr != nil {
		t.Fatalf("create cache path: %v", mkdirErr)
	}
	if saveErr := runCacheSave(context.Background()); saveErr != nil {
		t.Fatalf("save cache: %v", saveErr)
	}
}

func TestRunCacheHelperPolicyAndConfiguration(t *testing.T) {
	if restoreErr := runCacheRestore(context.Background()); restoreErr == nil {
		t.Fatal("expected restore configuration error")
	}
	root := configureCacheHelperForTest(t, "http://127.0.0.1:1", domain.CachePolicyOff)
	if restoreErr := runCacheRestore(context.Background()); restoreErr != nil {
		t.Fatalf("off-policy restore: %v", restoreErr)
	}
	if _, statErr := os.Stat(filepath.Join(root, "paths", "000")); statErr != nil {
		t.Fatalf("cache mount directory: %v", statErr)
	}
	if saveErr := runCacheSave(context.Background()); saveErr != nil {
		t.Fatalf("off-policy save: %v", saveErr)
	}
	t.Setenv(cacheHelperPolicy, string(domain.CachePolicyPull))
	if saveErr := runCacheSave(context.Background()); saveErr != nil {
		t.Fatalf("pull-policy save: %v", saveErr)
	}
}

func TestRunCacheRestoreTreatsMissAndTransportFailuresAsSideEffects(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/internal/workspace-helper/capabilities":
			_, _ = w.Write([]byte(`{"data":{"capability":"restore-capability"}}`))
		case "/api/internal/workspace-helper/cache/restore":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	root := configureCacheHelperForTest(t, server.URL, domain.CachePolicyPull)
	if restoreErr := runCacheRestore(context.Background()); restoreErr != nil {
		t.Fatalf("cache miss: %v", restoreErr)
	}
	if _, statErr := os.Stat(filepath.Join(root, "paths", "001")); statErr != nil {
		t.Fatalf("cache mount directory: %v", statErr)
	}
	t.Setenv(workspaceHelperAPIURL, "http://127.0.0.1:1")
	if restoreErr := runCacheRestore(context.Background()); restoreErr != nil {
		t.Fatalf("cache transport failure must be non-fatal: %v", restoreErr)
	}
}

func TestRunCacheSaveTreatsTransportFailuresAsSideEffects(t *testing.T) {
	root := configureCacheHelperForTest(t, "http://127.0.0.1:1", domain.CachePolicyPush)
	if mkdirErr := os.MkdirAll(filepath.Join(root, "paths", "000"), 0o755); mkdirErr != nil {
		t.Fatalf("create cache path: %v", mkdirErr)
	}
	if saveErr := runCacheSave(context.Background()); saveErr != nil {
		t.Fatalf("cache transport failure must be non-fatal: %v", saveErr)
	}
}

func TestRestoreCacheArchiveRecreatesMountPathsAfterFailure(t *testing.T) {
	root := t.TempDir()
	size := int64(7)
	publication := domain.WorkspaceRevisionPublication{ContentDigest: "sha256:invalid", SizeBytes: &size}
	if restoreErr := restoreCacheArchive(context.Background(), strings.NewReader("invalid"), publication, root, "go", "."); restoreErr == nil {
		t.Fatal("expected invalid archive error")
	}
	for _, path := range []string{"paths/000", "paths/001"} {
		if _, statErr := os.Stat(filepath.Join(root, path)); statErr != nil {
			t.Fatalf("cache mount directory %s: %v", path, statErr)
		}
	}
}

func configureCacheHelperForTest(t *testing.T, apiURL string, policy domain.CachePolicy) string {
	t.Helper()
	workspaceRoot := t.TempDir()
	if writeErr := os.WriteFile(filepath.Join(workspaceRoot, "go.mod"), []byte("module example.test/cache\n"), 0o644); writeErr != nil {
		t.Fatalf("write go.mod: %v", writeErr)
	}
	if writeErr := os.WriteFile(filepath.Join(workspaceRoot, "go.sum"), []byte("example.test/dependency v1.0.0 h1:checksum\n"), 0o644); writeErr != nil {
		t.Fatalf("write go.sum: %v", writeErr)
	}
	root := t.TempDir()
	stateRoot := t.TempDir()
	tokenPath := filepath.Join(t.TempDir(), "token")
	if writeErr := os.WriteFile(tokenPath, []byte("projected-token"), 0o600); writeErr != nil {
		t.Fatalf("write token: %v", writeErr)
	}
	t.Setenv(workspaceHelperAPIURL, apiURL)
	t.Setenv(workspaceHelperTokenPath, tokenPath)
	t.Setenv(workspaceHelperExecutionJobID, "execution-job")
	t.Setenv(workspaceHelperPodUID, "pod-uid")
	t.Setenv(workspaceHelperWorkspacePath, workspaceRoot)
	t.Setenv(cacheHelperRoot, root)
	t.Setenv(cacheHelperStateRoot, stateRoot)
	t.Setenv(cacheHelperPreset, "go")
	t.Setenv(cacheHelperPolicy, string(policy))
	t.Setenv(cacheHelperWorkingDir, ".")
	return root
}
