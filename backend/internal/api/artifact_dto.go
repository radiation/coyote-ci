package api

type ArtifactBrowseEnvelope struct {
	Data ArtifactBrowseResponse `json:"data"`
}

type ArtifactCatalogEnvelope struct {
	Data ArtifactCatalogResponse `json:"data"`
}

type ArtifactDetailEnvelope struct {
	Data ArtifactDetailResponse `json:"data"`
}

type ArtifactBrowseResponse struct {
	Artifacts []ArtifactBrowseItemResponse `json:"artifacts"`
}

type ArtifactCatalogResponse struct {
	Artifacts []ArtifactCatalogItemResponse `json:"artifacts"`
}

type ArtifactBrowseItemResponse struct {
	Key             string                          `json:"key"`
	Name            string                          `json:"name,omitempty"`
	Path            string                          `json:"path"`
	ProjectID       string                          `json:"project_id"`
	ProjectName     *string                         `json:"project_name,omitempty"`
	ProjectSlug     *string                         `json:"project_slug,omitempty"`
	JobID           *string                         `json:"job_id,omitempty"`
	JobName         *string                         `json:"job_name,omitempty"`
	ArtifactType    string                          `json:"artifact_type"`
	LatestCreatedAt string                          `json:"latest_created_at"`
	Versions        []ArtifactBrowseVersionResponse `json:"versions"`
}

type ArtifactBrowseVersionResponse struct {
	ArtifactID      string               `json:"artifact_id"`
	Name            string               `json:"name,omitempty"`
	BuildID         string               `json:"build_id"`
	BuildNumber     int64                `json:"build_number"`
	BuildStatus     string               `json:"build_status"`
	ProjectID       string               `json:"project_id"`
	ProjectName     *string              `json:"project_name,omitempty"`
	ProjectSlug     *string              `json:"project_slug,omitempty"`
	JobID           *string              `json:"job_id,omitempty"`
	JobName         *string              `json:"job_name,omitempty"`
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
	Lineage         *ArtifactLineage     `json:"lineage,omitempty"`
	CreatedAt       string               `json:"created_at"`
}

type ArtifactCatalogItemResponse struct {
	ID              string  `json:"id"`
	Name            string  `json:"name,omitempty"`
	Path            string  `json:"path"`
	ArtifactType    string  `json:"artifact_type"`
	BuildID         string  `json:"build_id"`
	BuildNumber     int64   `json:"build_number"`
	BuildStatus     string  `json:"build_status"`
	ProjectID       string  `json:"project_id"`
	ProjectName     *string `json:"project_name,omitempty"`
	ProjectSlug     *string `json:"project_slug,omitempty"`
	JobID           *string `json:"job_id,omitempty"`
	JobName         *string `json:"job_name,omitempty"`
	StepID          *string `json:"step_id,omitempty"`
	StepIndex       *int    `json:"step_index,omitempty"`
	StepName        *string `json:"step_name,omitempty"`
	SizeBytes       int64   `json:"size_bytes"`
	ContentType     *string `json:"content_type"`
	ChecksumSHA256  *string `json:"checksum_sha256"`
	StorageProvider string  `json:"storage_provider"`
	DownloadURLPath string  `json:"download_url_path"`
	CreatedAt       string  `json:"created_at"`
}

type ArtifactDetailResponse struct {
	ID              string               `json:"id"`
	Name            string               `json:"name,omitempty"`
	Path            string               `json:"path"`
	ArtifactType    string               `json:"artifact_type"`
	BuildID         string               `json:"build_id"`
	BuildNumber     int64                `json:"build_number"`
	BuildStatus     string               `json:"build_status"`
	ProjectID       string               `json:"project_id"`
	ProjectName     *string              `json:"project_name,omitempty"`
	ProjectSlug     *string              `json:"project_slug,omitempty"`
	JobID           *string              `json:"job_id,omitempty"`
	JobName         *string              `json:"job_name,omitempty"`
	StepID          *string              `json:"step_id,omitempty"`
	StepIndex       *int                 `json:"step_index,omitempty"`
	StepName        *string              `json:"step_name,omitempty"`
	SizeBytes       int64                `json:"size_bytes"`
	ContentType     *string              `json:"content_type"`
	ChecksumSHA256  *string              `json:"checksum_sha256"`
	StorageProvider string               `json:"storage_provider"`
	DownloadURLPath string               `json:"download_url_path"`
	VersionTags     []VersionTagResponse `json:"version_tags,omitempty"`
	Lineage         *ArtifactLineage     `json:"lineage,omitempty"`
	CreatedAt       string               `json:"created_at"`
}

type ArtifactLineage struct {
	ProjectID    string   `json:"project_id"`
	ProjectName  *string  `json:"project_name,omitempty"`
	JobID        *string  `json:"job_id,omitempty"`
	JobName      *string  `json:"job_name,omitempty"`
	BuildID      string   `json:"build_id"`
	BuildNumber  int64    `json:"build_number"`
	ArtifactID   string   `json:"artifact_id"`
	ArtifactName string   `json:"artifact_name,omitempty"`
	ArtifactPath string   `json:"artifact_path"`
	Versions     []string `json:"versions,omitempty"`
	Channels     []string `json:"channels,omitempty"`
	GitRef       *string  `json:"git_ref,omitempty"`
	GitSHA       *string  `json:"git_sha,omitempty"`
	CreatedAt    string   `json:"created_at"`
}
