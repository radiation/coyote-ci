package main

import (
	"context"
	"encoding/json"
	"errors"
	nethttp "net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/runner"
	"github.com/radiation/coyote-ci/backend/internal/service"
)

type fakeWorkerIterationService struct {
	claimStep service.RunnableStep
	claimOK   bool
	claimErr  error

	executeReport service.StepExecutionReport
	executeErr    error

	executeCalls int
}

type fakeWorkerStatusProvider struct {
	stats service.WorkerRecoveryStats
}

func (f *fakeWorkerStatusProvider) RecoveryStats() service.WorkerRecoveryStats {
	return f.stats
}

func (f *fakeWorkerIterationService) ClaimRunnableStep(_ context.Context) (service.RunnableStep, bool, error) {
	return f.claimStep, f.claimOK, f.claimErr
}

func (f *fakeWorkerIterationService) ExecuteRunnableStep(_ context.Context, _ service.RunnableStep) (service.StepExecutionReport, error) {
	f.executeCalls++
	return f.executeReport, f.executeErr
}

func TestRunWorkerIteration_Success(t *testing.T) {
	worker := &fakeWorkerIterationService{
		claimStep: service.RunnableStep{BuildID: "build-1", StepName: "default"},
		claimOK:   true,
		executeReport: service.StepExecutionReport{
			BuildID: "build-1",
			Step: domain.BuildStep{
				Name:   "default",
				Status: domain.BuildStepStatusSuccess,
			},
			Result: runner.RunStepResult{
				Status:     runner.RunStepStatusSuccess,
				ExitCode:   0,
				StartedAt:  time.Now().UTC(),
				FinishedAt: time.Now().UTC(),
			},
		},
	}

	if err := runWorkerIteration(context.Background(), worker); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if worker.executeCalls != 1 {
		t.Fatalf("expected execute to be called once, got %d", worker.executeCalls)
	}
}

func TestRunWorkerIteration_ExecutionFailure(t *testing.T) {
	worker := &fakeWorkerIterationService{
		claimStep:  service.RunnableStep{BuildID: "build-2", StepName: "default"},
		claimOK:    true,
		executeErr: errors.New("step failed"),
	}

	err := runWorkerIteration(context.Background(), worker)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() != "step failed" {
		t.Fatalf("expected step failed error, got %v", err)
	}
	if worker.executeCalls != 1 {
		t.Fatalf("expected execute to be called once, got %d", worker.executeCalls)
	}
}

func TestNewWorkerStatusHandler_Healthz(t *testing.T) {
	h := newWorkerStatusHandler(&fakeWorkerStatusProvider{})
	req := httptest.NewRequest(nethttp.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != nethttp.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if rr.Body.String() != "ok" {
		t.Fatalf("expected ok body, got %q", rr.Body.String())
	}
}

func TestNewWorkerStatusHandler_RecoveryStatus(t *testing.T) {
	h := newWorkerStatusHandler(&fakeWorkerStatusProvider{stats: service.WorkerRecoveryStats{
		ClaimsWon:     1,
		ReclaimsWon:   2,
		RenewalsWon:   3,
		RenewalsStale: 4,
		StaleComplete: 5,
		ReclaimMisses: 6,
	}})

	req := httptest.NewRequest(nethttp.MethodGet, "/internal/status/worker", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != nethttp.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp struct {
		WorkerRecovery service.WorkerRecoveryStats `json:"worker_recovery"`
		TimestampUTC   time.Time                   `json:"timestamp_utc"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode json: %v", err)
	}
	if resp.WorkerRecovery.ClaimsWon != 1 || resp.WorkerRecovery.ReclaimsWon != 2 || resp.WorkerRecovery.RenewalsWon != 3 {
		t.Fatalf("unexpected recovery stats payload: %+v", resp.WorkerRecovery)
	}
	if resp.TimestampUTC.IsZero() {
		t.Fatal("expected timestamp_utc to be set")
	}
}
