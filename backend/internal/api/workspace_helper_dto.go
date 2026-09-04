package api

type WorkspaceHelperCapabilityExchangeRequest struct {
	ExecutionJobID string `json:"execution_job_id"`
	PodUID         string `json:"pod_uid"`
	Role           string `json:"role"`
}

type WorkspaceHelperCapabilityExchangeResponse struct {
	Capability string `json:"capability"`
	ExpiresAt  string `json:"expires_at"`
}

type WorkspaceHelperPrepareRequest struct {
	ExecutionJobID string `json:"execution_job_id"`
	PodUID         string `json:"pod_uid"`
}

type WorkspaceHelperPublishResponse struct {
	RevisionID    string `json:"revision_id"`
	ContentDigest string `json:"content_digest"`
	SizeBytes     int64  `json:"size_bytes"`
}
