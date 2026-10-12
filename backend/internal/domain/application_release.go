package domain

import "time"

type ApplicationReleaseStatus string

const (
	ApplicationReleaseStatusPending  ApplicationReleaseStatus = "pending"
	ApplicationReleaseStatusComplete ApplicationReleaseStatus = "complete"
	ApplicationReleaseStatusFailed   ApplicationReleaseStatus = "failed"
)

type ApplicationReleaseOutputKind string

const (
	ApplicationReleaseOutputArtifact ApplicationReleaseOutputKind = "artifact"
	ApplicationReleaseOutputImage    ApplicationReleaseOutputKind = "image"
)

type ApplicationReleaseOutputStatus string

const (
	ApplicationReleaseOutputPending   ApplicationReleaseOutputStatus = "pending"
	ApplicationReleaseOutputPublished ApplicationReleaseOutputStatus = "published"
	ApplicationReleaseOutputFailed    ApplicationReleaseOutputStatus = "failed"
)

type ApplicationRelease struct {
	ID                 string
	BuildID            string
	ProjectID          string
	JobID              *string
	ApplicationVersion string
	Status             ApplicationReleaseStatus
	AutoChannelName    string
	FailureReason      string
	CreatedAt          time.Time
	CompletedAt        *time.Time
	FailedAt           *time.Time
	UpdatedAt          time.Time
	Outputs            []ApplicationReleaseOutput
}

type ApplicationReleaseOutput struct {
	ID                 string
	ReleaseID          string
	Kind               ApplicationReleaseOutputKind
	OutputKey          string
	DisplayName        string
	DefinitionJSON     string
	Status             ApplicationReleaseOutputStatus
	ArtifactID         *string
	ArtifactVersionID  *string
	ExecutionJobID     *string
	ImmutableReference string
	FailureReason      string
	PublishedAt        *time.Time
	FailedAt           *time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type ApplicationChannel struct {
	ID               string
	ProjectID        string
	JobID            *string
	Name             string
	CurrentReleaseID string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type ApplicationChannelEvent struct {
	ID                string
	ChannelID         string
	PreviousReleaseID *string
	NewReleaseID      string
	ActorUserID       *string
	Reason            string
	CreatedAt         time.Time
}
