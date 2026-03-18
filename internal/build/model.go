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
	Command   string
	Status    Status
	CreatedAt time.Time
}

type Step struct {
	ID      string
	BuildID string
	Name    string
	Command string
	Status  Status
}

type Worker struct {
	ID        string
	LastSeen  time.Time
	IsHealthy bool
}
