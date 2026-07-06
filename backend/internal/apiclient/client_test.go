package apiclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func TestClient_GetMeSendsBearerAndUserAgent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/me" {
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("expected bearer header, got %q", got)
		}
		if got := r.Header.Get("User-Agent"); got != "coyote/dev" {
			t.Fatalf("expected user agent, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"auth_mode":"oidc","auth_method":"api_token","email_verified":true,"user":{"id":"user-1","email":"dev@example.com","global_role":"admin"}}}`))
	}))
	defer server.Close()

	client, err := New(server.URL, "test-token", "coyote/dev", nil)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	me, err := client.GetMe(context.Background())
	if err != nil {
		t.Fatalf("get me: %v", err)
	}
	if me.AuthMethod != "api_token" || me.User.ID != "user-1" {
		t.Fatalf("unexpected response: %+v", me)
	}
}

func TestClient_BuildInspectionMethods(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.String() {
		case "/api/builds/build-1":
			_, _ = w.Write([]byte(`{"data":{"id":"build-1","project_id":"project-1","project_name":"Coyote","job_id":"job-1","status":"failed","created_at":"2026-07-04T00:00:00Z","queued_at":null,"started_at":"2026-07-04T00:00:10Z","finished_at":"2026-07-04T00:01:10Z","current_step_index":1,"attempt_number":1,"error_message":null,"trigger_type":"manual","trigger_kind":"manual","image":{"source_kind":"external"}}}`))
		case "/api/builds/build-1/steps":
			_, _ = w.Write([]byte(`{"data":{"build_id":"build-1","steps":[{"id":"step-1","build_id":"build-1","step_index":1,"name":"test","command":"go test ./...","status":"failed","image":{"source_kind":"external"},"job":{"id":"job-exec-1","build_id":"build-1","step_id":"step-1","name":"test","step_index":1,"attempt_number":1,"status":"failed","image":"golang:1.24","working_dir":"/workspace","command":["go","test","./..."],"command_preview":"go test ./...","environment":{},"spec_version":1,"created_at":"2026-07-04T00:00:00Z","outputs":[]},"worker_id":null,"started_at":"2026-07-04T00:00:10Z","finished_at":"2026-07-04T00:01:10Z","exit_code":1,"stdout":null,"stderr":null,"error_message":null}]}}`))
		case "/api/builds/build-1/logs?failed=true&tail=5":
			_, _ = w.Write([]byte(`{"data":{"build_id":"build-1","selected_step":{"step_index":1,"name":"test","status":"failed","exit_code":1},"logs":[{"step_index":1,"step_name":"test","timestamp":"2026-07-04T00:01:00Z","stream":"stderr","line":"FAIL","message":"FAIL"}],"truncated":false}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := New(server.URL, "test-token", "coyote/dev", nil)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	build, buildErr := client.GetBuild(context.Background(), "build-1")
	if buildErr != nil {
		t.Fatalf("get build: %v", buildErr)
	}
	steps, stepsErr := client.GetBuildSteps(context.Background(), "build-1")
	if stepsErr != nil {
		t.Fatalf("get build steps: %v", stepsErr)
	}
	tail := 5
	logs, logsErr := client.GetBuildLogs(context.Background(), "build-1", BuildLogsOptions{Failed: true, Tail: tail})
	if logsErr != nil {
		t.Fatalf("get build logs: %v", logsErr)
	}

	if build.ID != "build-1" || build.ProjectID != "project-1" {
		t.Fatalf("unexpected build response: %+v", build)
	}
	if len(steps) != 1 || steps[0].Job == nil || steps[0].Job.Name != "test" {
		t.Fatalf("unexpected step response: %+v", steps)
	}
	if logs.BuildID != "build-1" || logs.SelectedStep == nil || logs.SelectedStep.Name != "test" || len(logs.Logs) != 1 {
		t.Fatalf("unexpected logs response: %+v", logs)
	}
}

func TestClient_ProjectAndJobDiscoveryMethods(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.String() {
		case "/base/api/projects":
			_, _ = w.Write([]byte(`{"data":{"projects":[{"id":"project-1","name":"Platform","slug":"platform","created_at":"2026-07-06T00:00:00Z","updated_at":"2026-07-06T00:00:00Z"}]}}`))
		case "/base/api/projects/platform":
			_, _ = w.Write([]byte(`{"data":{"id":"project-1","name":"Platform","slug":"platform","created_at":"2026-07-06T00:00:00Z","updated_at":"2026-07-06T00:00:00Z"}}`))
		case "/base/api/projects/platform/jobs":
			_, _ = w.Write([]byte(`{"data":{"jobs":[{"id":"job-1","project_id":"project-1","name":"backend-ci","priority":5,"repository_url":"https://github.com/example/backend.git","default_ref":"main","push_enabled":true,"trigger_mode":"branches","pipeline_yaml":"version: 1","enabled":true,"created_at":"2026-07-06T00:00:00Z","updated_at":"2026-07-06T00:00:00Z"}]}}`))
		case "/base/api/jobs/job%2Fname?project=platform%2Fteam":
			_, _ = w.Write([]byte(`{"data":{"id":"job-1","project_id":"project-1","name":"job/name","priority":5,"repository_url":"https://github.com/example/backend.git","default_ref":"main","push_enabled":true,"trigger_mode":"branches","pipeline_yaml":"version: 1","enabled":true,"created_at":"2026-07-06T00:00:00Z","updated_at":"2026-07-06T00:00:00Z"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := New(server.URL+"/base", "test-token", "coyote/dev", nil)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	projects, listProjectsErr := client.ListProjects(context.Background())
	if listProjectsErr != nil {
		t.Fatalf("list projects: %v", listProjectsErr)
	}
	project, getProjectErr := client.GetProject(context.Background(), "platform")
	if getProjectErr != nil {
		t.Fatalf("get project: %v", getProjectErr)
	}
	jobs, listJobsErr := client.ListJobs(context.Background(), "platform")
	if listJobsErr != nil {
		t.Fatalf("list jobs: %v", listJobsErr)
	}
	job, getJobErr := client.GetJob(context.Background(), "job/name", GetJobOptions{Project: "platform/team"})
	if getJobErr != nil {
		t.Fatalf("get job: %v", getJobErr)
	}

	if len(projects.Projects) != 1 || projects.Projects[0].Slug != "platform" {
		t.Fatalf("unexpected project list: %+v", projects)
	}
	if project.ID != "project-1" || project.Slug != "platform" {
		t.Fatalf("unexpected project detail: %+v", project)
	}
	if len(jobs.Jobs) != 1 || jobs.Jobs[0].ID != "job-1" {
		t.Fatalf("unexpected job list: %+v", jobs)
	}
	if job.Name != "job/name" || job.ProjectID != "project-1" {
		t.Fatalf("unexpected job detail: %+v", job)
	}
}

func TestClient_ProjectAndJobDiscoveryTypedErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":"missing_token_scope","message":"api token does not have the required scope: build:read"}}`))
	}))
	defer server.Close()

	client, err := New(server.URL, "test-token", "coyote/dev", nil)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	for _, call := range []struct {
		name string
		run  func() error
	}{
		{name: "list projects", run: func() error { _, err := client.ListProjects(context.Background()); return err }},
		{name: "get project", run: func() error { _, err := client.GetProject(context.Background(), "platform"); return err }},
		{name: "list jobs", run: func() error { _, err := client.ListJobs(context.Background(), "platform"); return err }},
		{name: "get job", run: func() error {
			_, err := client.GetJob(context.Background(), "job-1", GetJobOptions{Project: "platform"})
			return err
		}},
	} {
		t.Run(call.name, func(t *testing.T) {
			err := call.run()
			var apiErr *Error
			if !errors.As(err, &apiErr) {
				t.Fatalf("expected typed api error, got %T", err)
			}
			if apiErr.Kind != ErrorKindAuthorization || apiErr.Code != "missing_token_scope" {
				t.Fatalf("unexpected api error: %+v", apiErr)
			}
		})
	}
}

func TestClient_ProjectAndJobDiscoveryCancellation(t *testing.T) {
	client, err := New("https://example.com/base", "token", "agent", &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return nil, context.Canceled
	})})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	for _, call := range []struct {
		name string
		run  func() error
	}{
		{name: "list projects", run: func() error { _, err := client.ListProjects(context.Background()); return err }},
		{name: "get project", run: func() error { _, err := client.GetProject(context.Background(), "platform"); return err }},
		{name: "list jobs", run: func() error { _, err := client.ListJobs(context.Background(), "platform"); return err }},
		{name: "get job", run: func() error {
			_, err := client.GetJob(context.Background(), "job-1", GetJobOptions{Project: "platform"})
			return err
		}},
	} {
		t.Run(call.name, func(t *testing.T) {
			if err := call.run(); !errors.Is(err, context.Canceled) {
				t.Fatalf("expected context canceled, got %v", err)
			}
		})
	}
}

