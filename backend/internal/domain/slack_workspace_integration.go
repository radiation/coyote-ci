package domain

import "time"

type SlackWorkspaceIntegration struct {
	ID                  string
	WorkspaceID         string
	WorkspaceName       *string
	WorkspaceURL        *string
	BotID               *string
	AuthedUserID        *string
	AppID               *string
	BotTokenSecret      string
	LinkedIdentityCount int
	Enabled             bool
	ConnectedAt         time.Time
	LastTestedAt        *time.Time
	LastTestSucceeded   *bool
	CreatedAt           time.Time
	UpdatedAt           time.Time
}
