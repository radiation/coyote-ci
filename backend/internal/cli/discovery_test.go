package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/cli/config"
	"github.com/radiation/coyote-ci/backend/internal/cli/credentials"
)

func TestProjectDiscoveryCommands(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer stored-token" {
			t.Fatalf("expected bearer token, got %q", got)
		}
		switch r.URL.String() {
		case "/api/projects":
			_, _ = w.Write([]byte(`{"data":{"projects":[{"id":"project-1","name":"Platform","slug":"platform","description":"Core services","created_at":"2026-07-06T00:00:00Z","updated_at":"2026-07-06T00:00:00Z"}]}}`))
		case "/api/projects/platform":
			_, _ = w.Write([]byte(`{"data":{"id":"project-1","name":"Platform","slug":"platform","description":"Core services","created_at":"2026-07-06T00:00:00Z","updated_at":"2026-07-06T00:00:00Z"}}`))
		case "/api/projects/project-1":
			_, _ = w.Write([]byte(`{"data":{"id":"project-1","name":"Platform","slug":"platform","description":"Core services","created_at":"2026-07-06T00:00:00Z","updated_at":"2026-07-06T00:00:00Z"}}`))
		case "/api/projects/empty":
			_, _ = w.Write([]byte(`{"data":{"projects":[]}}`))
		case "/api/projects/missing":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"code":"not_found","message":"project not found"}}`))
		case "/api/projects/forbidden":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":{"code":"forbidden","message":"project membership is required"}}`))
		case "/api/projects/no-scope":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":{"code":"missing_token_scope","message":"api token does not have the required scope: build:read"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	configStore, creds := discoveryTestConfig(t, server.URL)

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"project", "list"}}); code != 0 {
		t.Fatalf("project list exit code %d stderr=%s", code, stderr.String())
	}
	for _, want := range []string{"Projects", "project-1", "platform", "Platform"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("expected %q in human output, got %s", want, stdout.String())
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
	stdout.Reset()
	stderr.Reset()

	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"project", "list", "--json"}}); code != 0 {
		t.Fatalf("project list json exit code %d stderr=%s", code, stderr.String())
	}
	var listPayload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &listPayload); err != nil {
		t.Fatalf("decode project list: %v", err)
	}
	projects, ok := listPayload["projects"].([]any)
	if !ok || len(projects) != 1 {
		t.Fatalf("unexpected project list payload: %+v", listPayload)
	}
	if strings.Contains(stdout.String(), "Projects") {
		t.Fatalf("unexpected human prose in project list JSON: %s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()

	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"project", "show", "platform", "--json"}}); code != 0 {
		t.Fatalf("project show slug exit code %d stderr=%s", code, stderr.String())
	}
	var slugPayload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &slugPayload); err != nil {
		t.Fatalf("decode project show slug: %v", err)
	}
	projectData, ok := slugPayload["project"].(map[string]any)
	if !ok || projectData["id"] != "project-1" || projectData["slug"] != "platform" {
		t.Fatalf("unexpected project payload: %+v", slugPayload)
	}
	stdout.Reset()
	stderr.Reset()

	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"project", "show", "project-1"}}); code != 0 {
		t.Fatalf("project show id exit code %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Slug:    platform") {
		t.Fatalf("unexpected project show output: %s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()

	code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"project", "show", "missing", "--json"}})
	if code != 6 {
		t.Fatalf("expected not found exit code 6, got %d stderr=%s", code, stderr.String())
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), "project not found") {
		t.Fatalf("unexpected unknown project result stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()

	code = Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"project", "show", "forbidden", "--json"}})
	if code != 5 {
		t.Fatalf("expected forbidden exit code 5, got %d stderr=%s", code, stderr.String())
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), "project membership is required") {
		t.Fatalf("unexpected forbidden result stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()

	code = Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"project", "show", "no-scope", "--json"}})
	if code != 5 {
		t.Fatalf("expected missing scope exit code 5, got %d stderr=%s", code, stderr.String())
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), "api token does not have the required scope: build:read") {
		t.Fatalf("unexpected missing scope result stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if strings.Contains(stderr.String(), "stored-token") {
		t.Fatalf("stderr leaked token: %s", stderr.String())
	}
	stdout.Reset()
	stderr.Reset()

	emptyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"projects":[]}}`))
	}))
	defer emptyServer.Close()
	emptyStore, emptyCreds := discoveryTestConfig(t, emptyServer.URL)
	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: emptyStore, Credentials: emptyCreds, Args: []string{"project", "list"}}); code != 0 {
		t.Fatalf("empty project list exit code %d stderr=%s", code, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != "No projects found." {
		t.Fatalf("unexpected empty list output: %q", stdout.String())
	}
}

func TestJobDiscoveryCommands(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		switch r.URL.String() {
		case "/api/projects/default/jobs":
			_, _ = w.Write([]byte(`{"data":{"jobs":[{"id":"job-1","project_id":"project-1","name":"coyote-ci","priority":5,"repository_url":"https://github.com/example/backend.git","default_ref":"main","push_enabled":true,"trigger_mode":"branches","pipeline_yaml":"version: 1\nsteps:\n  - name: test","enabled":true,"created_at":"2026-07-06T00:00:00Z","updated_at":"2026-07-06T00:00:00Z","latest_build":{"id":"build-1","build_number":14,"status":"failed","created_at":"2026-07-06T00:00:00Z"}}]}}`))
		case "/api/projects/platform/jobs":
			_, _ = w.Write([]byte(`{"data":{"jobs":[{"id":"job-1","project_id":"project-1","name":"backend-ci","priority":5,"repository_url":"https://github.com/example/backend.git","default_ref":"main","push_enabled":true,"trigger_mode":"branches","pipeline_yaml":"version: 1\nsteps:\n  - name: test","enabled":true,"created_at":"2026-07-06T00:00:00Z","updated_at":"2026-07-06T00:00:00Z","latest_build":{"id":"build-1","build_number":14,"status":"failed","created_at":"2026-07-06T00:00:00Z"}}]}}`))
		case "/api/projects/missing/jobs":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"code":"not_found","message":"project not found"}}`))
		case "/api/projects/forbidden/jobs":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":{"code":"forbidden","message":"project membership is required"}}`))
		case "/api/jobs/job-1":
			_, _ = w.Write([]byte(`{"data":{"id":"job-1","project_id":"project-1","name":"backend-ci","priority":5,"repository_url":"https://github.com/example/backend.git","default_ref":"main","push_enabled":true,"trigger_mode":"branches","pipeline_yaml":"version: 1\nsteps:\n  - name: test","enabled":true,"created_at":"2026-07-06T00:00:00Z","updated_at":"2026-07-06T00:00:00Z","latest_build":{"id":"build-1","build_number":14,"status":"failed","created_at":"2026-07-06T00:00:00Z"}}}`))
		case "/api/jobs/resolve?name=backend-ci&project=platform":
			_, _ = w.Write([]byte(`{"data":{"id":"job-1","project_id":"project-1","name":"backend-ci","priority":5,"repository_url":"https://github.com/example/backend.git","default_ref":"main","push_enabled":true,"trigger_mode":"branches","pipeline_yaml":"version: 1\nsteps:\n  - name: test","enabled":true,"created_at":"2026-07-06T00:00:00Z","updated_at":"2026-07-06T00:00:00Z"}}`))
		case "/api/jobs/resolve?name=duplicate&project=platform":
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":{"code":"ambiguous_selector","message":"job selector matched multiple jobs in project"}}`))
		case "/api/jobs/job-2":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":{"code":"missing_token_scope","message":"api token does not have the required scope: build:read"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	configStore, creds := discoveryTestConfig(t, server.URL)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"job", "list", "--project", "platform"}}); code != 0 {
		t.Fatalf("job list exit code %d stderr=%s", code, stderr.String())
	}
	for _, want := range []string{"Jobs for project platform", "job-1", "backend-ci", "#14 failed"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("expected %q in job list output, got %s", want, stdout.String())
		}
	}
	stdout.Reset()
	stderr.Reset()

	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"job", "list", "--project", "platform", "--json"}}); code != 0 {
		t.Fatalf("job list json exit code %d stderr=%s", code, stderr.String())
	}
	var listPayload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &listPayload); err != nil {
		t.Fatalf("decode job list: %v", err)
	}
	jobs, ok := listPayload["jobs"].([]any)
	if !ok || len(jobs) != 1 {
		t.Fatalf("unexpected job list payload: %+v", listPayload)
	}
	if listPayload["project_selector"] != "platform" {
		t.Fatalf("unexpected project selector payload: %+v", listPayload)
	}
	if strings.Contains(stdout.String(), "Jobs for project") {
		t.Fatalf("unexpected human prose in job list JSON: %s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()

	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"job", "show", "job-1", "--json"}}); code != 0 {
		t.Fatalf("job show id exit code %d stderr=%s", code, stderr.String())
	}
	var showPayload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &showPayload); err != nil {
		t.Fatalf("decode job show id: %v", err)
	}
	jobData, ok := showPayload["job"].(map[string]any)
	if !ok || jobData["id"] != "job-1" || jobData["project_id"] != "project-1" {
		t.Fatalf("unexpected job payload: %+v", showPayload)
	}
	stdout.Reset()
	stderr.Reset()

	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"job", "show", "backend-ci", "--project", "platform"}}); code != 0 {
		t.Fatalf("job show by name exit code %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Pipeline:   inline") {
		t.Fatalf("unexpected job show output: %s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()

	code := Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"job", "show", "backend-ci"}})
	if code != 2 {
		t.Fatalf("expected missing project exit code 2, got %d stderr=%s", code, stderr.String())
	}
	if requestCount != 4 {
		t.Fatalf("expected missing project to stop before HTTP request, got %d requests", requestCount)
	}
	if !strings.Contains(stderr.String(), "job name requires --project") {
		t.Fatalf("unexpected missing project stderr: %s", stderr.String())
	}
	stdout.Reset()
	stderr.Reset()

	code = Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"job", "list", "--project", "missing", "--json"}})
	if code != 6 {
		t.Fatalf("expected missing project exit code 6, got %d stderr=%s", code, stderr.String())
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), "project not found") {
		t.Fatalf("unexpected unknown project list result stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()

	code = Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"job", "list", "--project", "forbidden", "--json"}})
	if code != 5 {
		t.Fatalf("expected forbidden project exit code 5, got %d stderr=%s", code, stderr.String())
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), "project membership is required") {
		t.Fatalf("unexpected forbidden project list result stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()

	code = Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"job", "show", "duplicate", "--project", "platform", "--json"}})
	if code != 2 {
		t.Fatalf("expected ambiguous selector exit code 2, got %d stderr=%s", code, stderr.String())
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), "job selector matched multiple jobs in project") {
		t.Fatalf("unexpected ambiguous selector result stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()

	code = Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"job", "show", "job-2", "--json"}})
	if code != 5 {
		t.Fatalf("expected missing scope exit code 5, got %d stderr=%s", code, stderr.String())
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), "api token does not have the required scope: build:read") {
		t.Fatalf("unexpected missing scope result stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if strings.Contains(stderr.String(), "stored-token") {
		t.Fatalf("stderr leaked token: %s", stderr.String())
	}
	stdout.Reset()
	stderr.Reset()

	code = Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"job", "list", "--project", "default"}})
	if code != 0 {
		t.Fatalf("job list default selector exit code %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "coyote-ci") {
		t.Fatalf("unexpected default selector job list output: %s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()

	code = Run(Dependencies{Stdout: stdout, Stderr: stderr, ConfigStore: configStore, Credentials: creds, Args: []string{"job", "list", "--project", "default", "--ref", "main", "--yes"}})
	if code != 1 {
		t.Fatalf("expected unknown flag exit code 1, got %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "unknown flag: --ref") {
		t.Fatalf("unexpected unknown flag stderr: %s", stderr.String())
	}
}

func TestJobRunCommand(t *testing.T) {
	originalInteractiveCheck := isInteractiveInputFunc
	t.Cleanup(func() {
		isInteractiveInputFunc = originalInteractiveCheck
	})

	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		switch r.URL.String() {
		case "/api/jobs/job-1":
			_, _ = w.Write([]byte(`{"data":{"id":"job-1","project_id":"project-1","name":"backend-ci","priority":5,"repository_url":"https://github.com/example/backend.git","default_ref":"main","push_enabled":true,"trigger_mode":"branches","pipeline_yaml":"version: 1\nsteps:\n  - name: test","enabled":true,"created_at":"2026-07-06T00:00:00Z","updated_at":"2026-07-06T00:00:00Z"}}`))
		case "/api/jobs/resolve?name=coyote-ci&project=default":
			_, _ = w.Write([]byte(`{"data":{"id":"job-1","project_id":"project-1","name":"coyote-ci","priority":5,"repository_url":"https://github.com/example/backend.git","default_ref":"main","push_enabled":true,"trigger_mode":"branches","pipeline_yaml":"version: 1\nsteps:\n  - name: test","enabled":true,"created_at":"2026-07-06T00:00:00Z","updated_at":"2026-07-06T00:00:00Z"}}`))
		case "/api/jobs/resolve?name=backend-ci&project=platform":
			_, _ = w.Write([]byte(`{"data":{"id":"job-1","project_id":"project-1","name":"backend-ci","priority":5,"repository_url":"https://github.com/example/backend.git","default_ref":"main","push_enabled":true,"trigger_mode":"branches","pipeline_yaml":"version: 1\nsteps:\n  - name: test","enabled":true,"created_at":"2026-07-06T00:00:00Z","updated_at":"2026-07-06T00:00:00Z"}}`))
		case "/api/jobs/resolve?name=duplicate&project=platform":
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":{"code":"ambiguous_selector","message":"job selector matched multiple jobs in project"}}`))
		case "/api/jobs/job-1/run":
			if r.Method != http.MethodPost {
				t.Fatalf("expected POST, got %s", r.Method)
			}
			var req map[string]any
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode run request: %v", err)
			}
			if req["ref"] != "release/2026.07" && req["ref"] != "main" {
				t.Fatalf("unexpected run ref payload: %+v", req)
			}
			_, _ = w.Write([]byte(`{"data":{"id":"build-2","build_number":15,"project_id":"project-1","project_name":"Coyote CI","job_id":"job-1","status":"queued","created_at":"2026-07-06T00:01:00Z","queued_at":"2026-07-06T00:01:00Z","started_at":null,"finished_at":null,"current_step_index":0,"attempt_number":1,"error_message":null,"trigger_type":"manual","trigger_kind":"manual","source":{"repository_url":"https://github.com/example/backend.git","ref":"release/2026.07"},"image":{"source_kind":"external"}}}`))
		case "/api/jobs/job-2":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":{"code":"missing_token_scope","message":"api token does not have the required scope: build:run"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	configStore, creds := discoveryTestConfig(t, server.URL)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, Stdin: strings.NewReader("ignored"), ConfigStore: configStore, Credentials: creds, Args: []string{"job", "run", "job-1", "--ref", "release/2026.07", "--yes"}}); code != 0 {
		t.Fatalf("job run exit code %d stderr=%s", code, stderr.String())
	}
	for _, want := range []string{"Started job backend-ci", "Build:  build-2", "Status: queued", "/builds/build-2", "coyote build status build-2"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("expected %q in job run output, got %s", want, stdout.String())
		}
	}
	stdout.Reset()
	stderr.Reset()

	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, Stdin: strings.NewReader("ignored"), ConfigStore: configStore, Credentials: creds, Args: []string{"job", "run", "backend-ci", "--project", "platform", "--ref", "main", "--yes", "--json"}}); code != 0 {
		t.Fatalf("job run json exit code %d stderr=%s", code, stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode job run json: %v", err)
	}
	runPayload, ok := payload["run"].(map[string]any)
	if !ok {
		t.Fatalf("expected run object, got %+v", payload)
	}
	if runPayload["job_id"] != "job-1" || runPayload["ref"] != "main" || runPayload["build_id"] != "build-2" || runPayload["status"] != "queued" {
		t.Fatalf("unexpected run payload: %+v", payload)
	}
	if strings.Contains(stdout.String(), "Started job") {
		t.Fatalf("unexpected human prose in job run JSON: %s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()

	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, Stdin: strings.NewReader("ignored"), ConfigStore: configStore, Credentials: creds, Args: []string{"job", "show", "coyote-ci", "--project", "default"}}); code != 0 {
		t.Fatalf("job show default selector exit code %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Job:        coyote-ci") {
		t.Fatalf("unexpected default selector job show output: %s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()

	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, Stdin: strings.NewReader("ignored"), ConfigStore: configStore, Credentials: creds, Args: []string{"job", "run", "coyote-ci", "--project", "default", "--ref", "main", "--yes"}}); code != 0 {
		t.Fatalf("job run default selector exit code %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Started job coyote-ci") {
		t.Fatalf("unexpected default selector job run output: %s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()

	isInteractiveInputFunc = func(io.Reader) bool { return true }
	if code := Run(Dependencies{Stdout: stdout, Stderr: stderr, Stdin: strings.NewReader("yes\n"), ConfigStore: configStore, Credentials: creds, Args: []string{"job", "run", "job-1", "--ref", "main"}}); code != 0 {
		t.Fatalf("interactive job run exit code %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "Run job backend-ci on ref main?") {
		t.Fatalf("expected prompt in stderr, got %q", stderr.String())
	}
	stdout.Reset()
	stderr.Reset()

	code := Run(Dependencies{Stdout: stdout, Stderr: stderr, Stdin: strings.NewReader("n\n"), ConfigStore: configStore, Credentials: creds, Args: []string{"job", "run", "job-1", "--ref", "main"}})
	if code != 2 {
		t.Fatalf("expected declined confirmation exit code 2, got %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "job run canceled") {
		t.Fatalf("unexpected declined confirmation stderr: %s", stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	isInteractiveInputFunc = originalInteractiveCheck

	beforeMissingProject := requestCount
	code = Run(Dependencies{Stdout: stdout, Stderr: stderr, Stdin: strings.NewReader("ignored"), ConfigStore: configStore, Credentials: creds, Args: []string{"job", "run", "backend-ci", "--ref", "main", "--yes"}})
	if code != 2 {
		t.Fatalf("expected missing project exit code 2, got %d stderr=%s", code, stderr.String())
	}
	if requestCount != beforeMissingProject {
		t.Fatalf("expected missing project to stop before HTTP request, got %d requests", requestCount-beforeMissingProject)
	}
	if !strings.Contains(stderr.String(), "job name requires --project") {
		t.Fatalf("unexpected missing project stderr: %s", stderr.String())
	}
	stdout.Reset()
	stderr.Reset()

	beforeMissingRef := requestCount
	code = Run(Dependencies{Stdout: stdout, Stderr: stderr, Stdin: strings.NewReader("ignored"), ConfigStore: configStore, Credentials: creds, Args: []string{"job", "run", "job-1", "--yes"}})
	if code != 2 {
		t.Fatalf("expected missing ref exit code 2, got %d stderr=%s", code, stderr.String())
	}
	if requestCount != beforeMissingRef {
		t.Fatalf("expected missing ref to stop before HTTP request, got %d requests", requestCount-beforeMissingRef)
	}
	if !strings.Contains(stderr.String(), "ref is required") {
		t.Fatalf("unexpected missing ref stderr: %s", stderr.String())
	}
	stdout.Reset()
	stderr.Reset()

	beforeNonInteractive := requestCount
	code = Run(Dependencies{Stdout: stdout, Stderr: stderr, Stdin: strings.NewReader(""), ConfigStore: configStore, Credentials: creds, Args: []string{"job", "run", "job-1", "--ref", "main"}})
	if code != 2 {
		t.Fatalf("expected noninteractive exit code 2, got %d stderr=%s", code, stderr.String())
	}
	if requestCount != beforeNonInteractive {
		t.Fatalf("expected noninteractive refusal before any HTTP request, got %d", requestCount-beforeNonInteractive)
	}
	if !strings.Contains(stderr.String(), "job run requires --yes when stdin is not interactive") {
		t.Fatalf("unexpected noninteractive stderr: %s", stderr.String())
	}
	stdout.Reset()
	stderr.Reset()

	beforeJSON := requestCount
	code = Run(Dependencies{Stdout: stdout, Stderr: stderr, Stdin: strings.NewReader("ignored"), ConfigStore: configStore, Credentials: creds, Args: []string{"job", "run", "job-1", "--ref", "main", "--json"}})
	if code != 2 {
		t.Fatalf("expected json without yes exit code 2, got %d stderr=%s", code, stderr.String())
	}
	if requestCount != beforeJSON {
		t.Fatalf("expected json refusal before any HTTP request, got %d", requestCount-beforeJSON)
	}
	if !strings.Contains(stderr.String(), "job run with --json requires --yes") {
		t.Fatalf("unexpected json stderr: %s", stderr.String())
	}
	stdout.Reset()
	stderr.Reset()

	code = Run(Dependencies{Stdout: stdout, Stderr: stderr, Stdin: strings.NewReader("ignored"), ConfigStore: configStore, Credentials: creds, Args: []string{"job", "run", "duplicate", "--project", "platform", "--ref", "main", "--yes", "--json"}})
	if code != 2 {
		t.Fatalf("expected ambiguous selector exit code 2, got %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "job selector matched multiple jobs in project") {
		t.Fatalf("unexpected ambiguous selector stderr: %s", stderr.String())
	}
	stdout.Reset()
	stderr.Reset()

	code = Run(Dependencies{Stdout: stdout, Stderr: stderr, Stdin: strings.NewReader("ignored"), ConfigStore: configStore, Credentials: creds, Args: []string{"job", "run", "job-2", "--ref", "main", "--yes", "--json"}})
	if code != 5 {
		t.Fatalf("expected missing scope exit code 5, got %d stderr=%s", code, stderr.String())
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), "api token does not have the required scope: build:run") {
		t.Fatalf("unexpected missing scope result stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if strings.Contains(stderr.String(), "stored-token") {
		t.Fatalf("stderr leaked token: %s", stderr.String())
	}
}

func discoveryTestConfig(t *testing.T, serverURL string) (*config.Store, *credentials.MemoryStore) {
	t.Helper()
	configStore := config.NewStore(filepath.Join(t.TempDir(), "config.json"))
	if err := configStore.Save(config.File{
		CurrentContext: "local",
		Contexts: map[string]config.Context{
			"local": {Name: "local", ServerURL: serverURL, CredentialRef: "context:local"},
		},
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	creds := credentials.NewMemoryStore()
	if setErr := creds.Set("context:local", "stored-token"); setErr != nil {
		t.Fatalf("set token: %v", setErr)
	}
	return configStore, creds
}
