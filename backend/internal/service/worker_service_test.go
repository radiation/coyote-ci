package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/pkg/contracts"
)

type fakeBuildExecutionBoundary struct {
	listBuildsResp []domain.Build
	listBuildsErr  error

	startCalls    int
	completeCalls int
	failCalls     int
	runStepCalls  int

	startErr    error
	completeErr error
	failErr     error
	runStepErr  error
	runStepResp contracts.RunStepResult

	lastBuildID string
	lastRequest contracts.RunStepRequest
}

func (f *fakeBuildExecutionBoundary) ListBuilds(_ context.Context) ([]domain.Build, error) {
	if f.listBuildsErr != nil {
		return nil, f.listBuildsErr
	}

	if f.listBuildsResp == nil {
		return []domain.Build{}, nil
	}

	return f.listBuildsResp, nil
}

func (f *fakeBuildExecutionBoundary) StartBuild(_ context.Context, id string) (domain.Build, error) {
	f.startCalls++
	f.lastBuildID = id
	if f.startErr != nil {
		return domain.Build{}, f.startErr
	}
	return domain.Build{ID: id, Status: domain.BuildStatusRunning}, nil
}

func (f *fakeBuildExecutionBoundary) CompleteBuild(_ context.Context, id string) (domain.Build, error) {
	f.completeCalls++
	f.lastBuildID = id
	if f.completeErr != nil {
		return domain.Build{}, f.completeErr
	}
	return domain.Build{ID: id, Status: domain.BuildStatusSuccess}, nil
}

func (f *fakeBuildExecutionBoundary) FailBuild(_ context.Context, id string) (domain.Build, error) {
	f.failCalls++
	f.lastBuildID = id
	if f.failErr != nil {
		return domain.Build{}, f.failErr
	}
	return domain.Build{ID: id, Status: domain.BuildStatusFailed}, nil
}

func (f *fakeBuildExecutionBoundary) RunStep(_ context.Context, request contracts.RunStepRequest) (contracts.RunStepResult, error) {
	f.runStepCalls++
	f.lastRequest = request
	if f.runStepErr != nil {
		return contracts.RunStepResult{}, f.runStepErr
	}
	return f.runStepResp, nil
}

func TestWorkerService_ExecuteRunnableStep_Success(t *testing.T) {
	boundary := &fakeBuildExecutionBoundary{runStepResp: contracts.RunStepResult{Status: contracts.RunStepStatusSuccess, ExitCode: 0, StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC()}}
	worker := NewWorkerService(boundary)

	report, err := worker.ExecuteRunnableStep(context.Background(), RunnableStep{
		BuildID:    "build-1",
		StepName:   "test",
		Command:    "echo",
		Args:       []string{"ok"},
		WorkingDir: "/tmp",
		Env:        map[string]string{"A": "1"},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if boundary.startCalls != 1 || boundary.runStepCalls != 1 || boundary.completeCalls != 1 || boundary.failCalls != 0 {
		t.Fatalf("unexpected call counts: start=%d run=%d complete=%d fail=%d", boundary.startCalls, boundary.runStepCalls, boundary.completeCalls, boundary.failCalls)
	}
	if boundary.lastRequest.Command != "echo" || boundary.lastRequest.StepName != "test" || boundary.lastRequest.BuildID != "build-1" {
		t.Fatalf("unexpected run step request: %+v", boundary.lastRequest)
	}
	if report.Step.Status != contracts.BuildStepStatusSuccess {
		t.Fatalf("expected step status success, got %q", report.Step.Status)
	}
	if report.Step.StartedAt == nil || report.Step.EndedAt == nil {
		t.Fatal("expected step started/ended timestamps")
	}
}

func TestWorkerService_ExecuteRunnableStep_CommandFailed(t *testing.T) {
	boundary := &fakeBuildExecutionBoundary{runStepResp: contracts.RunStepResult{Status: contracts.RunStepStatusFailed, ExitCode: 2, StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC()}}
	worker := NewWorkerService(boundary)

	report, err := worker.ExecuteRunnableStep(context.Background(), RunnableStep{BuildID: "build-2", StepName: "test", Command: "false"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if boundary.completeCalls != 0 || boundary.failCalls != 1 {
		t.Fatalf("expected fail path only, got complete=%d fail=%d", boundary.completeCalls, boundary.failCalls)
	}
	if report.Step.Status != contracts.BuildStepStatusFailed {
		t.Fatalf("expected step status failed, got %q", report.Step.Status)
	}
}

func TestWorkerService_ExecuteRunnableStep_RunStepError(t *testing.T) {
	boundary := &fakeBuildExecutionBoundary{runStepErr: errors.New("startup failed")}
	worker := NewWorkerService(boundary)

	report, err := worker.ExecuteRunnableStep(context.Background(), RunnableStep{BuildID: "build-3", StepName: "test", Command: "missing"})
	if err == nil || err.Error() != "startup failed" {
		t.Fatalf("expected startup failed error, got %v", err)
	}
	if boundary.failCalls != 1 {
		t.Fatalf("expected fail build to be called once, got %d", boundary.failCalls)
	}
	if report.Step.Status != contracts.BuildStepStatusFailed {
		t.Fatalf("expected step status failed, got %q", report.Step.Status)
	}
}

func TestWorkerService_ExecuteRunnableStep_TimeoutMarkedFailedWithReason(t *testing.T) {
	boundary := &fakeBuildExecutionBoundary{
		runStepResp: contracts.RunStepResult{
			Status:     contracts.RunStepStatusFailed,
			ExitCode:   -1,
			Stderr:     "step execution timed out after 1s",
			StartedAt:  time.Now().UTC(),
			FinishedAt: time.Now().UTC(),
		},
	}
	worker := NewWorkerService(boundary)

	report, err := worker.ExecuteRunnableStep(context.Background(), RunnableStep{BuildID: "build-timeout", StepName: "test", Command: "sleep", TimeoutSeconds: 1})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if report.Step.Status != contracts.BuildStepStatusFailed {
		t.Fatalf("expected step status failed, got %q", report.Step.Status)
	}
	if report.Result.ExitCode != -1 {
		t.Fatalf("expected timeout exit code -1, got %d", report.Result.ExitCode)
	}
	if !strings.Contains(report.Result.Stderr, "timed out") {
		t.Fatalf("expected timeout reason, got %q", report.Result.Stderr)
	}
	if boundary.failCalls != 1 {
		t.Fatalf("expected fail build to be called once, got %d", boundary.failCalls)
	}
}
