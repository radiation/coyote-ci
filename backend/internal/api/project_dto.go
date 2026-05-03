package api

type CreateProjectRequest struct {
	Name        string  `json:"name"`
	Slug        string  `json:"slug"`
	Description *string `json:"description,omitempty"`
}

type UpdateProjectRequest struct {
	Name        *string     `json:"name,omitempty"`
	Slug        *string     `json:"slug,omitempty"`
	Description StringPatch `json:"description,omitempty"`
}

type ProjectResponse struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Slug        string  `json:"slug"`
	Description *string `json:"description,omitempty"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

type ProjectListResponse struct {
	Projects []ProjectResponse `json:"projects"`
}

type ProjectEnvelope struct {
	Data ProjectResponse `json:"data"`
}

type ProjectListEnvelope struct {
	Data ProjectListResponse `json:"data"`
}
