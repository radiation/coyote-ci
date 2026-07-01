package domain

import "time"

type UserSlackIdentity struct {
	ID                          string
	UserID                      string
	SlackWorkspaceIntegrationID string
	SlackUserID                 string
	SlackDisplayName            *string
	SlackRealName               *string
	SlackHandle                 *string
	SlackEmail                  *string
	ProfileImageURL             *string
	Enabled                     bool
	LinkedAt                    time.Time
	LastVerifiedAt              *time.Time
	CreatedAt                   time.Time
	UpdatedAt                   time.Time
}
