package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/pkg/contracts"
)

type fakeBuildExecutionBoundary struct {
	listBuildsResp []domain.Build
	listBuildsErr  error
	stepsByBuildID map[string][]contracts.BuildStep
	getStepsErr    error
	claimErr       error
	claimMap       map[string]bool
	claimCalls     int
	queueCalls     int

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

func claimKey(buildID string, stepIndex int) string {
	return fmt.Sprintf("%s:%d", buildID, stepIndex)
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

func (f *fakeBuildExecutionBoundary) GetBuildSteps(_ context.Context, id string) ([]contracts.BuildStep, error) {
	if f.getStepsErr != nil {
		return nil, f.getStepsErr
	}

	steps := f.stepsByBuildID[id]
	out := make([]contracts.BuildStep, len(steps))
	copy(out, steps)
	return out, nil
}

func (f *fakeBuildExecutionBoundary) ClaimStepIfPending(_ context.Context, buildID string, stepIndex int, _ *string, startedAt time.Time) (contracts.BuildStep, bool, error) {
	f.claimCalls++
	if f.claimErr != nil {
		return contracts.BuildStep{}, false, f.claimErr
	}

	steps := f.stepsByBuildID[buildID]
	for idx := range steps {
		if steps[idx].StepIndex != stepIndex {
			continue
		}

		key := claimKey(buildID, stepIndex)
		if f.claimMap != nil {
			allowed, ok := f.claimMap[key]
			if ok && !allowed {
				return contracts.BuildStep{}, false, nil
			}
		}

		if steps[idx].Status != contracts.BuildStepStatusPending {
			return contracts.BuildStep{}, false, nil
		}

		steps[idx].Status = contracts.BuildStepStatusRunning
		steps[idx].StartedAt = &startedAt
		f.stepsByBuildID[buildID] = steps
		return steps[idx], true, nil
	}

	return contracts.BuildStep{}, false, nil
}

func (f *fakeBuildExecutionBoundary) QueueBuild(_ context.Context, id string) (domain.Build, error) {
	f.queueCalls++

	if f.stepsByBuildID == nil {
		f.stepsByBuildID = map[string][]contracts.BuildStep{}
	}
	if len(f.stepsByBuildID[id]) == 0 {
		f.stepsByBuildID[id] = []contracts.BuildStep{{
			StepIndex: 0,
			Name:      "default",
			Status:    contracts.BuildStepStatusPending,
		}}
	}

	return domain.Build{ID: id, Status: domain.BuildStatusQueued}, nil
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
	boundary := &fakeBuildExecutionBoundary{
		runStepResp: contracts.RunStepResult{Status: contracts.RunStepStatusSuccess, ExitCode: 0, StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC()},
		stepsByBuildID: map[string][]contracts.BuildStep{
			"build-1": {
				{Name: "test", Status: contracts.BuildStepStatusSuccess},
			},
		},
	}
	worker := NewWorkerService(boundary)

	report, err := worker.ExecuteRunnableStep(context.Background(), RunnableStep{
		BuildID:    "build-1",
		StepIndex:  0,
		StepName:   "test",
		Command:    "echo",
		Args:       []string{"ok"},
		WorkingDir: "/tmp",
		Env:        map[string]string{"A": "1"},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if boundary.startCalls != 0 || boundary.runStepCalls != 1 || boundary.completeCalls != 1 || boundary.failCalls != 0 {
		t.Fatalf("unexpected call counts: start=%d run=%d complete=%d fail=%d", boundary.startCalls, boundary.runStepCalls, boundary.completeCalls, boundary.failCalls)
	}
	if boundary.lastRequest.Command != "echo" || boundary.lastRequest.StepName != "test" || boundary.lastRequest.BuildID != "build-1" {
		t.Fatalf("unexpected run step request: %+v", boundary.lastRequest)
	}
	if report.Step.Status != contracts.BuildStepStatusSuccess {
		t.Fatalf("expected step status success, got %q", report.Step.Status)
	}
	if report.Step.StartedAt == nil || report.Step.FinishedAt == nil {
		t.Fatal("expected step started/ended timestamps")
	}
}

func TestWorkerService_ExecuteRunnableStep_CommandFailed(t *testing.T) {
	boundary := &fakeBuildExecutionBoundary{runStepResp: contracts.RunStepResult{Status: contracts.RunStepStatusFailed, ExitCode: 2, StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC()}}
	worker := NewWorkerService(boundary)

	report, err := worker.ExecuteRunnableStep(context.Background(), RunnableStep{BuildID: "build-2", StepIndex: 0, StepName: "test", Command: "false"})
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

	report, err := worker.ExecuteRunnableStep(context.Background(), RunnableStep{BuildID: "build-3", StepIndex: 0, StepName: "test", Command: "missing"})
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

	report, err := worker.ExecuteRunnableStep(context.Background(), RunnableStep{BuildID: "build-timeout", StepIndex: 0, StepName: "test", Command: "sleep", TimeoutSeconds: 1})
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

func TestWorkerService_ClaimRunnableStep_ClaimsFirstPendingStep(t *testing.T) {
	boundary := &fakeBuildExecutionBoundary{
		listBuildsResp: []domain.Build{{ID: "build-1", Status: domain.BuildStatusQueued}},
		stepsByBuildID: map[string][]contracts.BuildStep{
			"build-1": {
				{StepIndex: 0, Name: "lint", Status: contracts.BuildStepStatusPending},
				{StepIndex: 1, Name: "test", Status: contracts.BuildStepStatusPending},
			},
		},
	}

	worker := NewWorkerService(boundary)
	runnable, found, err := worker.ClaimRunnableStep(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !found {
		t.Fatal("expected runnable step to be found")
	}
	if runnable.StepIndex != 0 {
		t.Fatalf("expected step index 0, got %d", runnable.StepIndex)
	}
	if runnable.StepName != "lint" {
		t.Fatalf("expected step name lint, got %q", runnable.StepName)
	}
	if boundary.startCalls != 1 {
		t.Fatalf("expected build to transition to running once, got %d", boundary.startCalls)
	}
}

func TestWorkerService_ClaimRunnableStep_UsesPersistedStepIndex(t *testing.T) {
	boundary := &fakeBuildExecutionBoundary{
		listBuildsResp: []domain.Build{{ID: "build-1", Status: domain.BuildStatusQueued}},
		stepsByBuildID: map[string][]contracts.BuildStep{
			"build-1": {
				{StepIndex: 5, Name: "lint", Status: contracts.BuildStepStatusPending},
			},
		},
	}

	worker := NewWorkerService(boundary)
	runnable, found, err := worker.ClaimRunnableStep(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !found {
		t.Fatal("expected runnable step to be found")
	}
	if runnable.StepIndex != 5 {
		t.Fatalf("expected persisted step index 5, got %d", runnable.StepIndex)
	}
}

func TestWorkerService_ClaimRunnableStep_UsesPersistedExecutionConfig(t *testing.T) {
	boundary := &fakeBuildExecutionBoundary{
		listBuildsResp: []domain.Build{{ID: "build-1", Status: domain.BuildStatusQueued}},
		stepsByBuildID: map[string][]contracts.BuildStep{
			"build-1": {
				{StepIndex: 0, Name: "lint", Command: "go", Args: []string{"test", "./..."}, Env: map[string]string{"CGO_ENABLED": "0"}, WorkingDir: "/workspace", TimeoutSeconds: 300, Status: contracts.BuildStepStatusPending},
			},
		},
	}

	worker := NewWorkerService(boundary)
	runnable, found, err := worker.ClaimRunnableStep(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !found {
		t.Fatal("expected runnable step to be found")
	}
	if runnable.Command != "go" {
		t.Fatalf("expected command go, got %q", runnable.Command)
	}
	if len(runnable.Args) != 2 || runnable.Args[0] != "test" {
		t.Fatalf("expected args [test ./...], got %+v", runnable.Args)
	}
	if runnable.Env["CGO_ENABLED"] != "0" {
		t.Fatalf("expected env CGO_ENABLED=0, got %+v", runnable.Env)
	}
	if runnable.WorkingDir != "/workspace" {
		t.Fatalf("expected working dir /workspace, got %q", runnable.WorkingDir)
	}
	if runnable.TimeoutSeconds != 300 {
		t.Fatalf("expected timeout 300, got %d", runnable.TimeoutSeconds)
	}
}

func TestWorkerService_ClaimRunnableStep_OnlyFirstSequentialPendingIsRunnable(t *testing.T) {
	boundary := &fakeBuildExecutionBoundary{
		listBuildsResp: []domain.Build{{ID: "build-1", Status: domain.BuildStatusQueued}},
		stepsByBuildID: map[string][]contracts.BuildStep{
			"build-1": {
				{StepIndex: 0, Name: "lint", Status: contracts.BuildStepStatusSuccess},
				{StepIndex: 1, Name: "test", Status: contracts.BuildStepStatusPending},
				{StepIndex: 2, Name: "package", Status: contracts.BuildStepStatusPending},
			},
		},
	}

	worker := NewWorkerService(boundary)
	runnable, found, err := worker.ClaimRunnableStep(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !found {
		t.Fatal("expected runnable step to be found")
	}
	if runnable.StepIndex != 1 {
		t.Fatalf("expected step index 1, got %d", runnable.StepIndex)
	}
	if runnable.StepName != "test" {
		t.Fatalf("expected step name test, got %q", runnable.StepName)
	}
}

func TestWorkerService_ClaimRunnableStep_DoesNotReclaimRunningOrFinishedSteps(t *testing.T) {
	boundary := &fakeBuildExecutionBoundary{
		listBuildsResp: []domain.Build{{ID: "build-1", Status: domain.BuildStatusRunning}},
		stepsByBuildID: map[string][]contracts.BuildStep{
			"build-1": {
				{StepIndex: 0, Name: "lint", Status: contracts.BuildStepStatusRunning},
				{StepIndex: 1, Name: "test", Status: contracts.BuildStepStatusPending},
			},
		},
	}

	worker := NewWorkerService(boundary)
	_, found, err := worker.ClaimRunnableStep(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if found {
		t.Fatal("expected no runnable step while prior step is running")
	}
}

func TestWorkerService_ClaimRunnableStep_ConditionalClaimFailureIsClean(t *testing.T) {
	boundary := &fakeBuildExecutionBoundary{
		listBuildsResp: []domain.Build{{ID: "build-1", Status: domain.BuildStatusQueued}},
		stepsByBuildID: map[string][]contracts.BuildStep{
			"build-1": {
				{StepIndex: 0, Name: "lint", Status: contracts.BuildStepStatusPending},
			},
		},
		claimMap: map[string]bool{
			claimKey("build-1", 0): false,
		},
	}

	worker := NewWorkerService(boundary)
	_, found, err := worker.ClaimRunnableStep(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if found {
		t.Fatal("expected claim to fail cleanly when step is no longer pending")
	}
	if boundary.startCalls != 0 {
		t.Fatalf("expected build start to not be called when claim fails, got %d", boundary.startCalls)
	}
}

func TestWorkerService_ClaimRunnableStep_PendingBuildTransitionsToRunning(t *testing.T) {
	boundary := &fakeBuildExecutionBoundary{
		listBuildsResp: []domain.Build{{ID: "build-1", Status: domain.BuildStatusPending}},
		stepsByBuildID: map[string][]contracts.BuildStep{
			"build-1": {
				{StepIndex: 0, Name: "lint", Status: contracts.BuildStepStatusPending},
			},
		},
	}

	worker := NewWorkerService(boundary)
	_, found, err := worker.ClaimRunnableStep(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !found {
		t.Fatal("expected runnable step to be found")
	}
	if boundary.startCalls != 1 {
		t.Fatalf("expected pending build to transition to running once, got %d", boundary.startCalls)
	}
}

func TestWorkerService_ClaimRunnableStep_PendingBuildWithoutStepsBootstrapsQueue(t *testing.T) {
	boundary := &fakeBuildExecutionBoundary{
		listBuildsResp: []domain.Build{{ID: "build-1", Status: domain.BuildStatusPending}},
		stepsByBuildID: map[string][]contracts.BuildStep{},
	}

	worker := NewWorkerService(boundary)
	runnable, found, err := worker.ClaimRunnableStep(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !found {
		t.Fatal("expected runnable step to be found")
	}
	if runnable.StepIndex != 0 || runnable.StepName != "default" {
		t.Fatalf("expected claimed default step at index 0, got %q at %d", runnable.StepName, runnable.StepIndex)
	}
	if boundary.queueCalls != 1 {
		t.Fatalf("expected queue bootstrap once, got %d", boundary.queueCalls)
	}
	if boundary.startCalls != 1 {
		t.Fatalf("expected build start after claim, got %d", boundary.startCalls)
	}
}
