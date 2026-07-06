package cli

import (
	"bytes"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/api"
	"github.com/radiation/coyote-ci/backend/internal/cli/config"
	"github.com/radiation/coyote-ci/backend/internal/cli/credentials"
	"github.com/radiation/coyote-ci/backend/internal/cli/output"
)

type failOnWriteWriter struct {
	failAt int
	writes int
}

func (w *failOnWriteWriter) Write(p []byte) (int, error) {
	w.writes++
	if w.writes == w.failAt {
		return 0, errors.New("write failed")
	}
	return len(p), nil
}

type failReadReader struct{}

func (failReadReader) Read([]byte) (int, error) {
	return 0, errors.New("read failed")
}

func TestProjectDiscoveryHelpers(t *testing.T) {
	desc := "Core services"
	project := makeProjectView("https://ci.example.com/base?token=ignored#frag", api.ProjectResponse{
		ID:          "project-1",
		Name:        "Platform",
		Slug:        "platform",
		Description: &desc,
		CreatedAt:   "2026-07-06T00:00:00Z",
		UpdatedAt:   "2026-07-06T00:00:00Z",
	})
	if project.WebURL != "https://ci.example.com/base/projects/project-1" {
		t.Fatalf("unexpected project web url: %q", project.WebURL)
	}

	var human bytes.Buffer
	if err := writeProjectHuman(&human, projectPayload{Project: project}); err != nil {
		t.Fatalf("write project human: %v", err)
	}
	for _, want := range []string{"Project: Platform", "Desc:    Core services", "URL:     https://ci.example.com/base/projects/project-1"} {
		if !strings.Contains(human.String(), want) {
			t.Fatalf("expected %q in project output, got %s", want, human.String())
		}
	}

	project.Description = nil
	human.Reset()
	if err := writeProjectHuman(&human, projectPayload{Project: project}); err != nil {
		t.Fatalf("write project human without description: %v", err)
	}
	if strings.Contains(human.String(), "Desc:") {
		t.Fatalf("did not expect description line, got %s", human.String())
	}

	if got := resourceWebURL("://bad", "/projects/project-1"); got != "" {
		t.Fatalf("expected invalid server url to return blank web url, got %q", got)
	}

	var list bytes.Buffer
	if err := writeProjectListHuman(&list, projectListPayload{Projects: []projectView{project}}); err != nil {
		t.Fatalf("write project list human: %v", err)
	}
	for _, want := range []string{"Projects", "project-1", "platform", "Platform"} {
		if !strings.Contains(list.String(), want) {
			t.Fatalf("expected %q in project list output, got %s", want, list.String())
		}
	}

	list.Reset()
	if err := writeProjectListHuman(&list, projectListPayload{}); err != nil {
		t.Fatalf("write empty project list human: %v", err)
	}
	if strings.TrimSpace(list.String()) != "No projects found." {
		t.Fatalf("unexpected empty project list output: %q", list.String())
	}

	if err := writeProjectHuman(errWriter{}, projectPayload{Project: project}); err == nil || !strings.Contains(err.Error(), "write failed") {
		t.Fatalf("expected writeProjectHuman to surface writer error, got %v", err)
	}
	if err := writeProjectListHuman(errWriter{}, projectListPayload{}); err == nil || !strings.Contains(err.Error(), "write failed") {
		t.Fatalf("expected writeProjectListHuman empty branch to surface writer error, got %v", err)
	}
	for failAt := 1; failAt <= 6; failAt++ {
		writer := &failOnWriteWriter{failAt: failAt}
		if err := writeProjectHuman(writer, projectPayload{Project: project}); err == nil || !strings.Contains(err.Error(), "write failed") {
			t.Fatalf("expected writeProjectHuman failAt=%d to return write error, got %v", failAt, err)
		}
	}
}

