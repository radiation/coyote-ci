package api

type VersionTagResponse struct {
	ID                    string  `json:"id"`
	JobID                 string  `json:"job_id"`
	Version               string  `json:"version"`
	TargetType            string  `json:"target_type"`
	ArtifactID            *string `json:"artifact_id,omitempty"`
	ManagedImageVersionID *string `json:"managed_image_version_id,omitempty"`
	CreatedAt             string  `json:"created_at"`
}

type VersionTagCreateRequest struct {
	Version                string   `json:"version"`
	ArtifactIDs            []string `json:"artifact_ids,omitempty"`
	ManagedImageVersionIDs []string `json:"managed_image_version_ids,omitempty"`
}

type ArtifactVersionTagsResponse struct {
	ArtifactID string               `json:"artifact_id"`
	Tags       []VersionTagResponse `json:"tags"`
}

type ManagedImageVersionTagsResponse struct {
	ManagedImageVersionID string               `json:"managed_image_version_id"`
	Tags                  []VersionTagResponse `json:"tags"`
}

type JobVersionTagsResponse struct {
	JobID   string               `json:"job_id"`
	Version string               `json:"version"`
	Tags    []VersionTagResponse `json:"tags"`
}
