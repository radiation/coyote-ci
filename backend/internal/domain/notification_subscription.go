package domain

import "time"

type NotificationTargetType string

const (
	NotificationTargetTypeEmail        NotificationTargetType = "email"
	NotificationTargetTypeSlackWebhook NotificationTargetType = "slack_webhook"
)

type NotificationTarget struct {
	ID          string
	OwnerUserID *string
	Type        NotificationTargetType
	Name        string
	Recipient   string
	Enabled     bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type NotificationSubscription struct {
	ID        string
	TargetID  string
	ProjectID *string
	JobID     *string
	EventType NotificationEventType
	Enabled   bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

type NotificationSubscriptionMatch struct {
	Subscription NotificationSubscription
	Target       NotificationTarget
}
