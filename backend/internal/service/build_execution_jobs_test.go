package service

import (
	"context"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	memoryrepo "github.com/radiation/coyote-ci/backend/internal/repository/memory"
)

func TestBuildService_CreateBuildFromPipeline_PersistsDurableJobs(t *testing.T) {
	buildRepo := &fakeBuildRepository{}
	execRepo := memoryrepo.NewExecutionJobRepository()
	svc := NewBuildService(buildRepo, nil, &fakeLogSink{})
	svc.SetExecutionJobRepository(execRepo)
	svc.SetDefaultExecutionImage("golang:1.24")

	pipelineYAML := `
version: 1
pipeline:
  name: backend-ci
  image: golang:1.26
steps:
  - name: lint
    run: go vet ./...
    working_dir: backend
    timeout_seconds: 120
    env:
      GOFLAGS: -mod=readonly
`

	build, err := svc.CreateBuildFromPipeline(context.Background(), CreatePipelineBuildInput{
		ProjectID:    "project-1",
		PipelineYAML: pipelineYAML,
		Source: &CreateBuildSourceInput{
			RepositoryURL: "https://github.com/acme/repo.git",
			Ref:           "main",
			CommitSHA:     "abc123",
		},
	})
	if err != nil {
		t.Fatalf("create build from pipeline failed: %v", err)
	}

	jobs, err := execRepo.GetJobsByBuildID(context.Background(), build.ID)
	if err != nil {
		t.Fatalf("get jobs by build failed: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected one execution job, got %d", len(jobs))
	}

	job := jobs[0]
	if job.Image != "golang:1.26" {
		t.Fatalf("expected persisted pipeline image golang:1.26, got %q", job.Image)
	}
	if len(job.Command) != 3 || job.Command[2] != "go vet ./..." {
		t.Fatalf("expected frozen command from resolved pipeline step, got %#v", job.Command)
	}
	if job.WorkingDir != "backend" {
		t.Fatalf("expected working_dir backend, got %q", job.WorkingDir)
	}
	if job.Source.CommitSHA != "abc123" {
		t.Fatalf("expected source commit abc123, got %q", job.Source.CommitSHA)
	}
	if job.SpecVersion != 1 || job.SpecDigest == nil || *job.SpecDigest == "" {
		t.Fatalf("expected spec version and digest, got version=%d digest=%v", job.SpecVersion, job.SpecDigest)
	}
	if job.ResolvedSpecJSON == "" {
		t.Fatal("expected resolved spec json to be persisted")
	}
}

func TestBuildService_QueueBuildWithTemplate_PersistsDurableJobs(t *testing.T) {
	buildRepo := &fakeBuildRepository{build: defaultBuild("build-template")}
	execRepo := memoryrepo.NewExecutionJobRepository()
	svc := NewBuildService(buildRepo, nil, &fakeLogSink{})
	svc.SetExecutionJobRepository(execRepo)

	queued, err := svc.QueueBuildWithTemplate(context.Background(), "build-template", BuildTemplateTest)
	if err != nil {
		t.Fatalf("queue build with template failed: %v", err)
	}

	jobs, err := execRepo.GetJobsByBuildID(context.Background(), queued.ID)
	if err != nil {
		t.Fatalf("get jobs by build failed: %v", err)
	}
	if len(jobs) != 3 {
		t.Fatalf("expected three execution jobs for test template, got %d", len(jobs))
	}
	if jobs[0].StepIndex != 0 || jobs[1].StepIndex != 1 || jobs[2].StepIndex != 2 {
		t.Fatalf("expected ordered step indexes, got %d %d %d", jobs[0].StepIndex, jobs[1].StepIndex, jobs[2].StepIndex)
	}
}

func defaultBuild(id string) domain.Build {
	now := time.Now().UTC()
	return domain.Build{ID: id, ProjectID: "project-1", Status: domain.BuildStatusPending, CreatedAt: now}
}
