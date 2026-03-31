package api

type CreateJobRequest struct {
	ProjectID     string  `json:"project_id"`
	Name          string  `json:"name"`
	RepositoryURL string  `json:"repository_url"`
	DefaultRef    string  `json:"default_ref"`
	PushEnabled   *bool   `json:"push_enabled,omitempty"`
	PushBranch    *string `json:"push_branch,omitempty"`
	PipelineYAML  string  `json:"pipeline_yaml"`
	Enabled       *bool   `json:"enabled,omitempty"`
}

type UpdateJobRequest struct {
	Name          *string `json:"name,omitempty"`
	RepositoryURL *string `json:"repository_url,omitempty"`
	DefaultRef    *string `json:"default_ref,omitempty"`
	PushEnabled   *bool   `json:"push_enabled,omitempty"`
	PushBranch    *string `json:"push_branch,omitempty"`
	PipelineYAML  *string `json:"pipeline_yaml,omitempty"`
	Enabled       *bool   `json:"enabled,omitempty"`
}

type JobResponse struct {
	ID            string  `json:"id"`
	ProjectID     string  `json:"project_id"`
	Name          string  `json:"name"`
	RepositoryURL string  `json:"repository_url"`
	DefaultRef    string  `json:"default_ref"`
	PushEnabled   bool    `json:"push_enabled"`
	PushBranch    *string `json:"push_branch,omitempty"`
	PipelineYAML  string  `json:"pipeline_yaml"`
	Enabled       bool    `json:"enabled"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
}

type JobListResponse struct {
	Jobs []JobResponse `json:"jobs"`
}

type JobEnvelope struct {
	Data JobResponse `json:"data"`
}

type JobListEnvelope struct {
	Data JobListResponse `json:"data"`
}
