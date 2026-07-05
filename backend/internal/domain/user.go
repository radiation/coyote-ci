package domain

import "time"

type GlobalRole string

const (
	GlobalRoleAdmin GlobalRole = "admin"
	GlobalRoleUser  GlobalRole = "user"
)

type ProjectMemberRole string

const (
	ProjectMemberRoleOwner      ProjectMemberRole = "owner"
	ProjectMemberRoleMaintainer ProjectMemberRole = "maintainer"
	ProjectMemberRoleViewer     ProjectMemberRole = "viewer"
)

type User struct {
	ID          string
	Email       string
	DisplayName *string
	GlobalRole  GlobalRole
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type APIToken struct {
	ID          string
	UserID      string
	Name        string
	Scopes      []APITokenScope
	TokenHash   string
	TokenPrefix string
	ExpiresAt   *time.Time
	LastUsedAt  *time.Time
	RevokedAt   *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type ProjectMembership struct {
	ProjectID string
	UserID    string
	Role      ProjectMemberRole
	CreatedAt time.Time
	UpdatedAt time.Time
}

type ProjectMembershipWithUser struct {
	ProjectMembership
	Email       string
	DisplayName *string
}
