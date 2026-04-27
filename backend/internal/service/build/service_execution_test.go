package build

// BuildService execution-path tests:
// - RunStep orchestration
// - execution logging and side effects
// - completion semantics under runner/repository outcomes

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/artifact"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/logs"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	steprunner "github.com/radiation/coyote-ci/backend/internal/runner"
	inprocessrunner "github.com/radiation/coyote-ci/backend/internal/runner/inprocess"
)

// RunStep orchestration and runner integration behavior.
func TestBuildService_RunStep_DelegatesToRunner(t *testing.T) {
	runner := &fakeRunner{result: steprunner.RunStepResult{Status: steprunner.RunStepStatusSuccess, ExitCode: 0, Stdout: "ok\n", Stderr: ""}}
	logSink := &fakeLogSink{}
	claimToken := "claim-active"
	repo := &fakeBuildRepository{build: domain.Build{ID: "build-1", Status: domain.BuildStatusRunning, CurrentStepIndex: 0}, steps: []domain.BuildStep{{StepIndex: 0, Name: "test", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken}}}
	svc := NewBuildService(repo, runner, logSink)

	request := steprunner.RunStepRequest{BuildID: "build-1", StepName: "test", ClaimToken: claimToken, Command: "echo", Args: []string{"ok"}}
	result, report, err := svc.RunStep(context.Background(), request)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if report.CompletionOutcome != repository.StepCompletionCompleted {
		t.Fatalf("expected completion outcome completed, got %q", report.CompletionOutcome)
	}
	if report.SideEffectErr != nil {
		t.Fatalf("expected nil side effect error, got %v", report.SideEffectErr)
	}

	if !runner.called {
		t.Fatal("expected runner to be called")
	}
	if runner.lastRequest.Command != "echo" {
		t.Fatalf("expected command echo, got %q", runner.lastRequest.Command)
	}
	if result.Status != steprunner.RunStepStatusSuccess {
		t.Fatalf("expected success status, got %q", result.Status)
	}
	if repo.steps[0].ExitCode == nil || *repo.steps[0].ExitCode != 0 {
		t.Fatalf("expected persisted exit code 0, got %v", repo.steps[0].ExitCode)
	}
	if repo.steps[0].Stdout == nil || *repo.steps[0].Stdout != "ok\n" {
		t.Fatalf("expected persisted stdout ok, got %v", repo.steps[0].Stdout)
	}
	if logSink.calls == 0 {
		t.Fatal("expected at least one log write")
	}
	foundOutput := false
	for _, line := range logSink.lines {
		if line == "ok" {
			foundOutput = true
			break
		}
	}
	if !foundOutput {
		t.Fatalf("expected output line 'ok' in logs, got %#v", logSink.lines)
	}
}

func TestBuildService_RunStep_PreparesBuildScopedEnvironmentWithRepoMetadataAndDefaultImageFallback(t *testing.T) {
	runner := &fakeBuildScopedRunner{fakeRunner: fakeRunner{result: steprunner.RunStepResult{Status: steprunner.RunStepStatusSuccess, ExitCode: 0, Stdout: "ok\n", Stderr: ""}}}
	claimToken := "claim-active"
	repoURL := "https://github.com/org/repo.git"
	ref := "main"
	commitSHA := "abc123"
	buildID := "build-repo-reuse"

	buildRepo := &fakeBuildRepository{
		build: domain.Build{ID: buildID, Status: domain.BuildStatusRunning, CurrentStepIndex: 0, RepoURL: &repoURL, Ref: &ref, CommitSHA: &commitSHA},
		steps: []domain.BuildStep{{StepIndex: 0, Name: "test", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken}},
	}

	svc := NewBuildService(buildRepo, runner, &fakeLogSink{})
	svc.SetDefaultExecutionImage("golang:1.23-alpine")
	svc.SetExecutionWorkspaceRoot(t.TempDir())
	svc.SetSourceResolver(&fakeWorkspaceSourceResolver{})

	request := steprunner.RunStepRequest{BuildID: buildID, StepIndex: 0, StepName: "test", ClaimToken: claimToken, Command: "echo", Args: []string{"ok"}, WorkingDir: "backend"}
	if _, _, err := svc.RunStep(context.Background(), request); err != nil {
		t.Fatalf("expected run to succeed, got %v", err)
	}
	if runner.prepareCalls != 1 {
		t.Fatalf("expected one prepare call, got %d", runner.prepareCalls)
	}
	if runner.lastPrepare.BuildID != buildID {
		t.Fatalf("expected prepare build id %q, got %q", buildID, runner.lastPrepare.BuildID)
	}
	if runner.lastPrepare.RepoURL != repoURL {
		t.Fatalf("expected prepare repo url %q, got %q", repoURL, runner.lastPrepare.RepoURL)
	}
	if runner.lastPrepare.Ref != ref {
		t.Fatalf("expected prepare ref %q, got %q", ref, runner.lastPrepare.Ref)
	}
	if runner.lastPrepare.CommitSHA != commitSHA {
		t.Fatalf("expected prepare commit sha %q, got %q", commitSHA, runner.lastPrepare.CommitSHA)
	}
	if runner.lastPrepare.Image != "golang:1.23-alpine" {
		t.Fatalf("expected default execution image fallback, got %q", runner.lastPrepare.Image)
	}
}

func TestBuildService_RunStep_PreparesBuildScopedEnvironmentWithPipelineImageOverride(t *testing.T) {
	runner := &fakeBuildScopedRunner{fakeRunner: fakeRunner{result: steprunner.RunStepResult{Status: steprunner.RunStepStatusSuccess, ExitCode: 0, Stdout: "ok\n", Stderr: ""}}}
	claimToken := "claim-active"
	repoURL := "https://github.com/org/repo.git"
	ref := "main"
	commitSHA := "abc123"
	buildID := "build-repo-override"
	pipelineYAML := `
version: 1
pipeline:
  name: backend-ci
  image: golang:1.24
steps:
  - name: test
    run: go test ./...
`

	buildRepo := &fakeBuildRepository{
		build: domain.Build{ID: buildID, Status: domain.BuildStatusRunning, CurrentStepIndex: 0, RepoURL: &repoURL, Ref: &ref, CommitSHA: &commitSHA, PipelineConfigYAML: &pipelineYAML},
		steps: []domain.BuildStep{{StepIndex: 0, Name: "test", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken}},
	}

	svc := NewBuildService(buildRepo, runner, &fakeLogSink{})
	svc.SetDefaultExecutionImage("alpine:3.20")
	svc.SetExecutionWorkspaceRoot(t.TempDir())
	svc.SetSourceResolver(&fakeWorkspaceSourceResolver{})

	request := steprunner.RunStepRequest{BuildID: buildID, StepIndex: 0, StepName: "test", ClaimToken: claimToken, Command: "echo", Args: []string{"ok"}, WorkingDir: "backend"}
	if _, _, err := svc.RunStep(context.Background(), request); err != nil {
		t.Fatalf("expected run to succeed, got %v", err)
	}
	if runner.prepareCalls != 1 {
		t.Fatalf("expected one prepare call, got %d", runner.prepareCalls)
	}
	if runner.lastPrepare.Image != "golang:1.24" {
		t.Fatalf("expected pipeline execution image override, got %q", runner.lastPrepare.Image)
	}
}

func TestBuildService_RunStep_CleansUpBuildScopedEnvironmentOnTerminalBuild(t *testing.T) {
	runner := &fakeBuildScopedRunner{fakeRunner: fakeRunner{result: steprunner.RunStepResult{Status: steprunner.RunStepStatusSuccess, ExitCode: 0, Stdout: "ok\n", Stderr: ""}}}
	claimToken := "claim-active"
	repo := &fakeBuildRepository{
		build: domain.Build{ID: "build-terminal", Status: domain.BuildStatusRunning, CurrentStepIndex: 0},
		steps: []domain.BuildStep{{StepIndex: 0, Name: "step-1", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken}},
	}

	svc := NewBuildService(repo, runner, &fakeLogSink{})
	_, report, err := svc.RunStep(context.Background(), steprunner.RunStepRequest{BuildID: "build-terminal", StepIndex: 0, StepName: "step-1", ClaimToken: claimToken, Command: "echo", Args: []string{"ok"}})
	if err != nil {
		t.Fatalf("expected run to succeed, got %v", err)
	}
	if report.CompletionOutcome != repository.StepCompletionCompleted {
		t.Fatalf("expected completion to persist, got %q", report.CompletionOutcome)
	}
	if runner.cleanupCalls != 1 {
		t.Fatalf("expected cleanup to run once for terminal build, got %d", runner.cleanupCalls)
	}
}

