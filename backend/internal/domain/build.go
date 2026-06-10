package domain

import "time"

type BuildStatus string

const (
	BuildStatusPending   BuildStatus = "pending"
	BuildStatusQueued    BuildStatus = "queued"
	BuildStatusPreparing BuildStatus = "preparing"
	BuildStatusRunning   BuildStatus = "running"
	BuildStatusSuccess   BuildStatus = "success"
	BuildStatusFailed    BuildStatus = "failed"
	BuildStatusCanceled  BuildStatus = "canceled"
)

type Build struct {
	ID               string
	BuildNumber      int64
	ProjectID        string
	JobID            *string
	Priority         int
	Status           BuildStatus
	CreatedAt        time.Time
	QueuedAt         *time.Time
	StartedAt        *time.Time
	FinishedAt       *time.Time
	CurrentStepIndex int
	AttemptNumber    int
	RerunOfBuildID   *string
	RerunFromStepIdx *int
	ErrorMessage     *string

	// Pipeline snapshot: persisted at build creation time for replayability.
	PipelineConfigYAML *string
	PipelineName       *string
	PipelineSource     *string
	PipelinePath       *string

	// Source captures per-build source input and resolved source identity.
	Source *SourceSpec

	// Repo source: persisted when a build is created from a repository checkout.
	RepoURL   *string
	Ref       *string
	CommitSHA *string

	// Build metadata captures what source was run and why this build exists.
	SourceRef            *string
	SourceSHA            *string
	SourceAuthorName     *string
	SourceAuthorEmail    *string
	SourceCommitterName  *string
	SourceCommitterEmail *string
	TriggerType          BuildTriggerType
	TriggeredBy          *string

	// Trigger captures why/how this build was created (manual or webhook metadata).
	Trigger BuildTrigger

	// Image execution provenance stores what was requested and what immutable
	// image identity was actually used by execution.
	RequestedImageRef     *string
	ResolvedImageRef      *string
	ImageSourceKind       ImageSourceKind
	ManagedImageID        *string
	ManagedImageVersionID *string
}
