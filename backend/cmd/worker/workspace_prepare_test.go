package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	workspacepkg "github.com/radiation/coyote-ci/backend/internal/workspace"
)

func TestDownloadAndRestoreWorkspace(t *testing.T) {
	sourceRoot := t.TempDir()
	if writeErr := os.WriteFile(filepath.Join(sourceRoot, "input.txt"), []byte("prepared"), 0o644); writeErr != nil {
		t.Fatalf("write source file: %v", writeErr)
	}
	archive, publication, archiveErr := workspacepkg.ArchiveDirectory(context.Background(), sourceRoot)
	if archiveErr != nil {
		t.Fatalf("archive source: %v", archiveErr)
	}
	defer func() { _ = archive.Close() }()
	payload, readErr := io.ReadAll(archive)
	if readErr != nil {
		t.Fatalf("read archive: %v", readErr)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/internal/workspace-helper/prepare" || r.Header.Get("Authorization") != "Bearer capability" {
			t.Errorf("unexpected prepare request path=%s authorization=%q", r.URL.Path, r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Digest", publication.ContentDigest)
		w.Header().Set("Content-Length", ""+strconv.FormatInt(int64(len(payload)), 10))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	}))
	defer server.Close()
	destination := filepath.Join(t.TempDir(), "workspace")
	if restoreErr := downloadAndRestoreWorkspace(context.Background(), server.URL, "capability", "execution-job", "pod", destination); restoreErr != nil {
		t.Fatalf("download and restore: %v", restoreErr)
	}
	contents, readErr := os.ReadFile(filepath.Join(destination, "input.txt"))
	if readErr != nil {
		t.Fatalf("read restored file: %v", readErr)
	}
	if string(contents) != "prepared" {
		t.Fatalf("restored contents = %q, want prepared", contents)
	}
}

func TestRunWorkspacePrepareExchangesCapabilityAndRestoresWorkspace(t *testing.T) {
	archive, publication := workspacePrepareArchiveForTest(t, "prepared")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/internal/workspace-helper/capabilities":
			if r.Header.Get("Authorization") != "Bearer projected-token" {
				t.Fatalf("exchange authorization = %q", r.Header.Get("Authorization"))
			}
			_, _ = w.Write([]byte(`{"data":{"capability":"prepare-capability"}}`))
		case "/api/internal/workspace-helper/prepare":
			if r.Header.Get("Authorization") != "Bearer prepare-capability" {
				t.Fatalf("prepare authorization = %q", r.Header.Get("Authorization"))
			}
			w.Header().Set("Content-Digest", publication.ContentDigest)
			w.Header().Set("Content-Length", strconv.FormatInt(int64(len(archive)), 10))
			_, _ = w.Write(archive)
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	tokenPath := filepath.Join(t.TempDir(), "projected-token")
	if writeErr := os.WriteFile(tokenPath, []byte("projected-token\n"), 0o600); writeErr != nil {
		t.Fatalf("write projected token: %v", writeErr)
	}
	destination := filepath.Join(t.TempDir(), "workspace")
	t.Setenv(workspaceHelperAPIURL, server.URL)
	t.Setenv(workspaceHelperTokenPath, tokenPath)
	t.Setenv(workspaceHelperExecutionJobID, "execution-job")
	t.Setenv(workspaceHelperPodUID, "pod")
	t.Setenv(workspaceHelperDestination, destination)
	if prepareErr := runWorkspacePrepare(context.Background()); prepareErr != nil {
		t.Fatalf("run workspace prepare: %v", prepareErr)
	}
	contents, readErr := os.ReadFile(filepath.Join(destination, "input.txt"))
	if readErr != nil || string(contents) != "prepared" {
		t.Fatalf("restored contents=%q err=%v", contents, readErr)
	}
}

func TestRunWorkspacePrepareRejectsMissingConfiguration(t *testing.T) {
	if prepareErr := runWorkspacePrepare(context.Background()); prepareErr == nil {
		t.Fatal("expected configuration error")
	}
}

func TestRunWorkspacePrepareRejectsUnreadableAndEmptyTokens(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		tokenPath string
		token     string
	}{
		{name: "unreadable", tokenPath: filepath.Join(t.TempDir(), "missing-token")},
		{name: "empty", tokenPath: filepath.Join(t.TempDir(), "token"), token: " \n"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if testCase.token != "" {
				if writeErr := os.WriteFile(testCase.tokenPath, []byte(testCase.token), 0o600); writeErr != nil {
					t.Fatalf("write token: %v", writeErr)
				}
			}
			t.Setenv(workspaceHelperAPIURL, "http://example.test")
			t.Setenv(workspaceHelperTokenPath, testCase.tokenPath)
			t.Setenv(workspaceHelperExecutionJobID, "job")
			t.Setenv(workspaceHelperPodUID, "pod")
			t.Setenv(workspaceHelperDestination, filepath.Join(t.TempDir(), "workspace"))
			if prepareErr := runWorkspacePrepare(context.Background()); prepareErr == nil {
				t.Fatal("expected token error")
			}
		})
	}
}

