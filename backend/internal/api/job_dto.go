package api

import (
	"bytes"
	"encoding/json"
)

type CreateJobRequest struct {
	ProjectID        string                              `json:"project_id"`
	Name             string                              `json:"name"`
	RepositoryURL    string                              `json:"repository_url"`
	DefaultRef       string                              `json:"default_ref,omitempty"`
	DefaultCommitSHA string                              `json:"default_commit_sha,omitempty"`
	PushEnabled      *bool                               `json:"push_enabled,omitempty"`
	PushBranch       *string                             `json:"push_branch,omitempty"`
	TriggerMode      *string                             `json:"trigger_mode,omitempty"`
	BranchAllowlist  []string                            `json:"branch_allowlist,omitempty"`
	TagAllowlist     []string                            `json:"tag_allowlist,omitempty"`
	PipelineYAML     string                              `json:"pipeline_yaml,omitempty"`
	PipelinePath     string                              `json:"pipeline_path,omitempty"`
	ManagedImage     *CreateJobManagedImageConfigRequest `json:"managed_image,omitempty"`
	Enabled          *bool                               `json:"enabled,omitempty"`
}

type UpdateJobRequest struct {
	Name             *string                          `json:"name,omitempty"`
	RepositoryURL    *string                          `json:"repository_url,omitempty"`
	DefaultRef       *string                          `json:"default_ref,omitempty"`
	DefaultCommitSHA *string                          `json:"default_commit_sha,omitempty"`
	PushEnabled      *bool                            `json:"push_enabled,omitempty"`
	PushBranch       *string                          `json:"push_branch,omitempty"`
	TriggerMode      *string                          `json:"trigger_mode,omitempty"`
	BranchAllowlist  *[]string                        `json:"branch_allowlist,omitempty"`
	TagAllowlist     *[]string                        `json:"tag_allowlist,omitempty"`
	PipelineYAML     *string                          `json:"pipeline_yaml,omitempty"`
	PipelinePath     *string                          `json:"pipeline_path,omitempty"`
	ManagedImage     UpdateJobManagedImageConfigField `json:"managed_image,omitempty"`
	Enabled          *bool                            `json:"enabled,omitempty"`
}

type UpdateJobManagedImageConfigField struct {
	Set   bool
	Value *UpdateJobManagedImageConfigRequest
}

func (f *UpdateJobManagedImageConfigField) UnmarshalJSON(data []byte) error {
	f.Set = true
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		f.Value = nil
		return nil
	}

	var value UpdateJobManagedImageConfigRequest
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	f.Value = &value
	return nil
}

func (f UpdateJobManagedImageConfigField) Present() bool {
	return f.Set
}

func (f UpdateJobManagedImageConfigField) Request() *UpdateJobManagedImageConfigRequest {
	return f.Value
}

type CreateJobManagedImageConfigRequest struct {
	Enabled           bool    `json:"enabled"`
	ManagedImageName  string  `json:"managed_image_name"`
	PipelinePath      string  `json:"pipeline_path"`
	WriteCredentialID string  `json:"write_credential_id"`
	BotBranchPrefix   *string `json:"bot_branch_prefix,omitempty"`
	CommitAuthorName  *string `json:"commit_author_name,omitempty"`
	CommitAuthorEmail *string `json:"commit_author_email,omitempty"`
}

type UpdateJobManagedImageConfigRequest struct {
	Enabled           *bool   `json:"enabled,omitempty"`
	ManagedImageName  *string `json:"managed_image_name,omitempty"`
	PipelinePath      *string `json:"pipeline_path,omitempty"`
	WriteCredentialID *string `json:"write_credential_id,omitempty"`
	BotBranchPrefix   *string `json:"bot_branch_prefix,omitempty"`
	CommitAuthorName  *string `json:"commit_author_name,omitempty"`
	CommitAuthorEmail *string `json:"commit_author_email,omitempty"`
}

type JobManagedImageConfigResponse struct {
	Enabled           bool   `json:"enabled"`
	ManagedImageName  string `json:"managed_image_name"`
	PipelinePath      string `json:"pipeline_path"`
	WriteCredentialID string `json:"write_credential_id"`
	BotBranchPrefix   string `json:"bot_branch_prefix"`
	CommitAuthorName  string `json:"commit_author_name"`
	CommitAuthorEmail string `json:"commit_author_email"`
	CreatedAt         string `json:"created_at"`
	UpdatedAt         string `json:"updated_at"`
}

type JobResponse struct {
	ID               string                         `json:"id"`
	ProjectID        string                         `json:"project_id"`
	Name             string                         `json:"name"`
	RepositoryURL    string                         `json:"repository_url"`
	DefaultRef       string                         `json:"default_ref"`
	DefaultCommitSHA *string                        `json:"default_commit_sha,omitempty"`
	PushEnabled      bool                           `json:"push_enabled"`
	PushBranch       *string                        `json:"push_branch,omitempty"`
	TriggerMode      string                         `json:"trigger_mode"`
	BranchAllowlist  []string                       `json:"branch_allowlist,omitempty"`
	TagAllowlist     []string                       `json:"tag_allowlist,omitempty"`
	PipelineYAML     string                         `json:"pipeline_yaml"`
	PipelinePath     *string                        `json:"pipeline_path,omitempty"`
	ManagedImage     *JobManagedImageConfigResponse `json:"managed_image,omitempty"`
	Enabled          bool                           `json:"enabled"`
	CreatedAt        string                         `json:"created_at"`
	UpdatedAt        string                         `json:"updated_at"`
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
