package domain

import "time"

type NotificationInstanceSettings struct {
	DefaultCommitAuthorFailureEmailEnabled bool
	CreatedAt                              time.Time
	UpdatedAt                              time.Time
}
