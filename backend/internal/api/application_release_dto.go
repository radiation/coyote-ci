package api

type ApplicationReleaseOutputResponse struct {
	ID                 string  `json:"id"`
	Kind               string  `json:"kind"`
	Key                string  `json:"key"`
	Name               string  `json:"name"`
	Status             string  `json:"status"`
	ImmutableReference string  `json:"immutable_reference,omitempty"`
	FailureReason      string  `json:"failure_reason,omitempty"`
	ArtifactID         *string `json:"artifact_id,omitempty"`
	ExecutionJobID     *string `json:"execution_job_id,omitempty"`
}

type ApplicationReleaseResponse struct {
	ID                 string                             `json:"id"`
	BuildID            string                             `json:"build_id"`
	ProjectID          string                             `json:"project_id"`
	JobID              *string                            `json:"job_id,omitempty"`
	ApplicationVersion string                             `json:"application_version"`
	Status             string                             `json:"status"`
	AutoChannel        string                             `json:"auto_channel,omitempty"`
	FailureReason      string                             `json:"failure_reason,omitempty"`
	Outputs            []ApplicationReleaseOutputResponse `json:"outputs"`
}

type ApplicationReleaseEnvelope struct {
	Data ApplicationReleaseResponse `json:"data"`
}
