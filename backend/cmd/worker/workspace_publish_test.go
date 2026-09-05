package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func TestRunWorkspacePublishAfterBuildPublishesOnlyAfterTrustedSuccess(t *testing.T) {
	for _, testCase := range []struct {
		name          string
		exitCode      int32
		wantPublished bool
	}{
		{name: "success", exitCode: 0, wantPublished: true},
		{name: "build failure", exitCode: 7, wantPublished: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			workspacePath := t.TempDir()
			if writeErr := os.WriteFile(filepath.Join(workspacePath, "output.txt"), []byte("output"), 0o644); writeErr != nil {
				t.Fatalf("write workspace: %v", writeErr)
			}
			published := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/internal/workspace-helper/capabilities":
					_, _ = w.Write([]byte(`{"data":{"capability":"publish-capability"}}`))
				case "/api/internal/workspace-helper/publish":
					published = true
					payload, readErr := io.ReadAll(r.Body)
					if readErr != nil {
						t.Fatalf("read publication: %v", readErr)
					}
					digest := sha256.Sum256(payload)
					_, _ = w.Write([]byte(`{"data":{"revision_id":"` + domain.WorkspaceRevisionIDForExecutionJob("execution-job") + `","content_digest":"sha256:` + hex.EncodeToString(digest[:]) + `","size_bytes":` + strconv.Itoa(len(payload)) + `}}`))
				}
			}))
			defer server.Close()
			tokenPath := filepath.Join(t.TempDir(), "projected-token")
			if writeErr := os.WriteFile(tokenPath, []byte("projected-token"), 0o600); writeErr != nil {
				t.Fatalf("write token: %v", writeErr)
			}
			originalClient := newWorkspacePublishPodClient
			newWorkspacePublishPodClient = func() (workspacePublishPodClient, error) {
				return fakeWorkspacePublishPodClient{pod: workspacePublishTestPod(testCase.exitCode)}, nil
			}
			defer func() { newWorkspacePublishPodClient = originalClient }()
			t.Setenv(workspaceHelperAPIURL, server.URL)
			t.Setenv(workspaceHelperTokenPath, tokenPath)
			t.Setenv(workspaceHelperExecutionJobID, "execution-job")
			t.Setenv(workspaceHelperPodUID, "pod-uid")
			t.Setenv(workspaceHelperWorkspacePath, workspacePath)
			t.Setenv(workspaceHelperPodName, "pod")
			t.Setenv(workspaceHelperNamespace, "ci")
			if publishErr := runWorkspacePublishAfterBuild(context.Background()); publishErr != nil {
				t.Fatalf("publish after build: %v", publishErr)
			}
			if published != testCase.wantPublished {
				t.Fatalf("published=%t", published)
			}
		})
	}
}

func TestRunWorkspacePublishAfterBuildRejectsInvalidSetup(t *testing.T) {
	if publishErr := runWorkspacePublishAfterBuild(context.Background()); publishErr == nil {
		t.Fatal("expected missing Pod identity error")
	}
	t.Setenv(workspaceHelperPodName, "pod")
	t.Setenv(workspaceHelperNamespace, "ci")
	t.Setenv(workspaceHelperPodUID, "pod-uid")
	originalClient := newWorkspacePublishPodClient
	defer func() { newWorkspacePublishPodClient = originalClient }()
	clientErr := errors.New("client unavailable")
	newWorkspacePublishPodClient = func() (workspacePublishPodClient, error) { return nil, clientErr }
	if publishErr := runWorkspacePublishAfterBuild(context.Background()); !errors.Is(publishErr, clientErr) {
		t.Fatalf("publish error=%v", publishErr)
	}
}

func TestBuildContainerStatusRejectsUntrustedOrIncompletePods(t *testing.T) {
	for _, testCase := range []struct {
		name string
		pod  *corev1.Pod
		want string
	}{
		{name: "UID mismatch", pod: workspacePublishTestPod(0), want: "workspace publish Pod UID does not match helper identity"},
		{name: "missing build", pod: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: "pod-uid"}}, want: "workspace publish Pod has no build container"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			_, _, statusErr := buildContainerStatus(testCase.pod, "different-uid")
			if testCase.name == "missing build" {
				_, _, statusErr = buildContainerStatus(testCase.pod, "pod-uid")
			}
			if statusErr == nil || statusErr.Error() != testCase.want {
				t.Fatalf("status error=%v, want %q", statusErr, testCase.want)
			}
		})
	}
}

func TestBuildContainerStatusReportsPendingAndTerminatedStates(t *testing.T) {
	if terminal, succeeded, statusErr := buildContainerStatus(workspacePublishTestPod(0), "pod-uid"); statusErr != nil || !terminal || !succeeded {
		t.Fatalf("success terminal=%t succeeded=%t error=%v", terminal, succeeded, statusErr)
	}
	pending := workspacePublishTestPod(0)
	pending.Status.ContainerStatuses = nil
	if terminal, succeeded, statusErr := buildContainerStatus(pending, "pod-uid"); statusErr != nil || terminal || succeeded {
		t.Fatalf("pending terminal=%t succeeded=%t error=%v", terminal, succeeded, statusErr)
	}
	if _, _, statusErr := buildContainerStatus(nil, "pod-uid"); statusErr == nil {
		t.Fatal("expected nil Pod error")
	}
}

func TestWaitForSuccessfulBuildFailsSafely(t *testing.T) {
	lookupErr := errors.New("Kubernetes unavailable")
	if _, waitErr := waitForSuccessfulBuild(context.Background(), fakeWorkspacePublishPodClient{err: lookupErr}, "pod", "pod-uid"); !errors.Is(waitErr, lookupErr) {
		t.Fatalf("wait error=%v", waitErr)
	}
	pending := workspacePublishTestPod(0)
	pending.Status.ContainerStatuses = nil
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, waitErr := waitForSuccessfulBuild(canceled, fakeWorkspacePublishPodClient{pod: pending}, "pod", "pod-uid"); !errors.Is(waitErr, context.Canceled) {
		t.Fatalf("pending wait error=%v", waitErr)
	}
	if _, waitErr := waitForSuccessfulBuild(context.Background(), nil, "pod", "pod-uid"); waitErr == nil {
		t.Fatal("expected nil client error")
	}
}

type fakeWorkspacePublishPodClient struct {
	pod *corev1.Pod
	err error
}

func (c fakeWorkspacePublishPodClient) Get(context.Context, string, metav1.GetOptions) (*corev1.Pod, error) {
	return c.pod, c.err
}

func workspacePublishTestPod(exitCode int32) *corev1.Pod {
	return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod", Namespace: "ci", UID: "pod-uid"}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "build"}}}, Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{Name: "build", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: exitCode}}}}}}
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