func TestJobDiscoveryHelpers(t *testing.T) {
	finishedAt := "2026-07-06T00:02:00Z"
	errorMessage := "boom"
	pipelinePath := ".coyote/pipeline.yml"
	job := makeJobView("https://ci.example.com/base?token=ignored#frag", api.JobResponse{
		ID:            "job-1",
		ProjectID:     "project-1",
		Name:          "backend-ci",
		Priority:      5,
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		TriggerMode:   "branches",
		PipelinePath:  &pipelinePath,
		LatestBuild: &api.JobBuildSummaryResponse{
			ID:           "build-1",
			Status:       "failed",
			CreatedAt:    "2026-07-06T00:00:00Z",
			FinishedAt:   &finishedAt,
			ErrorMessage: &errorMessage,
		},
		Enabled:   true,
		CreatedAt: "2026-07-06T00:00:00Z",
		UpdatedAt: "2026-07-06T00:01:00Z",
	})
	if job.WebURL != "https://ci.example.com/base/jobs/job-1" {
		t.Fatalf("unexpected job web url: %q", job.WebURL)
	}
	if job.PipelineSource != ".coyote/pipeline.yml" {
		t.Fatalf("unexpected pipeline source: %q", job.PipelineSource)
	}
	if latestBuildLabel(nil) != "-" {
		t.Fatalf("expected nil latest build label fallback")
	}
	if latestBuildLongLabel(nil) != "-" {
		t.Fatalf("expected nil latest build long label fallback")
	}
	if latestBuildLabel(job.LatestBuild) != "build-1 failed" {
		t.Fatalf("unexpected build label: %q", latestBuildLabel(job.LatestBuild))
	}
	if latestBuildLongLabel(job.LatestBuild) != "build-1 (failed)" {
		t.Fatalf("unexpected build long label: %q", latestBuildLongLabel(job.LatestBuild))
	}
	job.LatestBuild.BuildNumber = 14
	if latestBuildLabel(job.LatestBuild) != "#14 failed" {
		t.Fatalf("unexpected numbered build label: %q", latestBuildLabel(job.LatestBuild))
	}
	if latestBuildLongLabel(job.LatestBuild) != "#14 failed" {
		t.Fatalf("unexpected numbered build long label: %q", latestBuildLongLabel(job.LatestBuild))
	}

	var human bytes.Buffer
	if err := writeJobHuman(&human, jobPayload{Job: job}); err != nil {
		t.Fatalf("write job human: %v", err)
	}
	for _, want := range []string{"Repo:       https://github.com/example/backend.git", "Ref:        main", "Trigger:    branches", "Pipeline:   .coyote/pipeline.yml", "Latest:     #14 failed", "URL:        https://ci.example.com/base/jobs/job-1"} {
		if !strings.Contains(human.String(), want) {
			t.Fatalf("expected %q in job output, got %s", want, human.String())
		}
	}

	human.Reset()
	if err := writeJobListHuman(&human, jobListPayload{Jobs: []jobView{{ID: "job-1", Name: "backend-ci", Enabled: true}}}); err != nil {
		t.Fatalf("write job list human: %v", err)
	}
	if !strings.Contains(human.String(), "Jobs") {
		t.Fatalf("expected generic jobs heading, got %s", human.String())
	}

	human.Reset()
	if err := writeJobListHuman(&human, jobListPayload{ProjectSelector: "platform", Jobs: []jobView{{ID: "job-1", Name: "backend-ci", Enabled: true}}}); err != nil {
		t.Fatalf("write project-scoped job list human: %v", err)
	}
	if !strings.Contains(human.String(), "Jobs for project platform") {
		t.Fatalf("expected project-scoped jobs heading, got %s", human.String())
	}

	human.Reset()
	if err := writeJobListHuman(&human, jobListPayload{}); err != nil {
		t.Fatalf("write empty job list human: %v", err)
	}
	if strings.TrimSpace(human.String()) != "No jobs found." {
		t.Fatalf("unexpected empty job list output: %q", human.String())
	}

	minimalJob := job
	minimalJob.RepositoryURL = ""
	minimalJob.DefaultRef = ""
	minimalJob.TriggerMode = ""
	minimalJob.PipelineSource = ""
	minimalJob.LatestBuild = nil
	minimalJob.WebURL = ""
	human.Reset()
	if err := writeJobHuman(&human, jobPayload{Job: minimalJob}); err != nil {
		t.Fatalf("write minimal job human: %v", err)
	}
	for _, unwanted := range []string{"Repo:", "Ref:", "Trigger:", "Pipeline:", "Latest:", "URL:"} {
		if strings.Contains(human.String(), unwanted) {
			t.Fatalf("did not expect %q in minimal job output: %s", unwanted, human.String())
		}
	}

	if err := writeJobHuman(errWriter{}, jobPayload{Job: job}); err == nil || !strings.Contains(err.Error(), "write failed") {
		t.Fatalf("expected writeJobHuman to surface writer error, got %v", err)
	}
	if err := writeJobListHuman(errWriter{}, jobListPayload{}); err == nil || !strings.Contains(err.Error(), "write failed") {
		t.Fatalf("expected writeJobListHuman empty branch to surface writer error, got %v", err)
	}
	for failAt := 1; failAt <= 10; failAt++ {
		writer := &failOnWriteWriter{failAt: failAt}
		if err := writeJobHuman(writer, jobPayload{Job: job}); err == nil || !strings.Contains(err.Error(), "write failed") {
			t.Fatalf("expected writeJobHuman failAt=%d to return write error, got %v", failAt, err)
		}
	}

	if looksLikeDirectJobID("") {
		t.Fatal("expected blank selector not to look like direct id")
	}
	if looksLikeDirectJobID("job-123") {
		t.Fatal("expected job- prefix selector without uuid format not to look like direct id")
	}
	if !looksLikeDirectJobID(uuid.NewString()) {
		t.Fatal("expected uuid selector to look like direct id")
	}
	if looksLikeDirectJobID("backend-ci") {
		t.Fatal("expected plain name not to look like direct id")
	}
}