func TestClient_BuildArtifactMethods(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.String() {
		case "/api/builds/build-1/artifacts":
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				t.Fatalf("expected bearer header, got %q", got)
			}
			_, _ = w.Write([]byte(`{"data":{"build_id":"build-1","artifacts":[{"id":"artifact-1","build_id":"build-1","name":"report.xml","path":"reports/report.xml","size_bytes":42,"content_type":"application/xml","storage_provider":"filesystem","download_url_path":"/builds/build-1/artifacts/artifact-1/download","created_at":"2026-07-05T00:00:00Z"}]}}`))
		case "/api/builds/build-1/artifacts/artifact-1/download":
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				t.Fatalf("expected bearer header, got %q", got)
			}
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte("artifact-body"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := New(server.URL, "test-token", "coyote/dev", nil)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	artifacts, listErr := client.ListBuildArtifacts(context.Background(), "build-1")
	if listErr != nil {
		t.Fatalf("list build artifacts: %v", listErr)
	}
	if artifacts.BuildID != "build-1" || len(artifacts.Artifacts) != 1 || artifacts.Artifacts[0].Path != "reports/report.xml" {
		t.Fatalf("unexpected artifacts response: %+v", artifacts)
	}

	var body bytes.Buffer
	if downloadErr := client.DownloadBuildArtifact(context.Background(), "build-1", "artifact-1", &body); downloadErr != nil {
		t.Fatalf("download build artifact: %v", downloadErr)
	}
	if body.String() != "artifact-body" {
		t.Fatalf("unexpected artifact body: %q", body.String())
	}
}

