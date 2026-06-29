package domain

import "time"

type NotificationInstanceSettings struct {
	DefaultCommitAuthorFailureEmailEnabled bool
	DefaultCommitAuthorSuccessEmailEnabled bool
	CreatedAt                              time.Time
	UpdatedAt                              time.Time
}
