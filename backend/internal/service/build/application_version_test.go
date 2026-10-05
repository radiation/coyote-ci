package build

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	memoryrepo "github.com/radiation/coyote-ci/backend/internal/repository/memory"
)

func TestPrepareBuildExecution_ResolvesAndPersistsApplicationVersionTemplate(t *testing.T) {
	pipelineYAML := `
version: 1
pipeline:
  application_version:
    template: "0.4.{build_number}"
steps:
  - name: build
    run: true
`
	repo := &fakeBuildRepository{build: domain.Build{
		ID:                 "build-1",
		BuildNumber:        137,
		Status:             domain.BuildStatusQueued,
		PipelineConfigYAML: &pipelineYAML,
	}}
	svc := NewBuildService(repo, nil, nil)

	build, prepErr := svc.PrepareBuildExecution(context.Background(), "build-1")
	if prepErr != nil {
		t.Fatalf("prepare build: %v", prepErr)
	}
	if build.ApplicationVersion == nil || *build.ApplicationVersion != "0.4.137" {
		t.Fatalf("application version=%v, want 0.4.137", build.ApplicationVersion)
	}

	reprepared, repeatErr := svc.resolveApplicationVersionForBuild(context.Background(), "build-1")
	if repeatErr != nil {
		t.Fatalf("repeat resolution: %v", repeatErr)
	}
	if reprepared.ApplicationVersion == nil || *reprepared.ApplicationVersion != "0.4.137" {
		t.Fatalf("repeat application version=%v, want 0.4.137", reprepared.ApplicationVersion)
	}
}

func TestPrepareBuildExecution_ResolvesApplicationVersionFileBeforeRunning(t *testing.T) {
	pipelineYAML := `
version: 1
pipeline:
  application_version:
    file: VERSION
steps:
  - name: build
    run: true
`
	workspaceRoot := t.TempDir()
	buildID := "build-file"
	workspacePath := filepath.Join(workspaceRoot, buildID)
	if mkdirErr := os.MkdirAll(workspacePath, 0o755); mkdirErr != nil {
		t.Fatalf("create workspace: %v", mkdirErr)
	}
	if writeErr := os.WriteFile(filepath.Join(workspacePath, "VERSION"), []byte("1.2.3\n"), 0o600); writeErr != nil {
		t.Fatalf("write version file: %v", writeErr)
	}
	repo := &fakeBuildRepository{build: domain.Build{
		ID:                 buildID,
		Status:             domain.BuildStatusQueued,
		PipelineConfigYAML: &pipelineYAML,
	}}
	svc := NewBuildService(repo, nil, nil)
	svc.SetExecutionWorkspaceRoot(workspaceRoot)

	build, prepErr := svc.PrepareBuildExecution(context.Background(), buildID)
	if prepErr != nil {
		t.Fatalf("prepare build: %v", prepErr)
	}
	if build.Status != domain.BuildStatusRunning {
		t.Fatalf("build status=%q, want running", build.Status)
	}
	if build.ApplicationVersion == nil || *build.ApplicationVersion != "1.2.3" {
		t.Fatalf("application version=%v, want 1.2.3", build.ApplicationVersion)
	}
}

