package domain

import "time"

type ExecutionKind string

const (
	ExecutionKindShell      ExecutionKind = "shell"
	ExecutionKindImageBuild ExecutionKind = "image_build"
)

type ImageBuildProvider string

const ImageBuildProviderCloudBuild ImageBuildProvider = "cloud_build"

type ExternalImageBuildSubmissionState string

const (
	ExternalImageBuildSubmissionIntent    ExternalImageBuildSubmissionState = "intent"
	ExternalImageBuildSubmissionStaged    ExternalImageBuildSubmissionState = "staged"
	ExternalImageBuildSubmissionSubmitted ExternalImageBuildSubmissionState = "submitted"
	ExternalImageBuildSubmissionTerminal  ExternalImageBuildSubmissionState = "terminal"
)

type ImageBuildStatus string

const (
	ImageBuildStatusQueued   ImageBuildStatus = "queued"
	ImageBuildStatusRunning  ImageBuildStatus = "running"
	ImageBuildStatusSuccess  ImageBuildStatus = "success"
	ImageBuildStatusFailed   ImageBuildStatus = "failed"
	ImageBuildStatusCanceled ImageBuildStatus = "canceled"
)

func (s ImageBuildStatus) Terminal() bool {
	return s == ImageBuildStatusSuccess || s == ImageBuildStatusFailed || s == ImageBuildStatusCanceled
}

// RemoteImageBuildSpec is a provider-neutral execution contract.
type RemoteImageBuildSpec struct {
	ContextPath          string                    `json:"context_path"`
	DockerfilePath       string                    `json:"dockerfile_path"`
	Target               string                    `json:"target,omitempty"`
	BuildArgs            map[string]string         `json:"build_args,omitempty"`
	TargetImageReference string                    `json:"target_image_reference"`
	ArtifactInputs       []ImageBuildArtifactInput `json:"artifact_inputs,omitempty"`
}

// ImageBuildArtifactInput declares a named artifact from an upstream step that
// must be materialized at Destination relative to the Docker build context.
type ImageBuildArtifactInput struct {
	Name        string `json:"name"`
	Destination string `json:"destination"`
	Platform    string `json:"platform,omitempty"`
}

// ImageBuildArtifact records one verified artifact consumed to assemble an
// image. It is persisted with the image build for provenance and debugging.
type ImageBuildArtifact struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	StorageKey     string `json:"storage_key"`
	ChecksumSHA256 string `json:"checksum_sha256"`
	SizeBytes      int64  `json:"size_bytes"`
	TargetPlatform string `json:"target_platform,omitempty"`
	Destination    string `json:"destination"`
}

type ImageBuildSource struct {
	Bucket     string
	Object     string
	Generation string
}

type ImageBuildRequest struct {
	ExecutionJobID string
	Source         ImageBuildSource
	Spec           RemoteImageBuildSpec
	Timeout        time.Duration
	Artifacts      []ImageBuildArtifact
}

type ImageBuildHandle struct {
	ID           string
	ResourceName string
}

type ImageBuildResult struct {
	Status         ImageBuildStatus
	ImageDigest    string
	ExternalLogURL string
	FailureDetail  string
	Timing         *ExecutionTiming
}

type ExternalImageBuild struct {
	ExecutionJobID       string
	Provider             ImageBuildProvider
	SubmissionState      ExternalImageBuildSubmissionState
	ExternalBuildID      string
	ExternalResourceName string
	Source               ImageBuildSource
	ConsumedArtifacts    []ImageBuildArtifact
	TargetImageReference string
	SubmittedAt          *time.Time
	LastProviderStatus   string
	TerminalResult       string
	ImageDigest          string
	ExternalLogURL       string
	FailureDetail        string
	CreatedAt            time.Time
	UpdatedAt            time.Time
}