func TestExchangeWorkspacePrepareCapabilityRejectsFailedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	if _, exchangeErr := exchangeWorkspacePrepareCapability(context.Background(), server.URL, "projected", "job", "pod"); exchangeErr == nil {
		t.Fatal("expected exchange failure")
	}
}

func TestExchangeWorkspacePrepareCapabilityRejectsInvalidResponses(t *testing.T) {
	for _, responseBody := range []string{"{", `{"data":{}}`} {
		t.Run(responseBody, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(responseBody))
			}))
			defer server.Close()
			if _, exchangeErr := exchangeWorkspacePrepareCapability(context.Background(), server.URL, "projected", "job", "pod"); exchangeErr == nil {
				t.Fatal("expected invalid exchange response error")
			}
		})
	}
}

func TestDownloadAndRestoreWorkspaceRejectsMissingIntegrityMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("archive"))
	}))
	defer server.Close()
	if restoreErr := downloadAndRestoreWorkspace(context.Background(), server.URL, "capability", "job", "pod", filepath.Join(t.TempDir(), "workspace")); restoreErr == nil {
		t.Fatal("expected missing metadata error")
	}
}

func TestDownloadAndRestoreWorkspaceRejectsHTTPFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	if restoreErr := downloadAndRestoreWorkspace(context.Background(), server.URL, "capability", "job", "pod", filepath.Join(t.TempDir(), "workspace")); restoreErr == nil {
		t.Fatal("expected HTTP failure")
	}
}

func TestDownloadAndRestoreWorkspaceRejectsAuthoritativeDigestMismatch(t *testing.T) {
	_, publicationA := workspacePrepareArchiveForTest(t, "source-a")
	archiveB, publicationB := workspacePrepareArchiveForTest(t, "source-b")
	if publicationA.ContentDigest == publicationB.ContentDigest {
		t.Fatal("expected distinct archive digests")
	}
	server := workspacePrepareServerForTest(t, archiveB, publicationA.ContentDigest, int64(len(archiveB)))
	defer server.Close()
	destination := filepath.Join(t.TempDir(), "workspace")
	if restoreErr := downloadAndRestoreWorkspace(context.Background(), server.URL, "capability", "execution-job", "pod", destination); restoreErr == nil {
		t.Fatal("expected digest mismatch")
	}
	if _, statErr := os.Stat(destination); !os.IsNotExist(statErr) {
		t.Fatalf("destination was promoted: %v", statErr)
	}
}

func TestDownloadAndRestoreWorkspaceRejectsAuthoritativeSizeMismatch(t *testing.T) {
	archive, publication := workspacePrepareArchiveForTest(t, "source")
	server := workspacePrepareServerForTest(t, archive, publication.ContentDigest, int64(len(archive))+1)
	defer server.Close()
	destination := filepath.Join(t.TempDir(), "workspace")
	if restoreErr := downloadAndRestoreWorkspace(context.Background(), server.URL, "capability", "execution-job", "pod", destination); restoreErr == nil {
		t.Fatal("expected size mismatch")
	}
	if _, statErr := os.Stat(destination); !os.IsNotExist(statErr) {
		t.Fatalf("destination was promoted: %v", statErr)
	}
}

func workspacePrepareArchiveForTest(t *testing.T, contents string) ([]byte, domain.WorkspaceRevisionPublication) {
	t.Helper()
	sourceRoot := t.TempDir()
	if writeErr := os.WriteFile(filepath.Join(sourceRoot, "input.txt"), []byte(contents), 0o644); writeErr != nil {
		t.Fatalf("write source file: %v", writeErr)
	}
	archive, publication, archiveErr := workspacepkg.ArchiveDirectory(context.Background(), sourceRoot)
	if archiveErr != nil {
		t.Fatalf("archive source: %v", archiveErr)
	}
	defer func() { _ = archive.Close() }()
	payload, readErr := io.ReadAll(archive)
	if readErr != nil {
		t.Fatalf("read archive: %v", readErr)
	}
	return payload, publication
}

func workspacePrepareServerForTest(t *testing.T, archive []byte, digest string, size int64) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Digest", digest)
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(archive)
	}))
}
