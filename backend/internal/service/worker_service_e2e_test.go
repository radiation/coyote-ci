package service

import (
	"context"
	"strings"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/execution"
	"github.com/radiation/coyote-ci/backend/internal/logs"
	inprocessrunner "github.com/radiation/coyote-ci/backend/internal/runner/inprocess"
	storememory "github.com/radiation/coyote-ci/backend/internal/store/memory"
	"github.com/radiation/coyote-ci/backend/pkg/contracts"
)

func TestWorkerExecutionVerticalSlice_Success(t *testing.T) {
	ctx := context.Background()
	buildStore := storememory.NewBuildStore()
	logSink := logs.NewMemorySink()
	stepRunner := inprocessrunner.New(execution.NewLocalExecutor())
	buildService := NewBuildServiceWithExecution(buildStore, stepRunner, logSink)
	worker := NewWorkerService(buildService)

	build, err := buildService.CreateBuild(ctx, CreateBuildInput{ProjectID: "project-1"})
	if err != nil {
		t.Fatalf("create build failed: %v", err)
	}
	_, err = buildService.QueueBuild(ctx, build.ID)
	if err != nil {
		t.Fatalf("queue build failed: %v", err)
	}

	runnable, found, err := worker.ClaimRunnableStep(ctx)
	if err != nil {
		t.Fatalf("claim runnable step failed: %v", err)
	}
	if !found {
		t.Fatal("expected runnable step to be found")
	}
	runnable.Command = "sh"
	runnable.Args = []string{"-c", "printf 'ok-line\\n'"}
	runnable.WorkingDir = "."

	// Worker claiming is represented by taking a runnable step and executing it.
	report, err := worker.ExecuteRunnableStep(ctx, runnable)
	if err != nil {
		t.Fatalf("execute runnable step failed: %v", err)
	}

	if report.Step.Status != contracts.BuildStepStatusSuccess {
		t.Fatalf("expected step status success, got %q", report.Step.Status)
	}
	if report.Step.StartedAt == nil || report.Step.EndedAt == nil {
		t.Fatal("expected step timestamps")
	}
	if report.Result.Status != contracts.RunStepStatusSuccess {
		t.Fatalf("expected run step status success, got %q", report.Result.Status)
	}
	if report.Result.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", report.Result.ExitCode)
	}

	buildLogs, err := buildService.GetBuildLogs(ctx, build.ID)
	if err != nil {
		t.Fatalf("get build logs failed: %v", err)
	}
	if len(buildLogs) == 0 {
		t.Fatal("expected captured logs for successful command")
	}
	if buildLogs[0].StepName != "default" {
		t.Fatalf("expected step name default, got %q", buildLogs[0].StepName)
	}
	if buildLogs[0].Message != "ok-line" {
		t.Fatalf("expected log line ok-line, got %q", buildLogs[0].Message)
	}

	updatedBuild, err := buildService.GetBuild(ctx, build.ID)
	if err != nil {
		t.Fatalf("get build failed: %v", err)
	}
	if updatedBuild.Status != domain.BuildStatusSuccess {
		t.Fatalf("expected build status success, got %q", updatedBuild.Status)
	}
}