func TestJobRunHelpersAndValidation(t *testing.T) {
	payload := makeJobRunPayload("https://ci.example.com/base?token=ignored#frag", api.JobResponse{
		ID:        "job-1",
		ProjectID: "project-1",
		Name:      "backend-ci",
	}, " main ", api.BuildResponse{ID: "build-1", Status: "queued"})
	if payload.Run.JobName != "backend-ci" || payload.Run.Ref != "main" || payload.Run.ProjectID != "project-1" {
		t.Fatalf("unexpected job run payload: %+v", payload)
	}
	if payload.Run.WebURL != "https://ci.example.com/base/builds/build-1" {
		t.Fatalf("unexpected job run web url: %q", payload.Run.WebURL)
	}

	var human bytes.Buffer
	if err := writeJobRunHuman(&human, payload); err != nil {
		t.Fatalf("write job run human: %v", err)
	}
	for _, want := range []string{"Started job backend-ci", "Build:  build-1", "Status: queued", "coyote build status build-1"} {
		if !strings.Contains(human.String(), want) {
			t.Fatalf("expected %q in job run output, got %s", want, human.String())
		}
	}

	human.Reset()
	if err := writeJobRunHuman(&human, jobRunPayload{Run: jobRunView{JobID: "job-2", BuildID: "build-2", Status: "queued"}}); err != nil {
		t.Fatalf("write job run fallback human: %v", err)
	}
	if !strings.Contains(human.String(), "Started job job-2") {
		t.Fatalf("expected job id fallback in output, got %s", human.String())
	}
	if err := writeJobRunHuman(errWriter{}, payload); err == nil || !strings.Contains(err.Error(), "write failed") {
		t.Fatalf("expected writeJobRunHuman to surface writer error, got %v", err)
	}

	originalInteractiveCheck := isInteractiveInputFunc
	t.Cleanup(func() {
		isInteractiveInputFunc = originalInteractiveCheck
	})

	application := &app{stdin: strings.NewReader("ignored"), stderr: &bytes.Buffer{}}
	if err := application.validateJobRunInvocation(output.ModeHuman, true); err != nil {
		t.Fatalf("assume yes should bypass validation, got %v", err)
	}

	err := application.validateJobRunInvocation(output.ModeJSON, false)
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 2 || !strings.Contains(err.Error(), "requires --yes") {
		t.Fatalf("unexpected json validation error: %v", err)
	}

	isInteractiveInputFunc = func(io.Reader) bool { return false }
	err = application.validateJobRunInvocation(output.ModeHuman, false)
	if !errors.As(err, &exitErr) || exitErr.Code != 2 || !strings.Contains(err.Error(), "stdin is not interactive") {
		t.Fatalf("unexpected noninteractive validation error: %v", err)
	}

	isInteractiveInputFunc = func(io.Reader) bool { return true }
	if validationErr := application.validateJobRunInvocation(output.ModeHuman, false); validationErr != nil {
		t.Fatalf("interactive validation should pass, got %v", validationErr)
	}
	if confirmErr := application.confirmJobRun("backend-ci", "main", true); confirmErr != nil {
		t.Fatalf("assume yes confirmation should pass, got %v", confirmErr)
	}

	stderr := &bytes.Buffer{}
	application = &app{stdin: strings.NewReader("yes\n"), stderr: stderr}
	if confirmErr := application.confirmJobRun("backend-ci", "main", false); confirmErr != nil {
		t.Fatalf("expected confirmation success, got %v", confirmErr)
	}
	if !strings.Contains(stderr.String(), "Run job backend-ci on ref main?") {
		t.Fatalf("expected prompt in stderr, got %q", stderr.String())
	}

	stderr.Reset()
	application = &app{stdin: strings.NewReader("yes\n"), stderr: stderr}
	if confirmErr := application.confirmJobRun("   ", "main", false); confirmErr != nil {
		t.Fatalf("expected blank-name confirmation success, got %v", confirmErr)
	}
	if !strings.Contains(stderr.String(), "Run job job on ref main?") {
		t.Fatalf("expected generic job label prompt, got %q", stderr.String())
	}

	application = &app{stdin: strings.NewReader("n\n"), stderr: &bytes.Buffer{}}
	err = application.confirmJobRun("backend-ci", "main", false)
	if !errors.As(err, &exitErr) || exitErr.Code != 2 || !strings.Contains(err.Error(), "job run canceled") {
		t.Fatalf("unexpected declined confirmation error: %v", err)
	}

	application = &app{stdin: strings.NewReader("yes\n"), stderr: errWriter{}}
	if confirmErr := application.confirmJobRun("backend-ci", "main", false); confirmErr == nil || confirmErr.Error() != "write failed" {
		t.Fatalf("unexpected stderr write error: %v", confirmErr)
	}

	application = &app{stdin: failReadReader{}, stderr: &bytes.Buffer{}}
	if confirmErr := application.confirmJobRun("backend-ci", "main", false); confirmErr == nil || confirmErr.Error() != "read failed" {
		t.Fatalf("unexpected stdin read error: %v", confirmErr)
	}

	application = &app{stdin: strings.NewReader(""), stderr: &bytes.Buffer{}}
	err = application.confirmJobRun("backend-ci", "main", false)
	if !errors.As(err, &exitErr) || exitErr.Code != 2 || !strings.Contains(err.Error(), "job run canceled") {
		t.Fatalf("unexpected eof confirmation error: %v", err)
	}
}

