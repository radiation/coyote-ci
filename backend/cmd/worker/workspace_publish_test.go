package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

func TestRunWorkspacePublishExchangesCapabilityAndUploadsArchive(t *testing.T) {
	workspacePath := t.TempDir()
	if writeErr := os.WriteFile(filepath.Join(workspacePath, "output.txt"), []byte("output"), 0o644); writeErr != nil {
		t.Fatalf("write workspace: %v", writeErr)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/internal/workspace-helper/capabilities":
			_, _ = w.Write([]byte(`{"data":{"capability":"publish-capability"}}`))
		case "/api/internal/workspace-helper/publish":
			if r.Header.Get("Authorization") != "Bearer publish-capability" || r.Header.Get("Coyote-Execution-Job-ID") != "execution-job" {
				t.Fatalf("unexpected publish headers")
			}
			payload, readErr := io.ReadAll(r.Body)
			if readErr != nil || len(payload) == 0 {
				t.Fatalf("archive payload len=%d err=%v", len(payload), readErr)
			}
			digest := sha256.Sum256(payload)
			_, _ = w.Write([]byte(`{"data":{"revision_id":"` + domain.WorkspaceRevisionIDForExecutionJob("execution-job") + `","content_digest":"sha256:` + hex.EncodeToString(digest[:]) + `","size_bytes":` + strconv.Itoa(len(payload)) + `}}`))
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	tokenPath := filepath.Join(t.TempDir(), "projected-token")
	if writeErr := os.WriteFile(tokenPath, []byte("projected-token"), 0o600); writeErr != nil {
		t.Fatalf("write token: %v", writeErr)
	}
	t.Setenv(workspaceHelperAPIURL, server.URL)
	t.Setenv(workspaceHelperTokenPath, tokenPath)
	t.Setenv(workspaceHelperExecutionJobID, "execution-job")
	t.Setenv(workspaceHelperPodUID, "pod")
	t.Setenv(workspaceHelperWorkspacePath, workspacePath)
	if publishErr := runWorkspacePublish(context.Background()); publishErr != nil {
		t.Fatalf("publish workspace: %v", publishErr)
	}
}

func TestRunWorkspacePublishRejectsMismatchedPublicationMetadata(t *testing.T) {
	for _, response := range []string{
		`{"data":{"revision_id":"` + domain.WorkspaceRevisionIDForExecutionJob("execution-job") + `","content_digest":"sha256:mismatch","size_bytes":1}}`,
		`{"data":{"revision_id":"` + domain.WorkspaceRevisionIDForExecutionJob("execution-job") + `","content_digest":"%s","size_bytes":0}}`,
	} {
		t.Run(response, func(t *testing.T) {
			workspacePath := t.TempDir()
			if writeErr := os.WriteFile(filepath.Join(workspacePath, "output.txt"), []byte("output"), 0o644); writeErr != nil {
				t.Fatalf("write workspace: %v", writeErr)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/internal/workspace-helper/capabilities" {
					_, _ = w.Write([]byte(`{"data":{"capability":"publish-capability"}}`))
					return
				}
				payload, readErr := io.ReadAll(r.Body)
				if readErr != nil {
					t.Fatalf("read upload: %v", readErr)
				}
				digest := sha256.Sum256(payload)
				responseBody := fmt.Sprintf(response, "sha256:"+hex.EncodeToString(digest[:]))
				_, _ = w.Write([]byte(responseBody))
			}))
			defer server.Close()
			tokenPath := filepath.Join(t.TempDir(), "projected-token")
			if writeErr := os.WriteFile(tokenPath, []byte("projected-token"), 0o600); writeErr != nil {
				t.Fatalf("write token: %v", writeErr)
			}
			t.Setenv(workspaceHelperAPIURL, server.URL)
			t.Setenv(workspaceHelperTokenPath, tokenPath)
			t.Setenv(workspaceHelperExecutionJobID, "execution-job")
			t.Setenv(workspaceHelperPodUID, "pod")
			t.Setenv(workspaceHelperWorkspacePath, workspacePath)
			if publishErr := runWorkspacePublish(context.Background()); publishErr == nil {
				t.Fatal("expected publication metadata mismatch")
			}
		})
	}
}

