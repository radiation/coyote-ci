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
	AuthMode   string       `json:"auth_mode"`
	AuthMethod string       `json:"auth_method,omitempty"`
	User       UserResponse `json:"user"`
}

type CreateAPITokenRequest struct {
	Name      string  `json:"name"`
	ExpiresAt *string `json:"expires_at,omitempty"`
}

type APITokenResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	TokenPrefix string `json:"token_prefix"`
	ExpiresAt   string `json:"expires_at,omitempty"`
	LastUsedAt  string `json:"last_used_at,omitempty"`
	CreatedAt   string `json:"created_at"`
	RevokedAt   string `json:"revoked_at,omitempty"`
}

type APITokenListResponse struct {
	Tokens []APITokenResponse `json:"tokens"`
}

type CreatedAPITokenResponse struct {
	APITokenResponse
	Token string `json:"token"`
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

type APITokenEnvelope struct {
	Data CreatedAPITokenResponse `json:"data"`
}

type APITokenListEnvelope struct {
	Data APITokenListResponse `json:"data"`
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
