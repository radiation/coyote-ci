package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/api"
)

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

	if looksLikeDirectJobID("") {
		t.Fatal("expected blank selector not to look like direct id")
	}
	if !looksLikeDirectJobID("job-123") {
		t.Fatal("expected job- prefix selector to look like direct id")
	}
	if !looksLikeDirectJobID(uuid.NewString()) {
		t.Fatal("expected uuid selector to look like direct id")
	}
	if looksLikeDirectJobID("backend-ci") {
		t.Fatal("expected plain name not to look like direct id")
	}
}
