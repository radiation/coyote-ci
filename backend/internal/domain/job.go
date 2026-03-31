package domain

import "time"

// Job is a durable reusable CI definition that can be manually executed.
type Job struct {
	ID            string
	ProjectID     string
	Name          string
	RepositoryURL string
	DefaultRef    string
	PushEnabled   bool
	PushBranch    *string
	PipelineYAML  string
	Enabled       bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
}
