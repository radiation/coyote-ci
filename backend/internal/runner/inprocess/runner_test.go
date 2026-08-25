package inprocess

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/runner"
)

func TestRunner_RunStep_Success(t *testing.T) {
	r := New()

	res, err := r.RunStep(context.Background(), runner.RunStepRequest{
		Command: "sh",
		Args:    []string{"-c", "echo hello"},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if res.Status != runner.RunStepStatusSuccess {
		t.Fatalf("expected success status, got %q", res.Status)
	}
	if res.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", res.ExitCode)
	}
	if res.Stdout != "hello\n" {
		t.Fatalf("expected stdout hello, got %q", res.Stdout)
	}
	if res.StartedAt.IsZero() || res.FinishedAt.IsZero() {
		t.Fatal("expected started/finished timestamps to be set")
	}
}

func TestRunner_RunStep_NonZeroExit(t *testing.T) {
	r := New()

	res, err := r.RunStep(context.Background(), runner.RunStepRequest{
		Command: "sh",
		Args:    []string{"-c", "echo boom 1>&2; exit 3"},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if res.Status != runner.RunStepStatusFailed {
		t.Fatalf("expected failed status, got %q", res.Status)
	}
	if res.ExitCode != 3 {
		t.Fatalf("expected exit code 3, got %d", res.ExitCode)
	}
	if res.Stderr != "boom\n" {
		t.Fatalf("expected stderr boom, got %q", res.Stderr)
	}
}

func TestRunner_RunStep_Timeout(t *testing.T) {
	r := New()

	res, err := r.RunStep(context.Background(), runner.RunStepRequest{
		Command:        "sh",
		Args:           []string{"-c", "sleep 2"},
		TimeoutSeconds: 1,
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if res.Status != runner.RunStepStatusFailed {
		t.Fatalf("expected failed status, got %q", res.Status)
	}
	if res.ExitCode != -1 {
		t.Fatalf("expected timeout exit code -1, got %d", res.ExitCode)
	}
	if !res.TimedOut {
		t.Fatal("expected typed timeout result")
	}
	if !strings.Contains(res.Stderr, "step execution timed out after") {
		t.Fatalf("expected timeout reason in stderr, got %q", res.Stderr)
	}
}

func TestRunner_RunStep_ContextDeadlineExceeded(t *testing.T) {
	r := New()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	res, err := r.RunStep(ctx, runner.RunStepRequest{
		Command: "sh",
		Args:    []string{"-c", "sleep 2"},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if res.Status != runner.RunStepStatusFailed {
		t.Fatalf("expected failed status, got %q", res.Status)
	}
	if res.ExitCode != -1 {
		t.Fatalf("expected timeout exit code -1, got %d", res.ExitCode)
	}
	if !res.TimedOut {
		t.Fatal("expected typed timeout result")
	}
	if !strings.Contains(res.Stderr, "step execution timed out") {
		t.Fatalf("expected timeout reason in stderr, got %q", res.Stderr)
	}
}

func TestRunner_RunStep_EmptyCommand(t *testing.T) {
	r := New()

	_, err := r.RunStep(context.Background(), runner.RunStepRequest{})
	if err == nil {
		t.Fatal("expected error for empty command, got nil")
	}
}

func TestRunner_RunStep_CommandNotFound(t *testing.T) {
	r := New()

	_, err := r.RunStep(context.Background(), runner.RunStepRequest{
		Command: "definitely-not-a-real-command",
	})
	if err == nil {
		t.Fatal("expected runtime error, got nil")
	}
}

func TestRunner_RunStep_UsesPreparedBuildWorkspace(t *testing.T) {
	workspaceRoot := t.TempDir()
	r := NewWithWorkspaceRoot(workspaceRoot)

	if err := r.PrepareBuild(context.Background(), runner.PrepareBuildRequest{BuildID: "build-1"}); err != nil {
		t.Fatalf("prepare build failed: %v", err)
	}

	res, err := r.RunStep(context.Background(), runner.RunStepRequest{
		BuildID:    "build-1",
		WorkingDir: ".",
		Command:    "sh",
		Args:       []string{"-c", "mkdir -p dist reports && echo hello > dist/hello.txt && echo '<testsuite></testsuite>' > reports/test.xml"},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if res.Status != runner.RunStepStatusSuccess {
		t.Fatalf("expected success status, got %q", res.Status)
	}

	if _, err := os.Stat(filepath.Join(workspaceRoot, "build-1", "dist", "hello.txt")); err != nil {
		t.Fatalf("expected artifact file in build workspace, stat failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workspaceRoot, "build-1", "reports", "test.xml")); err != nil {
		t.Fatalf("expected report file in build workspace, stat failed: %v", err)
	}
}

func TestRunner_CleanupBuild_RemovesPreparedWorkspace(t *testing.T) {
	workspaceRoot := t.TempDir()
	r := NewWithWorkspaceRoot(workspaceRoot)

	if err := r.PrepareBuild(context.Background(), runner.PrepareBuildRequest{BuildID: "build-1"}); err != nil {
		t.Fatalf("prepare build failed: %v", err)
	}

	if err := r.CleanupBuild(context.Background(), "build-1"); err != nil {
		t.Fatalf("cleanup build failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(workspaceRoot, "build-1")); !os.IsNotExist(err) {
		t.Fatalf("expected workspace to be removed, got err=%v", err)
	}
}

func TestMergeEnvironment_AddsWorkspaceAndTriggerArtifactPaths(t *testing.T) {
	workspaceRoot := t.TempDir()
	env := mergeEnvironment(runner.RunStepRequest{
		BuildID: "build-1",
		StepID:  "step-1",
		Env: map[string]string{
			runner.EnvTriggerArtifactLocalRelative: ".coyote/trigger-artifacts/dist/app.tgz",
		},
	}, workspaceRoot)
	joined := strings.Join(env, "\n")
	if !strings.Contains(joined, runner.EnvWorkspace+"="+workspaceRoot) {
		t.Fatalf("expected workspace env, got %q", joined)
	}
	if !strings.Contains(joined, runner.EnvTriggerArtifactLocalDir+"="+filepath.Join(workspaceRoot, ".coyote", "trigger-artifacts")) {
		t.Fatalf("expected local dir env, got %q", joined)
	}
	if !strings.Contains(joined, runner.EnvTriggerArtifactLocalPath+"="+filepath.Join(workspaceRoot, ".coyote", "trigger-artifacts", "dist", "app.tgz")) {
		t.Fatalf("expected local path env, got %q", joined)
	}
	if !strings.Contains(joined, runner.EnvBuildID+"=build-1") || !strings.Contains(joined, runner.EnvStepID+"=step-1") {
		t.Fatalf("expected build and step env, got %q", joined)
	}
	if !strings.Contains(joined, "CI=true") {
		t.Fatalf("expected CI env, got %q", joined)
	}
}

func TestMergeEnvironment_WithoutWorkspaceSkipsLocalArtifactAbsolutePaths(t *testing.T) {
	env := mergeEnvironment(runner.RunStepRequest{
		BuildID: "build-1",
		StepID:  "step-1",
		Env: map[string]string{
			runner.EnvTriggerArtifactLocalRelative: ".coyote/trigger-artifacts/dist/app.tgz",
		},
	}, "")
	joined := strings.Join(env, "\n")
	if strings.Contains(joined, runner.EnvWorkspace+"=") || strings.Contains(joined, runner.EnvTriggerArtifactLocalPath+"=") || strings.Contains(joined, runner.EnvTriggerArtifactLocalDir+"=") {
		t.Fatalf("expected no workspace-derived env without workspace path, got %q", joined)
	}
}

func TestRunner_StepVisibleWorkspaceRoot_FallbackLookup(t *testing.T) {
	workspaceRoot := t.TempDir()
	buildWorkspace := filepath.Join(workspaceRoot, "build-1")
	if err := os.MkdirAll(buildWorkspace, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	r := NewWithWorkspaceRoot(workspaceRoot)
	resolvedExpected, err := filepath.EvalSymlinks(buildWorkspace)
	if err != nil {
		t.Fatalf("resolve expected workspace path: %v", err)
	}
	if got, ok := r.StepVisibleWorkspaceRoot("build-1"); !ok || got != resolvedExpected {
		t.Fatalf("expected fallback workspace lookup %q, got %q ok=%v", resolvedExpected, got, ok)
	}
	if got, ok := r.StepVisibleWorkspaceRoot(" "); ok || got != "" {
		t.Fatalf("expected blank build id to fail lookup, got %q ok=%v", got, ok)
	}
}

func TestResolveWorkingDir(t *testing.T) {
	workspaceRoot := t.TempDir()
	if _, err := resolveWorkingDir("", "."); err == nil {
		t.Fatal("expected empty workspace path error")
	}
	if got, err := resolveWorkingDir(workspaceRoot, "."); err != nil || got != workspaceRoot {
		t.Fatalf("expected workspace root for dot, got %q err=%v", got, err)
	}
	abs := filepath.Join(workspaceRoot, "backend")
	if got, err := resolveWorkingDir(workspaceRoot, abs); err != nil || got != abs {
		t.Fatalf("expected absolute path to pass through, got %q err=%v", got, err)
	}
	if _, err := resolveWorkingDir(workspaceRoot, "../escape"); err == nil {
		t.Fatal("expected traversal working dir error")
	}
}