func TestClient_DownloadBuildArtifactReturnsTypedError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":"missing_token_scope","message":"api token does not have the required scope: artifact:read"}}`))
	}))
	defer server.Close()

	client, err := New(server.URL, "test-token", "coyote/dev", nil)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	var sink bytes.Buffer
	err = client.DownloadBuildArtifact(context.Background(), "build-1", "artifact-1", &sink)
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected typed api error, got %T", err)
	}
	if apiErr.Kind != ErrorKindAuthorization || apiErr.Code != "missing_token_scope" {
		t.Fatalf("unexpected api error: %+v", apiErr)
	}
}

func TestClient_ListBuildArtifactsReturnsTypedError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":"missing_token_scope","message":"api token does not have the required scope: artifact:read"}}`))
	}))
	defer server.Close()

	client, err := New(server.URL, "test-token", "coyote/dev", nil)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	_, err = client.ListBuildArtifacts(context.Background(), "build-1")
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected typed api error, got %T", err)
	}
	if apiErr.Kind != ErrorKindAuthorization || apiErr.Code != "missing_token_scope" {
		t.Fatalf("unexpected api error: %+v", apiErr)
	}
}

func TestClient_DownloadBuildArtifactNilWriterAndWriterFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-ID", "req-alt")
		_, _ = w.Write([]byte("artifact-body"))
	}))
	defer server.Close()

	client, err := New(server.URL, "", "", nil)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if nilWriterErr := client.DownloadBuildArtifact(context.Background(), "build-1", "artifact-1", nil); nilWriterErr != nil {
		t.Fatalf("expected nil writer download to succeed, got %v", nilWriterErr)
	}

	err = client.DownloadBuildArtifact(context.Background(), "build-1", "artifact-1", failingWriter{})
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected typed api error, got %T", err)
	}
	if apiErr.Kind != ErrorKindUnexpected || apiErr.RequestID != "req-alt" || !strings.Contains(apiErr.Error(), "stream artifact response") {
		t.Fatalf("unexpected api error: %+v", apiErr)
	}
}

