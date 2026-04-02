package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/runner"
)

type fakeBuildExecutionBoundary struct {
	jobsQueue      []domain.ExecutionJob
	listBuildsResp []domain.Build
	listBuildsErr  error
	stepsByBuildID map[string][]domain.BuildStep
	jobsByStepID   map[string]domain.ExecutionJob
	getStepsErr    error
	claimErr       error
	claimMap       map[string]bool
	claimCalls     int
	reclaimMap     map[string]bool
	reclaimCalls   int
	queueCalls     int
	renewCalls     int
	renewErr       error
	renewStale     bool
	renewedLeaseAt *time.Time
	runStepDelay   time.Duration

	startCalls    int
	completeCalls int
	failCalls     int
	runStepCalls  int

	startErr    error
	completeErr error
	failErr     error
	runStepErr  error
	runStepResp runner.RunStepResult
	runOutcome  repository.StepCompletionOutcome
	runSideErr  error

	lastBuildID string
	lastRequest runner.RunStepRequest
}

func (f *fakeBuildExecutionBoundary) ClaimNextRunnableJob(_ context.Context, claim repository.StepClaim) (domain.ExecutionJob, bool, error) {
	if len(f.jobsQueue) == 0 {
		return domain.ExecutionJob{}, false, nil
	}
	job := f.jobsQueue[0]
	f.jobsQueue = f.jobsQueue[1:]
	job.Status = domain.ExecutionJobStatusRunning
	job.ClaimedBy = &claim.WorkerID
	job.ClaimToken = &claim.ClaimToken
	job.ClaimExpiresAt = &claim.LeaseExpiresAt
	if f.jobsByStepID == nil {
		f.jobsByStepID = map[string]domain.ExecutionJob{}
	}
	f.jobsByStepID[job.StepID] = job
	return job, true, nil
}

func (f *fakeBuildExecutionBoundary) GetJobByStepID(_ context.Context, stepID string) (domain.ExecutionJob, error) {
	if f.jobsByStepID == nil {
		return domain.ExecutionJob{}, repository.ErrExecutionJobNotFound
	}
	job, ok := f.jobsByStepID[stepID]
	if !ok {
		return domain.ExecutionJob{}, repository.ErrExecutionJobNotFound
	}
	return job, nil
}

func (f *fakeBuildExecutionBoundary) ClaimJobByStepID(_ context.Context, stepID string, claim repository.StepClaim) (domain.ExecutionJob, bool, error) {
	if f.jobsByStepID == nil {
		return domain.ExecutionJob{}, false, nil
	}
	job, ok := f.jobsByStepID[stepID]
	if !ok {
		return domain.ExecutionJob{}, false, nil
	}
	job.Status = domain.ExecutionJobStatusRunning
	job.ClaimedBy = &claim.WorkerID
	job.ClaimToken = &claim.ClaimToken
	job.ClaimExpiresAt = &claim.LeaseExpiresAt
	f.jobsByStepID[stepID] = job
	return job, true, nil
}

func (f *fakeBuildExecutionBoundary) RenewJobLease(_ context.Context, jobID string, claimToken string, leaseExpiresAt time.Time) (domain.ExecutionJob, bool, error) {
	if f.jobsByStepID == nil {
		return domain.ExecutionJob{}, false, nil
	}
	for stepID, job := range f.jobsByStepID {
		if job.ID != jobID {
			continue
		}
		if job.ClaimToken == nil || *job.ClaimToken != claimToken {
			return job, false, ErrStaleStepClaim
		}
		job.ClaimExpiresAt = &leaseExpiresAt
		f.jobsByStepID[stepID] = job
		return job, true, nil
	}
	return domain.ExecutionJob{}, false, nil
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

func (f *fakeBuildExecutionBoundary) GetBuildSteps(_ context.Context, id string) ([]domain.BuildStep, error) {
	if f.getStepsErr != nil {
		return nil, f.getStepsErr
	}

	steps := f.stepsByBuildID[id]
	out := make([]domain.BuildStep, len(steps))
	copy(out, steps)
	return out, nil
}

func (f *fakeBuildExecutionBoundary) ClaimPendingStep(_ context.Context, buildID string, stepIndex int, claim repository.StepClaim) (domain.BuildStep, bool, error) {
	f.claimCalls++
	if f.claimErr != nil {
		return domain.BuildStep{}, false, f.claimErr
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
				return domain.BuildStep{}, false, nil
			}
		}

		if steps[idx].Status != domain.BuildStepStatusPending {
			return domain.BuildStep{}, false, nil
		}

		steps[idx].Status = domain.BuildStepStatusRunning
		steps[idx].WorkerID = &claim.WorkerID
		steps[idx].ClaimToken = &claim.ClaimToken
		steps[idx].ClaimedAt = &claim.ClaimedAt
		steps[idx].LeaseExpiresAt = &claim.LeaseExpiresAt
		steps[idx].StartedAt = &claim.ClaimedAt
		f.stepsByBuildID[buildID] = steps
		return steps[idx], true, nil
	}

	return domain.BuildStep{}, false, nil
}

