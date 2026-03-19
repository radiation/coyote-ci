package build

import "time"

type Status string

const (
	StatusPending Status = "pending"
	StatusRunning Status = "running"
	StatusSuccess Status = "success"
	StatusFailed  Status = "failed"
)

type Build struct {
	ID        string
	Repo      string
	CommitSHA string
	Status    Status
	Output    string
	CreatedAt time.Time
}

type Step struct {
	ID        string
	BuildID   string
	Name      string
	Command   string
	Output    string
	Status    Status
	CreatedAt time.Time
}

type StepSpec struct {
	Name    string
	Command string
}

type Worker struct {
	ID        string
	LastSeen  time.Time
	IsHealthy bool
}
