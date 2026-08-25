package worker

import (
	"context"
	"log"

	"github.com/radiation/coyote-ci/backend/internal/service/execution"
)

type synchronousExecutionService interface {
	ClaimRunnableStep(ctx context.Context) (WorkerRunnableStep, bool, error)
	ExecuteRunnableStep(ctx context.Context, step WorkerRunnableStep) (WorkerStepExecutionReport, error)
}

// SynchronousController adapts the existing worker execution path to the
// controller contract. It intentionally waits for a claimed step to finish.
type SynchronousController struct {
	service synchronousExecutionService
}

var _ execution.Controller = (*SynchronousController)(nil)

func NewSynchronousController(service synchronousExecutionService) *SynchronousController {
	return &SynchronousController{service: service}
}

func (c *SynchronousController) Reconcile(ctx context.Context) error {
	log.Printf("polling for runnable work")

	step, found, err := c.service.ClaimRunnableStep(ctx)
	if err != nil {
		return err
	}
	if !found {
		log.Printf("no runnable work found")
		return nil
	}

	_, executeErr := c.service.ExecuteRunnableStep(ctx, step)
	if executeErr != nil {
		return executeErr
	}
	log.Printf("worker iteration completed for claimed work: build_id=%s step=%s", step.BuildID, step.StepName)

	return nil
}
