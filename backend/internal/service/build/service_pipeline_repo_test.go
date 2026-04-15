package build

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
		if build.PipelineSource == nil || *build.PipelineSource != "inline" {
			t.Errorf("expected pipeline_source, got %v", build.PipelineSource)
		}
		if build.PipelinePath != nil {
			t.Errorf("expected nil pipeline_path for inline pipeline build, got %v", build.PipelinePath)
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

	t.Run("expands parallel group into dependency-aware steps", func(t *testing.T) {
		repo := &fakeBuildRepository{}
		svc := NewBuildService(repo, nil, nil)

		groupedYAML := `
version: 1
steps:
  - name: setup
    run: ./setup.sh
  - group:
      name: test-matrix
      steps:
        - name: unit-tests
          run: pytest tests/unit
        - name: integration-tests
          run: pytest tests/integration
  - name: package
    run: ./package.sh
`

		_, err := svc.CreateBuildFromPipeline(context.Background(), CreatePipelineBuildInput{
			ProjectID:    "proj-1",
			PipelineYAML: groupedYAML,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(repo.steps) != 4 {
			t.Fatalf("expected 4 expanded steps, got %d", len(repo.steps))
		}
		setup := repo.steps[0]
		unit := repo.steps[1]
		integration := repo.steps[2]
		packageStep := repo.steps[3]

		if setup.NodeID == "" || unit.NodeID == "" || integration.NodeID == "" || packageStep.NodeID == "" {
			t.Fatal("expected all expanded steps to have node ids")
		}
		if unit.GroupName == nil || *unit.GroupName != "test-matrix" {
			t.Fatalf("expected grouped step to include group name, got %v", unit.GroupName)
		}
		if integration.GroupName == nil || *integration.GroupName != "test-matrix" {
			t.Fatalf("expected grouped step to include group name, got %v", integration.GroupName)
		}
		if len(unit.DependsOnNodes) != 1 || unit.DependsOnNodes[0] != setup.NodeID {
			t.Fatalf("expected unit to depend on setup node %q, got %#v", setup.NodeID, unit.DependsOnNodes)
		}
		if len(integration.DependsOnNodes) != 1 || integration.DependsOnNodes[0] != setup.NodeID {
			t.Fatalf("expected integration to depend on setup node %q, got %#v", setup.NodeID, integration.DependsOnNodes)
		}
		if len(packageStep.DependsOnNodes) != 2 {
			t.Fatalf("expected package to depend on two group nodes, got %#v", packageStep.DependsOnNodes)
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
		if build.PipelineSource == nil || *build.PipelineSource != "repo" {
			t.Errorf("expected logical pipeline_source, got %v", build.PipelineSource)
		}
		if build.PipelinePath == nil || *build.PipelinePath != ".coyote/pipeline.yml" {
			t.Errorf("expected effective pipeline_path, got %v", build.PipelinePath)
		}
	})

	t.Run("uses custom pipeline path", func(t *testing.T) {
		tmpDir := t.TempDir()
		if err := os.MkdirAll(tmpDir+"/scenarios/success-basic", 0o755); err != nil {
			t.Fatal(err)
		}
		customPath := "scenarios/success-basic/coyote.yml"
		if err := os.WriteFile(tmpDir+"/"+customPath, []byte(validYAML), 0o644); err != nil {
			t.Fatal(err)
		}

		repo := &fakeBuildRepository{}
		svc := NewBuildService(repo, nil, nil)
		svc.SetRepoFetcher(&fakeRepoFetcher{localPath: tmpDir, commitSHA: "abc123def456"})

		build, err := svc.CreateBuildFromRepo(context.Background(), CreateRepoBuildInput{
			ProjectID:    "proj-1",
			RepoURL:      "https://github.com/org/repo.git",
			Ref:          "main",
			PipelinePath: customPath,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if build.PipelinePath == nil || *build.PipelinePath != customPath {
			t.Fatalf("expected pipeline_path %q, got %v", customPath, build.PipelinePath)
		}
		if build.PipelineSource == nil || *build.PipelineSource != "repo" {
			t.Fatalf("expected pipeline_source %q, got %v", "repo", build.PipelineSource)
		}
	})

	t.Run("defaults omitted working_dir to pipeline directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		if err := os.MkdirAll(tmpDir+"/scenarios/success-basic", 0o755); err != nil {
			t.Fatal(err)
		}
		customPath := "scenarios/success-basic/coyote.yml"
		yaml := `version: 1
steps:
  - name: run
    run: ./scripts/run.sh
`
		if err := os.WriteFile(tmpDir+"/"+customPath, []byte(yaml), 0o644); err != nil {
			t.Fatal(err)
		}

		repo := &fakeBuildRepository{}
		svc := NewBuildService(repo, nil, nil)
		svc.SetRepoFetcher(&fakeRepoFetcher{localPath: tmpDir, commitSHA: "abc123def456"})

		_, err := svc.CreateBuildFromRepo(context.Background(), CreateRepoBuildInput{
			ProjectID:    "proj-1",
			RepoURL:      "https://github.com/org/repo.git",
			Ref:          "main",
			PipelinePath: customPath,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(repo.steps) != 1 {
			t.Fatalf("expected 1 step, got %d", len(repo.steps))
		}
		if repo.steps[0].WorkingDir != "scenarios/success-basic" {
			t.Fatalf("expected working_dir scenarios/success-basic, got %q", repo.steps[0].WorkingDir)
		}
	})

	t.Run("resolves relative working_dir from pipeline directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		if err := os.MkdirAll(tmpDir+"/scenarios/success-basic", 0o755); err != nil {
			t.Fatal(err)
		}
		customPath := "scenarios/success-basic/coyote.yml"
		yaml := `version: 1
steps:
  - name: run
    run: ./run.sh
    working_dir: scripts
`
		if err := os.WriteFile(tmpDir+"/"+customPath, []byte(yaml), 0o644); err != nil {
			t.Fatal(err)
		}

		repo := &fakeBuildRepository{}
		svc := NewBuildService(repo, nil, nil)
		svc.SetRepoFetcher(&fakeRepoFetcher{localPath: tmpDir, commitSHA: "abc123def456"})

		_, err := svc.CreateBuildFromRepo(context.Background(), CreateRepoBuildInput{
			ProjectID:    "proj-1",
			RepoURL:      "https://github.com/org/repo.git",
			Ref:          "main",
			PipelinePath: customPath,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(repo.steps) != 1 {
			t.Fatalf("expected 1 step, got %d", len(repo.steps))
		}
		if repo.steps[0].WorkingDir != "scenarios/success-basic/scripts" {
			t.Fatalf("expected working_dir scenarios/success-basic/scripts, got %q", repo.steps[0].WorkingDir)
		}
	})

	t.Run("repo-root pipeline keeps default working_dir at root", func(t *testing.T) {
		tmpDir := t.TempDir()
		customPath := "coyote.yml"
		yaml := `version: 1
steps:
  - name: run
    run: ./scripts/run.sh
`
		if err := os.WriteFile(tmpDir+"/"+customPath, []byte(yaml), 0o644); err != nil {
			t.Fatal(err)
		}

		repo := &fakeBuildRepository{}
		svc := NewBuildService(repo, nil, nil)
		svc.SetRepoFetcher(&fakeRepoFetcher{localPath: tmpDir, commitSHA: "abc123def456"})

		_, err := svc.CreateBuildFromRepo(context.Background(), CreateRepoBuildInput{
			ProjectID:    "proj-1",
			RepoURL:      "https://github.com/org/repo.git",
			Ref:          "main",
			PipelinePath: customPath,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(repo.steps) != 1 {
			t.Fatalf("expected 1 step, got %d", len(repo.steps))
		}
		if repo.steps[0].WorkingDir != "." {
			t.Fatalf("expected working_dir ., got %q", repo.steps[0].WorkingDir)
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
		if repo.steps[0].WorkingDir != "." {
			t.Errorf("step 0 working_dir: expected '.', got %q", repo.steps[0].WorkingDir)
		}
		if repo.steps[1].WorkingDir != "." {
			t.Errorf("step 1 working_dir: expected '.', got %q", repo.steps[1].WorkingDir)
		}
	})

	t.Run("default pipeline path resolves relative working_dir from repo root", func(t *testing.T) {
		tmpDir := setupTempRepo(t, `version: 1
steps:
  - name: run
    run: ./run.sh
    working_dir: scripts
`)
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
		if len(repo.steps) != 1 {
			t.Fatalf("expected 1 step, got %d", len(repo.steps))
		}
		if repo.steps[0].WorkingDir != "scripts" {
			t.Fatalf("expected working_dir scripts, got %q", repo.steps[0].WorkingDir)
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

	t.Run("custom pipeline file not found", func(t *testing.T) {
		tmpDir := setupTempRepo(t, validYAML)
		repo := &fakeBuildRepository{}
		svc := NewBuildService(repo, nil, nil)
		svc.SetRepoFetcher(&fakeRepoFetcher{localPath: tmpDir, commitSHA: "abc"})

		_, err := svc.CreateBuildFromRepo(context.Background(), CreateRepoBuildInput{
			ProjectID:    "proj-1",
			RepoURL:      "https://example.com/repo.git",
			Ref:          "main",
			PipelinePath: "scenarios/missing/coyote.yml",
		})
		if !errors.Is(err, ErrPipelineFileNotFound) {
			t.Fatalf("expected ErrPipelineFileNotFound, got %v", err)
		}
		if !strings.Contains(err.Error(), "scenarios/missing/coyote.yml") {
			t.Fatalf("expected missing path in error, got %v", err)
		}
	})

	t.Run("rejects traversal pipeline path", func(t *testing.T) {
		tmpDir := setupTempRepo(t, validYAML)
		repo := &fakeBuildRepository{}
		svc := NewBuildService(repo, nil, nil)
		svc.SetRepoFetcher(&fakeRepoFetcher{localPath: tmpDir, commitSHA: "abc"})

		_, err := svc.CreateBuildFromRepo(context.Background(), CreateRepoBuildInput{
			ProjectID:    "proj-1",
			RepoURL:      "https://example.com/repo.git",
			Ref:          "main",
			PipelinePath: "../../foo",
		})
		if !errors.Is(err, ErrInvalidPipelinePath) {
			t.Fatalf("expected ErrInvalidPipelinePath, got %v", err)
		}
	})

	t.Run("rejects step working_dir that escapes repository root", func(t *testing.T) {
		tmpDir := t.TempDir()
		if err := os.MkdirAll(tmpDir+"/scenarios/success-basic", 0o755); err != nil {
			t.Fatal(err)
		}
		customPath := "scenarios/success-basic/coyote.yml"
		yaml := `version: 1
steps:
  - name: run
    run: ./run.sh
    working_dir: ../../../outside
`
		if err := os.WriteFile(tmpDir+"/"+customPath, []byte(yaml), 0o644); err != nil {
			t.Fatal(err)
		}

		repo := &fakeBuildRepository{}
		svc := NewBuildService(repo, nil, nil)
		svc.SetRepoFetcher(&fakeRepoFetcher{localPath: tmpDir, commitSHA: "abc"})

		_, err := svc.CreateBuildFromRepo(context.Background(), CreateRepoBuildInput{
			ProjectID:    "proj-1",
			RepoURL:      "https://example.com/repo.git",
			Ref:          "main",
			PipelinePath: customPath,
		})
		if err == nil {
			t.Fatal("expected working_dir traversal error")
		}
		if !strings.Contains(err.Error(), "working_dir") {
			t.Fatalf("expected working_dir validation error, got %v", err)
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
