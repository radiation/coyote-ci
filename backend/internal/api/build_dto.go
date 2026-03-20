package api

type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}

type DataResponse struct {
	Data any `json:"data"`
}

type BuildResponse struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

type BuildListResponse struct {
	Builds []BuildResponse `json:"builds"`
}

type BuildStepResponse struct {
	Name      string  `json:"name"`
	Status    string  `json:"status"`
	StartedAt *string `json:"started_at"`
	EndedAt   *string `json:"ended_at"`
}

type BuildStepsResponse struct {
	BuildID string              `json:"build_id"`
	Steps   []BuildStepResponse `json:"steps"`
}

type BuildLogResponse struct {
	Timestamp string `json:"timestamp"`
	Message   string `json:"message"`
}

type BuildLogsResponse struct {
	BuildID string             `json:"build_id"`
	Logs    []BuildLogResponse `json:"logs"`
}
