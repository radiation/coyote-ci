package domain

import "time"

type BuildStatus string

const (
	BuildStatusPending BuildStatus = "pending"
	BuildStatusQueued  BuildStatus = "queued"
	BuildStatusRunning BuildStatus = "running"
	BuildStatusSuccess BuildStatus = "success"
	BuildStatusFailed  BuildStatus = "failed"
)

type Build struct {
	ID        string
	ProjectID string
	Status    BuildStatus
	CreatedAt time.Time
}