func TestClient_DownloadBuildArtifactTransportAndCancellation(t *testing.T) {
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	client, err := New("https://example.com", "token", "agent", &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return nil, context.Canceled
	})})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if canceledErr := client.DownloadBuildArtifact(canceledCtx, "build-1", "artifact-1", &bytes.Buffer{}); !errors.Is(canceledErr, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", canceledErr)
	}

	client, err = New("https://example.com", "token", "agent", &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return nil, errors.New("dial failed")
	})})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	err = client.DownloadBuildArtifact(context.Background(), "build-1", "artifact-1", &bytes.Buffer{})
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected typed api error, got %T", err)
	}
	if apiErr.Kind != ErrorKindTransport || !strings.Contains(apiErr.Error(), "request failed") {
		t.Fatalf("unexpected transport error: %+v", apiErr)
	}
}

func TestClient_RerunBuild(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.String() != "/api/builds/build-1/rerun" {
			t.Fatalf("unexpected request path %q", r.URL.String())
		}
		_, _ = w.Write([]byte(`{"data":{"id":"build-2","project_id":"project-1","project_name":"Coyote","job_id":"job-1","status":"queued","created_at":"2026-07-04T00:02:00Z","queued_at":"2026-07-04T00:02:00Z","started_at":null,"finished_at":null,"current_step_index":0,"attempt_number":2,"rerun_of_build_id":"build-1","error_message":null,"trigger_type":"rerun","trigger_kind":"manual","image":{"source_kind":"external"}}}`))
	}))
	defer server.Close()

	client, err := New(server.URL, "test-token", "coyote/dev", nil)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	build, rerunErr := client.RerunBuild(context.Background(), "build-1")
	if rerunErr != nil {
		t.Fatalf("rerun build: %v", rerunErr)
	}
	if build.ID != "build-2" || build.RerunOfBuildID == nil || *build.RerunOfBuildID != "build-1" || build.Status != "queued" {
		t.Fatalf("unexpected rerun response: %+v", build)
	}
}

func TestClient_RerunBuildReturnsTypedError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":"missing_token_scope","message":"api token does not have the required scope: build:run"}}`))
	}))
	defer server.Close()

	client, err := New(server.URL, "test-token", "coyote/dev", nil)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	_, rerunErr := client.RerunBuild(context.Background(), "build-1")
	var apiErr *Error
	if !errors.As(rerunErr, &apiErr) {
		t.Fatalf("expected typed api error, got %T", rerunErr)
	}
	if apiErr.Kind != ErrorKindAuthorization || apiErr.Code != "missing_token_scope" {
		t.Fatalf("unexpected api error: %+v", apiErr)
	}
}

func TestClient_GetBuildLogsWithoutExplicitTailUsesServerDefaultBehavior(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.String() != "/api/builds/build-1/logs" {
			t.Fatalf("unexpected request path %q", r.URL.String())
		}
		_, _ = w.Write([]byte(`{"data":{"build_id":"build-1","logs":[],"truncated":true}}`))
	}))
	defer server.Close()

	client, err := New(server.URL, "test-token", "coyote/dev", nil)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	logs, getErr := client.GetBuildLogs(context.Background(), "build-1", BuildLogsOptions{})
	if getErr != nil {
		t.Fatalf("get build logs: %v", getErr)
	}
	if !logs.Truncated {
		t.Fatalf("expected truncated flag to round-trip, got %+v", logs)
	}
}

func TestClient_GetBuildLogs_WithStepSelection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.String() != "/api/builds/build-1/logs?step=3&tail=2" {
			t.Fatalf("unexpected request path %q", r.URL.String())
		}
		_, _ = w.Write([]byte(`{"data":{"build_id":"build-1","logs":[],"truncated":false}}`))
	}))
	defer server.Close()

	client, err := New(server.URL, "test-token", "coyote/dev", nil)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	step := 3
	if _, getErr := client.GetBuildLogs(context.Background(), "build-1", BuildLogsOptions{Step: &step, Tail: 2}); getErr != nil {
		t.Fatalf("get build logs: %v", getErr)
	}
}

func TestClient_RequestURLPreservesPathPrefixAndQuery(t *testing.T) {
	baseURL, err := normalizeBaseURL("https://example.com/platform/coyote/")
	if err != nil {
		t.Fatalf("normalize base url: %v", err)
	}

	resolved, err := resolveRequestURL(baseURL, "api/builds?status=running")
	if err != nil {
		t.Fatalf("resolve request url: %v", err)
	}
	if resolved.String() != "https://example.com/platform/coyote/api/builds?status=running" {
		t.Fatalf("unexpected resolved url: %s", resolved.String())
	}
}

