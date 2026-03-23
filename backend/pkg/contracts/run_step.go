package contracts

import "time"

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

type RunStepStatus string

const (
	RunStepStatusSuccess RunStepStatus = "success"
	RunStepStatusFailed  RunStepStatus = "failed"
)

type RunStepResult struct {
	Status     RunStepStatus
	ExitCode   int
	Stdout     string
	Stderr     string
	StartedAt  time.Time
	FinishedAt time.Time
}
