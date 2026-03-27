package service

import (
	"context"
	"strings"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/domain"

	"github.com/radiation/coyote-ci/backend/internal/logs"
	repositorymemory "github.com/radiation/coyote-ci/backend/internal/repository/memory"
	"github.com/radiation/coyote-ci/backend/internal/runner"
	inprocessrunner "github.com/radiation/coyote-ci/backend/internal/runner/inprocess"
)

func TestWorkerExecutionVerticalSlice_Success(t *testing.T) {
	ctx := context.Background()
	buildStore := repositorymemory.NewBuildRepository()
	logSink := logs.NewMemorySink()
	stepRunner := inprocessrunner.New()
	buildService := NewBuildService(buildStore, stepRunner, logSink)
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

	if report.Step.Status != domain.BuildStepStatusSuccess {
		t.Fatalf("expected step status success, got %q", report.Step.Status)
	}
	if report.Step.StartedAt == nil || report.Step.FinishedAt == nil {
		t.Fatal("expected step timestamps")
	}
	if report.Result.Status != runner.RunStepStatusSuccess {
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
	buildStore := repositorymemory.NewBuildRepository()
	logSink := logs.NewMemorySink()
	stepRunner := inprocessrunner.New()
	buildService := NewBuildService(buildStore, stepRunner, logSink)
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

	if report.Step.Status != domain.BuildStepStatusFailed {
		t.Fatalf("expected step status failed, got %q", report.Step.Status)
	}
	if report.Step.StartedAt == nil || report.Step.FinishedAt == nil {
		t.Fatal("expected step timestamps")
	}
	if report.Result.Status != runner.RunStepStatusFailed {
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
	buildStore := repositorymemory.NewBuildRepository()
	logSink := logs.NewMemorySink()
	stepRunner := inprocessrunner.New()
	buildService := NewBuildService(buildStore, stepRunner, logSink)
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

	if report.Step.Status != domain.BuildStepStatusFailed {
		t.Fatalf("expected step status failed, got %q", report.Step.Status)
	}
	if report.Step.StartedAt == nil || report.Step.FinishedAt == nil {
		t.Fatal("expected step timestamps")
	}
	if report.Result.Status != runner.RunStepStatusFailed {
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

func TestWorkerExecutionVerticalSlice_ExitZeroStepSucceeds(t *testing.T) {
	ctx := context.Background()
	buildStore := repositorymemory.NewBuildRepository()
	logSink := logs.NewMemorySink()
	stepRunner := inprocessrunner.New()
	buildService := NewBuildService(buildStore, stepRunner, logSink)
	worker := NewWorkerService(buildService)

	build, err := buildService.CreateBuild(ctx, CreateBuildInput{
		ProjectID: "project-1",
		Steps: []CreateBuildStepInput{
			{Name: "success", Command: "sh", Args: []string{"-c", "echo success && exit 0"}, WorkingDir: "."},
		},
	})
	if err != nil {
		t.Fatalf("create build failed: %v", err)
	}

	runnable, found, err := worker.ClaimRunnableStep(ctx)
	if err != nil {
		t.Fatalf("claim runnable step failed: %v", err)
	}
	if !found {
		t.Fatal("expected runnable step to be found")
	}

	report, err := worker.ExecuteRunnableStep(ctx, runnable)
	if err != nil {
		t.Fatalf("execute runnable step failed: %v", err)
	}

	if report.Result.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", report.Result.ExitCode)
	}
	if report.Result.Status != runner.RunStepStatusSuccess {
		t.Fatalf("expected run step success, got %q", report.Result.Status)
	}

	steps, err := buildService.GetBuildSteps(ctx, build.ID)
	if err != nil {
		t.Fatalf("get build steps failed: %v", err)
	}
	if len(steps) != 1 {
		t.Fatalf("expected one step, got %d", len(steps))
	}
	if steps[0].Status != domain.BuildStepStatusSuccess {
		t.Fatalf("expected step success, got %q", steps[0].Status)
	}
	if steps[0].Stdout == nil || !strings.Contains(*steps[0].Stdout, "success") {
		t.Fatalf("expected persisted stdout to include success, got %v", steps[0].Stdout)
	}

	updatedBuild, err := buildService.GetBuild(ctx, build.ID)
	if err != nil {
		t.Fatalf("get build failed: %v", err)
	}
	if updatedBuild.Status != domain.BuildStatusSuccess {
		t.Fatalf("expected build status success, got %q", updatedBuild.Status)
	}
}

func TestWorkerExecutionVerticalSlice_MultiStepSuccessThenFailure(t *testing.T) {
	ctx := context.Background()
	buildStore := repositorymemory.NewBuildRepository()
	logSink := logs.NewMemorySink()
	stepRunner := inprocessrunner.New()
	buildService := NewBuildService(buildStore, stepRunner, logSink)
	worker := NewWorkerService(buildService)

	build, err := buildService.CreateBuild(ctx, CreateBuildInput{
		ProjectID: "project-1",
		Steps: []CreateBuildStepInput{
			{Name: "setup", Command: "sh", Args: []string{"-c", "echo success && exit 0"}, WorkingDir: "."},
			{Name: "verify", Command: "sh", Args: []string{"-c", "echo failure 1>&2 && exit 1"}, WorkingDir: "."},
		},
	})
	if err != nil {
		t.Fatalf("create build failed: %v", err)
	}

	first, found, err := worker.ClaimRunnableStep(ctx)
	if err != nil {
		t.Fatalf("claim first runnable step failed: %v", err)
	}
	if !found {
		t.Fatal("expected first runnable step")
	}
	if first.StepName != "setup" {
		t.Fatalf("expected setup step first, got %q", first.StepName)
	}

	firstReport, err := worker.ExecuteRunnableStep(ctx, first)
	if err != nil {
		t.Fatalf("execute first step failed: %v", err)
	}
	if firstReport.Result.Status != runner.RunStepStatusSuccess {
		t.Fatalf("expected first step success, got %q", firstReport.Result.Status)
	}

	second, found, err := worker.ClaimRunnableStep(ctx)
	if err != nil {
		t.Fatalf("claim second runnable step failed: %v", err)
	}
	if !found {
		t.Fatal("expected second runnable step")
	}
	if second.StepName != "verify" {
		t.Fatalf("expected verify step second, got %q", second.StepName)
	}

	secondReport, err := worker.ExecuteRunnableStep(ctx, second)
	if err != nil {
		t.Fatalf("execute second step failed: %v", err)
	}
	if secondReport.Result.Status != runner.RunStepStatusFailed {
		t.Fatalf("expected second step failed, got %q", secondReport.Result.Status)
	}
	if secondReport.Result.ExitCode != 1 {
		t.Fatalf("expected second step exit code 1, got %d", secondReport.Result.ExitCode)
	}

	steps, err := buildService.GetBuildSteps(ctx, build.ID)
	if err != nil {
		t.Fatalf("get build steps failed: %v", err)
	}
	if len(steps) != 2 {
		t.Fatalf("expected two steps, got %d", len(steps))
	}
	if steps[0].Status != domain.BuildStepStatusSuccess {
		t.Fatalf("expected first step success, got %q", steps[0].Status)
	}
	if steps[1].Status != domain.BuildStepStatusFailed {
		t.Fatalf("expected second step failed, got %q", steps[1].Status)
	}
	if steps[0].Stdout == nil || !strings.Contains(*steps[0].Stdout, "success") {
		t.Fatalf("expected first step stdout to include success, got %v", steps[0].Stdout)
	}
	if steps[1].Stderr == nil || !strings.Contains(*steps[1].Stderr, "failure") {
		t.Fatalf("expected second step stderr to include failure, got %v", steps[1].Stderr)
	}

	updatedBuild, err := buildService.GetBuild(ctx, build.ID)
	if err != nil {
		t.Fatalf("get build failed: %v", err)
	}
	if updatedBuild.Status != domain.BuildStatusFailed {
		t.Fatalf("expected build status failed, got %q", updatedBuild.Status)
	}
}

func TestWorkerExecutionVerticalSlice_MultiStepSuccessPath(t *testing.T) {
	ctx := context.Background()
	buildStore := repositorymemory.NewBuildRepository()
	logSink := logs.NewMemorySink()
	stepRunner := inprocessrunner.New()
	buildService := NewBuildService(buildStore, stepRunner, logSink)
	worker := NewWorkerService(buildService)

	build, err := buildService.CreateBuild(ctx, CreateBuildInput{
		ProjectID: "project-1",
		Steps: []CreateBuildStepInput{
			{Name: "setup", Command: "sh", Args: []string{"-c", "echo setup && exit 0"}, WorkingDir: "."},
			{Name: "test", Command: "sh", Args: []string{"-c", "echo test && exit 0"}, WorkingDir: "."},
		},
	})
	if err != nil {
		t.Fatalf("create build failed: %v", err)
	}

	first, found, err := worker.ClaimRunnableStep(ctx)
	if err != nil {
		t.Fatalf("claim first runnable step failed: %v", err)
	}
	if !found || first.StepName != "setup" {
		t.Fatalf("expected setup as first runnable step, got found=%v step=%q", found, first.StepName)
	}
	_, execErr := worker.ExecuteRunnableStep(ctx, first)
	if execErr != nil {
		t.Fatalf("execute first step failed: %v", execErr)
	}

	second, found, err := worker.ClaimRunnableStep(ctx)
	if err != nil {
		t.Fatalf("claim second runnable step failed: %v", err)
	}
	if !found || second.StepName != "test" {
		t.Fatalf("expected test as second runnable step, got found=%v step=%q", found, second.StepName)
	}
	_, execErr = worker.ExecuteRunnableStep(ctx, second)
	if execErr != nil {
		t.Fatalf("execute second step failed: %v", execErr)
	}

	updatedBuild, err := buildService.GetBuild(ctx, build.ID)
	if err != nil {
		t.Fatalf("get build failed: %v", err)
	}
	if updatedBuild.Status != domain.BuildStatusSuccess {
		t.Fatalf("expected build status success, got %q", updatedBuild.Status)
	}
}

func TestWorkerExecutionVerticalSlice_MultiStepFailFastStopsLaterSteps(t *testing.T) {
	ctx := context.Background()
	buildStore := repositorymemory.NewBuildRepository()
	logSink := logs.NewMemorySink()
	stepRunner := inprocessrunner.New()
	buildService := NewBuildService(buildStore, stepRunner, logSink)
	worker := NewWorkerService(buildService)

	_, err := buildService.CreateBuild(ctx, CreateBuildInput{
		ProjectID: "project-1",
		Steps: []CreateBuildStepInput{
			{Name: "setup", Command: "sh", Args: []string{"-c", "echo setup && exit 0"}, WorkingDir: "."},
			{Name: "verify", Command: "sh", Args: []string{"-c", "echo boom 1>&2 && exit 5"}, WorkingDir: "."},
			{Name: "package", Command: "sh", Args: []string{"-c", "echo should-not-run && exit 0"}, WorkingDir: "."},
		},
	})
	if err != nil {
		t.Fatalf("create build failed: %v", err)
	}

	first, found, err := worker.ClaimRunnableStep(ctx)
	if err != nil {
		t.Fatalf("claim first runnable step failed: %v", err)
	}
	if !found {
		t.Fatal("expected first runnable step")
	}
	_, execErr := worker.ExecuteRunnableStep(ctx, first)
	if execErr != nil {
		t.Fatalf("execute first step failed: %v", execErr)
	}

	second, found, err := worker.ClaimRunnableStep(ctx)
	if err != nil {
		t.Fatalf("claim second runnable step failed: %v", err)
	}
	if !found || second.StepName != "verify" {
		t.Fatalf("expected verify as second runnable step, got found=%v step=%q", found, second.StepName)
	}
	_, execErr = worker.ExecuteRunnableStep(ctx, second)
	if execErr != nil {
		t.Fatalf("execute second step failed: %v", execErr)
	}

	third, found, err := worker.ClaimRunnableStep(ctx)
	if err != nil {
		t.Fatalf("claim third runnable step failed: %v", err)
	}
	if found {
		t.Fatalf("expected no third runnable step after failure, got %+v", third)
	}
}
