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
	ContextPath          string            `json:"context_path"`
	DockerfilePath       string            `json:"dockerfile_path"`
	BuildArgs            map[string]string `json:"build_args,omitempty"`
	TargetImageReference string            `json:"target_image_reference"`
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
}

type ExternalImageBuild struct {
	ExecutionJobID       string
	Provider             ImageBuildProvider
	SubmissionState      ExternalImageBuildSubmissionState
	ExternalBuildID      string
	ExternalResourceName string
	Source               ImageBuildSource
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
