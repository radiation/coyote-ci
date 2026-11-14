package build

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	memoryrepo "github.com/radiation/coyote-ci/backend/internal/repository/memory"
)

func TestBuildService_CreateBuildFromPipeline_PersistsImageBuildPlatforms(t *testing.T) {
	for _, test := range []struct {
		name      string
		platforms string
		want      []string
	}{
		{name: "multi-platform", platforms: "      platforms: [linux/amd64, linux/arm64]\n", want: []string{"linux/amd64", "linux/arm64"}},
		{name: "legacy omitted platforms"},
	} {
		t.Run(test.name, func(t *testing.T) {
			buildRepo := memoryrepo.NewBuildRepository()
			execRepo := memoryrepo.NewExecutionJobRepository()
			svc := NewBuildService(buildRepo, nil, &fakeLogSink{})
			svc.SetExecutionJobRepository(execRepo)
			pipelineYAML := `version: 1
steps:
  - name: image
    image_build:
      context: .
      dockerfile: Dockerfile
      image: acme/api
` + test.platforms
			build, createErr := svc.CreateBuildFromPipeline(t.Context(), CreatePipelineBuildInput{ProjectID: "project-1", PipelineYAML: pipelineYAML})
			if createErr != nil {
				t.Fatal(createErr)
			}
			steps, stepsErr := buildRepo.GetStepsByBuildID(t.Context(), build.ID)
			if stepsErr != nil || len(steps) != 1 {
				t.Fatalf("steps=%+v error=%v", steps, stepsErr)
			}
			jobs, jobsErr := execRepo.GetJobsByBuildID(t.Context(), build.ID)
			if jobsErr != nil || len(jobs) != 1 {
				t.Fatalf("jobs=%+v error=%v", jobs, jobsErr)
			}
			var spec domain.ExecutionJobSpec
			if decodeErr := json.Unmarshal([]byte(jobs[0].ResolvedSpecJSON), &spec); decodeErr != nil {
				t.Fatal(decodeErr)
			}
			if steps[0].ExecutionKind != domain.ExecutionKindImageBuild || spec.ExecutionKind != domain.ExecutionKindImageBuild {
				t.Fatal("image build execution kind was lost")
			}
			for name, imageSpec := range map[string]*domain.RemoteImageBuildSpec{"build step": steps[0].RemoteImageBuild, "execution job": spec.RemoteImageBuild} {
				if imageSpec == nil || !slices.Equal(imageSpec.Platforms, test.want) {
					t.Errorf("%s spec=%+v, want platforms=%v", name, imageSpec, test.want)
				}
			}
		})
	}
}

