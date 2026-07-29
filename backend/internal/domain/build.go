package domain

import (
	"fmt"
	"strings"
	"time"
)

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

	// Repository identity is snapshotted from a registered job repository.
	RegisteredRepositoryID *string
	SCMConnectionID        *string
	ProviderRepositoryID   *string

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

type RepositoryIdentitySnapshot struct {
	RegisteredRepositoryID string
	SCMConnectionID        string
	ProviderRepositoryID   string
}

func (s RepositoryIdentitySnapshot) Validate() error {
	if strings.TrimSpace(s.RegisteredRepositoryID) == "" || strings.TrimSpace(s.SCMConnectionID) == "" || strings.TrimSpace(s.ProviderRepositoryID) == "" {
		return fmt.Errorf("repository identity snapshot fields are required")
	}
	return nil
}

func ValidateRepositoryIdentitySnapshot(registeredRepositoryID, scmConnectionID, providerRepositoryID *string) error {
	values := []*string{registeredRepositoryID, scmConnectionID, providerRepositoryID}
	present := 0
	for _, value := range values {
		if value != nil {
			present++
			if strings.TrimSpace(*value) == "" {
				return fmt.Errorf("repository identity snapshot fields are required when present")
			}
		}
	}
	if present != 0 && present != len(values) {
		return fmt.Errorf("repository identity snapshot must be all present or all absent")
	}
	return nil
}

func (b Build) ValidateRepositoryIdentitySnapshot() error {
	return ValidateRepositoryIdentitySnapshot(b.RegisteredRepositoryID, b.SCMConnectionID, b.ProviderRepositoryID)
}
