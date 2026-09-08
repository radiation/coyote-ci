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
	"strings"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

func TestRunArtifactCollectAfterBuildUploadsPlannedFiles(t *testing.T) {
	var uploads []struct {
		path   string
		stepID string
		body   string
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/internal/workspace-helper/capabilities":
			var request struct {
				Role string `json:"role"`
			}
			if decodeErr := json.NewDecoder(r.Body).Decode(&request); decodeErr != nil || request.Role != string(domain.WorkspaceHelperRoleArtifactCollect) || r.Header.Get("Authorization") != "Bearer projected-token" {
				t.Fatalf("capability request=%+v authorization=%q err=%v", request, r.Header.Get("Authorization"), decodeErr)
			}
			_, _ = w.Write([]byte(`{"data":{"capability":"artifact-capability"}}`))
		case "/api/internal/workspace-helper/artifacts/plan":
			_, _ = w.Write([]byte(`{"data":{"collect":true,"scopes":[{"step_id":"step-1","patterns":["reports/*.txt"]},{"patterns":["shared/*.txt"]}]}}`))
		case "/api/internal/workspace-helper/artifacts/upload":
			body, readErr := io.ReadAll(r.Body)
			if readErr != nil {
				t.Fatalf("read upload: %v", readErr)
			}
			uploads = append(uploads, struct {
				path   string
				stepID string
				body   string
			}{path: r.Header.Get("Coyote-Artifact-Path"), stepID: r.Header.Get("Coyote-Step-ID"), body: string(body)})
			if r.Header.Get("Authorization") != "Bearer artifact-capability" || r.Header.Get("Coyote-Execution-Job-ID") != "execution-job" || r.Header.Get("Coyote-Pod-UID") != "pod-uid" || r.Header.Get("Coyote-Build-Succeeded") != "true" {
				t.Fatalf("upload headers=%v", r.Header)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	workspacePath := configureArtifactHelperForTest(t, server.URL)
	for path, contents := range map[string]string{"reports/result.txt": "report", "shared/output.txt": "shared", "ignored.txt": "ignored"} {
		fullPath := filepath.Join(workspacePath, path)
		if mkdirErr := os.MkdirAll(filepath.Dir(fullPath), 0o755); mkdirErr != nil {
			t.Fatalf("create directory: %v", mkdirErr)
		}
		if writeErr := os.WriteFile(fullPath, []byte(contents), 0o644); writeErr != nil {
			t.Fatalf("write artifact: %v", writeErr)
		}
	}
	t.Setenv(workspaceHelperPodName, "pod")
	t.Setenv(workspaceHelperNamespace, "ci")
	originalClient := newWorkspacePublishPodClient
	t.Cleanup(func() { newWorkspacePublishPodClient = originalClient })
	newWorkspacePublishPodClient = func() (workspacePublishPodClient, error) {
		return fakeWorkspacePublishPodClient{pod: workspacePublishTestPod(0)}, nil
	}

	if collectErr := runArtifactCollectAfterBuild(context.Background()); collectErr != nil {
		t.Fatalf("collect artifacts: %v", collectErr)
	}
	if len(uploads) != 2 || !hasArtifactUpload(uploads, "reports/result.txt", "step-1", "report") || !hasArtifactUpload(uploads, "shared/output.txt", "", "shared") {
		t.Fatalf("uploads=%+v", uploads)
	}
}

func TestRunArtifactCollectAfterBuildRejectsMissingIdentityAndClientFailure(t *testing.T) {
	if collectErr := runArtifactCollectAfterBuild(context.Background()); collectErr == nil {
		t.Fatal("expected missing identity error")
	}
	t.Setenv(workspaceHelperPodName, "pod")
	t.Setenv(workspaceHelperNamespace, "ci")
	t.Setenv(workspaceHelperPodUID, "pod-uid")
	originalClient := newWorkspacePublishPodClient
	t.Cleanup(func() { newWorkspacePublishPodClient = originalClient })
	newWorkspacePublishPodClient = func() (workspacePublishPodClient, error) {
		return nil, errors.New("Kubernetes API unavailable")
	}
	if collectErr := runArtifactCollectAfterBuild(context.Background()); collectErr == nil {
		t.Fatal("expected Pod client error")
	}
}

func TestCollectPodArtifactsDefersAndRejectsUploadFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/internal/workspace-helper/capabilities":
			_, _ = w.Write([]byte(`{"data":{"capability":"artifact-capability"}}`))
		case "/api/internal/workspace-helper/artifacts/plan":
			_, _ = w.Write([]byte(`{"data":{"collect":false}}`))
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	configureArtifactHelperForTest(t, server.URL)
	if collectErr := collectPodArtifacts(context.Background(), false); collectErr != nil {
		t.Fatalf("deferred artifact collection: %v", collectErr)
	}

	uploadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer uploadServer.Close()
	workspacePath := t.TempDir()
	if writeErr := os.WriteFile(filepath.Join(workspacePath, "report.txt"), []byte("report"), 0o644); writeErr != nil {
		t.Fatalf("write artifact: %v", writeErr)
	}
	if uploadErr := uploadArtifactScope(context.Background(), uploadServer.URL, "capability", "execution-job", "pod-uid", workspacePath, false, "step-1", []string{"*.txt"}); uploadErr == nil || !strings.Contains(uploadErr.Error(), "HTTP 500") {
		t.Fatalf("upload error=%v", uploadErr)
	}
}

func TestCollectPodArtifactsRequiresConfiguration(t *testing.T) {
	for _, key := range []string{workspaceHelperAPIURL, workspaceHelperTokenPath, workspaceHelperExecutionJobID, workspaceHelperPodUID, workspaceHelperWorkspacePath} {
		t.Setenv(key, "")
	}
	if collectErr := collectPodArtifacts(context.Background(), true); collectErr == nil {
		t.Fatal("expected configuration error")
	}
}

func configureArtifactHelperForTest(t *testing.T, apiURL string) string {
	t.Helper()
	workspacePath := t.TempDir()
	tokenPath := filepath.Join(t.TempDir(), "token")
	if writeErr := os.WriteFile(tokenPath, []byte("projected-token"), 0o600); writeErr != nil {
		t.Fatalf("write token: %v", writeErr)
	}
	t.Setenv(workspaceHelperAPIURL, apiURL)
	t.Setenv(workspaceHelperTokenPath, tokenPath)
	t.Setenv(workspaceHelperExecutionJobID, "execution-job")
	t.Setenv(workspaceHelperPodUID, "pod-uid")
	t.Setenv(workspaceHelperWorkspacePath, workspacePath)
	return workspacePath
}

func hasArtifactUpload(uploads []struct {
	path   string
	stepID string
	body   string
}, path, stepID, body string) bool {
	for _, upload := range uploads {
		if upload.path == path && upload.stepID == stepID && upload.body == body {
			return true
		}
	}
	return false
}
