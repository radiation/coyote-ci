package runner

import (
	"context"
	"time"
)

// RunStepRequest describes the command a runner should execute for a build step.
type RunStepRequest struct {
	BuildID        string
	StepIndex      int
	StepName       string
	Command        string
	Args           []string
	Env            map[string]string
	WorkingDir     string
	TimeoutSeconds int
}

// RunStepStatus indicates the outcome of a step execution.
type RunStepStatus string

const (
	RunStepStatusSuccess RunStepStatus = "success"
	RunStepStatusFailed  RunStepStatus = "failed"
)

// RunStepResult is the outcome returned by a runner after executing a step.
type RunStepResult struct {
	Status     RunStepStatus
	ExitCode   int
	Stdout     string
	Stderr     string
	StartedAt  time.Time
	FinishedAt time.Time
}

type Runner interface {
	RunStep(ctx context.Context, request RunStepRequest) (RunStepResult, error)
}