func (f *fakeBuildExecutionBoundary) ReclaimExpiredStep(_ context.Context, buildID string, stepIndex int, reclaimBefore time.Time, claim repository.StepClaim) (domain.BuildStep, bool, error) {
	f.reclaimCalls++

	steps := f.stepsByBuildID[buildID]
	for idx := range steps {
		if steps[idx].StepIndex != stepIndex {
			continue
		}

		key := claimKey(buildID, stepIndex)
		if f.reclaimMap != nil {
			allowed, ok := f.reclaimMap[key]
			if ok && !allowed {
				return domain.BuildStep{}, false, nil
			}
		}

		if steps[idx].Status != domain.BuildStepStatusRunning {
			return domain.BuildStep{}, false, nil
		}
		if steps[idx].LeaseExpiresAt == nil || steps[idx].LeaseExpiresAt.After(reclaimBefore) {
			return domain.BuildStep{}, false, nil
		}

		steps[idx].WorkerID = &claim.WorkerID
		steps[idx].ClaimToken = &claim.ClaimToken
		steps[idx].ClaimedAt = &claim.ClaimedAt
		steps[idx].LeaseExpiresAt = &claim.LeaseExpiresAt
		f.stepsByBuildID[buildID] = steps
		return steps[idx], true, nil
	}

	return domain.BuildStep{}, false, nil
}