func TestWorkerExecutionVerticalSlice_FailedCommand(t *testing.T) {
	ctx := context.Background()
	buildStore := storememory.NewBuildStore()
	logSink := logs.NewMemorySink()
	stepRunner := inprocessrunner.New(execution.NewLocalExecutor())
	buildService := NewBuildServiceWithExecution(buildStore, stepRunner, logSink)
	worker := NewWorkerService(buildService)

	build, err := buildService.CreateBuild(ctx, CreateBuildInput{ProjectID: "project-1"})
	if err != nil {
		t.Fatalf("create build failed: %v", err)
	}
	_, err = buildService.QueueBuild(ctx, build.ID)
	if err != nil {
		t.Fatalf("queue build failed: %v", err)
	}

	runnable, found, err := worker.ClaimRunnableStep(ctx)
	if err != nil {
		t.Fatalf("claim runnable step failed: %v", err)
	}
	if !found {
		t.Fatal("expected runnable step to be found")
	}
	runnable.Command = "sh"
	runnable.Args = []string{"-c", "echo fail-line 1>&2; exit 7"}
	runnable.WorkingDir = "."

	report, err := worker.ExecuteRunnableStep(ctx, runnable)
	if err != nil {
		t.Fatalf("execute runnable step failed: %v", err)
	}

	if report.Step.Status != contracts.BuildStepStatusFailed {
		t.Fatalf("expected step status failed, got %q", report.Step.Status)
	}
	if report.Step.StartedAt == nil || report.Step.EndedAt == nil {
		t.Fatal("expected step timestamps")
	}
	if report.Result.Status != contracts.RunStepStatusFailed {
		t.Fatalf("expected run step status failed, got %q", report.Result.Status)
	}
	if report.Result.ExitCode != 7 {
		t.Fatalf("expected exit code 7, got %d", report.Result.ExitCode)
	}
	if !strings.Contains(report.Result.Stderr, "fail-line") {
		t.Fatalf("expected stderr to include fail-line, got %q", report.Result.Stderr)
	}

	buildLogs, err := buildService.GetBuildLogs(ctx, build.ID)
	if err != nil {
		t.Fatalf("get build logs failed: %v", err)
	}
	if len(buildLogs) == 0 {
		t.Fatal("expected captured logs for failed command")
	}
	if buildLogs[0].StepName != "default" {
		t.Fatalf("expected step name default, got %q", buildLogs[0].StepName)
	}
	if buildLogs[0].Message != "fail-line" {
		t.Fatalf("expected log line fail-line, got %q", buildLogs[0].Message)
	}

	updatedBuild, err := buildService.GetBuild(ctx, build.ID)
	if err != nil {
		t.Fatalf("get build failed: %v", err)
	}
	if updatedBuild.Status != domain.BuildStatusFailed {
		t.Fatalf("expected build status failed, got %q", updatedBuild.Status)
	}
}

func TestWorkerExecutionVerticalSlice_Timeout(t *testing.T) {
	ctx := context.Background()
	buildStore := storememory.NewBuildStore()
	logSink := logs.NewMemorySink()
	stepRunner := inprocessrunner.New(execution.NewLocalExecutor())
	buildService := NewBuildServiceWithExecution(buildStore, stepRunner, logSink)
	worker := NewWorkerService(buildService)

	build, err := buildService.CreateBuild(ctx, CreateBuildInput{ProjectID: "project-1"})
	if err != nil {
		t.Fatalf("create build failed: %v", err)
	}
	_, err = buildService.QueueBuild(ctx, build.ID)
	if err != nil {
		t.Fatalf("queue build failed: %v", err)
	}

	runnable, found, err := worker.ClaimRunnableStep(ctx)
	if err != nil {
		t.Fatalf("claim runnable step failed: %v", err)
	}
	if !found {
		t.Fatal("expected runnable step to be found")
	}
	runnable.Command = "sh"
	runnable.Args = []string{"-c", "sleep 2"}
	runnable.WorkingDir = "."
	runnable.TimeoutSeconds = 1

	report, err := worker.ExecuteRunnableStep(ctx, runnable)
	if err != nil {
		t.Fatalf("execute runnable step failed: %v", err)
	}

	if report.Step.Status != contracts.BuildStepStatusFailed {
		t.Fatalf("expected step status failed, got %q", report.Step.Status)
	}
	if report.Step.StartedAt == nil || report.Step.EndedAt == nil {
		t.Fatal("expected step timestamps")
	}
	if report.Result.Status != contracts.RunStepStatusFailed {
		t.Fatalf("expected run step status failed, got %q", report.Result.Status)
	}
	if report.Result.ExitCode != -1 {
		t.Fatalf("expected timeout exit code -1, got %d", report.Result.ExitCode)
	}
	if !strings.Contains(report.Result.Stderr, "timed out") {
		t.Fatalf("expected timeout reason in stderr, got %q", report.Result.Stderr)
	}

	buildLogs, err := buildService.GetBuildLogs(ctx, build.ID)
	if err != nil {
		t.Fatalf("get build logs failed: %v", err)
	}
	if len(buildLogs) == 0 {
		t.Fatal("expected captured logs for timed out command")
	}
	if buildLogs[0].StepName != "default" {
		t.Fatalf("expected step name default, got %q", buildLogs[0].StepName)
	}
	if !strings.Contains(buildLogs[0].Message, "timed out") {
		t.Fatalf("expected timeout log line, got %q", buildLogs[0].Message)
	}

	updatedBuild, err := buildService.GetBuild(ctx, build.ID)
	if err != nil {
		t.Fatalf("get build failed: %v", err)
	}
	if updatedBuild.Status != domain.BuildStatusFailed {
		t.Fatalf("expected build status failed, got %q", updatedBuild.Status)
	}
}
