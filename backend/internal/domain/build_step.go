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
	ID             string
	BuildID        string
	StepIndex      int
	NodeID         string
	GroupName      *string
	DependsOnNodes []string
	Name           string
	Image          string
	Command        string
	Args           []string
	Env            map[string]string
	WorkingDir     string
	TimeoutSeconds int
	ArtifactPaths  []string
	Cache          *StepCacheConfig
	Status         BuildStepStatus
	WorkerID       *string
	ClaimToken     *string
	ClaimedAt      *time.Time
	LeaseExpiresAt *time.Time
	StartedAt      *time.Time
	FinishedAt     *time.Time
	ExitCode       *int
	Stdout         *string
	Stderr         *string
	ErrorMessage   *string
}
