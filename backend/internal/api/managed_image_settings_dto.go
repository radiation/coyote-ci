package api

type CreateSourceCredentialRequest struct {
	ProjectID string  `json:"project_id"`
	Name      string  `json:"name"`
	Kind      string  `json:"kind"`
	Username  *string `json:"username,omitempty"`
	SecretRef string  `json:"secret_ref"`
}

type UpdateSourceCredentialRequest struct {
	Name      *string  `json:"name,omitempty"`
	Kind      *string  `json:"kind,omitempty"`
	Username  **string `json:"username,omitempty"`
	SecretRef *string  `json:"secret_ref,omitempty"`
}

type SourceCredentialResponse struct {
	ID        string  `json:"id"`
	ProjectID string  `json:"project_id"`
	Name      string  `json:"name"`
	Kind      string  `json:"kind"`
	Username  *string `json:"username,omitempty"`
	SecretRef string  `json:"secret_ref"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
}

type SourceCredentialListResponse struct {
	Credentials []SourceCredentialResponse `json:"credentials"`
}

type SourceCredentialEnvelope struct {
	Data SourceCredentialResponse `json:"data"`
}

type SourceCredentialListEnvelope struct {
	Data SourceCredentialListResponse `json:"data"`
}

type CreateRepoWritebackConfigRequest struct {
	ProjectID         string  `json:"project_id"`
	RepositoryURL     string  `json:"repository_url"`
	PipelinePath      string  `json:"pipeline_path"`
	ManagedImageName  string  `json:"managed_image_name"`
	WriteCredentialID string  `json:"write_credential_id"`
	BotBranchPrefix   *string `json:"bot_branch_prefix,omitempty"`
	CommitAuthorName  *string `json:"commit_author_name,omitempty"`
	CommitAuthorEmail *string `json:"commit_author_email,omitempty"`
	Enabled           *bool   `json:"enabled,omitempty"`
}

type UpdateRepoWritebackConfigRequest struct {
	RepositoryURL     *string `json:"repository_url,omitempty"`
	PipelinePath      *string `json:"pipeline_path,omitempty"`
	ManagedImageName  *string `json:"managed_image_name,omitempty"`
	WriteCredentialID *string `json:"write_credential_id,omitempty"`
	BotBranchPrefix   *string `json:"bot_branch_prefix,omitempty"`
	CommitAuthorName  *string `json:"commit_author_name,omitempty"`
	CommitAuthorEmail *string `json:"commit_author_email,omitempty"`
	Enabled           *bool   `json:"enabled,omitempty"`
}

type RepoWritebackConfigResponse struct {
	ID                string `json:"id"`
	ProjectID         string `json:"project_id"`
	RepositoryURL     string `json:"repository_url"`
	PipelinePath      string `json:"pipeline_path"`
	ManagedImageName  string `json:"managed_image_name"`
	WriteCredentialID string `json:"write_credential_id"`
	BotBranchPrefix   string `json:"bot_branch_prefix"`
	CommitAuthorName  string `json:"commit_author_name"`
	CommitAuthorEmail string `json:"commit_author_email"`
	Enabled           bool   `json:"enabled"`
	CreatedAt         string `json:"created_at"`
	UpdatedAt         string `json:"updated_at"`
}

type RepoWritebackConfigListResponse struct {
	Configs []RepoWritebackConfigResponse `json:"configs"`
}

type RepoWritebackConfigEnvelope struct {
	Data RepoWritebackConfigResponse `json:"data"`
}

type RepoWritebackConfigListEnvelope struct {
	Data RepoWritebackConfigListResponse `json:"data"`
}
