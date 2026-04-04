package api

type PushEventRequest struct {
	RepositoryURL string `json:"repository_url"`
	Ref           string `json:"ref"`
	CommitSHA     string `json:"commit_sha"`
}

type PushEventMatchedJob struct {
	JobID       string `json:"job_id"`
	JobName     string `json:"job_name"`
	BuildID     string `json:"build_id"`
	BuildStatus string `json:"build_status"`
}

type PushEventResponse struct {
	RepositoryURL string                `json:"repository_url"`
	Ref           string                `json:"ref"`
	CommitSHA     string                `json:"commit_sha"`
	Duplicate     bool                  `json:"duplicate,omitempty"`
	MatchedJobs   int                   `json:"matched_jobs"`
	CreatedBuilds int                   `json:"created_builds"`
	Builds        []PushEventMatchedJob `json:"builds"`
}

type PushEventEnvelope struct {
	Data PushEventResponse `json:"data"`
}
