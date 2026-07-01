package api

type CreateNotificationTargetRequest struct {
	Type       string `json:"type,omitempty"`
	Name       string `json:"name"`
	Address    string `json:"address,omitempty"`
	WebhookURL string `json:"webhook_url,omitempty"`
	Enabled    *bool  `json:"enabled,omitempty"`
}

type UpdateNotificationTargetRequest struct {
	Name       *string `json:"name,omitempty"`
	Address    *string `json:"address,omitempty"`
	WebhookURL *string `json:"webhook_url,omitempty"`
	Enabled    *bool   `json:"enabled,omitempty"`
}

type NotificationTargetResponse struct {
	ID                string  `json:"id"`
	OwnerUserID       *string `json:"owner_user_id,omitempty"`
	Type              string  `json:"type"`
	Name              string  `json:"name"`
	Address           string  `json:"address,omitempty"`
	WebhookConfigured bool    `json:"webhook_configured"`
	Enabled           bool    `json:"enabled"`
	CreatedAt         string  `json:"created_at"`
	UpdatedAt         string  `json:"updated_at"`
}

type NotificationTargetListResponse struct {
	Targets []NotificationTargetResponse `json:"targets"`
}

type NotificationTargetEnvelope struct {
	Data NotificationTargetResponse `json:"data"`
}

type NotificationTargetListEnvelope struct {
	Data NotificationTargetListResponse `json:"data"`
}

type MyEmailNotificationTargetResponse struct {
	Target *NotificationTargetResponse `json:"target"`
}

type MyEmailNotificationTargetEnvelope struct {
	Data MyEmailNotificationTargetResponse `json:"data"`
}

type PutMyEmailNotificationTargetRequest struct {
	Enabled *bool `json:"enabled"`
}

type CommitAuthorFailureNotificationPreferenceResponse struct {
	Enabled           bool                        `json:"enabled"`
	Eligible          bool                        `json:"eligible"`
	DeliveryActive    bool                        `json:"delivery_active"`
	Target            *NotificationTargetResponse `json:"target"`
	UnavailableReason *string                     `json:"unavailable_reason,omitempty"`
}

type CommitAuthorFailureNotificationPreferenceEnvelope struct {
	Data CommitAuthorFailureNotificationPreferenceResponse `json:"data"`
}

type CommitAuthorSuccessNotificationPreferenceResponse struct {
	Enabled           bool                        `json:"enabled"`
	Eligible          bool                        `json:"eligible"`
	DeliveryActive    bool                        `json:"delivery_active"`
	Target            *NotificationTargetResponse `json:"target"`
	UnavailableReason *string                     `json:"unavailable_reason,omitempty"`
}

type CommitAuthorSuccessNotificationPreferenceEnvelope struct {
	Data CommitAuthorSuccessNotificationPreferenceResponse `json:"data"`
}

type NotificationDefaultsResponse struct {
	DefaultCommitAuthorFailureEmailEnabled bool `json:"default_commit_author_failure_email_enabled"`
	DefaultCommitAuthorSuccessEmailEnabled bool `json:"default_commit_author_success_email_enabled"`
}

type NotificationDefaultsEnvelope struct {
	Data NotificationDefaultsResponse `json:"data"`
}

type SlackWorkspaceIntegrationResponse struct {
	ID                string  `json:"id"`
	WorkspaceID       string  `json:"workspace_id"`
	WorkspaceName     *string `json:"workspace_name,omitempty"`
	WorkspaceURL      *string `json:"workspace_url,omitempty"`
	BotUserID         *string `json:"bot_user_id,omitempty"`
	AuthedUserID      *string `json:"authed_user_id,omitempty"`
	AppID             *string `json:"app_id,omitempty"`
	Enabled           bool    `json:"enabled"`
	ConnectedAt       string  `json:"connected_at"`
	LastTestedAt      *string `json:"last_tested_at,omitempty"`
	LastTestSucceeded *bool   `json:"last_test_succeeded,omitempty"`
	UpdatedAt         string  `json:"updated_at"`
}

type SlackWorkspaceIntegrationStatusResponse struct {
	Configured  bool                               `json:"configured"`
	Integration *SlackWorkspaceIntegrationResponse `json:"integration,omitempty"`
}

type SlackWorkspaceIntegrationStatusEnvelope struct {
	Data SlackWorkspaceIntegrationStatusResponse `json:"data"`
}

type PutSlackWorkspaceIntegrationRequest struct {
	BotToken        string `json:"bot_token"`
	ReplaceExisting *bool  `json:"replace_existing,omitempty"`
}

type PatchSlackWorkspaceIntegrationRequest struct {
	Enabled *bool `json:"enabled"`
}

type PutNotificationDefaultsRequest struct {
	DefaultCommitAuthorFailureEmailEnabled *bool `json:"default_commit_author_failure_email_enabled"`
	DefaultCommitAuthorSuccessEmailEnabled *bool `json:"default_commit_author_success_email_enabled"`
}

type PutCommitAuthorFailureNotificationPreferenceRequest struct {
	Enabled *bool `json:"enabled"`
}

type PutCommitAuthorSuccessNotificationPreferenceRequest struct {
	Enabled *bool `json:"enabled"`
}

type CreateNotificationSubscriptionRequest struct {
	TargetID  string  `json:"target_id"`
	ProjectID *string `json:"project_id,omitempty"`
	JobID     *string `json:"job_id,omitempty"`
	EventType string  `json:"event_type"`
	Enabled   *bool   `json:"enabled,omitempty"`
}

type UpdateNotificationSubscriptionRequest struct {
	Enabled *bool `json:"enabled,omitempty"`
}

type NotificationSubscriptionResponse struct {
	ID        string  `json:"id"`
	TargetID  string  `json:"target_id"`
	ProjectID *string `json:"project_id,omitempty"`
	JobID     *string `json:"job_id,omitempty"`
	EventType string  `json:"event_type"`
	Enabled   bool    `json:"enabled"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
}

type NotificationSubscriptionListResponse struct {
	Subscriptions []NotificationSubscriptionResponse `json:"subscriptions"`
}

type NotificationSubscriptionEnvelope struct {
	Data NotificationSubscriptionResponse `json:"data"`
}

type NotificationSubscriptionListEnvelope struct {
	Data NotificationSubscriptionListResponse `json:"data"`
}
