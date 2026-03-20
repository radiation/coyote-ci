package inprocess

import (
	"context"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/execution"
	"github.com/radiation/coyote-ci/backend/internal/runner"
	"github.com/radiation/coyote-ci/backend/pkg/contracts"
)

var _ runner.Runner = (*Runner)(nil)

type Runner struct {
	executor execution.Executor
}

func New(executor execution.Executor) *Runner {
	if executor == nil {
		executor = execution.NewLocalExecutor()
	}

	return &Runner{executor: executor}
}

func (r *Runner) RunStep(ctx context.Context, request contracts.RunStepRequest) (contracts.RunStepResult, error) {
	timeout := time.Duration(request.TimeoutSeconds) * time.Second

	execResult, err := r.executor.Execute(ctx, execution.CommandRequest{
		Command:    request.Command,
		Args:       request.Args,
		Env:        request.Env,
		WorkingDir: request.WorkingDir,
		Timeout:    timeout,
	})
	if err != nil {
		return contracts.RunStepResult{}, err
	}

	status := contracts.RunStepStatusSuccess
	if execResult.ExitCode != 0 {
		status = contracts.RunStepStatusFailed
	}

	return contracts.RunStepResult{
		Status:     status,
		ExitCode:   execResult.ExitCode,
		Stdout:     execResult.Stdout,
		Stderr:     execResult.Stderr,
		StartedAt:  execResult.StartedAt,
		FinishedAt: execResult.FinishedAt,
	}, nil
}
