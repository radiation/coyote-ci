package domain

import "time"

type Worker struct {
	ID              string
	Name            string
	LastHeartbeatAt time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type WorkerHeartbeat struct {
	ID          string
	Name        string
	HeartbeatAt time.Time
}

type WorkerStatus string

const (
	WorkerStatusIdle  WorkerStatus = "idle"
	WorkerStatusBusy  WorkerStatus = "busy"
	WorkerStatusStale WorkerStatus = "stale"
)

type WorkerVisibility struct {
	ID               string
	Name             string
	Status           WorkerStatus
	LastHeartbeatAt  time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
	CurrentBuildID   *string
	CurrentBuildNum  *int64
	CurrentStepID    *string
	CurrentStepIndex *int
	CurrentStepName  *string
	LeaseExpiresAt   *time.Time
	ClaimedAt        *time.Time
	ProjectID        *string
	ProjectName      *string
	ProjectSlug      *string
	JobID            *string
	JobName          *string
	StaleLease       bool
	StaleHeartbeat   bool
}
