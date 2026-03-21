package contracts

import "time"

type BuildStepStatus string

const (
	BuildStepStatusPending BuildStepStatus = "pending"
	BuildStepStatusRunning BuildStepStatus = "running"
	BuildStepStatusSuccess BuildStepStatus = "success"
	BuildStepStatusFailed  BuildStepStatus = "failed"
)

type BuildStep struct {
	Name      string
	Status    BuildStepStatus
	StartedAt *time.Time
	EndedAt   *time.Time
}

type BuildLogLine struct {
	StepName  string
	Timestamp time.Time
	Message   string
}