func (f *fakeBuildExecutionBoundary) QueueBuild(_ context.Context, id string) (domain.Build, error) {
	f.queueCalls++

	if f.stepsByBuildID == nil {
		f.stepsByBuildID = map[string][]domain.BuildStep{}
	}
	if len(f.stepsByBuildID[id]) == 0 {
		f.stepsByBuildID[id] = []domain.BuildStep{{
			StepIndex: 0,
			Name:      "default",
			Status:    domain.BuildStepStatusPending,
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

func (f *fakeBuildExecutionBoundary) RunStep(_ context.Context, request runner.RunStepRequest) (runner.RunStepResult, StepCompletionReport, error) {
	f.runStepCalls++
	f.lastRequest = request
	if f.runStepDelay > 0 {
		time.Sleep(f.runStepDelay)
	}
	report := StepCompletionReport{CompletionOutcome: f.runOutcome, SideEffectErr: f.runSideErr}
	if report.CompletionOutcome == "" {
		report.CompletionOutcome = repository.StepCompletionCompleted
	}
	if f.runStepErr != nil {
		return runner.RunStepResult{}, report, f.runStepErr
	}
	return f.runStepResp, report, nil
}

func (f *fakeBuildExecutionBoundary) RenewStepLease(_ context.Context, buildID string, stepIndex int, claimToken string, leaseExpiresAt time.Time) (domain.BuildStep, bool, error) {
	f.renewCalls++
	if f.renewErr != nil {
		return domain.BuildStep{}, false, f.renewErr
	}
	if f.renewStale {
		return domain.BuildStep{}, false, ErrStaleStepClaim
	}

	steps := f.stepsByBuildID[buildID]
	for idx := range steps {
		if steps[idx].StepIndex != stepIndex {
			continue
		}
		if steps[idx].Status != domain.BuildStepStatusRunning {
			return domain.BuildStep{}, false, nil
		}
		if steps[idx].ClaimToken == nil || *steps[idx].ClaimToken != claimToken {
			return steps[idx], false, ErrStaleStepClaim
		}
		steps[idx].LeaseExpiresAt = &leaseExpiresAt
		f.stepsByBuildID[buildID] = steps
		f.renewedLeaseAt = &leaseExpiresAt
		return steps[idx], true, nil
	}

	return domain.BuildStep{}, false, nil
}

func TestWorkerService_ExecuteRunnableStep_Success(t *testing.T) {
	boundary := &fakeBuildExecutionBoundary{
		runStepResp: runner.RunStepResult{Status: runner.RunStepStatusSuccess, ExitCode: 0, StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC()},
		stepsByBuildID: map[string][]domain.BuildStep{
			"build-1": {
				{Name: "test", Status: domain.BuildStepStatusSuccess},
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
	if boundary.startCalls != 0 || boundary.runStepCalls != 1 || boundary.completeCalls != 0 || boundary.failCalls != 0 {
		t.Fatalf("unexpected call counts: start=%d run=%d complete=%d fail=%d", boundary.startCalls, boundary.runStepCalls, boundary.completeCalls, boundary.failCalls)
	}
	if boundary.lastRequest.Command != "echo" || boundary.lastRequest.StepName != "test" || boundary.lastRequest.BuildID != "build-1" {
		t.Fatalf("unexpected run step request: %+v", boundary.lastRequest)
	}
	if report.Step.Status != domain.BuildStepStatusSuccess {
		t.Fatalf("expected step status success, got %q", report.Step.Status)
	}
	if report.Step.StartedAt == nil || report.Step.FinishedAt == nil {
		t.Fatal("expected step StartedAt/FinishedAt timestamps")
	}
}

func TestWorkerService_ExecuteRunnableStep_CommandFailed(t *testing.T) {
	boundary := &fakeBuildExecutionBoundary{runStepResp: runner.RunStepResult{Status: runner.RunStepStatusFailed, ExitCode: 2, StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC()}}
	worker := NewWorkerService(boundary)

	report, err := worker.ExecuteRunnableStep(context.Background(), RunnableStep{BuildID: "build-2", StepIndex: 0, StepName: "test", Command: "false"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if boundary.completeCalls != 0 || boundary.failCalls != 0 {
		t.Fatalf("expected worker to avoid direct build mutation, got complete=%d fail=%d", boundary.completeCalls, boundary.failCalls)
	}
	if report.Step.Status != domain.BuildStepStatusFailed {
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
	if boundary.failCalls != 0 {
		t.Fatalf("expected worker to avoid direct fail build call, got %d", boundary.failCalls)
	}
	if report.Step.Status != domain.BuildStepStatusFailed {
		t.Fatalf("expected step status failed, got %q", report.Step.Status)
	}
}

func TestWorkerService_ExecuteRunnableStep_InvalidTransitionOutcomeWithErrorIsNotIgnored(t *testing.T) {
	boundary := &fakeBuildExecutionBoundary{
		runStepErr: errors.New("persistence unavailable"),
		runOutcome: repository.StepCompletionInvalidTransition,
	}
	worker := NewWorkerService(boundary)

	report, err := worker.ExecuteRunnableStep(context.Background(), RunnableStep{BuildID: "build-op", StepIndex: 0, StepName: "test", Command: "echo"})
	if err == nil || err.Error() != "persistence unavailable" {
		t.Fatalf("expected persistence unavailable error, got %v", err)
	}
	if report.Step.Status != domain.BuildStepStatusFailed {
		t.Fatalf("expected step status failed, got %q", report.Step.Status)
	}
}

func TestWorkerService_ClaimRunnableStep_UsesPersistedJobSpec(t *testing.T) {
	now := time.Now().UTC()
	boundary := &fakeBuildExecutionBoundary{
		listBuildsResp: []domain.Build{{ID: "build-1", Status: domain.BuildStatusQueued}},
		stepsByBuildID: map[string][]domain.BuildStep{
			"build-1": {
				{ID: "step-1", BuildID: "build-1", StepIndex: 0, Name: "step-1", Status: domain.BuildStepStatusPending, Command: "sh", Args: []string{"-c", "echo from-step"}, WorkingDir: ".", Env: map[string]string{"A": "step"}},
			},
		},
		jobsByStepID: map[string]domain.ExecutionJob{
			"step-1": {
				ID:             "job-1",
				BuildID:        "build-1",
				StepID:         "step-1",
				StepIndex:      0,
				Name:           "step-1",
				Status:         domain.ExecutionJobStatusQueued,
				Command:        []string{"sh", "-c", "echo from-job"},
				WorkingDir:     "backend",
				Environment:    map[string]string{"A": "job"},
				TimeoutSeconds: intPtr(120),
				CreatedAt:      now,
			},
		},
	}

	worker := NewWorkerServiceWithLease(boundary, "worker-1", 30*time.Second)
	runnable, found, err := worker.ClaimRunnableStep(context.Background())
	if err != nil {
		t.Fatalf("claim runnable step failed: %v", err)
	}
	if !found {
		t.Fatal("expected runnable work")
	}
	if runnable.JobID != "job-1" {
		t.Fatalf("expected job id job-1, got %q", runnable.JobID)
	}
	if len(runnable.Args) != 2 || !strings.Contains(runnable.Args[1], "from-job") {
		t.Fatalf("expected args from persisted job spec, got %#v", runnable.Args)
	}
	if runnable.WorkingDir != "backend" {
		t.Fatalf("expected working dir from job spec, got %q", runnable.WorkingDir)
	}
	if runnable.Env["A"] != "job" {
		t.Fatalf("expected env from job spec, got %#v", runnable.Env)
	}
}

func TestWorkerService_ClaimRunnableStep_ClaimsJobDirectly(t *testing.T) {
	now := time.Now().UTC()
	boundary := &fakeBuildExecutionBoundary{
		jobsQueue: []domain.ExecutionJob{
			{
				ID:             "job-1",
				BuildID:        "build-1",
				StepID:         "step-1",
				StepIndex:      0,
				Name:           "lint",
				Status:         domain.ExecutionJobStatusQueued,
				Command:        []string{"sh", "-c", "echo from-job"},
				WorkingDir:     "backend",
				Environment:    map[string]string{"A": "job"},
				TimeoutSeconds: intPtr(90),
				CreatedAt:      now,
			},
		},
		stepsByBuildID: map[string][]domain.BuildStep{
			"build-1": {
				{ID: "step-1", BuildID: "build-1", StepIndex: 0, Name: "lint", Status: domain.BuildStepStatusPending},
			},
		},
	}

	worker := NewWorkerServiceWithLease(boundary, "worker-1", 30*time.Second)
	runnable, found, err := worker.ClaimRunnableStep(context.Background())
	if err != nil {
		t.Fatalf("claim runnable step failed: %v", err)
	}
	if !found {
		t.Fatal("expected runnable work")
	}
	if runnable.JobID != "job-1" {
		t.Fatalf("expected job claim, got job id %q", runnable.JobID)
	}
	if runnable.StepID != "step-1" {
		t.Fatalf("expected linked step id step-1, got %q", runnable.StepID)
	}
	if runnable.TimeoutSeconds != 90 {
		t.Fatalf("expected timeout from job contract, got %d", runnable.TimeoutSeconds)
	}
	if boundary.startCalls != 1 {
		t.Fatalf("expected start build call once, got %d", boundary.startCalls)
	}
}

func intPtr(value int) *int {
	return &value
}

func TestWorkerService_ExecuteRunnableStep_SideEffectFailureIsReported(t *testing.T) {
	boundary := &fakeBuildExecutionBoundary{
		runStepResp: runner.RunStepResult{Status: runner.RunStepStatusSuccess, ExitCode: 0, StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC()},
		runOutcome:  repository.StepCompletionCompleted,
		runSideErr:  errors.New("log write failed"),
	}
	worker := NewWorkerService(boundary)

	report, err := worker.ExecuteRunnableStep(context.Background(), RunnableStep{BuildID: "build-side", StepIndex: 0, StepName: "test", Command: "echo"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if report.Step.Status != domain.BuildStepStatusSuccess {
		t.Fatalf("expected step status success, got %q", report.Step.Status)
	}
	if report.SideEffectError == nil {
		t.Fatal("expected side effect error to be present on report")
	}
	if *report.SideEffectError != "log write failed" {
		t.Fatalf("expected side effect message to be preserved, got %q", *report.SideEffectError)
	}
}

func TestWorkerService_ExecuteRunnableStep_TimeoutMarkedFailedWithReason(t *testing.T) {
	boundary := &fakeBuildExecutionBoundary{
		runStepResp: runner.RunStepResult{
			Status:     runner.RunStepStatusFailed,
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
	if report.Step.Status != domain.BuildStepStatusFailed {
		t.Fatalf("expected step status failed, got %q", report.Step.Status)
	}
	if report.Result.ExitCode != -1 {
		t.Fatalf("expected timeout exit code -1, got %d", report.Result.ExitCode)
	}
	if !strings.Contains(report.Result.Stderr, "timed out") {
		t.Fatalf("expected timeout reason, got %q", report.Result.Stderr)
	}
	if boundary.failCalls != 0 {
		t.Fatalf("expected worker to avoid direct fail build call, got %d", boundary.failCalls)
	}
}

func TestWorkerService_ClaimRunnableStep_ClaimsFirstPendingStep(t *testing.T) {
	boundary := &fakeBuildExecutionBoundary{
		listBuildsResp: []domain.Build{{ID: "build-1", Status: domain.BuildStatusQueued}},
		stepsByBuildID: map[string][]domain.BuildStep{
			"build-1": {
				{StepIndex: 0, Name: "lint", Status: domain.BuildStepStatusPending},
				{StepIndex: 1, Name: "test", Status: domain.BuildStepStatusPending},
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
		stepsByBuildID: map[string][]domain.BuildStep{
			"build-1": {
				{StepIndex: 5, Name: "lint", Status: domain.BuildStepStatusPending},
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
		stepsByBuildID: map[string][]domain.BuildStep{
			"build-1": {
				{StepIndex: 0, Name: "lint", Command: "go", Args: []string{"test", "./..."}, Env: map[string]string{"CGO_ENABLED": "0"}, WorkingDir: "/workspace", TimeoutSeconds: 300, Status: domain.BuildStepStatusPending},
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
		stepsByBuildID: map[string][]domain.BuildStep{
			"build-1": {
				{StepIndex: 0, Name: "lint", Status: domain.BuildStepStatusSuccess},
				{StepIndex: 1, Name: "test", Status: domain.BuildStepStatusPending},
				{StepIndex: 2, Name: "package", Status: domain.BuildStepStatusPending},
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
		stepsByBuildID: map[string][]domain.BuildStep{
			"build-1": {
				{StepIndex: 0, Name: "lint", Status: domain.BuildStepStatusRunning},
				{StepIndex: 1, Name: "test", Status: domain.BuildStepStatusPending},
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
		stepsByBuildID: map[string][]domain.BuildStep{
			"build-1": {
				{StepIndex: 0, Name: "lint", Status: domain.BuildStepStatusPending},
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
		stepsByBuildID: map[string][]domain.BuildStep{
			"build-1": {
				{StepIndex: 0, Name: "lint", Status: domain.BuildStepStatusPending},
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
	if boundary.queueCalls != 1 {
		t.Fatalf("expected pending build to be queued before claim, got %d queue calls", boundary.queueCalls)
	}
	if boundary.startCalls != 1 {
		t.Fatalf("expected pending build to transition to running once, got %d", boundary.startCalls)
	}
}

func TestWorkerService_ClaimRunnableStep_PendingBuildWithoutStepsBootstrapsQueue(t *testing.T) {
	boundary := &fakeBuildExecutionBoundary{
		listBuildsResp: []domain.Build{{ID: "build-1", Status: domain.BuildStatusPending}},
		stepsByBuildID: map[string][]domain.BuildStep{},
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

func TestWorkerService_ClaimRunnableStep_ReclaimsExpiredRunningStep(t *testing.T) {
	now := time.Now().UTC()
	expiredAt := now.Add(-time.Second)
	boundary := &fakeBuildExecutionBoundary{
		listBuildsResp: []domain.Build{{ID: "build-1", Status: domain.BuildStatusRunning}},
		stepsByBuildID: map[string][]domain.BuildStep{
			"build-1": {
				{StepIndex: 0, Name: "lint", Status: domain.BuildStepStatusRunning, LeaseExpiresAt: &expiredAt},
				{StepIndex: 1, Name: "test", Status: domain.BuildStepStatusPending},
			},
		},
	}

	worker := NewWorkerServiceWithLease(boundary, "worker-b", 30*time.Second)
	worker.clock = func() time.Time { return now }

	runnable, found, err := worker.ClaimRunnableStep(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !found {
		t.Fatal("expected reclaimable runnable step")
	}
	if runnable.StepIndex != 0 {
		t.Fatalf("expected reclaim of step 0, got %d", runnable.StepIndex)
	}
	if boundary.reclaimCalls != 1 {
		t.Fatalf("expected one reclaim attempt, got %d", boundary.reclaimCalls)
	}
}

func TestWorkerService_ClaimRunnableStep_DoesNotReclaimNonExpiredRunningStep(t *testing.T) {
	now := time.Now().UTC()
	leaseExpiresAt := now.Add(30 * time.Second)
	boundary := &fakeBuildExecutionBoundary{
		listBuildsResp: []domain.Build{{ID: "build-1", Status: domain.BuildStatusRunning}},
		stepsByBuildID: map[string][]domain.BuildStep{
			"build-1": {
				{StepIndex: 0, Name: "lint", Status: domain.BuildStepStatusRunning, LeaseExpiresAt: &leaseExpiresAt},
				{StepIndex: 1, Name: "test", Status: domain.BuildStepStatusPending},
			},
		},
	}

	worker := NewWorkerServiceWithLease(boundary, "worker-b", 30*time.Second)
	worker.clock = func() time.Time { return now }

	_, found, err := worker.ClaimRunnableStep(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if found {
		t.Fatal("expected no reclaim while lease is active")
	}
}

func TestWorkerService_ExecuteRunnableStep_RenewsLeaseWhileRunning(t *testing.T) {
	claimToken := "claim-a"
	workerID := "worker-a"
	now := time.Now().UTC()
	boundary := &fakeBuildExecutionBoundary{
		runStepDelay: 120 * time.Millisecond,
		runStepResp: runner.RunStepResult{
			Status:     runner.RunStepStatusSuccess,
			ExitCode:   0,
			StartedAt:  now,
			FinishedAt: now.Add(120 * time.Millisecond),
		},
		stepsByBuildID: map[string][]domain.BuildStep{
			"build-1": {
				{StepIndex: 0, Name: "test", Status: domain.BuildStepStatusRunning, WorkerID: &workerID, ClaimToken: &claimToken, LeaseExpiresAt: ptrTime(now.Add(50 * time.Millisecond))},
			},
		},
	}

	worker := NewWorkerServiceWithLease(boundary, workerID, 90*time.Millisecond)
	worker.clock = time.Now

	_, err := worker.ExecuteRunnableStep(context.Background(), RunnableStep{BuildID: "build-1", StepIndex: 0, StepName: "test", WorkerID: workerID, ClaimToken: claimToken, Command: "echo", Args: []string{"ok"}})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if boundary.renewCalls == 0 {
		t.Fatal("expected at least one lease renewal during execution")
	}

	renewCallsAfter := boundary.renewCalls
	time.Sleep(80 * time.Millisecond)
	if boundary.renewCalls != renewCallsAfter {
		t.Fatalf("expected heartbeat to stop after execution; renew calls changed from %d to %d", renewCallsAfter, boundary.renewCalls)
	}
}

func TestWorkerService_ExecuteRunnableStep_StaleRenewalStopsHeartbeat(t *testing.T) {
	claimToken := "claim-a"
	workerID := "worker-a"
	now := time.Now().UTC()
	boundary := &fakeBuildExecutionBoundary{
		runStepDelay: 300 * time.Millisecond,
		renewStale:   true,
		runStepResp: runner.RunStepResult{
			Status:     runner.RunStepStatusSuccess,
			ExitCode:   0,
			StartedAt:  now,
			FinishedAt: now.Add(300 * time.Millisecond),
		},
		stepsByBuildID: map[string][]domain.BuildStep{
			"build-1": {
				{StepIndex: 0, Name: "test", Status: domain.BuildStepStatusRunning, WorkerID: &workerID, ClaimToken: &claimToken, LeaseExpiresAt: ptrTime(now.Add(40 * time.Millisecond))},
			},
		},
	}

	worker := NewWorkerServiceWithLease(boundary, workerID, 120*time.Millisecond)
	worker.clock = time.Now

	_, err := worker.ExecuteRunnableStep(context.Background(), RunnableStep{BuildID: "build-1", StepIndex: 0, StepName: "test", WorkerID: workerID, ClaimToken: claimToken, Command: "echo", Args: []string{"ok"}})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if boundary.renewCalls == 0 {
		t.Fatal("expected at least one renewal attempt")
	}

	attempts := boundary.renewCalls
	time.Sleep(100 * time.Millisecond)
	if boundary.renewCalls != attempts {
		t.Fatalf("expected stale renewal to stop heartbeat; renew calls changed from %d to %d", attempts, boundary.renewCalls)
	}
}

func ptrTime(t time.Time) *time.Time {
	return &t
}

func TestWorkerService_HeartbeatIntervalForStep_AddsBoundedJitter(t *testing.T) {
	worker := NewWorkerServiceWithLease(&fakeBuildExecutionBoundary{}, "worker-a", 30*time.Second)
	base := worker.heartbeatInterval()
	window := base / 5

	stepA := RunnableStep{WorkerID: "worker-a", ClaimToken: "claim-a"}
	stepB := RunnableStep{WorkerID: "worker-a", ClaimToken: "claim-b"}

	intervalA := worker.heartbeatIntervalForStep(stepA)
	intervalB := worker.heartbeatIntervalForStep(stepB)

	if intervalA < base-window || intervalA > base+window {
		t.Fatalf("intervalA out of jitter bounds: base=%s window=%s got=%s", base, window, intervalA)
	}
	if intervalB < base-window || intervalB > base+window {
		t.Fatalf("intervalB out of jitter bounds: base=%s window=%s got=%s", base, window, intervalB)
	}
	if intervalA == intervalB {
		t.Fatalf("expected jittered intervals to differ for different claim tokens, got %s and %s", intervalA, intervalB)
	}
}

func TestWorkerService_RecoveryStatsSnapshot(t *testing.T) {
	workerID := "worker-a"
	now := time.Now().UTC()
	boundary := &fakeBuildExecutionBoundary{
		listBuildsResp: []domain.Build{{ID: "build-1", Status: domain.BuildStatusQueued}},
		runStepDelay:   120 * time.Millisecond,
		runStepResp: runner.RunStepResult{
			Status:     runner.RunStepStatusSuccess,
			ExitCode:   0,
			StartedAt:  now,
			FinishedAt: now.Add(120 * time.Millisecond),
		},
		stepsByBuildID: map[string][]domain.BuildStep{
			"build-1": {
				{StepIndex: 0, Name: "test", Status: domain.BuildStepStatusPending},
			},
		},
	}

	worker := NewWorkerServiceWithLease(boundary, workerID, 90*time.Millisecond)
	worker.clock = time.Now

	runnable, found, err := worker.ClaimRunnableStep(context.Background())
	if err != nil {
		t.Fatalf("claim failed: %v", err)
	}
	if !found {
		t.Fatal("expected runnable step")
	}

	if _, err := worker.ExecuteRunnableStep(context.Background(), runnable); err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	stats := worker.RecoveryStats()
	if stats.ClaimsWon != 1 {
		t.Fatalf("expected claims_won=1, got %d", stats.ClaimsWon)
	}
	if stats.RenewalsWon == 0 {
		t.Fatal("expected renewals_won > 0")
	}
}