func TestPrepareBuildExecution_FailsForInvalidApplicationVersionFile(t *testing.T) {
	pipelineYAML := `
version: 1
pipeline:
  application_version:
    file: VERSION
steps:
  - name: build
    run: true
`
	workspaceRoot := t.TempDir()
	buildID := "build-invalid-file"
	workspacePath := filepath.Join(workspaceRoot, buildID)
	if mkdirErr := os.MkdirAll(workspacePath, 0o755); mkdirErr != nil {
		t.Fatalf("create workspace: %v", mkdirErr)
	}
	if writeErr := os.WriteFile(filepath.Join(workspacePath, "VERSION"), []byte("not-a-version\n"), 0o600); writeErr != nil {
		t.Fatalf("write version file: %v", writeErr)
	}
	repo := &fakeBuildRepository{build: domain.Build{
		ID:                 buildID,
		Status:             domain.BuildStatusQueued,
		PipelineConfigYAML: &pipelineYAML,
	}}
	svc := NewBuildService(repo, nil, nil)
	svc.SetExecutionWorkspaceRoot(workspaceRoot)

	build, prepErr := svc.PrepareBuildExecution(context.Background(), buildID)
	if prepErr != nil {
		t.Fatalf("prepare build: %v", prepErr)
	}
	if build.Status != domain.BuildStatusFailed {
		t.Fatalf("build status=%q, want failed", build.Status)
	}
	if build.ErrorMessage == nil || !strings.Contains(*build.ErrorMessage, "application version resolution failed") {
		t.Fatalf("failure message=%v, want application version resolution detail", build.ErrorMessage)
	}
}

func TestBuildService_RerunResolvesNewApplicationVersionWithoutInheritance(t *testing.T) {
	pipelineYAML := `
version: 1
pipeline:
  application_version:
    template: "0.4.{build_number}"
steps:
  - name: build
    run: true
`
	buildRepo := memoryrepo.NewBuildRepository()
	executionJobRepo := memoryrepo.NewExecutionJobRepository()
	svc := NewBuildService(buildRepo, nil, &fakeLogSink{})
	svc.SetExecutionJobRepository(executionJobRepo)

	sourceBuild, createErr := buildRepo.CreateQueuedBuild(context.Background(), domain.Build{
		ID:                 "build-source",
		ProjectID:          "project-1",
		PipelineConfigYAML: &pipelineYAML,
	}, []domain.BuildStep{{
		ID:      "step-source",
		Name:    "build",
		Command: "true",
		Status:  domain.BuildStepStatusPending,
	}})
	if createErr != nil {
		t.Fatalf("create source build: %v", createErr)
	}

	preparedSource, prepareErr := svc.PrepareBuildExecution(context.Background(), sourceBuild.ID)
	if prepareErr != nil {
		t.Fatalf("prepare source build: %v", prepareErr)
	}
	if preparedSource.ApplicationVersion == nil || *preparedSource.ApplicationVersion != "0.4.1" {
		t.Fatalf("source application version=%v, want 0.4.1", preparedSource.ApplicationVersion)
	}

	repreparedSource, resolutionErr := svc.resolveApplicationVersionForBuild(context.Background(), sourceBuild.ID)
	if resolutionErr != nil {
		t.Fatalf("re-resolve source build: %v", resolutionErr)
	}
	if repreparedSource.ApplicationVersion == nil || *repreparedSource.ApplicationVersion != "0.4.1" {
		t.Fatalf("re-prepared source application version=%v, want 0.4.1", repreparedSource.ApplicationVersion)
	}

	_, failErr := buildRepo.UpdateStatus(context.Background(), sourceBuild.ID, domain.BuildStatusFailed, nil)
	if failErr != nil {
		t.Fatalf("mark source build failed: %v", failErr)
	}
	rerunBuild, rerunErr := svc.RerunBuild(context.Background(), sourceBuild.ID)
	if rerunErr != nil {
		t.Fatalf("rerun build: %v", rerunErr)
	}
	if rerunBuild.ApplicationVersion != nil {
		t.Fatalf("rerun inherited application version=%v, want nil", rerunBuild.ApplicationVersion)
	}
	if rerunBuild.BuildNumber != 2 {
		t.Fatalf("rerun build number=%d, want 2", rerunBuild.BuildNumber)
	}

	preparedRerun, prepareRerunErr := svc.PrepareBuildExecution(context.Background(), rerunBuild.ID)
	if prepareRerunErr != nil {
		t.Fatalf("prepare rerun build: %v", prepareRerunErr)
	}
	if preparedRerun.ApplicationVersion == nil || *preparedRerun.ApplicationVersion != "0.4.2" {
		t.Fatalf("rerun application version=%v, want 0.4.2", preparedRerun.ApplicationVersion)
	}
}
