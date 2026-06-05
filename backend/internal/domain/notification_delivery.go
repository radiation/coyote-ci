package domain

import "time"

type NotificationEventType string

const (
	NotificationEventTypeBuildSucceeded NotificationEventType = "build_succeeded"
	NotificationEventTypeBuildFailed    NotificationEventType = "build_failed"
)

type NotificationDeliveryStatus string

const (
	NotificationDeliveryStatusPending NotificationDeliveryStatus = "pending"
	NotificationDeliveryStatusSent    NotificationDeliveryStatus = "sent"
	NotificationDeliveryStatusFailed  NotificationDeliveryStatus = "failed"
)

type NotificationDelivery struct {
	ID        string
	BuildID   string
	EventType NotificationEventType
	Recipient string
	Status    NotificationDeliveryStatus
	Attempts  int
	LastError *string
	CreatedAt time.Time
	UpdatedAt time.Time
	SentAt    *time.Time
}
