package domain

import "time"

type BuildStepStatus string

const (
	BuildStepStatusPending BuildStepStatus = "pending"
	BuildStepStatusRunning BuildStepStatus = "running"
	BuildStepStatusSuccess BuildStepStatus = "success"
	BuildStepStatusFailed  BuildStepStatus = "failed"
)

type BuildStep struct {
	ID           string
	BuildID      string
	StepIndex    int
	Name         string
	Status       BuildStepStatus
	WorkerID     *string
	StartedAt    *time.Time
	FinishedAt   *time.Time
	ExitCode     *int
	ErrorMessage *string
}
