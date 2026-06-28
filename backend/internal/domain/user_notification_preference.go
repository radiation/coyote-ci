package domain

import "time"

type UserNotificationPreference struct {
	UserID                     string
	CommitAuthorFailureEnabled bool
	CreatedAt                  time.Time
	UpdatedAt                  time.Time
}
