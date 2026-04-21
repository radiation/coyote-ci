package domain

import "time"

type JobManagedImageConfig struct {
	ID                string
	JobID             string
	ManagedImageName  string
	PipelinePath      string
	WriteCredentialID string
	BotBranchPrefix   string
	CommitAuthorName  string
	CommitAuthorEmail string
	Enabled           bool
	CreatedAt         time.Time
	UpdatedAt         time.Time
}