func TestDiscoveryCommandsResolveTargetErrors(t *testing.T) {
	store := config.NewStore(filepath.Join(t.TempDir(), "config.json"))
	if err := store.Save(config.File{
		CurrentContext: "local",
		Contexts: map[string]config.Context{
			"local": {Name: "local", ServerURL: "https://stored.example.com", CredentialRef: "context:local"},
		},
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	creds := credentials.NewMemoryStore()
	if err := creds.Set("context:local", "stored-token"); err != nil {
		t.Fatalf("set credential: %v", err)
	}
	application := &app{configStore: store, credentials: creds, flagContext: "missing", stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}, getenv: func(string) string { return "" }}

	projectListCmd := application.newProjectListCommand()
	if err := projectListCmd.RunE(projectListCmd, nil); err == nil || !strings.Contains(err.Error(), "unknown context \"missing\"") {
		t.Fatalf("expected project list resolveTarget error, got %v", err)
	}

	projectShowCmd := application.newProjectShowCommand()
	if err := projectShowCmd.RunE(projectShowCmd, []string{"platform"}); err == nil || !strings.Contains(err.Error(), "unknown context \"missing\"") {
		t.Fatalf("expected project show resolveTarget error, got %v", err)
	}

	jobListCmd := application.newJobListCommand()
	if err := jobListCmd.Flags().Set("project", "platform"); err != nil {
		t.Fatalf("set job list project flag: %v", err)
	}
	if err := jobListCmd.RunE(jobListCmd, nil); err == nil || !strings.Contains(err.Error(), "unknown context \"missing\"") {
		t.Fatalf("expected job list resolveTarget error, got %v", err)
	}

	jobShowCmd := application.newJobShowCommand()
	if err := jobShowCmd.Flags().Set("project", "platform"); err != nil {
		t.Fatalf("set job show project flag: %v", err)
	}
	if err := jobShowCmd.RunE(jobShowCmd, []string{"backend-ci"}); err == nil || !strings.Contains(err.Error(), "unknown context \"missing\"") {
		t.Fatalf("expected job show resolveTarget error, got %v", err)
	}
}
