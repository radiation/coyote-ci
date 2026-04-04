package domain

import "time"

type WebhookDeliveryStatus string

const (
	WebhookDeliveryStatusReceived       WebhookDeliveryStatus = "received"
	WebhookDeliveryStatusVerified       WebhookDeliveryStatus = "verified"
	WebhookDeliveryStatusUnsupported    WebhookDeliveryStatus = "unsupported"
	WebhookDeliveryStatusDuplicate      WebhookDeliveryStatus = "duplicate"
	WebhookDeliveryStatusMatched        WebhookDeliveryStatus = "matched"
	WebhookDeliveryStatusIgnoredNoMatch WebhookDeliveryStatus = "ignored_no_match"
	WebhookDeliveryStatusQueued         WebhookDeliveryStatus = "queued"
	WebhookDeliveryStatusFailed         WebhookDeliveryStatus = "failed"
)

type WebhookDelivery struct {
	ID              string
	Provider        string
	DeliveryID      string
	EventType       *string
	RepositoryOwner *string
	RepositoryName  *string
	TriggerRef      *string
	CommitSHA       *string
	Actor           *string
	Status          WebhookDeliveryStatus
	MatchedJobID    *string
	QueuedBuildID   *string
	Reason          *string
	ReceivedAt      time.Time
	UpdatedAt       time.Time
}
