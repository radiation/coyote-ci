package domain

import "time"

type QueueEntry struct {
	Build          Build
	ProjectName    *string
	ProjectSlug    *string
	JobName        *string
	WorkerID       *string
	LeaseExpiresAt *time.Time
}
