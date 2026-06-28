package domain

import "time"

type UserNotificationPreferenceSource string

const (
	UserNotificationPreferenceSourceInstanceDefault UserNotificationPreferenceSource = "instance_default"
	UserNotificationPreferenceSourceUser            UserNotificationPreferenceSource = "user"
)

type UserNotificationPreference struct {
	UserID                     string
	CommitAuthorFailureEnabled bool
	Source                     UserNotificationPreferenceSource
	CreatedAt                  time.Time
	UpdatedAt                  time.Time
}