func TestBuildService_RunStep_CollectsArtifactsBeforeCleanup(t *testing.T) {
	workspaceRoot := t.TempDir()
	buildID := "build-terminal"
	claimToken := "claim-active"

	workspacePath := filepath.Join(workspaceRoot, buildID)
	if err := os.MkdirAll(filepath.Join(workspacePath, "dist"), 0o755); err != nil {
		t.Fatalf("failed creating workspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, "dist", "app"), []byte("artifact-body"), 0o644); err != nil {
		t.Fatalf("failed writing artifact file: %v", err)
	}

	runner := &fakeBuildScopedRunner{fakeRunner: fakeRunner{result: steprunner.RunStepResult{Status: steprunner.RunStepStatusSuccess, ExitCode: 0, Stdout: "ok\n", Stderr: ""}}}
	events := make([]string, 0)
	runner.onCleanup = func() {
		events = append(events, "cleanup")
	}

	pipelineYAML := "version: 1\nsteps:\n  - name: build\n    run: make build\nartifacts:\n  paths:\n    - dist/**\n"
	repo := &fakeBuildRepository{
		build: domain.Build{ID: buildID, Status: domain.BuildStatusRunning, CurrentStepIndex: 0, PipelineConfigYAML: &pipelineYAML},
		steps: []domain.BuildStep{{StepIndex: 0, Name: "step-1", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken}},
	}
	artifactRepo := &fakeArtifactRepository{}

	svc := NewBuildService(repo, runner, &fakeLogSink{})
	svc.SetArtifactPersistence(artifactRepo, testStoreResolver(&recordingStore{events: &events}), workspaceRoot)

	_, report, err := svc.RunStep(context.Background(), steprunner.RunStepRequest{BuildID: buildID, StepIndex: 0, StepName: "step-1", ClaimToken: claimToken, Command: "echo", Args: []string{"ok"}, WorkingDir: "."})
	if err != nil {
		t.Fatalf("expected run to succeed, got %v", err)
	}
	if report.SideEffectErr != nil {
		t.Fatalf("expected nil side effect error, got %v", report.SideEffectErr)
	}

	if len(events) < 2 {
		t.Fatalf("expected artifact save and cleanup events, got %#v", events)
	}
	if !strings.HasPrefix(events[0], "save:") {
		t.Fatalf("expected first event to be artifact save, got %#v", events)
	}
	if events[len(events)-1] != "cleanup" {
		t.Fatalf("expected cleanup after artifact collection, got %#v", events)
	}

	artifacts := artifactRepo.artifacts[buildID]
	if len(artifacts) != 1 {
		t.Fatalf("expected one persisted artifact, got %d", len(artifacts))
	}
	if artifacts[0].LogicalPath != "dist/app" {
		t.Fatalf("expected logical path dist/app, got %q", artifacts[0].LogicalPath)
	}
}

func TestBuildService_RunStep_MissingArtifactPathsDoNotFailBuild(t *testing.T) {
	workspaceRoot := t.TempDir()
	buildID := "build-terminal"
	claimToken := "claim-active"

	if err := os.MkdirAll(filepath.Join(workspaceRoot, buildID), 0o755); err != nil {
		t.Fatalf("failed creating workspace: %v", err)
	}

	runner := &fakeBuildScopedRunner{fakeRunner: fakeRunner{result: steprunner.RunStepResult{Status: steprunner.RunStepStatusSuccess, ExitCode: 0, Stdout: "ok\n", Stderr: ""}}}
	pipelineYAML := "version: 1\nsteps:\n  - name: build\n    run: make build\nartifacts:\n  paths:\n    - reports/*.xml\n"
	repo := &fakeBuildRepository{
		build: domain.Build{ID: buildID, Status: domain.BuildStatusRunning, CurrentStepIndex: 0, PipelineConfigYAML: &pipelineYAML},
		steps: []domain.BuildStep{{StepIndex: 0, Name: "step-1", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken}},
	}
	artifactRepo := &fakeArtifactRepository{}

	svc := NewBuildService(repo, runner, &fakeLogSink{})
	svc.SetArtifactPersistence(artifactRepo, testStoreResolver(artifact.NewFilesystemStore(t.TempDir())), workspaceRoot)

	_, report, err := svc.RunStep(context.Background(), steprunner.RunStepRequest{BuildID: buildID, StepIndex: 0, StepName: "step-1", ClaimToken: claimToken, Command: "echo", Args: []string{"ok"}, WorkingDir: "."})
	if err != nil {
		t.Fatalf("expected run to succeed, got %v", err)
	}
	if report.CompletionOutcome != repository.StepCompletionCompleted {
		t.Fatalf("expected completed outcome, got %q", report.CompletionOutcome)
	}
	if report.SideEffectErr != nil {
		t.Fatalf("expected no side effect error for missing artifact paths, got %v", report.SideEffectErr)
	}
	if runner.cleanupCalls != 1 {
		t.Fatalf("expected cleanup to run once, got %d", runner.cleanupCalls)
	}
	if len(artifactRepo.artifacts[buildID]) != 0 {
		t.Fatalf("expected no persisted artifacts for unmatched paths, got %d", len(artifactRepo.artifacts[buildID]))
	}
}

func TestBuildService_RunStep_ConvergesAfterPartialArtifactPersistence(t *testing.T) {
	workspaceRoot := t.TempDir()
	buildID := "build-terminal"
	claimToken := "claim-active"
	now := time.Now().UTC()

	workspacePath := filepath.Join(workspaceRoot, buildID)
	if err := os.MkdirAll(filepath.Join(workspacePath, "dist"), 0o755); err != nil {
		t.Fatalf("failed creating workspace: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(workspacePath, "reports"), 0o755); err != nil {
		t.Fatalf("failed creating workspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, "dist", "app"), []byte("artifact-one"), 0o644); err != nil {
		t.Fatalf("failed writing dist artifact: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, "reports", "junit.xml"), []byte("artifact-two"), 0o644); err != nil {
		t.Fatalf("failed writing report artifact: %v", err)
	}

	runner := &fakeBuildScopedRunner{fakeRunner: fakeRunner{result: steprunner.RunStepResult{Status: steprunner.RunStepStatusSuccess, ExitCode: 0, Stdout: "ok\n", Stderr: ""}}}
	events := make([]string, 0)
	runner.onCleanup = func() {
		events = append(events, "cleanup")
	}

	pipelineYAML := "version: 1\nsteps:\n  - name: build\n    run: make build\nartifacts:\n  paths:\n    - dist/**\n    - reports/*.xml\n"
	repo := &fakeBuildRepository{
		build: domain.Build{ID: buildID, Status: domain.BuildStatusRunning, CurrentStepIndex: 0, PipelineConfigYAML: &pipelineYAML},
		steps: []domain.BuildStep{{StepIndex: 0, Name: "step-1", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken}},
	}
	artifactRepo := &fakeArtifactRepository{artifacts: map[string][]domain.BuildArtifact{
		buildID: {
			{ID: "existing", BuildID: buildID, LogicalPath: "dist/app", StorageKey: buildID + "/dist/app", CreatedAt: now},
		},
	}}

	svc := NewBuildService(repo, runner, &fakeLogSink{})
	svc.SetArtifactPersistence(artifactRepo, testStoreResolver(&recordingStore{events: &events}), workspaceRoot)

	_, report, err := svc.RunStep(context.Background(), steprunner.RunStepRequest{BuildID: buildID, StepIndex: 0, StepName: "step-1", ClaimToken: claimToken, Command: "echo", Args: []string{"ok"}, WorkingDir: "."})
	if err != nil {
		t.Fatalf("expected run to succeed, got %v", err)
	}
	if report.SideEffectErr != nil {
		t.Fatalf("expected nil side effect error, got %v", report.SideEffectErr)
	}

	artifacts := artifactRepo.artifacts[buildID]
	if len(artifacts) != 2 {
		t.Fatalf("expected two artifacts after convergence, got %d", len(artifacts))
	}

	seen := map[string]int{}
	for _, item := range artifacts {
		seen[item.LogicalPath]++
	}
	if seen["dist/app"] != 1 || seen["reports/junit.xml"] != 1 {
		t.Fatalf("expected one entry per logical path, got %#v", seen)
	}

	saveCount := 0
	for _, event := range events {
		if strings.HasPrefix(event, "save:") {
			saveCount++
		}
	}
	if saveCount != 1 {
		t.Fatalf("expected only one save for missing artifact, got %d events=%#v", saveCount, events)
	}
}

func TestBuildService_RunStep_SkipsCleanupWhenArtifactCollectionFails(t *testing.T) {
	workspaceRoot := t.TempDir()
	buildID := "build-terminal"
	claimToken := "claim-active"

	workspacePath := filepath.Join(workspaceRoot, buildID)
	if err := os.MkdirAll(filepath.Join(workspacePath, "dist"), 0o755); err != nil {
		t.Fatalf("failed creating workspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, "dist", "app"), []byte("artifact-body"), 0o644); err != nil {
		t.Fatalf("failed writing artifact file: %v", err)
	}

	runner := &fakeBuildScopedRunner{fakeRunner: fakeRunner{result: steprunner.RunStepResult{Status: steprunner.RunStepStatusSuccess, ExitCode: 0, Stdout: "ok\n", Stderr: ""}}}
	pipelineYAML := "version: 1\nsteps:\n  - name: build\n    run: make build\nartifacts:\n  paths:\n    - dist/**\n"
	repo := &fakeBuildRepository{
		build: domain.Build{ID: buildID, Status: domain.BuildStatusRunning, CurrentStepIndex: 0, PipelineConfigYAML: &pipelineYAML},
		steps: []domain.BuildStep{{StepIndex: 0, Name: "step-1", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken}},
	}

	logSink := &fakeLogSink{}
	svc := NewBuildService(repo, runner, logSink)
	svc.SetArtifactPersistence(&fakeArtifactRepository{}, testStoreResolver(&failingStore{err: errors.New("store unavailable")}), workspaceRoot)

	_, report, err := svc.RunStep(context.Background(), steprunner.RunStepRequest{BuildID: buildID, StepIndex: 0, StepName: "step-1", ClaimToken: claimToken, Command: "echo", Args: []string{"ok"}})
	if err != nil {
		t.Fatalf("expected run to succeed, got %v", err)
	}
	if report.SideEffectErr == nil {
		t.Fatal("expected side effect error from artifact collection failure")
	}
	assertMessagesContain(t, logSink.lines,
		"Artifact collection failed",
		"Failure reason: artifact collection failed",
	)
	if runner.cleanupCalls != 0 {
		t.Fatalf("expected cleanup to be skipped on artifact failure, got %d", runner.cleanupCalls)
	}
}

func TestBuildService_CollectArtifactsIfTerminal_IsIdempotent(t *testing.T) {
	workspaceRoot := t.TempDir()
	buildID := "build-idempotent"

	workspacePath := filepath.Join(workspaceRoot, buildID)
	if err := os.MkdirAll(filepath.Join(workspacePath, "dist"), 0o755); err != nil {
		t.Fatalf("failed creating workspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, "dist", "app"), []byte("artifact-body"), 0o644); err != nil {
		t.Fatalf("failed writing artifact file: %v", err)
	}

	pipelineYAML := "version: 1\nsteps:\n  - name: build\n    run: make build\nartifacts:\n  paths:\n    - dist/**\n"
	repo := &fakeBuildRepository{
		build: domain.Build{ID: buildID, Status: domain.BuildStatusSuccess, CurrentStepIndex: 1, PipelineConfigYAML: &pipelineYAML},
	}
	events := make([]string, 0)
	artifactRepo := &fakeArtifactRepository{}

	svc := NewBuildService(repo, nil, &fakeLogSink{})
	svc.SetArtifactPersistence(artifactRepo, testStoreResolver(&recordingStore{events: &events}), workspaceRoot)

	if _, err := svc.collectArtifactsIfTerminal(context.Background(), buildID); err != nil {
		t.Fatalf("expected first collection to succeed, got %v", err)
	}
	if _, err := svc.collectArtifactsIfTerminal(context.Background(), buildID); err != nil {
		t.Fatalf("expected second collection to succeed, got %v", err)
	}

	artifacts := artifactRepo.artifacts[buildID]
	if len(artifacts) != 1 {
		t.Fatalf("expected one persisted artifact without duplicates, got %d", len(artifacts))
	}

	saveCount := 0
	for _, event := range events {
		if strings.HasPrefix(event, "save:") {
			saveCount++
		}
	}
	if saveCount != 1 {
		t.Fatalf("expected one storage save across repeated runs, got %d events=%#v", saveCount, events)
	}
}

func TestBuildService_RunStep_InprocessRunner_PersistsArtifactsToStorageRoot(t *testing.T) {
	workspaceRoot := t.TempDir()
	storageRoot := t.TempDir()
	buildID := "build-inprocess"
	claimToken := "claim-active"

	pipelineYAML := "version: 1\nsteps:\n  - name: build\n    run: make build\nartifacts:\n  paths:\n    - dist/**\n    - reports/*.xml\n"
	repo := &fakeBuildRepository{
		build: domain.Build{ID: buildID, Status: domain.BuildStatusRunning, CurrentStepIndex: 0, PipelineConfigYAML: &pipelineYAML},
		steps: []domain.BuildStep{{StepIndex: 0, Name: "step-1", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken}},
	}
	artifactRepo := &fakeArtifactRepository{}

	svc := NewBuildService(repo, inprocessrunner.NewWithWorkspaceRoot(workspaceRoot), &fakeLogSink{})
	svc.SetArtifactPersistence(artifactRepo, testStoreResolver(artifact.NewFilesystemStore(storageRoot)), workspaceRoot)

	_, report, err := svc.RunStep(context.Background(), steprunner.RunStepRequest{
		BuildID:    buildID,
		StepIndex:  0,
		StepName:   "step-1",
		ClaimToken: claimToken,
		WorkingDir: ".",
		Command:    "sh",
		Args: []string{
			"-c",
			"mkdir -p dist reports && echo 'hello world' > dist/hello.txt && echo '{\"ok\":true}' > dist/result.json && echo '<testsuite></testsuite>' > reports/test.xml",
		},
	})
	if err != nil {
		t.Fatalf("expected run to succeed, got %v", err)
	}
	if report.CompletionOutcome != repository.StepCompletionCompleted {
		t.Fatalf("expected completed outcome, got %q", report.CompletionOutcome)
	}
	if report.SideEffectErr != nil {
		t.Fatalf("expected nil side effect error, got %v", report.SideEffectErr)
	}

	artifacts := artifactRepo.artifacts[buildID]
	if len(artifacts) != 3 {
		t.Fatalf("expected three persisted artifacts, got %d", len(artifacts))
	}

	// Verify artifacts exist on disk at their storage key paths
	for _, a := range artifacts {
		storagePath := filepath.Join(storageRoot, a.StorageKey)
		if _, statErr := os.Stat(storagePath); statErr != nil {
			t.Fatalf("expected persisted artifact at %s (logical=%s), stat failed: %v", storagePath, a.LogicalPath, statErr)
		}
	}
}

func TestBuildService_RunStep_RunnerError(t *testing.T) {
	runner := &fakeRunner{err: errors.New("runner failed")}
	claimToken := "claim-active"
	repo := &fakeBuildRepository{build: domain.Build{ID: "build-1", Status: domain.BuildStatusRunning, CurrentStepIndex: 0}, steps: []domain.BuildStep{{StepIndex: 0, Name: "echo", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken}}}
	svc := NewBuildService(repo, runner, &fakeLogSink{})

	_, report, err := svc.RunStep(context.Background(), steprunner.RunStepRequest{BuildID: "build-1", StepName: "echo", ClaimToken: claimToken, Command: "echo"})
	if err == nil || err.Error() != "runner failed" {
		t.Fatalf("expected runner error, got %v", err)
	}
	if report.CompletionOutcome != repository.StepCompletionCompleted {
		t.Fatalf("expected completion outcome completed, got %q", report.CompletionOutcome)
	}
	if report.SideEffectErr != nil {
		t.Fatalf("expected nil side effect error, got %v", report.SideEffectErr)
	}
}

func TestBuildService_RunStep_ReturnsExecutionResultWhenCompletionPersistenceFails(t *testing.T) {
	runner := &fakeRunner{result: steprunner.RunStepResult{Status: steprunner.RunStepStatusFailed, ExitCode: 7, Stderr: "boom"}}
	claimToken := "claim-active"
	repo := &fakeBuildRepository{
		build:     domain.Build{ID: "build-1", Status: domain.BuildStatusRunning, CurrentStepIndex: 0},
		steps:     []domain.BuildStep{{StepIndex: 0, Name: "echo", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken}},
		updateErr: errors.New("persist failed"),
	}
	svc := NewBuildService(repo, runner, &fakeLogSink{})

	result, report, err := svc.RunStep(context.Background(), steprunner.RunStepRequest{BuildID: "build-1", StepIndex: 0, StepName: "echo", ClaimToken: claimToken, Command: "sh", Args: []string{"-c", "exit 1"}})
	if err == nil || err.Error() != "persist failed" {
		t.Fatalf("expected persistence error, got %v", err)
	}
	if report.CompletionOutcome != repository.StepCompletionInvalidTransition {
		t.Fatalf("expected invalid transition outcome on persistence error, got %q", report.CompletionOutcome)
	}
	if result.Status != steprunner.RunStepStatusFailed {
		t.Fatalf("expected failed status from runner result, got %q", result.Status)
	}
	if result.ExitCode != 7 {
		t.Fatalf("expected runner exit code 7, got %d", result.ExitCode)
	}
}

func TestBuildService_RunStep_PersistsLogsForSuccessAndFailedResults(t *testing.T) {
	tests := []struct {
		name          string
		runnerResult  steprunner.RunStepResult
		expectedLines []string
	}{
		{
			name: "success output logs",
			runnerResult: steprunner.RunStepResult{
				Status: steprunner.RunStepStatusSuccess,
				Stdout: "line-1\nline-2\n",
				Stderr: "",
			},
			expectedLines: []string{"line-1", "line-2"},
		},
		{
			name: "failed output logs",
			runnerResult: steprunner.RunStepResult{
				Status: steprunner.RunStepStatusFailed,
				Stdout: "",
				Stderr: "err-1\nerr-2\n",
			},
			expectedLines: []string{"err-1", "err-2"},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			claimToken := "claim-active"
			repo := &fakeBuildRepository{build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: time.Now().UTC()}, steps: []domain.BuildStep{{StepIndex: 0, Name: "step-1", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken}}}
			runner := &fakeRunner{result: tc.runnerResult}
			logStore := logs.NewMemorySink()
			svc := NewBuildService(repo, runner, logStore)

			if _, report, err := svc.RunStep(context.Background(), steprunner.RunStepRequest{BuildID: "build-1", StepName: "step-1", ClaimToken: claimToken, Command: "echo"}); err != nil {
				t.Fatalf("run step failed: %v", err)
			} else if report.CompletionOutcome != repository.StepCompletionCompleted {
				t.Fatalf("expected completion outcome completed, got %q", report.CompletionOutcome)
			} else if report.SideEffectErr != nil {
				t.Fatalf("expected nil side effect error, got %v", report.SideEffectErr)
			}

			buildLogs, err := svc.GetBuildLogs(context.Background(), "build-1")
			if err != nil {
				t.Fatalf("get build logs failed: %v", err)
			}
			for _, expectedLine := range tc.expectedLines {
				found := false
				for _, buildLog := range buildLogs {
					if buildLog.Message == expectedLine {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("expected log line %q in logs, got %#v", expectedLine, buildLogs)
				}
			}
		})
	}
}

func TestBuildService_RunStep_WritesStructuredStepAndBuildMarkers(t *testing.T) {
	startedAt := time.Now().UTC()
	finishedAt := startedAt.Add(800 * time.Millisecond)

	claimToken := "claim-active"
	repo := &fakeBuildRepository{
		build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: startedAt.Add(-2 * time.Second)},
		steps: []domain.BuildStep{{StepIndex: 0, Name: "step-1", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken}},
	}
	r := &fakeRunner{result: steprunner.RunStepResult{Status: steprunner.RunStepStatusSuccess, ExitCode: 0, Stdout: "ok\n", StartedAt: startedAt, FinishedAt: finishedAt}}
	logStore := logs.NewMemorySink()

	svc := NewBuildService(repo, r, logStore)
	svc.SetDefaultExecutionImage("golang:1.26")

	_, report, err := svc.RunStep(context.Background(), steprunner.RunStepRequest{
		BuildID:    "build-1",
		StepIndex:  0,
		StepName:   "step-1",
		ClaimToken: claimToken,
		Command:    "sh",
		Args:       []string{"-c", "echo \"hello\""},
	})
	if err != nil {
		t.Fatalf("run step failed: %v", err)
	}
	if report.CompletionOutcome != repository.StepCompletionCompleted {
		t.Fatalf("expected completion outcome completed, got %q", report.CompletionOutcome)
	}

	buildLogs, err := svc.GetBuildLogs(context.Background(), "build-1")
	if err != nil {
		t.Fatalf("get build logs failed: %v", err)
	}

	messages := make([]string, 0, len(buildLogs))
	for _, line := range buildLogs {
		messages = append(messages, line.Message)
	}

	assertMessagesContain(t, messages,
		"Starting build",
		"Pipeline image: golang:1.26",
		"Workspace: /workspace",
		"Steps: 1",
		"==> Step 1/1: step-1",
		"Image: golang:1.26",
		"Working directory: /workspace",
		"Command:",
		"echo \"hello\"",
		"<== Step 1/1: step-1 succeeded in 0.8s",
		"Build succeeded in",
		"Artifacts collected: 0",
	)
}

func TestBuildService_RunStep_WritesFailureMarkerWithExitCode(t *testing.T) {
	startedAt := time.Now().UTC()
	finishedAt := startedAt.Add(4200 * time.Millisecond)

	claimToken := "claim-active"
	repo := &fakeBuildRepository{
		build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: startedAt.Add(-2 * time.Second)},
		steps: []domain.BuildStep{{StepIndex: 0, Name: "test", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken}},
	}
	r := &fakeRunner{result: steprunner.RunStepResult{Status: steprunner.RunStepStatusFailed, ExitCode: 1, Stderr: "boom\n", StartedAt: startedAt, FinishedAt: finishedAt}}
	logStore := logs.NewMemorySink()

	svc := NewBuildService(repo, r, logStore)
	svc.SetDefaultExecutionImage("golang:1.26")

	_, report, err := svc.RunStep(context.Background(), steprunner.RunStepRequest{
		BuildID:    "build-1",
		StepIndex:  0,
		StepName:   "test",
		ClaimToken: claimToken,
		Command:    "sh",
		Args:       []string{"-c", "go test ./..."},
	})
	if err != nil {
		t.Fatalf("run step failed: %v", err)
	}
	if report.CompletionOutcome != repository.StepCompletionCompleted {
		t.Fatalf("expected completion outcome completed, got %q", report.CompletionOutcome)
	}

	buildLogs, err := svc.GetBuildLogs(context.Background(), "build-1")
	if err != nil {
		t.Fatalf("get build logs failed: %v", err)
	}

	messages := make([]string, 0, len(buildLogs))
	for _, line := range buildLogs {
		messages = append(messages, line.Message)
	}

	assertMessagesContain(t, messages,
		"<== Step 1/1: test failed in 4.2s (exit code 1)",
		"Failure reason: command exited with code 1",
		"Build failed in",
		"Failure summary: see failed step marker(s) above for exit details",
	)
}

func TestBuildService_RunStep_WritesTimeoutFailureMarkerAndReason(t *testing.T) {
	startedAt := time.Now().UTC()
	finishedAt := startedAt.Add(600 * time.Second)

	claimToken := "claim-active"
	repo := &fakeBuildRepository{
		build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: startedAt.Add(-2 * time.Second)},
		steps: []domain.BuildStep{{StepIndex: 0, Name: "test", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken}},
	}
	r := &fakeRunner{result: steprunner.RunStepResult{Status: steprunner.RunStepStatusFailed, ExitCode: -1, Stderr: "step execution timed out after 10m0s", StartedAt: startedAt, FinishedAt: finishedAt}}
	logStore := logs.NewMemorySink()

	svc := NewBuildService(repo, r, logStore)

	_, report, err := svc.RunStep(context.Background(), steprunner.RunStepRequest{
		BuildID:    "build-1",
		StepIndex:  0,
		StepName:   "test",
		ClaimToken: claimToken,
		Command:    "sh",
		Args:       []string{"-c", "sleep 999"},
	})
	if err != nil {
		t.Fatalf("run step failed: %v", err)
	}
	if report.CompletionOutcome != repository.StepCompletionCompleted {
		t.Fatalf("expected completion outcome completed, got %q", report.CompletionOutcome)
	}

	buildLogs, err := svc.GetBuildLogs(context.Background(), "build-1")
	if err != nil {
		t.Fatalf("get build logs failed: %v", err)
	}

	messages := make([]string, 0, len(buildLogs))
	for _, line := range buildLogs {
		messages = append(messages, line.Message)
	}

	assertMessagesContain(t, messages,
		"<== Step 1/1: test failed in 600.0s (timed out)",
		"Failure reason: step execution timed out after 10m0s",
	)
}

func TestBuildService_RunStep_PrepareFailureEmitsCanonicalContainerStartupFailure(t *testing.T) {
	claimToken := "claim-active"
	repo := &fakeBuildRepository{
		build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: time.Now().UTC()},
		steps: []domain.BuildStep{{StepIndex: 0, Name: "step-1", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken}},
	}
	r := &fakeBuildScopedRunner{prepareErr: errors.New("creating build container: docker command failed")}
	logStore := logs.NewMemorySink()

	svc := NewBuildService(repo, r, logStore)
	_, report, err := svc.RunStep(context.Background(), steprunner.RunStepRequest{
		BuildID:    "build-1",
		StepIndex:  0,
		StepName:   "step-1",
		ClaimToken: claimToken,
		Command:    "echo",
	})
	if err == nil {
		t.Fatal("expected prepare failure error")
	}
	if report.CompletionOutcome != repository.StepCompletionCompleted {
		t.Fatalf("expected completion outcome completed, got %q", report.CompletionOutcome)
	}

	buildLogs, getErr := svc.GetBuildLogs(context.Background(), "build-1")
	if getErr != nil {
		t.Fatalf("get build logs failed: %v", getErr)
	}

	messages := make([]string, 0, len(buildLogs))
	for _, line := range buildLogs {
		messages = append(messages, line.Message)
	}

	assertMessagesContain(t, messages,
		"Failed to prepare workspace",
		"Failure reason: docker create failed",
	)
}

func TestBuildService_RunStep_EmitsHighSignalPhaseMarkers(t *testing.T) {
	workspaceRoot := t.TempDir()
	buildID := "build-phase-markers"
	claimToken := "claim-active"

	workspacePath := filepath.Join(workspaceRoot, buildID)
	if err := os.MkdirAll(filepath.Join(workspacePath, "dist"), 0o755); err != nil {
		t.Fatalf("failed creating workspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, "dist", "app"), []byte("artifact-body"), 0o644); err != nil {
		t.Fatalf("failed writing artifact file: %v", err)
	}

	pipelineYAML := "version: 1\nsteps:\n  - name: build\n    run: make build\nartifacts:\n  - dist/**\n"
	repo := &fakeBuildRepository{
		build: domain.Build{ID: buildID, Status: domain.BuildStatusRunning, CurrentStepIndex: 0, PipelineConfigYAML: &pipelineYAML, CreatedAt: time.Now().UTC()},
		steps: []domain.BuildStep{{StepIndex: 0, Name: "step-1", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken}},
	}
	r := &fakeBuildScopedRunner{fakeRunner: fakeRunner{result: steprunner.RunStepResult{Status: steprunner.RunStepStatusSuccess, ExitCode: 0, Stdout: "ok\n", StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC()}}}
	logStore := logs.NewMemorySink()
	artifactRepo := &fakeArtifactRepository{}

	svc := NewBuildService(repo, r, logStore)
	svc.SetArtifactPersistence(artifactRepo, testStoreResolver(artifact.NewFilesystemStore(t.TempDir())), workspaceRoot)

	_, report, err := svc.RunStep(context.Background(), steprunner.RunStepRequest{BuildID: buildID, StepIndex: 0, StepName: "step-1", ClaimToken: claimToken, Command: "echo", Args: []string{"ok"}, WorkingDir: "."})
	if err != nil {
		t.Fatalf("run step failed: %v", err)
	}
	if report.CompletionOutcome != repository.StepCompletionCompleted {
		t.Fatalf("expected completion outcome completed, got %q", report.CompletionOutcome)
	}

	buildLogs, err := svc.GetBuildLogs(context.Background(), buildID)
	if err != nil {
		t.Fatalf("get build logs failed: %v", err)
	}
	messages := make([]string, 0, len(buildLogs))
	for _, line := range buildLogs {
		messages = append(messages, line.Message)
	}

	assertMessagesContain(t, messages,
		"Attaching workspace",
		"Workspace attached",
		"Executing pipeline steps",
		"Collecting artifacts",
		"Assigning version tags",
		"Finalizing build",
	)
}

func TestBuildService_RunStep_AutoTagsOutputsAfterTerminalSuccess(t *testing.T) {
	workspaceRoot := t.TempDir()
	buildID := "build-auto-tags"
	jobID := "job-1"
	claimToken := "claim-active"
	managedImageVersionID := "managed-version-1"

	workspacePath := filepath.Join(workspaceRoot, buildID)
	if err := os.MkdirAll(filepath.Join(workspacePath, "dist"), 0o755); err != nil {
		t.Fatalf("failed creating workspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, "dist", "app"), []byte("artifact-body"), 0o644); err != nil {
		t.Fatalf("failed writing artifact file: %v", err)
	}

	pipelineYAML := "version: 1\nrelease:\n  strategy: template\n  template: 0.1.{build_number}\nsteps:\n  - name: build\n    run: make build\nartifacts:\n  - dist/**\n"
	repo := &fakeBuildRepository{
		build: domain.Build{
			ID:                    buildID,
			BuildNumber:           7,
			ProjectID:             "project-1",
			JobID:                 &jobID,
			Status:                domain.BuildStatusRunning,
			CurrentStepIndex:      0,
			PipelineConfigYAML:    &pipelineYAML,
			ManagedImageVersionID: &managedImageVersionID,
			CreatedAt:             time.Now().UTC(),
		},
		steps: []domain.BuildStep{{StepIndex: 0, Name: "step-1", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken, ArtifactPaths: []string{"dist/**"}}},
	}
	r := &fakeBuildScopedRunner{fakeRunner: fakeRunner{result: steprunner.RunStepResult{Status: steprunner.RunStepStatusSuccess, ExitCode: 0, Stdout: "ok\n", StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC()}}}
	logStore := logs.NewMemorySink()
	artifactRepo := &fakeArtifactRepository{}
	tagger := &fakeBuildVersionTagger{resolvedVersion: "0.1.7"}

	svc := NewBuildService(repo, r, logStore)
	svc.SetArtifactPersistence(artifactRepo, testStoreResolver(artifact.NewFilesystemStore(t.TempDir())), workspaceRoot)
	svc.versionTagger = tagger

	_, report, err := svc.RunStep(context.Background(), steprunner.RunStepRequest{BuildID: buildID, StepIndex: 0, StepName: "step-1", ClaimToken: claimToken, Command: "echo", Args: []string{"ok"}, WorkingDir: "."})
	if err != nil {
		t.Fatalf("run step failed: %v", err)
	}
	if report.CompletionOutcome != repository.StepCompletionCompleted {
		t.Fatalf("expected completion outcome completed, got %q", report.CompletionOutcome)
	}
	if report.SideEffectErr != nil {
		t.Fatalf("expected no side effect error, got %v", report.SideEffectErr)
	}
	if tagger.calls != 1 {
		t.Fatalf("expected one auto-tagging call, got %d", tagger.calls)
	}
	if tagger.jobID != jobID {
		t.Fatalf("expected job id %q, got %q", jobID, tagger.jobID)
	}
	if tagger.resolvedBuild.BuildNumber != 7 {
		t.Fatalf("expected build number 7, got %d", tagger.resolvedBuild.BuildNumber)
	}
	if tagger.input.Version != "0.1.7" {
		t.Fatalf("expected resolved release version 0.1.7, got %q", tagger.input.Version)
	}
	if len(tagger.input.ArtifactIDs) != 1 {
		t.Fatalf("expected one collected artifact id, got %d", len(tagger.input.ArtifactIDs))
	}
	if got := artifactRepo.artifacts[buildID][0].ArtifactType; got != "" {
		t.Fatalf("expected untyped collected artifact for legacy declaration, got %q", got)
	}
	if len(tagger.input.ManagedImageVersionIDs) != 1 || tagger.input.ManagedImageVersionIDs[0] != managedImageVersionID {
		t.Fatalf("expected managed image version id %q, got %#v", managedImageVersionID, tagger.input.ManagedImageVersionIDs)
	}
}

func TestBuildService_RunStep_PersistsExplicitArtifactTypeFromPipelineDeclaration(t *testing.T) {
	workspaceRoot := t.TempDir()
	buildID := "build-artifact-type"
	claimToken := "claim-active"

	workspacePath := filepath.Join(workspaceRoot, buildID)
	if err := os.MkdirAll(filepath.Join(workspacePath, "images"), 0o755); err != nil {
		t.Fatalf("failed creating workspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, "images", "backend-image.tar"), []byte("artifact-body"), 0o644); err != nil {
		t.Fatalf("failed writing artifact file: %v", err)
	}

	pipelineYAML := "version: 1\nsteps:\n  - name: build\n    run: make build\n    artifacts:\n      - path: images/backend-image.tar\n        type: docker_image\n"
	repo := &fakeBuildRepository{
		build: domain.Build{
			ID:                 buildID,
			Status:             domain.BuildStatusRunning,
			CurrentStepIndex:   0,
			PipelineConfigYAML: &pipelineYAML,
			CreatedAt:          time.Now().UTC(),
		},
		steps: []domain.BuildStep{{ID: "step-1", StepIndex: 0, Name: "step-1", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken, ArtifactPaths: []string{"images/backend-image.tar"}}},
	}
	r := &fakeBuildScopedRunner{fakeRunner: fakeRunner{result: steprunner.RunStepResult{Status: steprunner.RunStepStatusSuccess, ExitCode: 0, Stdout: "ok\n", StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC()}}}
	logStore := logs.NewMemorySink()
	artifactRepo := &fakeArtifactRepository{}

	svc := NewBuildService(repo, r, logStore)
	svc.SetArtifactPersistence(artifactRepo, testStoreResolver(artifact.NewFilesystemStore(t.TempDir())), workspaceRoot)

	_, report, err := svc.RunStep(context.Background(), steprunner.RunStepRequest{BuildID: buildID, StepIndex: 0, StepName: "step-1", ClaimToken: claimToken, Command: "echo", Args: []string{"ok"}, WorkingDir: "."})
	if err != nil {
		t.Fatalf("run step failed: %v", err)
	}
	if report.SideEffectErr != nil {
		t.Fatalf("expected no side effect error, got %v", report.SideEffectErr)
	}
	if len(artifactRepo.artifacts[buildID]) != 1 {
		t.Fatalf("expected one collected artifact, got %d", len(artifactRepo.artifacts[buildID]))
	}
	if artifactRepo.artifacts[buildID][0].ArtifactType != domain.ArtifactTypeDockerImage {
		t.Fatalf("expected docker_image artifact type, got %q", artifactRepo.artifacts[buildID][0].ArtifactType)
	}
}

func TestBuildService_RunStep_DoesNotDuplicateBuildHeaderWhenBuildAlreadyStarted(t *testing.T) {
	started := time.Now().UTC().Add(-20 * time.Second)
	finished := started.Add(2 * time.Second)

	claimToken := "claim-active"
	repo := &fakeBuildRepository{
		build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: started.Add(-2 * time.Second), StartedAt: &started},
		steps: []domain.BuildStep{{StepIndex: 0, Name: "step-1", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken}},
	}
	r := &fakeRunner{result: steprunner.RunStepResult{Status: steprunner.RunStepStatusSuccess, ExitCode: 0, Stdout: "ok\n", StartedAt: started, FinishedAt: finished}}
	logStore := logs.NewMemorySink()

	svc := NewBuildService(repo, r, logStore)
	if _, _, err := svc.RunStep(context.Background(), steprunner.RunStepRequest{BuildID: "build-1", StepIndex: 0, StepName: "step-1", ClaimToken: claimToken, Command: "echo"}); err != nil {
		t.Fatalf("run step failed: %v", err)
	}

	buildLogs, err := svc.GetBuildLogs(context.Background(), "build-1")
	if err != nil {
		t.Fatalf("get build logs failed: %v", err)
	}

	count := 0
	for _, line := range buildLogs {
		if line.Message == "Starting build" {
			count++
		}
	}
	if count != 0 {
		t.Fatalf("expected no duplicate build header when already started, found %d", count)
	}
}

func TestTerminalBuildSummaryDuration_PrefersDeterministicTimestamps(t *testing.T) {
	now := time.Date(2026, time.March, 30, 14, 0, 0, 0, time.UTC)
	created := now.Add(-10 * time.Second)
	started := now.Add(-8 * time.Second)
	finished := now.Add(-3 * time.Second)

	if got := terminalBuildSummaryDuration(domain.Build{CreatedAt: created, StartedAt: &started, FinishedAt: &finished}, now); got != 5*time.Second {
		t.Fatalf("expected finished-started duration of 5s, got %s", got)
	}

	if got := terminalBuildSummaryDuration(domain.Build{CreatedAt: created, FinishedAt: &finished}, now); got != 7*time.Second {
		t.Fatalf("expected finished-created duration of 7s, got %s", got)
	}

	if got := terminalBuildSummaryDuration(domain.Build{CreatedAt: created}, now); got != 10*time.Second {
		t.Fatalf("expected fallback now-created duration of 10s, got %s", got)
	}

	futureCreated := now.Add(2 * time.Second)
	if got := terminalBuildSummaryDuration(domain.Build{CreatedAt: futureCreated}, now); got != 0 {
		t.Fatalf("expected non-negative fallback duration, got %s", got)
	}
}

func TestBuildService_RunStep_EmitsTerminalSummaryInStepChunkFlow(t *testing.T) {
	createdAt := time.Now().UTC().Add(-10 * time.Second)
	startedAt := createdAt.Add(2 * time.Second)
	finishedAt := startedAt.Add(3 * time.Second)

	claimToken := "claim-active"
	repo := &fakeBuildRepository{
		build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: createdAt},
		steps: []domain.BuildStep{{StepIndex: 0, Name: "step-1", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken}},
	}
	r := &fakeRunner{result: steprunner.RunStepResult{Status: steprunner.RunStepStatusSuccess, ExitCode: 0, Stdout: "ok\n", StartedAt: startedAt, FinishedAt: finishedAt}}
	logStore := logs.NewMemorySink()

	svc := NewBuildService(repo, r, logStore)
	svc.SetDefaultExecutionImage("golang:1.26")

	_, report, err := svc.RunStep(context.Background(), steprunner.RunStepRequest{
		BuildID:    "build-1",
		StepID:     "step-id-1",
		StepIndex:  0,
		StepName:   "step-1",
		ClaimToken: claimToken,
		Command:    "sh",
		Args:       []string{"-c", "echo hello"},
	})
	if err != nil {
		t.Fatalf("run step failed: %v", err)
	}
	if report.CompletionOutcome != repository.StepCompletionCompleted {
		t.Fatalf("expected completion outcome completed, got %q", report.CompletionOutcome)
	}

	chunks, err := svc.GetStepLogChunks(context.Background(), "build-1", 0, 0, 500)
	if err != nil {
		t.Fatalf("get step log chunks failed: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected step chunks to be persisted")
	}

	if !containsSystemChunkText(chunks, "==> Step 1/1: step-1") {
		t.Fatalf("expected step marker in chunk flow, got %#v", chunks)
	}
	if !containsSystemChunkText(chunks, "Finalizing build") {
		t.Fatalf("expected finalizing marker in chunk flow, got %#v", chunks)
	}
	if !containsSystemChunkText(chunks, "Build succeeded in") {
		t.Fatalf("expected terminal summary in chunk flow, got %#v", chunks)
	}
}

func assertMessagesContain(t *testing.T, messages []string, expected ...string) {
	t.Helper()
	for _, want := range expected {
		found := false
		for _, got := range messages {
			if strings.Contains(got, want) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected log message containing %q, got %#v", want, messages)
		}
	}
}

func containsSystemChunkText(chunks []logs.StepLogChunk, needle string) bool {
	for _, chunk := range chunks {
		if chunk.Stream == logs.StepLogStreamSystem && strings.Contains(chunk.ChunkText, needle) {
			return true
		}
	}
	return false
}

func TestBuildService_HandleStepResult_PersistedCompletionWithSideEffectFailure(t *testing.T) {
	claimToken := "claim-active"
	repo := &fakeBuildRepository{
		build: domain.Build{ID: "build-1", Status: domain.BuildStatusRunning, CurrentStepIndex: 0},
		steps: []domain.BuildStep{{StepIndex: 0, Name: "step-1", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken}},
	}
	logErr := errors.New("log sink unavailable")
	svc := NewBuildService(repo, nil, &fakeLogSink{err: logErr})

	report, err := svc.HandleStepResult(context.Background(), steprunner.RunStepRequest{BuildID: "build-1", StepIndex: 0, StepName: "step-1", ClaimToken: claimToken}, steprunner.RunStepResult{Status: steprunner.RunStepStatusSuccess, ExitCode: 0, Stdout: "ok\n", StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC()})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if report.CompletionOutcome != repository.StepCompletionCompleted {
		t.Fatalf("expected completion outcome completed, got %q", report.CompletionOutcome)
	}
	if report.SideEffectErr == nil {
		t.Fatal("expected side effect error to be reported")
	}
	if !errors.Is(report.SideEffectErr, logErr) {
		t.Fatalf("expected side effect error %v, got %v", logErr, report.SideEffectErr)
	}
	if repo.steps[0].Status != domain.BuildStepStatusSuccess {
		t.Fatalf("expected step success to remain persisted, got %q", repo.steps[0].Status)
	}
}

func TestBuildService_RunStep_ReportsSideEffectFailureSeparately(t *testing.T) {
	claimToken := "claim-active"
	runner := &fakeRunner{result: steprunner.RunStepResult{Status: steprunner.RunStepStatusSuccess, ExitCode: 0, Stdout: "ok\n", StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC()}}
	repo := &fakeBuildRepository{
		build: domain.Build{ID: "build-1", Status: domain.BuildStatusRunning, CurrentStepIndex: 0},
		steps: []domain.BuildStep{{StepIndex: 0, Name: "step-1", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken}},
	}
	logErr := errors.New("write failed")
	svc := NewBuildService(repo, runner, &fakeLogSink{err: logErr})

	_, report, err := svc.RunStep(context.Background(), steprunner.RunStepRequest{BuildID: "build-1", StepIndex: 0, StepName: "step-1", ClaimToken: claimToken, Command: "echo"})
	if err != nil {
		t.Fatalf("expected nil error from run step, got %v", err)
	}
	if report.CompletionOutcome != repository.StepCompletionCompleted {
		t.Fatalf("expected completion outcome completed, got %q", report.CompletionOutcome)
	}
	if report.SideEffectErr == nil {
		t.Fatal("expected side effect error to be reported")
	}
	if !errors.Is(report.SideEffectErr, logErr) {
		t.Fatalf("expected side effect error %v, got %v", logErr, report.SideEffectErr)
	}
}

func TestBuildService_RunStep_PersistsChunkLogsWithoutStepID(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		expectedText string
		expectedType logs.StepLogStream
	}{
		{
			name:         "echo stdout",
			args:         []string{"-c", "echo hello"},
			expectedText: "hello",
			expectedType: logs.StepLogStreamStdout,
		},
		{
			name:         "printf without newline",
			args:         []string{"-c", "printf hello"},
			expectedText: "hello",
			expectedType: logs.StepLogStreamStdout,
		},
		{
			name:         "stderr output",
			args:         []string{"-c", "echo hello 1>&2"},
			expectedText: "hello",
			expectedType: logs.StepLogStreamStderr,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			buildID := "build-" + strings.ReplaceAll(tc.name, " ", "-")
			claimToken := "claim-active"
			repo := &fakeBuildRepository{
				build: domain.Build{ID: buildID, Status: domain.BuildStatusRunning, CurrentStepIndex: 0, CreatedAt: time.Now().UTC()},
				steps: []domain.BuildStep{{StepIndex: 0, Name: "step-1", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken}},
			}
			logStore := logs.NewMemorySink()
			svc := NewBuildService(repo, inprocessrunner.New(), logStore)

			_, report, err := svc.RunStep(context.Background(), steprunner.RunStepRequest{
				BuildID:    buildID,
				StepIndex:  0,
				StepName:   "step-1",
				ClaimToken: claimToken,
				Command:    "sh",
				Args:       tc.args,
			})
			if err != nil {
				t.Fatalf("run step failed: %v", err)
			}
			if report.CompletionOutcome != repository.StepCompletionCompleted {
				t.Fatalf("expected completion outcome completed, got %q", report.CompletionOutcome)
			}

			chunks, err := svc.GetStepLogChunks(context.Background(), buildID, 0, 0, 100)
			if err != nil {
				t.Fatalf("get step log chunks failed: %v", err)
			}
			if len(chunks) == 0 {
				t.Fatal("expected persisted step log chunks")
			}
			found := false
			for _, chunk := range chunks {
				if chunk.ChunkText == tc.expectedText && chunk.Stream == tc.expectedType {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("expected chunk text/stream (%q/%q), got %#v", tc.expectedText, tc.expectedType, chunks)
			}
		})
	}
}

func TestBuildService_HandleStepResult_DuplicateCompletionIsNoOp(t *testing.T) {
	claimToken := "claim-active"
	repo := &fakeBuildRepository{
		build: domain.Build{ID: "build-1", Status: domain.BuildStatusRunning, CurrentStepIndex: 0},
		steps: []domain.BuildStep{{StepIndex: 0, Name: "step-1", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken}},
	}
	logStore := logs.NewMemorySink()
	svc := NewBuildService(repo, nil, logStore)
	request := steprunner.RunStepRequest{BuildID: "build-1", StepIndex: 0, StepName: "step-1", ClaimToken: claimToken}
	result := steprunner.RunStepResult{
		Status:     steprunner.RunStepStatusSuccess,
		ExitCode:   0,
		Stdout:     "ok\n",
		StartedAt:  time.Now().UTC(),
		FinishedAt: time.Now().UTC(),
	}

	report, err := svc.HandleStepResult(context.Background(), request, result)
	if err != nil {
		t.Fatalf("first completion failed: %v", err)
	}
	if report.CompletionOutcome != repository.StepCompletionCompleted {
		t.Fatal("expected first completion to complete step")
	}
	if report.SideEffectErr != nil {
		t.Fatalf("expected nil side effect error, got %v", report.SideEffectErr)
	}
	if repo.build.CurrentStepIndex != 1 {
		t.Fatalf("expected current step index to advance to 1, got %d", repo.build.CurrentStepIndex)
	}

	buildLogs, err := svc.GetBuildLogs(context.Background(), "build-1")
	if err != nil {
		t.Fatalf("get build logs after first completion failed: %v", err)
	}
	if len(buildLogs) != 1 {
		t.Fatalf("expected one log after first completion, got %d", len(buildLogs))
	}

	report, err = svc.HandleStepResult(context.Background(), request, result)
	if err != nil {
		t.Fatalf("duplicate completion should be no-op, got error %v", err)
	}
	if report.CompletionOutcome != repository.StepCompletionDuplicateTerminal {
		t.Fatal("expected duplicate completion to be no-op")
	}
	if repo.build.CurrentStepIndex != 1 {
		t.Fatalf("expected current step index to remain 1, got %d", repo.build.CurrentStepIndex)
	}

	buildLogs, err = svc.GetBuildLogs(context.Background(), "build-1")
	if err != nil {
		t.Fatalf("get build logs after duplicate completion failed: %v", err)
	}
	if len(buildLogs) != 1 {
		t.Fatalf("expected duplicate completion to not write extra logs, got %d", len(buildLogs))
	}
}

func TestBuildService_HandleStepResult_MultiStepSuccessDoesNotCompleteBuild(t *testing.T) {
	claimToken := "claim-active"
	repo := &fakeBuildRepository{
		build: domain.Build{ID: "build-1", Status: domain.BuildStatusRunning, CurrentStepIndex: 0},
		steps: []domain.BuildStep{
			{StepIndex: 0, Name: "step-1", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken},
			{StepIndex: 1, Name: "step-2", Status: domain.BuildStepStatusPending},
		},
	}
	svc := NewBuildService(repo, nil, logs.NewMemorySink())

	report, err := svc.HandleStepResult(context.Background(), steprunner.RunStepRequest{BuildID: "build-1", StepIndex: 0, StepName: "step-1", ClaimToken: claimToken}, steprunner.RunStepResult{Status: steprunner.RunStepStatusSuccess, ExitCode: 0, StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC()})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if report.CompletionOutcome != repository.StepCompletionCompleted {
		t.Fatal("expected completion to persist")
	}
	if repo.build.Status != domain.BuildStatusRunning {
		t.Fatalf("expected build to remain running, got %q", repo.build.Status)
	}
	if repo.steps[1].Status != domain.BuildStepStatusPending {
		t.Fatalf("expected second step to remain pending/runnable, got %q", repo.steps[1].Status)
	}
}

func TestBuildService_HandleStepResult_FailureMarksBuildFailed(t *testing.T) {
	claimToken := "claim-active"
	repo := &fakeBuildRepository{
		build: domain.Build{ID: "build-1", Status: domain.BuildStatusRunning, CurrentStepIndex: 0},
		steps: []domain.BuildStep{
			{StepIndex: 0, Name: "step-1", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken},
			{StepIndex: 1, Name: "step-2", Status: domain.BuildStepStatusPending},
		},
	}
	svc := NewBuildService(repo, nil, logs.NewMemorySink())

	report, err := svc.HandleStepResult(context.Background(), steprunner.RunStepRequest{BuildID: "build-1", StepIndex: 0, StepName: "step-1", ClaimToken: claimToken}, steprunner.RunStepResult{Status: steprunner.RunStepStatusFailed, ExitCode: 7, Stderr: "boom", StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC()})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if report.CompletionOutcome != repository.StepCompletionCompleted {
		t.Fatal("expected completion to persist")
	}
	if repo.build.Status != domain.BuildStatusFailed {
		t.Fatalf("expected build failed, got %q", repo.build.Status)
	}
	if repo.steps[1].Status != domain.BuildStepStatusPending {
		t.Fatalf("expected later step to remain pending after fail-fast, got %q", repo.steps[1].Status)
	}
}

func TestBuildService_HandleStepResult_DuplicateFailureDoesNotDuplicateLogsOrFinalizeTwice(t *testing.T) {
	claimToken := "claim-active"
	repo := &fakeBuildRepository{
		build: domain.Build{ID: "build-1", Status: domain.BuildStatusRunning, CurrentStepIndex: 0},
		steps: []domain.BuildStep{{StepIndex: 0, Name: "step-1", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken}},
	}
	logStore := logs.NewMemorySink()
	svc := NewBuildService(repo, nil, logStore)
	request := steprunner.RunStepRequest{BuildID: "build-1", StepIndex: 0, StepName: "step-1", ClaimToken: claimToken}
	result := steprunner.RunStepResult{Status: steprunner.RunStepStatusFailed, ExitCode: 9, Stderr: "boom\n", StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC()}

	report, err := svc.HandleStepResult(context.Background(), request, result)
	if err != nil {
		t.Fatalf("first failure completion failed: %v", err)
	}
	if report.CompletionOutcome != repository.StepCompletionCompleted {
		t.Fatal("expected first failure completion to persist")
	}
	if repo.build.Status != domain.BuildStatusFailed {
		t.Fatalf("expected build failed after first completion, got %q", repo.build.Status)
	}

	logsAfterFirst, err := svc.GetBuildLogs(context.Background(), "build-1")
	if err != nil {
		t.Fatalf("get build logs after first failure failed: %v", err)
	}
	if len(logsAfterFirst) != 1 {
		t.Fatalf("expected one log line after first failure, got %d", len(logsAfterFirst))
	}

	report, err = svc.HandleStepResult(context.Background(), request, result)
	if err != nil {
		t.Fatalf("duplicate failure completion should be no-op, got %v", err)
	}
	if report.CompletionOutcome != repository.StepCompletionDuplicateTerminal {
		t.Fatal("expected duplicate failure completion to be no-op")
	}
	if repo.build.Status != domain.BuildStatusFailed {
		t.Fatalf("expected build status to remain failed, got %q", repo.build.Status)
	}

	logsAfterDuplicate, err := svc.GetBuildLogs(context.Background(), "build-1")
	if err != nil {
		t.Fatalf("get build logs after duplicate failure failed: %v", err)
	}
	if len(logsAfterDuplicate) != 1 {
		t.Fatalf("expected duplicate failure to not write extra logs, got %d", len(logsAfterDuplicate))
	}
}

func TestBuildService_HandleStepResult_NonRunningStepReturnsInvalidStepTransition(t *testing.T) {
	repo := &fakeBuildRepository{
		build: domain.Build{ID: "build-1", Status: domain.BuildStatusRunning, CurrentStepIndex: 0},
		steps: []domain.BuildStep{{StepIndex: 0, Name: "step-1", Status: domain.BuildStepStatusPending}},
	}
	svc := NewBuildService(repo, nil, logs.NewMemorySink())

	report, err := svc.HandleStepResult(context.Background(), steprunner.RunStepRequest{BuildID: "build-1", StepIndex: 0, StepName: "step-1", ClaimToken: "claim-any"}, steprunner.RunStepResult{Status: steprunner.RunStepStatusSuccess, ExitCode: 0, Stdout: "ok", StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC()})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if report.CompletionOutcome != repository.StepCompletionInvalidTransition {
		t.Fatal("expected non-running completion to not complete")
	}
}

func TestBuildService_HandleStepResult_MissingClaimTokenReturnsInvalidTransition(t *testing.T) {
	repo := &fakeBuildRepository{
		build: domain.Build{ID: "build-1", Status: domain.BuildStatusRunning, CurrentStepIndex: 0},
		steps: []domain.BuildStep{{StepIndex: 0, Name: "step-1", Status: domain.BuildStepStatusRunning}},
	}
	svc := NewBuildService(repo, nil, logs.NewMemorySink())

	report, err := svc.HandleStepResult(context.Background(), steprunner.RunStepRequest{BuildID: "build-1", StepIndex: 0, StepName: "step-1"}, steprunner.RunStepResult{Status: steprunner.RunStepStatusSuccess, ExitCode: 0, StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC()})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if report.CompletionOutcome != repository.StepCompletionInvalidTransition {
		t.Fatal("expected completion without claim token to be rejected")
	}
}

func TestBuildService_HandleStepResult_StaleClaimReturnsStaleOutcome(t *testing.T) {
	claimToken := "claim-active"
	repo := &fakeBuildRepository{
		build: domain.Build{ID: "build-1", Status: domain.BuildStatusRunning, CurrentStepIndex: 0},
		steps: []domain.BuildStep{{StepIndex: 0, Name: "step-1", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken}},
	}
	svc := NewBuildService(repo, nil, logs.NewMemorySink())

	report, err := svc.HandleStepResult(context.Background(), steprunner.RunStepRequest{BuildID: "build-1", StepIndex: 0, StepName: "step-1", ClaimToken: "claim-stale"}, steprunner.RunStepResult{Status: steprunner.RunStepStatusSuccess, ExitCode: 0, StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC()})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if report.CompletionOutcome != repository.StepCompletionStaleClaim {
		t.Fatal("expected stale claim completion to be rejected")
	}
}

func TestBuildService_HandleStepResult_ClaimedCompletionFinalizesBuild(t *testing.T) {
	claimToken := "claim-active"
	repo := &fakeBuildRepository{
		build: domain.Build{ID: "build-1", Status: domain.BuildStatusRunning, CurrentStepIndex: 0},
		steps: []domain.BuildStep{{StepIndex: 0, Name: "step-1", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken}},
	}
	svc := NewBuildService(repo, nil, logs.NewMemorySink())

	report, err := svc.HandleStepResult(context.Background(), steprunner.RunStepRequest{BuildID: "build-1", StepIndex: 0, StepName: "step-1", ClaimToken: "claim-active"}, steprunner.RunStepResult{Status: steprunner.RunStepStatusSuccess, ExitCode: 0, StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC()})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if report.CompletionOutcome != repository.StepCompletionCompleted {
		t.Fatal("expected claimed completion to persist")
	}
	if report.Step.Status != domain.BuildStepStatusSuccess {
		t.Fatalf("expected step success, got %q", report.Step.Status)
	}
	if repo.build.Status != domain.BuildStatusSuccess {
		t.Fatalf("expected build success, got %q", repo.build.Status)
	}
}

func TestBuildService_HandleStepResult_AfterCancelReturnsDuplicateTerminal(t *testing.T) {
	claimToken := "claim-active"
	repo := &fakeBuildRepository{
		build: domain.Build{ID: "build-1", Status: domain.BuildStatusRunning, CurrentStepIndex: 0},
		steps: []domain.BuildStep{{StepIndex: 0, Name: "step-1", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken}},
	}
	svc := NewBuildService(repo, nil, logs.NewMemorySink())

	if _, err := svc.CancelBuild(context.Background(), "build-1"); err != nil {
		t.Fatalf("cancel build failed: %v", err)
	}

	report, err := svc.HandleStepResult(context.Background(), steprunner.RunStepRequest{BuildID: "build-1", StepIndex: 0, StepName: "step-1", ClaimToken: "claim-active"}, steprunner.RunStepResult{Status: steprunner.RunStepStatusSuccess, ExitCode: 0, StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC()})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if report.CompletionOutcome != repository.StepCompletionDuplicateTerminal {
		t.Fatalf("expected duplicate terminal completion outcome after cancel, got %q", report.CompletionOutcome)
	}
}

func TestBuildService_RenewStepLease_StaleClaimReturnsDomainError(t *testing.T) {
	active := "claim-active"
	repo := &fakeBuildRepository{
		build: domain.Build{ID: "build-1", Status: domain.BuildStatusRunning, CurrentStepIndex: 0},
		steps: []domain.BuildStep{{StepIndex: 0, Name: "step-1", Status: domain.BuildStepStatusRunning, ClaimToken: &active}},
	}
	svc := NewBuildService(repo, nil, logs.NewMemorySink())

	_, renewed, err := svc.RenewStepLease(context.Background(), "build-1", 0, "claim-stale", time.Now().UTC().Add(time.Minute))
	if !errors.Is(err, ErrStaleStepClaim) {
		t.Fatalf("expected ErrStaleStepClaim, got %v", err)
	}
	if renewed {
		t.Fatal("expected stale renewal to fail")
	}
}

func TestBuildService_RenewStepLease_SucceedsForActiveClaim(t *testing.T) {
	active := "claim-active"
	lease := time.Now().UTC().Add(time.Minute)
	repo := &fakeBuildRepository{
		build: domain.Build{ID: "build-1", Status: domain.BuildStatusRunning, CurrentStepIndex: 0},
		steps: []domain.BuildStep{{StepIndex: 0, Name: "step-1", Status: domain.BuildStepStatusRunning, ClaimToken: &active}},
	}
	svc := NewBuildService(repo, nil, logs.NewMemorySink())

	step, renewed, err := svc.RenewStepLease(context.Background(), "build-1", 0, "claim-active", lease)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !renewed {
		t.Fatal("expected renewal success")
	}
	if step.LeaseExpiresAt == nil || !step.LeaseExpiresAt.Equal(lease) {
		t.Fatalf("expected lease extension to %s, got %v", lease, step.LeaseExpiresAt)
	}
}

// testStoreResolver wraps a single Store into a StoreResolver defaulting to filesystem.
func testStoreResolver(store artifact.Store) *artifact.StoreResolver {
	return artifact.NewStoreResolver(domain.StorageProviderFilesystem, map[domain.StorageProvider]artifact.Store{
		domain.StorageProviderFilesystem: store,
	})
}