func TestNormalizeBaseURLValidation(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "root host", input: "https://ci.example.com", want: "https://ci.example.com"},
		{name: "one level prefix", input: "https://example.com/coyote", want: "https://example.com/coyote"},
		{name: "multi level prefix", input: "https://example.com/platform/coyote/", want: "https://example.com/platform/coyote"},
		{name: "embedded credentials", input: "https://user:pass@example.com/coyote", wantErr: true},
		{name: "fragment", input: "https://example.com/coyote#frag", wantErr: true},
		{name: "query", input: "https://example.com/coyote?x=1", wantErr: true},
		{name: "malformed", input: "://bad", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeBaseURL(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("normalize base url: %v", err)
			}
			if got.String() != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got.String())
			}
		})
	}
}

func TestClient_GetServerInfoReturnsTypedValidationError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-Id", "req-123")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":"invalid_request","message":"bad input"}}`))
	}))
	defer server.Close()

	client, err := New(server.URL, "", "coyote/dev", nil)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	_, err = client.GetServerInfo(context.Background())
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected api error, got %T", err)
	}
	if apiErr.Kind != ErrorKindValidation || apiErr.RequestID != "req-123" || apiErr.Code != "invalid_request" {
		t.Fatalf("unexpected api error: %+v", apiErr)
	}
}

func TestClient_GetServerInfoHandlesNonJSONServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer server.Close()

	client, err := New(server.URL, "", "coyote/dev", nil)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	_, err = client.GetServerInfo(context.Background())
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected api error, got %T", err)
	}
	if apiErr.Kind != ErrorKindServer || apiErr.Message != "boom" {
		t.Fatalf("unexpected api error: %+v", apiErr)
	}
}

func TestClient_UsesContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	client, err := New(server.URL, "", "coyote/dev", &http.Client{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = client.GetServerInfo(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
}

func TestClient_InvalidJSONResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":`))
	}))
	defer server.Close()

	client, err := New(server.URL, "", "coyote/dev", nil)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	_, err = client.GetServerInfo(context.Background())
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected api error, got %T", err)
	}
	if apiErr.Kind != ErrorKindUnexpected || !strings.Contains(apiErr.Error(), "invalid json response") {
		t.Fatalf("unexpected error: %+v", apiErr)
	}
}

