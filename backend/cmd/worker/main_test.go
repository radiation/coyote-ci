package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/service"
	"github.com/radiation/coyote-ci/backend/pkg/contracts"
)

type fakeWorkerIterationService struct {
	claimStep service.RunnableStep
	claimOK   bool
	claimErr  error

	executeReport service.StepExecutionReport
	executeErr    error

	executeCalls int
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
			Step: contracts.BuildStep{
				Name:   "default",
				Status: contracts.BuildStepStatusSuccess,
			},
			Result: contracts.RunStepResult{
				Status:     contracts.RunStepStatusSuccess,
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
