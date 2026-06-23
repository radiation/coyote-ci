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
	ID                string `json:"id"`
	Type              string `json:"type"`
	Name              string `json:"name"`
	Address           string `json:"address,omitempty"`
	WebhookConfigured bool   `json:"webhook_configured"`
	Enabled           bool   `json:"enabled"`
	CreatedAt         string `json:"created_at"`
	UpdatedAt         string `json:"updated_at"`
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
