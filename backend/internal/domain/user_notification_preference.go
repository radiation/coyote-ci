package domain

import "time"

type UserNotificationPreferenceSource string

const (
	UserNotificationPreferenceSourceInstanceDefault UserNotificationPreferenceSource = "instance_default"
	UserNotificationPreferenceSourceUser            UserNotificationPreferenceSource = "user"
)

type UserNotificationPreference struct {
	UserID                          string
	CommitAuthorFailureEmailEnabled bool
	CommitAuthorFailureSlackEnabled bool
	CommitAuthorFailureEmailSource  UserNotificationPreferenceSource
	CommitAuthorSuccessEmailEnabled bool
	CommitAuthorSuccessSlackEnabled bool
	CommitAuthorSuccessEmailSource  *UserNotificationPreferenceSource
	CreatedAt                       time.Time
	UpdatedAt                       time.Time
}
