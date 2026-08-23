package api

type PublicProjectResponse struct {
	ID          string  `json:"id"`
	Slug        string  `json:"slug"`
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
}

type PublicProjectListResponse struct {
	Projects []PublicProjectResponse `json:"projects"`
}

type PublicProjectEnvelope struct {
	Data PublicProjectResponse `json:"data"`
}

type PublicProjectListEnvelope struct {
	Data PublicProjectListResponse `json:"data"`
}

type PublicBuildResponse struct {
	ID          string                    `json:"id"`
	Number      int64                     `json:"number"`
	Status      string                    `json:"status"`
	JobName     *string                   `json:"job_name,omitempty"`
	Attempt     int                       `json:"attempt"`
	CreatedAt   string                    `json:"created_at"`
	StartedAt   *string                   `json:"started_at,omitempty"`
	CompletedAt *string                   `json:"completed_at,omitempty"`
	Steps       []PublicBuildStepResponse `json:"steps,omitempty"`
}

type PublicBuildStepResponse struct {
	Index       int     `json:"index"`
	Name        string  `json:"name"`
	Status      string  `json:"status"`
	StartedAt   *string `json:"started_at,omitempty"`
	CompletedAt *string `json:"completed_at,omitempty"`
}

type PublicBuildListResponse struct {
	Builds []PublicBuildResponse `json:"builds"`
}

type PublicBuildEnvelope struct {
	Data PublicBuildResponse `json:"data"`
}

type PublicBuildListEnvelope struct {
	Data PublicBuildListResponse `json:"data"`
}
