package service

// BuildService pipeline/repository creation tests.

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

// Pipeline-driven build creation behavior.
func TestBuildService_CreateBuildFromPipeline(t *testing.T) {
	validYAML := `
version: 1
pipeline:
  name: backend-ci
env:
  CI: "true"
steps:
  - name: Lint
    run: golangci-lint run
    working_dir: backend
    timeout_seconds: 300
    env:
      LINT_STRICT: "1"
  - name: Test
    run: go test ./...
`

	t.Run("creates queued build with correct steps", func(t *testing.T) {
		repo := &fakeBuildRepository{}
		svc := NewBuildService(repo, nil, nil)

		build, err := svc.CreateBuildFromPipeline(context.Background(), CreatePipelineBuildInput{
			ProjectID:    "proj-1",
			PipelineYAML: validYAML,
			SourcePath:   ".coyote/pipeline.yml",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if build.Status != domain.BuildStatusQueued {
			t.Errorf("expected queued status, got %s", build.Status)
		}
		if build.ProjectID != "proj-1" {
			t.Errorf("expected project_id proj-1, got %s", build.ProjectID)
		}
		if build.PipelineConfigYAML == nil {
			t.Fatal("expected pipeline_config_yaml to be set")
		}
		if build.PipelineName == nil || *build.PipelineName != "backend-ci" {
			t.Errorf("expected pipeline_name backend-ci, got %v", build.PipelineName)
		}
		if build.PipelineSource == nil || *build.PipelineSource != ".coyote/pipeline.yml" {
			t.Errorf("expected pipeline_source, got %v", build.PipelineSource)
		}

		if len(repo.steps) != 2 {
			t.Fatalf("expected 2 steps, got %d", len(repo.steps))
		}

		lint := repo.steps[0]
		if lint.Name != "Lint" {
			t.Errorf("step 0 name: got %q", lint.Name)
		}
		if lint.Command != "sh" || len(lint.Args) < 2 || lint.Args[1] != "golangci-lint run" {
			t.Errorf("step 0 command not resolved correctly: %s %v", lint.Command, lint.Args)
		}
		if lint.WorkingDir != "backend" {
			t.Errorf("step 0 working_dir: got %q", lint.WorkingDir)
		}
		if lint.TimeoutSeconds != 300 {
			t.Errorf("step 0 timeout: got %d", lint.TimeoutSeconds)
		}
		if lint.Env["CI"] != "true" {
			t.Errorf("step 0 should inherit pipeline env CI=true")
		}
		if lint.Env["LINT_STRICT"] != "1" {
			t.Errorf("step 0 should have LINT_STRICT=1")
		}

		test := repo.steps[1]
		if test.Name != "Test" {
			t.Errorf("step 1 name: got %q", test.Name)
		}
		if test.Env["CI"] != "true" {
			t.Errorf("step 1 should inherit pipeline env CI=true")
		}
	})

	t.Run("missing project_id", func(t *testing.T) {
		repo := &fakeBuildRepository{}
		svc := NewBuildService(repo, nil, nil)

		_, err := svc.CreateBuildFromPipeline(context.Background(), CreatePipelineBuildInput{
			PipelineYAML: validYAML,
		})
		if !errors.Is(err, ErrProjectIDRequired) {
			t.Errorf("expected ErrProjectIDRequired, got %v", err)
		}
	})

	t.Run("empty yaml", func(t *testing.T) {
		repo := &fakeBuildRepository{}
		svc := NewBuildService(repo, nil, nil)

		_, err := svc.CreateBuildFromPipeline(context.Background(), CreatePipelineBuildInput{
			ProjectID:    "proj-1",
			PipelineYAML: "",
		})
		if !errors.Is(err, ErrPipelineYAMLRequired) {
			t.Errorf("expected ErrPipelineYAMLRequired, got %v", err)
		}
	})

	t.Run("invalid yaml", func(t *testing.T) {
		repo := &fakeBuildRepository{}
		svc := NewBuildService(repo, nil, nil)

		_, err := svc.CreateBuildFromPipeline(context.Background(), CreatePipelineBuildInput{
			ProjectID:    "proj-1",
			PipelineYAML: "version: 2\nsteps:\n  - name: X\n    run: echo",
		})
		if err == nil {
			t.Fatal("expected validation error")
		}
		if !strings.Contains(err.Error(), "version") {
			t.Errorf("expected version error, got: %v", err)
		}
	})

	t.Run("env merge step overrides pipeline", func(t *testing.T) {
		yaml := `
version: 1
env:
  SHARED: from-pipeline
steps:
  - name: Step1
    run: echo
    env:
      SHARED: from-step
`
		repo := &fakeBuildRepository{}
		svc := NewBuildService(repo, nil, nil)
		_, err := svc.CreateBuildFromPipeline(context.Background(), CreatePipelineBuildInput{
			ProjectID:    "proj-1",
			PipelineYAML: yaml,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if repo.steps[0].Env["SHARED"] != "from-step" {
			t.Errorf("step env should override pipeline env, got %q", repo.steps[0].Env["SHARED"])
		}
	})

	t.Run("pipeline snapshot persisted", func(t *testing.T) {
		repo := &fakeBuildRepository{}
		svc := NewBuildService(repo, nil, nil)
		build, err := svc.CreateBuildFromPipeline(context.Background(), CreatePipelineBuildInput{
			ProjectID:    "proj-1",
			PipelineYAML: validYAML,
			SourcePath:   ".coyote/pipeline.yml",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if build.PipelineConfigYAML == nil {
			t.Fatal("expected pipeline YAML snapshot")
		}
		if !strings.Contains(*build.PipelineConfigYAML, "golangci-lint") {
			t.Error("pipeline YAML snapshot should contain original YAML content")
		}
	})
}

// fakeRepoFetcher implements source.RepoFetcher for testing.
type fakeRepoFetcher struct {
	localPath string
	commitSHA string
	err       error
	calls     int
	lastRef   string
}

func (f *fakeRepoFetcher) Fetch(_ context.Context, _ string, ref string) (string, string, error) {
	f.calls++
	f.lastRef = ref
	if f.err != nil {
		return "", "", f.err
	}
	return f.localPath, f.commitSHA, nil
}

// Repository-backed build creation behavior.
func TestBuildService_CreateBuildFromRepo(t *testing.T) {
	// Set up a temp dir with a valid pipeline file.
	setupTempRepo := func(t *testing.T, yamlContent string) string {
		t.Helper()
		tmpDir := t.TempDir()
		coyoteDir := tmpDir + "/.coyote"
		if err := os.MkdirAll(coyoteDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(coyoteDir+"/pipeline.yml", []byte(yamlContent), 0o644); err != nil {
			t.Fatal(err)
		}
		return tmpDir
	}

	validYAML := `version: 1
pipeline:
  name: repo-ci
steps:
  - name: test
    run: go test ./...
  - name: lint
    run: golangci-lint run
`

	t.Run("creates build with repo metadata", func(t *testing.T) {
		tmpDir := setupTempRepo(t, validYAML)
		repo := &fakeBuildRepository{}
		svc := NewBuildService(repo, nil, nil)
		svc.SetRepoFetcher(&fakeRepoFetcher{
			localPath: tmpDir,
			commitSHA: "abc123def456",
		})

		build, err := svc.CreateBuildFromRepo(context.Background(), CreateRepoBuildInput{
			ProjectID: "proj-1",
			RepoURL:   "https://github.com/org/repo.git",
			Ref:       "main",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if build.Status != domain.BuildStatusQueued {
			t.Errorf("expected queued, got %s", build.Status)
		}
		if build.RepoURL == nil || *build.RepoURL != "https://github.com/org/repo.git" {
			t.Errorf("expected repo_url, got %v", build.RepoURL)
		}
		if build.Ref == nil || *build.Ref != "main" {
			t.Errorf("expected ref main, got %v", build.Ref)
		}
		if build.CommitSHA == nil || *build.CommitSHA != "abc123def456" {
			t.Errorf("expected commit_sha, got %v", build.CommitSHA)
		}
		if build.PipelineConfigYAML == nil {
			t.Fatal("expected pipeline YAML snapshot")
		}
		if build.PipelineName == nil || *build.PipelineName != "repo-ci" {
			t.Errorf("expected pipeline_name repo-ci, got %v", build.PipelineName)
		}
		if build.PipelineSource == nil || *build.PipelineSource != ".coyote/pipeline.yml" {
			t.Errorf("expected logical pipeline_source, got %v", build.PipelineSource)
		}
	})

	t.Run("converts steps correctly", func(t *testing.T) {
		tmpDir := setupTempRepo(t, validYAML)
		repo := &fakeBuildRepository{}
		svc := NewBuildService(repo, nil, nil)
		svc.SetRepoFetcher(&fakeRepoFetcher{localPath: tmpDir, commitSHA: "abc"})

		_, err := svc.CreateBuildFromRepo(context.Background(), CreateRepoBuildInput{
			ProjectID: "proj-1",
			RepoURL:   "https://example.com/repo.git",
			Ref:       "main",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(repo.steps) != 2 {
			t.Fatalf("expected 2 steps, got %d", len(repo.steps))
		}
		if repo.steps[0].Name != "test" || repo.steps[0].Command != "sh" {
			t.Errorf("step 0: got name=%q command=%q", repo.steps[0].Name, repo.steps[0].Command)
		}
		if repo.steps[1].Name != "lint" {
			t.Errorf("step 1: got name=%q", repo.steps[1].Name)
		}
	})

	t.Run("missing project_id", func(t *testing.T) {
		repo := &fakeBuildRepository{}
		svc := NewBuildService(repo, nil, nil)
		svc.SetRepoFetcher(&fakeRepoFetcher{localPath: "/tmp", commitSHA: "abc"})

		_, err := svc.CreateBuildFromRepo(context.Background(), CreateRepoBuildInput{
			RepoURL: "https://example.com/repo.git",
			Ref:     "main",
		})
		if !errors.Is(err, ErrProjectIDRequired) {
			t.Errorf("expected ErrProjectIDRequired, got %v", err)
		}
	})

	t.Run("missing repo_url", func(t *testing.T) {
		repo := &fakeBuildRepository{}
		svc := NewBuildService(repo, nil, nil)
		svc.SetRepoFetcher(&fakeRepoFetcher{localPath: "/tmp", commitSHA: "abc"})

		_, err := svc.CreateBuildFromRepo(context.Background(), CreateRepoBuildInput{
			ProjectID: "proj-1",
			Ref:       "main",
		})
		if !errors.Is(err, ErrRepoURLRequired) {
			t.Errorf("expected ErrRepoURLRequired, got %v", err)
		}
	})

	t.Run("missing ref", func(t *testing.T) {
		repo := &fakeBuildRepository{}
		svc := NewBuildService(repo, nil, nil)
		svc.SetRepoFetcher(&fakeRepoFetcher{localPath: "/tmp", commitSHA: "abc"})

		_, err := svc.CreateBuildFromRepo(context.Background(), CreateRepoBuildInput{
			ProjectID: "proj-1",
			RepoURL:   "https://example.com/repo.git",
		})
		if !errors.Is(err, ErrSourceTargetRequired) {
			t.Errorf("expected ErrSourceTargetRequired, got %v", err)
		}
	})

	t.Run("commit sha can be used without ref", func(t *testing.T) {
		tmpDir := setupTempRepo(t, validYAML)
		repo := &fakeBuildRepository{}
		fetcher := &fakeRepoFetcher{localPath: tmpDir, commitSHA: "abc123"}
		svc := NewBuildService(repo, nil, nil)
		svc.SetRepoFetcher(fetcher)

		_, err := svc.CreateBuildFromRepo(context.Background(), CreateRepoBuildInput{
			ProjectID: "proj-1",
			RepoURL:   "https://example.com/repo.git",
			CommitSHA: "abc123",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if fetcher.calls != 1 {
			t.Fatalf("expected one fetch call, got %d", fetcher.calls)
		}
		if fetcher.lastRef != "abc123" {
			t.Fatalf("expected fetch target commit sha, got %q", fetcher.lastRef)
		}
	})

	t.Run("commit sha takes precedence over ref", func(t *testing.T) {
		tmpDir := setupTempRepo(t, validYAML)
		repo := &fakeBuildRepository{}
		fetcher := &fakeRepoFetcher{localPath: tmpDir, commitSHA: "cafebabedeadbeef"}
		svc := NewBuildService(repo, nil, nil)
		svc.SetRepoFetcher(fetcher)

		build, err := svc.CreateBuildFromRepo(context.Background(), CreateRepoBuildInput{
			ProjectID: "proj-1",
			RepoURL:   "https://example.com/repo.git",
			Ref:       "main",
			CommitSHA: "cafebabedeadbeef",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if fetcher.lastRef != "cafebabedeadbeef" {
			t.Fatalf("expected commit target to take precedence, got %q", fetcher.lastRef)
		}
		if build.CommitSHA == nil || *build.CommitSHA != "cafebabedeadbeef" {
			t.Fatalf("expected persisted commit sha, got %v", build.CommitSHA)
		}
	})

	t.Run("repo fetcher not configured", func(t *testing.T) {
		repo := &fakeBuildRepository{}
		svc := NewBuildService(repo, nil, nil)

		_, err := svc.CreateBuildFromRepo(context.Background(), CreateRepoBuildInput{
			ProjectID: "proj-1",
			RepoURL:   "https://example.com/repo.git",
			Ref:       "main",
		})
		if !errors.Is(err, ErrRepoFetcherNotConfigured) {
			t.Errorf("expected ErrRepoFetcherNotConfigured, got %v", err)
		}
	})

	t.Run("repo fetch error", func(t *testing.T) {
		repo := &fakeBuildRepository{}
		svc := NewBuildService(repo, nil, nil)
		svc.SetRepoFetcher(&fakeRepoFetcher{err: errors.New("clone failed")})

		_, err := svc.CreateBuildFromRepo(context.Background(), CreateRepoBuildInput{
			ProjectID: "proj-1",
			RepoURL:   "https://example.com/repo.git",
			Ref:       "main",
		})
		if err == nil || !strings.Contains(err.Error(), "clone failed") {
			t.Errorf("expected clone error, got %v", err)
		}
	})

	t.Run("pipeline file not found", func(t *testing.T) {
		// Use a temp dir without .coyote/pipeline.yml.
		tmpDir := t.TempDir()
		repo := &fakeBuildRepository{}
		svc := NewBuildService(repo, nil, nil)
		svc.SetRepoFetcher(&fakeRepoFetcher{localPath: tmpDir, commitSHA: "abc"})

		_, err := svc.CreateBuildFromRepo(context.Background(), CreateRepoBuildInput{
			ProjectID: "proj-1",
			RepoURL:   "https://example.com/repo.git",
			Ref:       "main",
		})
		if !errors.Is(err, ErrPipelineFileNotFound) {
			t.Errorf("expected ErrPipelineFileNotFound, got %v", err)
		}
	})

	t.Run("invalid pipeline YAML", func(t *testing.T) {
		tmpDir := setupTempRepo(t, "not: valid: pipeline")
		repo := &fakeBuildRepository{}
		svc := NewBuildService(repo, nil, nil)
		svc.SetRepoFetcher(&fakeRepoFetcher{localPath: tmpDir, commitSHA: "abc"})

		_, err := svc.CreateBuildFromRepo(context.Background(), CreateRepoBuildInput{
			ProjectID: "proj-1",
			RepoURL:   "https://example.com/repo.git",
			Ref:       "main",
		})
		if err == nil {
			t.Fatal("expected parse/validation error")
		}
	})
}