func TestBuildService_CreateBuildFromPipeline_PersistsDurableJobs(t *testing.T) {
	buildRepo := &fakeBuildRepository{}
	execRepo := memoryrepo.NewExecutionJobRepository()
	outputRepo := memoryrepo.NewExecutionJobOutputRepository()
	svc := NewBuildService(buildRepo, nil, &fakeLogSink{})
	svc.SetExecutionJobRepository(execRepo)
	svc.SetExecutionJobOutputRepository(outputRepo)
	svc.SetDefaultExecutionImage("golang:1.24")

	pipelineYAML := `
version: 1
pipeline:
  name: backend-ci
  image: golang:1.27.1
steps:
  - name: lint
    run: go vet ./...
    working_dir: backend
    timeout_seconds: 120
    env:
      GOFLAGS: -mod=readonly
artifacts:
  paths:
    - dist/**
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
	if job.Image != "golang:1.27.1" {
		t.Fatalf("expected persisted pipeline image golang:1.27.1, got %q", job.Image)
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

	outputs, err := outputRepo.ListByBuildID(context.Background(), build.ID)
	if err != nil {
		t.Fatalf("list outputs failed: %v", err)
	}
	if len(outputs) != 1 {
		t.Fatalf("expected one declared output, got %d", len(outputs))
	}
	if outputs[0].DeclaredPath != "dist/**" {
		t.Fatalf("expected declared output path dist/**, got %q", outputs[0].DeclaredPath)
	}
}

func TestBuildService_CreateBuild_ManualStepsUsePersistedFallbackNodesForWorkspaceLineage(t *testing.T) {
	buildRepo := memoryrepo.NewBuildRepository()
	execRepo := memoryrepo.NewExecutionJobRepository()
	svc := NewBuildService(buildRepo, nil, &fakeLogSink{})
	svc.SetExecutionJobRepository(execRepo)

	build, err := svc.CreateBuild(context.Background(), CreateBuildInput{
		ProjectID: "project-1",
		Steps: []CreateBuildStepInput{
			{Name: "setup", Command: "sh", Args: []string{"-c", "true"}},
			{Name: "test", Command: "sh", Args: []string{"-c", "true"}},
			{Name: "package", Command: "sh", Args: []string{"-c", "true"}},
		},
	})
	if err != nil {
		t.Fatalf("create manual build: %v", err)
	}

	jobs, err := execRepo.GetJobsByBuildID(context.Background(), build.ID)
	if err != nil {
		t.Fatalf("get durable jobs: %v", err)
	}
	if len(jobs) != 3 {
		t.Fatalf("expected three durable jobs, got %d", len(jobs))
	}
	if jobs[0].NodeID != domain.FallbackNodeID(0) || jobs[1].NodeID != domain.FallbackNodeID(1) || jobs[2].NodeID != domain.FallbackNodeID(2) {
		t.Fatalf("expected persisted fallback nodes, got %q %q %q", jobs[0].NodeID, jobs[1].NodeID, jobs[2].NodeID)
	}
	assertWorkspaceInput(t, jobs[0], domain.WorkspaceInputPlan{Mode: domain.WorkspaceInputModeSource})
	assertWorkspaceInput(t, jobs[1], domain.WorkspaceInputPlan{Mode: domain.WorkspaceInputModePredecessor, ProducerNodeID: jobs[0].NodeID})
	assertWorkspaceInput(t, jobs[2], domain.WorkspaceInputPlan{Mode: domain.WorkspaceInputModePredecessor, ProducerNodeID: jobs[1].NodeID})
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

func TestBuildService_CreateBuildFromPipeline_ArtifactTriggerInjectsEnv(t *testing.T) {
	buildRepo := &fakeBuildRepository{}
	execRepo := memoryrepo.NewExecutionJobRepository()
	svc := NewBuildService(buildRepo, nil, &fakeLogSink{})
	svc.SetExecutionJobRepository(execRepo)
	svc.SetDefaultExecutionImage("golang:1.24")

	pipelineYAML := `
version: 1
steps:
  - name: test
    run: go test ./...
`
	artifactSize := int64(123)
	build, err := svc.CreateBuildFromPipeline(context.Background(), CreatePipelineBuildInput{
		ProjectID:    "project-1",
		PipelineYAML: pipelineYAML,
		Trigger: &CreateBuildTriggerInput{
			Kind:                   string(domain.BuildTriggerKindArtifact),
			ProducerProjectID:      "project-1",
			ProducerJobID:          "job-upstream",
			ProducerBuildID:        "build-upstream",
			ArtifactID:             "artifact-1",
			ArtifactPath:           "dist/app.tgz",
			ArtifactName:           "app",
			ArtifactSizeBytes:      &artifactSize,
			ArtifactChecksumSHA256: "abc123",
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
	if jobs[0].Environment["COYOTE_TRIGGER_TYPE"] != "artifact_uploaded" {
		t.Fatalf("expected artifact trigger env, got %#v", jobs[0].Environment)
	}
	if jobs[0].Environment["COYOTE_TRIGGER_ARTIFACT_PATH"] != "dist/app.tgz" {
		t.Fatalf("expected artifact path env, got %#v", jobs[0].Environment)
	}
	if jobs[0].Environment["COYOTE_TRIGGER_ARTIFACT_SIZE_BYTES"] != "123" {
		t.Fatalf("expected artifact size env, got %#v", jobs[0].Environment)
	}
}

func defaultBuild(id string) domain.Build {
	now := time.Now().UTC()
	return domain.Build{ID: id, ProjectID: "project-1", Status: domain.BuildStatusPending, CreatedAt: now}
}
