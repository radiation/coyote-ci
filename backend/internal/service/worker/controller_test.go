package worker

import (
	"context"
	"errors"
	"testing"
)

type controllerTestService struct {
	step         WorkerRunnableStep
	found        bool
	claimErr     error
	executeErr   error
	executeCalls int
}

func (s *controllerTestService) ClaimRunnableStep(context.Context) (WorkerRunnableStep, bool, error) {
	return s.step, s.found, s.claimErr
}

func (s *controllerTestService) ExecuteRunnableStep(context.Context, WorkerRunnableStep) (WorkerStepExecutionReport, error) {
	s.executeCalls++
	return WorkerStepExecutionReport{}, s.executeErr
}

func TestSynchronousController_ReconcileExecutesClaimedStep(t *testing.T) {
	service := &controllerTestService{
		step:  WorkerRunnableStep{BuildID: "build-1", StepName: "test"},
		found: true,
	}

	if err := NewSynchronousController(service).Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if service.executeCalls != 1 {
		t.Fatalf("expected one execution, got %d", service.executeCalls)
	}
}

func TestSynchronousController_ReconcileReturnsClaimAndExecutionErrors(t *testing.T) {
	claimErr := errors.New("claim failed")
	if err := NewSynchronousController(&controllerTestService{claimErr: claimErr}).Reconcile(context.Background()); !errors.Is(err, claimErr) {
		t.Fatalf("expected claim error, got %v", err)
	}

	executeErr := errors.New("execution failed")
	service := &controllerTestService{found: true, executeErr: executeErr}
	if err := NewSynchronousController(service).Reconcile(context.Background()); !errors.Is(err, executeErr) {
		t.Fatalf("expected execution error, got %v", err)
	}
	if service.executeCalls != 1 {
		t.Fatalf("expected one execution, got %d", service.executeCalls)
	}
}