func TestResolveRequestURLRejectsAbsoluteRequestURLs(t *testing.T) {
	baseURL := &url.URL{Scheme: "https", Host: "example.com", Path: "/coyote"}
	_, err := resolveRequestURL(baseURL, "https://other.example.com/api/me")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveRequestURLEmptyPathAndDownloadPathEncoding(t *testing.T) {
	baseURL := &url.URL{Scheme: "https", Host: "example.com", Path: "/coyote"}
	if _, err := resolveRequestURL(baseURL, "   "); err == nil {
		t.Fatal("expected empty path error")
	}
	if got := buildArtifactDownloadPath(" build 1 ", "artifact/1"); got != "api/builds/build%201/artifacts/artifact%2F1/download" {
		t.Fatalf("unexpected download path: %s", got)
	}
}

func TestErrorFormattingTimeoutAndHelpers(t *testing.T) {
	wrapped := errors.New("boom")
	err := &Error{StatusCode: http.StatusUnauthorized, Message: "auth failed", RequestID: "req-1", Err: wrapped}
	if err.Error() != "auth failed (request_id=req-1)" {
		t.Fatalf("unexpected error string: %q", err.Error())
	}
	if !errors.Is(err, wrapped) {
		t.Fatalf("expected unwrap to expose wrapped error")
	}

	client, newErr := New("https://example.com", "", "agent", &http.Client{})
	if newErr != nil {
		t.Fatalf("new client: %v", newErr)
	}
	if client.httpClient.Timeout != defaultTimeout {
		t.Fatalf("expected default timeout, got %v", client.httpClient.Timeout)
	}

	customClient := &http.Client{Timeout: 3 * time.Second}
	client, newErr = New("https://example.com", "", "agent", customClient)
	if newErr != nil {
		t.Fatalf("new client with custom timeout: %v", newErr)
	}
	if client.httpClient.Timeout != 3*time.Second {
		t.Fatalf("expected preserved timeout, got %v", client.httpClient.Timeout)
	}
}

func TestClientDoJSONRequestPathTransportAndNoOutput(t *testing.T) {
	client, err := New("https://example.com/coyote", "", "agent", &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("network down")
	})})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	err = client.doJSON(context.Background(), http.MethodGet, "", nil, nil)
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Kind != ErrorKindUnexpected {
		t.Fatalf("expected invalid request path error, got %v", err)
	}

	err = client.doJSON(context.Background(), http.MethodGet, "api/info", nil, nil)
	if !errors.As(err, &apiErr) || apiErr.Kind != ErrorKindTransport {
		t.Fatalf("expected transport error, got %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client, err = New(server.URL, "", "agent", nil)
	if err != nil {
		t.Fatalf("new client no output: %v", err)
	}
	if err := client.doJSON(context.Background(), http.MethodGet, "api/info", nil, nil); err != nil {
		t.Fatalf("doJSON with nil output: %v", err)
	}
}

func TestDecodeErrorResponseAndClassifyStatus(t *testing.T) {
	response := &http.Response{
		StatusCode: http.StatusBadGateway,
		Body:       io.NopCloser(strings.NewReader("")),
	}
	err := decodeErrorResponse(response, "req-2")
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Message != http.StatusText(http.StatusBadGateway) {
		t.Fatalf("unexpected decoded empty-body error: %v", err)
	}

	tests := []struct {
		status int
		want   ErrorKind
	}{
		{status: http.StatusUnauthorized, want: ErrorKindAuthentication},
		{status: http.StatusForbidden, want: ErrorKindAuthorization},
		{status: http.StatusNotFound, want: ErrorKindNotFound},
		{status: http.StatusConflict, want: ErrorKindConflict},
		{status: http.StatusBadRequest, want: ErrorKindValidation},
		{status: http.StatusUnprocessableEntity, want: ErrorKindValidation},
		{status: http.StatusInternalServerError, want: ErrorKindServer},
		{status: http.StatusTeapot, want: ErrorKindUnexpected},
	}
	for _, tc := range tests {
		if got := classifyStatus(tc.status); got != tc.want {
			t.Fatalf("classifyStatus(%d) = %q, want %q", tc.status, got, tc.want)
		}
	}

	client, err := New("https://example.com", "", "agent", nil)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if _, err := resolveRequestURL(client.baseURL, " /api/info "); err != nil {
		t.Fatalf("resolve request url with trimmed path: %v", err)
	}

	transportErr := fmt.Errorf("wrapped transport")
	wrapped := (&Error{Err: transportErr}).Unwrap()
	if wrapped != transportErr {
		t.Fatalf("unexpected unwrap value: %v", wrapped)
	}
}

func TestErrorFallbackFormattingAndWrappers(t *testing.T) {
	if (*Error)(nil).Error() != "" {
		t.Fatal("expected nil error string")
	}
	if (*Error)(nil).Unwrap() != nil {
		t.Fatal("expected nil unwrap for nil error")
	}
	err := &Error{StatusCode: http.StatusUnauthorized}
	if err.Error() != http.StatusText(http.StatusUnauthorized) {
		t.Fatalf("expected status text fallback, got %q", err.Error())
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/me":
			_, _ = w.Write([]byte(`{"data":{"auth_mode":"header","auth_method":"api_token","email_verified":true,"user":{"id":"user-3","email":"me@example.com","global_role":"user"}}}`))
		case "/api/info":
			_, _ = w.Write([]byte(`{"data":{"version":"1.0.1","commit":"abc","build_date":"2026-07-04","api_version":"0.1"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, newErr := New(server.URL, " token ", "agent", nil)
	if newErr != nil {
		t.Fatalf("new client: %v", newErr)
	}
	me, getMeErr := client.GetMe(context.Background())
	if getMeErr != nil || me.User.Email != "me@example.com" {
		t.Fatalf("unexpected get me result: %+v err=%v", me, getMeErr)
	}
	info, infoErr := client.GetServerInfo(context.Background())
	if infoErr != nil || info.Version != "1.0.1" {
		t.Fatalf("unexpected server info result: %+v err=%v", info, infoErr)
	}
}
