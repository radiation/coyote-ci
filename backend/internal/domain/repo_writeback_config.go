package domain

import "time"

type RepoWritebackConfig struct {
	ID                string
	ProjectID         string
	RepositoryURL     string
	PipelinePath      string
	ManagedImageName  string
	WriteCredentialID string
	BotBranchPrefix   string
	CommitAuthorName  string
	CommitAuthorEmail string
	Enabled           bool
	CreatedAt         time.Time
	UpdatedAt         time.Time
}
