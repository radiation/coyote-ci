package api

type CreateGitHubAppRegistrationRequest struct {
	AppID               string  `json:"app_id"`
	DisplayName         *string `json:"display_name,omitempty"`
	APIBaseURL          string  `json:"api_base_url,omitempty"`
	WebBaseURL          string  `json:"web_base_url,omitempty"`
	PrivateKeySecretRef string  `json:"private_key_secret_ref"`
	WebhookSecretRef    string  `json:"webhook_secret_ref"`
}

type CreateGitHubAppInstallationConnectionRequest struct {
	AppRegistrationID string `json:"app_registration_id"`
	DisplayName       string `json:"display_name"`
	Enabled           *bool  `json:"enabled"`
	InstallationID    string `json:"installation_id"`
	AccountLogin      string `json:"account_login"`
	AccountType       string `json:"account_type"`
	TargetID          string `json:"target_id"`
}

type PatchSCMConnectionRequest struct {
	Enabled *bool `json:"enabled"`
}

type SCMConnectionResponse struct {
	ID                  string                         `json:"id"`
	Provider            string                         `json:"provider"`
	DisplayName         string                         `json:"display_name"`
	DeploymentKind      string                         `json:"deployment_kind"`
	APIBaseURL          string                         `json:"api_base_url"`
	WebBaseURL          string                         `json:"web_base_url"`
	Enabled             bool                           `json:"enabled"`
	HealthStatus        string                         `json:"health_status"`
	HealthSummary       *string                        `json:"health_summary,omitempty"`
	LastHealthCheckedAt *string                        `json:"last_health_checked_at,omitempty"`
	GitHubApp           *GitHubAppRegistrationResponse `json:"github_app,omitempty"`
	GitHubInstallation  *GitHubAppInstallationResponse `json:"github_installation,omitempty"`
	CreatedAt           string                         `json:"created_at"`
	UpdatedAt           string                         `json:"updated_at"`
}

type GitHubAppRegistrationResponse struct {
	ID                   string  `json:"id"`
	AppID                string  `json:"app_id"`
	DisplayName          *string `json:"display_name,omitempty"`
	APIBaseURL           string  `json:"api_base_url"`
	WebBaseURL           string  `json:"web_base_url"`
	PrivateKeyConfigured bool    `json:"private_key_configured"`
	WebhookConfigured    bool    `json:"webhook_configured"`
	CreatedAt            string  `json:"created_at"`
	UpdatedAt            string  `json:"updated_at"`
}

type GitHubAppInstallationResponse struct {
	ConnectionID      string `json:"connection_id"`
	AppRegistrationID string `json:"app_registration_id"`
	InstallationID    string `json:"installation_id"`
	AccountLogin      string `json:"account_login"`
	AccountType       string `json:"account_type"`
	TargetID          string `json:"target_id"`
	CreatedAt         string `json:"created_at"`
	UpdatedAt         string `json:"updated_at"`
}

type GitHubAppRegistrationEnvelope struct {
	Data GitHubAppRegistrationResponse `json:"data"`
}

type GitHubAppRegistrationListResponse struct {
	GitHubApps []GitHubAppRegistrationResponse `json:"github_apps"`
}

type GitHubAppRegistrationListEnvelope struct {
	Data GitHubAppRegistrationListResponse `json:"data"`
}

type SCMConnectionListResponse struct {
	Connections []SCMConnectionResponse `json:"connections"`
}

type SCMConnectionEnvelope struct {
	Data SCMConnectionResponse `json:"data"`
}

type SCMConnectionListEnvelope struct {
	Data SCMConnectionListResponse `json:"data"`
}

type CreateSCMRepositoryRegistrationRequest struct {
	ConnectionID         string `json:"connection_id"`
	ProviderRepositoryID string `json:"provider_repository_id,omitempty"`
	Owner                string `json:"owner,omitempty"`
	Name                 string `json:"name,omitempty"`
}

type SCMRepositoryRegistrationResponse struct {
	ID                   string  `json:"id"`
	ConnectionID         string  `json:"connection_id"`
	ProviderRepositoryID string  `json:"provider_repository_id"`
	Owner                string  `json:"owner"`
	Name                 string  `json:"name"`
	FullName             string  `json:"full_name"`
	CloneURL             string  `json:"clone_url"`
	WebURL               string  `json:"web_url"`
	DefaultBranch        *string `json:"default_branch,omitempty"`
	Archived             bool    `json:"archived"`
	Disabled             bool    `json:"disabled"`
	MetadataRefreshedAt  string  `json:"metadata_refreshed_at"`
	CreatedAt            string  `json:"created_at"`
	UpdatedAt            string  `json:"updated_at"`
}

type SCMRepositoryRegistrationListResponse struct {
	Repositories []SCMRepositoryRegistrationResponse `json:"repositories"`
}

type SCMRepositoryRegistrationEnvelope struct {
	Data SCMRepositoryRegistrationResponse `json:"data"`
}

type SCMRepositoryRegistrationListEnvelope struct {
	Data SCMRepositoryRegistrationListResponse `json:"data"`
}
