package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"
)

type ExecutionJobStatus string

const (
	ExecutionJobStatusQueued   ExecutionJobStatus = "queued"
	ExecutionJobStatusRunning  ExecutionJobStatus = "running"
	ExecutionJobStatusSuccess  ExecutionJobStatus = "success"
	ExecutionJobStatusFailed   ExecutionJobStatus = "failed"
	ExecutionJobStatusCanceled ExecutionJobStatus = "canceled"
)

type ExecutionFailureKind string

const (
	ExecutionFailureKindExecution ExecutionFailureKind = "execution"
	ExecutionFailureKindTimeout   ExecutionFailureKind = "timeout"
	ExecutionFailureKindWorkspace ExecutionFailureKind = "workspace"
)

// SourceSnapshotRef is the durable source identity for a queued execution job.
//
// Rerun model:
//   - retry job = same source snapshot + same resolved spec + same declared inputs
//   - rerun from step = new build attempt starts at a selected step using the same
//     source identity and the same resolved execution basis unless explicitly refreshed
type SourceSnapshotRef struct {
	RepositoryURL string
	CommitSHA     string
	RefName       *string
	ArchiveURI    *string
	ArchiveDigest *string
}

// ArtifactRef points to an output artifact without embedding large binary data.
type ArtifactRef struct {
	Name       string
	URI        string
	Digest     *string
	MediaType  *string
	SizeBytes  *int64
	SourceStep *int
}

// WorkspaceInputMode identifies the logical workspace baseline required by an execution job.
// It intentionally does not describe how a backend materializes that baseline.
type WorkspaceInputMode string

const (
	WorkspaceInputModeSource      WorkspaceInputMode = "source"
	WorkspaceInputModePredecessor WorkspaceInputMode = "predecessor"
	WorkspaceInputModeFanIn       WorkspaceInputMode = "fan_in"
)

// WorkspaceInputPlan describes a job's provider-neutral workspace lineage input.
// ProducerNodeID applies to a single predecessor. CommonAncestorNodeID applies to
// fan-in; an empty common ancestor means the immutable source snapshot is the baseline.
type WorkspaceInputPlan struct {
	Mode                       WorkspaceInputMode `json:"mode"`
	ProducerNodeID             string             `json:"producer_node_id,omitempty"`
	CommonAncestorNodeID       string             `json:"common_ancestor_node_id,omitempty"`
	IsolatedWritableDescendant bool               `json:"isolated_writable_descendant,omitempty"`
}

// ExecutionJobSpec captures the immutable worker-facing runtime contract.
type ExecutionJobSpec struct {
	Version          int                `json:"version"`
	Image            string             `json:"image"`
	WorkingDir       string             `json:"working_dir"`
	Command          []string           `json:"command"`
	Environment      map[string]string  `json:"environment"`
	TimeoutSeconds   int                `json:"timeout_seconds"`
	PipelineFilePath string             `json:"pipeline_file_path,omitempty"`
	ContextDir       string             `json:"context_dir,omitempty"`
	Source           SourceSnapshotRef  `json:"source"`
	WorkspaceInput   WorkspaceInputPlan `json:"workspace_input"`
}

func (s ExecutionJobSpec) ToJSON() (string, error) {
	raw, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

type ExecutionJob struct {
	ID               string
	BuildID          string
	StepID           string
	NodeID           string
	GroupName        *string
	DependsOnNodeIDs []string
	Name             string
	StepIndex        int
	AttemptNumber    int
	RetryOfJobID     *string
	LineageRootJobID *string
	Status           ExecutionJobStatus
	QueueName        *string
	Image            string
	WorkingDir       string
	Command          []string
	Environment      map[string]string
	TimeoutSeconds   *int
	PipelineFilePath *string
	ContextDir       *string
	Source           SourceSnapshotRef
	SpecVersion      int
	SpecDigest       *string
	ResolvedSpecJSON string
	ClaimToken       *string
	ClaimedBy        *string
	ClaimExpiresAt   *time.Time
	CreatedAt        time.Time
	StartedAt        *time.Time
	FinishedAt       *time.Time
	ErrorMessage     *string
	FailureKind      *ExecutionFailureKind
	ExitCode         *int
	OutputRefs       []ArtifactRef
}

func BuildSpecDigest(specJSON string) *string {
	trimmed := strings.TrimSpace(specJSON)
	if trimmed == "" {
		return nil
	}
	digest := sha256.Sum256([]byte(trimmed))
	value := hex.EncodeToString(digest[:])
	return &value
}
