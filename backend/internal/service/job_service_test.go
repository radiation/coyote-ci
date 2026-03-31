package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/repository/memory"
)

func TestJobService_CreateListGetUpdate(t *testing.T) {
	jobRepo := memory.NewJobRepository()
	buildRepo := memory.NewBuildRepository()
	buildService := NewBuildService(buildRepo, nil, nil)
	jobService := NewJobService(jobRepo, buildService)

	job, err := jobService.CreateJob(context.Background(), CreateJobInput{
		ProjectID:     "project-1",
		Name:          "backend-ci",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PipelineYAML:  "version: 1\nsteps:\n  - name: test\n    run: go test ./...\n",
		Enabled:       boolPtr(true),
	})
	if err != nil {
		t.Fatalf("create job failed: %v", err)
	}
	if !job.Enabled {
		t.Fatal("expected created job enabled=true")
	}

	list, err := jobService.ListJobs(context.Background())
	if err != nil {
		t.Fatalf("list jobs failed: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 job, got %d", len(list))
	}

	got, err := jobService.GetJob(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("get job failed: %v", err)
	}
	if got.Name != "backend-ci" {
		t.Fatalf("expected backend-ci, got %q", got.Name)
	}

	updated, err := jobService.UpdateJob(context.Background(), job.ID, UpdateJobInput{
		Name:    strPtr("backend-ci-updated"),
		Enabled: boolPtr(false),
	})
	if err != nil {
		t.Fatalf("update job failed: %v", err)
	}
	if updated.Enabled {
		t.Fatal("expected updated enabled=false")
	}
	if updated.Name != "backend-ci-updated" {
		t.Fatalf("expected updated name, got %q", updated.Name)
	}
}

func TestJobService_CreateRejectsInvalidPipelineYAML(t *testing.T) {
	jobService := NewJobService(memory.NewJobRepository(), NewBuildService(memory.NewBuildRepository(), nil, nil))

	_, err := jobService.CreateJob(context.Background(), CreateJobInput{
		ProjectID:     "project-1",
		Name:          "backend-ci",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PipelineYAML:  "version: 2\nsteps:\n  - name: bad\n    run: echo bad\n",
	})
	if err == nil {
		t.Fatal("expected invalid pipeline error")
	}
}

func TestJobService_RunNowCreatesNormalBuildAndSnapshotsPipeline(t *testing.T) {
	jobRepo := memory.NewJobRepository()
	buildRepo := memory.NewBuildRepository()
	buildService := NewBuildService(buildRepo, nil, nil)
	jobService := NewJobService(jobRepo, buildService)

	job, err := jobService.CreateJob(context.Background(), CreateJobInput{
		ProjectID:     "project-1",
		Name:          "backend-ci",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PipelineYAML:  "version: 1\nsteps:\n  - name: test\n    run: go test ./...\n",
		Enabled:       boolPtr(true),
	})
	if err != nil {
		t.Fatalf("create job failed: %v", err)
	}

	build, err := jobService.RunJobNow(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("run job now failed: %v", err)
	}
	if build.RepoURL == nil || *build.RepoURL != "https://github.com/example/backend.git" {
		t.Fatalf("expected build source repository_url, got %v", build.RepoURL)
	}
	if build.Ref == nil || *build.Ref != "main" {
		t.Fatalf("expected build source ref main, got %v", build.Ref)
	}
	if build.PipelineConfigYAML == nil || !strings.Contains(*build.PipelineConfigYAML, "go test ./...") {
		t.Fatalf("expected build pipeline snapshot, got %v", build.PipelineConfigYAML)
	}

	_, err = jobService.UpdateJob(context.Background(), job.ID, UpdateJobInput{
		PipelineYAML: strPtr("version: 1\nsteps:\n  - name: lint\n    run: golangci-lint run\n"),
	})
	if err != nil {
		t.Fatalf("update job failed: %v", err)
	}

	storedBuild, err := buildService.GetBuild(context.Background(), build.ID)
	if err != nil {
		t.Fatalf("get build failed: %v", err)
	}
	if storedBuild.PipelineConfigYAML == nil || !strings.Contains(*storedBuild.PipelineConfigYAML, "go test ./...") {
		t.Fatalf("expected old build snapshot unchanged, got %v", storedBuild.PipelineConfigYAML)
	}
}

func TestJobService_RunNowDisabledJobRejected(t *testing.T) {
	jobRepo := memory.NewJobRepository()
	buildRepo := memory.NewBuildRepository()
	jobService := NewJobService(jobRepo, NewBuildService(buildRepo, nil, nil))

	job, err := jobService.CreateJob(context.Background(), CreateJobInput{
		ProjectID:     "project-1",
		Name:          "backend-ci",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PipelineYAML:  "version: 1\nsteps:\n  - name: test\n    run: go test ./...\n",
		Enabled:       boolPtr(false),
	})
	if err != nil {
		t.Fatalf("create job failed: %v", err)
	}

	_, err = jobService.RunJobNow(context.Background(), job.ID)
	if !errors.Is(err, ErrJobDisabled) {
		t.Fatalf("expected ErrJobDisabled, got %v", err)
	}
}

func strPtr(v string) *string {
	return &v
}

func boolPtr(v bool) *bool {
	return &v
}
