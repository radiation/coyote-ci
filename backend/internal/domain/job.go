package domain

import "time"

type JobTriggerMode string

const (
	JobTriggerModeBranches        JobTriggerMode = "branches"
	JobTriggerModeTags            JobTriggerMode = "tags"
	JobTriggerModeBranchesAndTags JobTriggerMode = "branches_and_tags"
)

// Job is a durable reusable CI definition that can be manually executed.
type Job struct {
	ID                 string
	ProjectID          string
	Name               string
	RepositoryURL      string
	DefaultRef         string
	DefaultCommitSHA   *string
	PushEnabled        bool
	PushBranch         *string
	TriggerMode        JobTriggerMode
	BranchAllowlist    []string
	TagAllowlist       []string
	PipelineYAML       string
	PipelinePath       *string
	ManagedImageConfig *JobManagedImageConfig
	Enabled            bool
	CreatedAt          time.Time
	UpdatedAt          time.Time
}
