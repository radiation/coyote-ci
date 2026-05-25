package api

type WorkerResponse struct {
	ID               string  `json:"id"`
	Name             string  `json:"name"`
	Status           string  `json:"status"`
	LastHeartbeatAt  string  `json:"last_heartbeat_at"`
	CreatedAt        string  `json:"created_at"`
	UpdatedAt        string  `json:"updated_at"`
	CurrentBuildID   *string `json:"current_build_id,omitempty"`
	CurrentBuildNum  *int64  `json:"current_build_number,omitempty"`
	CurrentStepID    *string `json:"current_step_id,omitempty"`
	CurrentStepIndex *int    `json:"current_step_index,omitempty"`
	CurrentStepName  *string `json:"current_step_name,omitempty"`
	LeaseExpiresAt   *string `json:"lease_expires_at,omitempty"`
	ClaimedAt        *string `json:"claimed_at,omitempty"`
	ProjectID        *string `json:"project_id,omitempty"`
	ProjectName      *string `json:"project_name,omitempty"`
	ProjectSlug      *string `json:"project_slug,omitempty"`
	JobID            *string `json:"job_id,omitempty"`
	JobName          *string `json:"job_name,omitempty"`
	StaleLease       bool    `json:"stale_lease"`
	StaleHeartbeat   bool    `json:"stale_heartbeat"`
}

type WorkerListResponse struct {
	Workers []WorkerResponse `json:"workers"`
}
