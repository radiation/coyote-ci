package api

type CreateUserRequest struct {
	Email       string  `json:"email"`
	DisplayName *string `json:"display_name,omitempty"`
	GlobalRole  string  `json:"global_role,omitempty"`
}

type UpdateUserRequest struct {
	Email       *string     `json:"email,omitempty"`
	DisplayName StringPatch `json:"display_name,omitempty"`
	GlobalRole  *string     `json:"global_role,omitempty"`
}

type UserResponse struct {
	ID          string  `json:"id"`
	Email       string  `json:"email"`
	DisplayName *string `json:"display_name,omitempty"`
	GlobalRole  string  `json:"global_role"`
	CreatedAt   string  `json:"created_at,omitempty"`
	UpdatedAt   string  `json:"updated_at,omitempty"`
}

type UserListResponse struct {
	Users []UserResponse `json:"users"`
}

type MeResponse struct {
	AuthMode string       `json:"auth_mode"`
	User     UserResponse `json:"user"`
}

type AuthConfigResponse struct {
	AuthMode string  `json:"auth_mode"`
	LoginURL *string `json:"login_url"`
}

type UserEnvelope struct {
	Data UserResponse `json:"data"`
}

type UserListEnvelope struct {
	Data UserListResponse `json:"data"`
}

type MeEnvelope struct {
	Data MeResponse `json:"data"`
}

type AuthConfigEnvelope struct {
	Data AuthConfigResponse `json:"data"`
}

type UpsertProjectMembershipRequest struct {
	Role string `json:"role"`
}

type ProjectMembershipResponse struct {
	ProjectID   string  `json:"project_id"`
	UserID      string  `json:"user_id"`
	Email       string  `json:"email,omitempty"`
	DisplayName *string `json:"display_name,omitempty"`
	Role        string  `json:"role"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

type ProjectMembershipListResponse struct {
	Members []ProjectMembershipResponse `json:"members"`
}

type ProjectMembershipEnvelope struct {
	Data ProjectMembershipResponse `json:"data"`
}

type ProjectMembershipListEnvelope struct {
	Data ProjectMembershipListResponse `json:"data"`
}
