package api

type ArtifactBrowseEnvelope struct {
	Data ArtifactBrowseResponse `json:"data"`
}

type ArtifactBrowseResponse struct {
	Artifacts []ArtifactBrowseItemResponse `json:"artifacts"`
}

type ArtifactBrowseItemResponse struct {
	Key             string                          `json:"key"`
	Path            string                          `json:"path"`
	ProjectID       string                          `json:"project_id"`
	JobID           *string                         `json:"job_id,omitempty"`
	ArtifactType    string                          `json:"artifact_type"`
	LatestCreatedAt string                          `json:"latest_created_at"`
	Versions        []ArtifactBrowseVersionResponse `json:"versions"`
}

type ArtifactBrowseVersionResponse struct {
	ArtifactID      string               `json:"artifact_id"`
	BuildID         string               `json:"build_id"`
	BuildNumber     int64                `json:"build_number"`
	BuildStatus     string               `json:"build_status"`
	ProjectID       string               `json:"project_id"`
	JobID           *string              `json:"job_id,omitempty"`
	StepID          *string              `json:"step_id,omitempty"`
	StepIndex       *int                 `json:"step_index,omitempty"`
	StepName        *string              `json:"step_name,omitempty"`
	Path            string               `json:"path"`
	SizeBytes       int64                `json:"size_bytes"`
	ContentType     *string              `json:"content_type"`
	ChecksumSHA256  *string              `json:"checksum_sha256"`
	StorageProvider string               `json:"storage_provider"`
	DownloadURLPath string               `json:"download_url_path"`
	VersionTags     []VersionTagResponse `json:"version_tags,omitempty"`
	CreatedAt       string               `json:"created_at"`
}