func TestRunWorkspacePublishRejectsMissingWorkspaceAndServerFailure(t *testing.T) {
	if publishErr := runWorkspacePublish(context.Background()); publishErr == nil {
		t.Fatal("expected missing configuration error")
	}
	workspacePath := t.TempDir()
	if writeErr := os.WriteFile(filepath.Join(workspacePath, "output.txt"), []byte("output"), 0o644); writeErr != nil {
		t.Fatalf("write workspace: %v", writeErr)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/internal/workspace-helper/capabilities" {
			_, _ = w.Write([]byte(`{"data":{"capability":"publish-capability"}}`))
			return
		}
		w.WriteHeader(http.StatusConflict)
	}))
	defer server.Close()
	tokenPath := filepath.Join(t.TempDir(), "projected-token")
	if writeErr := os.WriteFile(tokenPath, []byte("projected-token"), 0o600); writeErr != nil {
		t.Fatalf("write token: %v", writeErr)
	}
	t.Setenv(workspaceHelperAPIURL, server.URL)
	t.Setenv(workspaceHelperTokenPath, tokenPath)
	t.Setenv(workspaceHelperExecutionJobID, "execution-job")
	t.Setenv(workspaceHelperPodUID, "pod")
	t.Setenv(workspaceHelperWorkspacePath, workspacePath)
	if publishErr := runWorkspacePublish(context.Background()); publishErr == nil {
		t.Fatal("expected server rejection")
	}
}

func TestRunWorkspacePublishRejectsLocalAndServerFailures(t *testing.T) {
	for _, testCase := range []struct {
		name          string
		workspacePath string
		tokenPath     string
		capability    int
		publish       string
	}{
		{name: "unreadable token", workspacePath: t.TempDir(), tokenPath: filepath.Join(t.TempDir(), "missing-token")},
		{name: "capability rejection", workspacePath: t.TempDir(), tokenPath: filepath.Join(t.TempDir(), "token"), capability: http.StatusUnauthorized},
		{name: "invalid workspace", workspacePath: filepath.Join(t.TempDir(), "workspace-file"), tokenPath: filepath.Join(t.TempDir(), "token")},
		{name: "malformed publication", workspacePath: t.TempDir(), tokenPath: filepath.Join(t.TempDir(), "token"), publish: "{"},
		{name: "invalid publication", workspacePath: t.TempDir(), tokenPath: filepath.Join(t.TempDir(), "token"), publish: `{"data":{"revision_id":"","content_digest":"","size_bytes":-1}}`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if testCase.name == "invalid workspace" {
				if writeErr := os.WriteFile(testCase.workspacePath, []byte("not a directory"), 0o600); writeErr != nil {
					t.Fatalf("write workspace file: %v", writeErr)
				}
			} else if writeErr := os.WriteFile(filepath.Join(testCase.workspacePath, "output.txt"), []byte("output"), 0o644); writeErr != nil {
				t.Fatalf("write workspace: %v", writeErr)
			}
			if testCase.tokenPath != "" && testCase.name != "unreadable token" {
				if writeErr := os.WriteFile(testCase.tokenPath, []byte("projected-token"), 0o600); writeErr != nil {
					t.Fatalf("write token: %v", writeErr)
				}
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/internal/workspace-helper/capabilities" {
					if testCase.capability != 0 {
						w.WriteHeader(testCase.capability)
						return
					}
					_, _ = w.Write([]byte(`{"data":{"capability":"publish-capability"}}`))
					return
				}
				if testCase.publish != "" {
					_, _ = w.Write([]byte(testCase.publish))
				}
			}))
			defer server.Close()
			t.Setenv(workspaceHelperAPIURL, server.URL)
			t.Setenv(workspaceHelperTokenPath, testCase.tokenPath)
			t.Setenv(workspaceHelperExecutionJobID, "execution-job")
			t.Setenv(workspaceHelperPodUID, "pod")
			t.Setenv(workspaceHelperWorkspacePath, testCase.workspacePath)
			if publishErr := runWorkspacePublish(context.Background()); publishErr == nil {
				t.Fatal("expected publish failure")
			}
		})
	}
}
