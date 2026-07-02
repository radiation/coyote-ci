package domain

import (
	"fmt"
	"strings"
	"time"
)

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

type NotificationTransport string

const (
	NotificationTransportEmail        NotificationTransport = "email"
	NotificationTransportSlackWebhook NotificationTransport = "slack_webhook"
	NotificationTransportSlackDM      NotificationTransport = "slack_dm"
)

type NotificationDestinationKind string

const (
	NotificationDestinationKindPersonalEmail NotificationDestinationKind = "personal_email"
	NotificationDestinationKindSharedTarget  NotificationDestinationKind = "shared_target"
	NotificationDestinationKindSlackIdentity NotificationDestinationKind = "slack_identity"
)

type NotificationDelivery struct {
	ID                          string
	BuildID                     string
	EventType                   NotificationEventType
	Transport                   NotificationTransport
	DestinationKind             NotificationDestinationKind
	DestinationKey              string
	NotificationTargetID        *string
	RecipientUserID             *string
	SlackWorkspaceIntegrationID *string
	Recipient                   string
	Status                      NotificationDeliveryStatus
	Attempts                    int
	LastError                   *string
	CreatedAt                   time.Time
	UpdatedAt                   time.Time
	SentAt                      *time.Time
}

func (d NotificationDelivery) Normalize() NotificationDelivery {
	d.BuildID = strings.TrimSpace(d.BuildID)
	d.EventType = NotificationEventType(strings.TrimSpace(string(d.EventType)))
	d.Transport = NotificationTransport(strings.TrimSpace(string(d.Transport)))
	d.DestinationKind = NotificationDestinationKind(strings.TrimSpace(string(d.DestinationKind)))
	d.DestinationKey = strings.TrimSpace(d.DestinationKey)
	d.NotificationTargetID = trimNotificationOptionalString(d.NotificationTargetID)
	d.RecipientUserID = trimNotificationOptionalString(d.RecipientUserID)
	d.SlackWorkspaceIntegrationID = trimNotificationOptionalString(d.SlackWorkspaceIntegrationID)
	d.Recipient = strings.TrimSpace(d.Recipient)
	d.Status = NotificationDeliveryStatus(strings.TrimSpace(string(d.Status)))
	d.LastError = trimNotificationOptionalString(d.LastError)
	return d
}

func (d NotificationDelivery) ValidateIdentity() error {
	if strings.TrimSpace(d.BuildID) == "" {
		return fmt.Errorf("notification delivery build id is required")
	}
	if strings.TrimSpace(string(d.EventType)) == "" {
		return fmt.Errorf("notification delivery event type is required")
	}
	if !d.Transport.IsValid() {
		return fmt.Errorf("unsupported notification transport %q", d.Transport)
	}
	if !d.DestinationKind.IsValid() {
		return fmt.Errorf("unsupported notification destination kind %q", d.DestinationKind)
	}
	if strings.TrimSpace(d.DestinationKey) == "" {
		return fmt.Errorf("notification delivery destination key is required")
	}
	if !d.IsTransportDestinationKindValid() {
		return fmt.Errorf("unsupported notification delivery transport/destination combination %q/%q", d.Transport, d.DestinationKind)
	}
	return nil
}

func (t NotificationTransport) IsValid() bool {
	switch t {
	case NotificationTransportEmail, NotificationTransportSlackWebhook, NotificationTransportSlackDM:
		return true
	default:
		return false
	}
}

func (k NotificationDestinationKind) IsValid() bool {
	switch k {
	case NotificationDestinationKindPersonalEmail, NotificationDestinationKindSharedTarget, NotificationDestinationKindSlackIdentity:
		return true
	default:
		return false
	}
}

func (d NotificationDelivery) IsTransportDestinationKindValid() bool {
	switch d.Transport {
	case NotificationTransportEmail:
		return d.DestinationKind == NotificationDestinationKindPersonalEmail || d.DestinationKind == NotificationDestinationKindSharedTarget
	case NotificationTransportSlackWebhook:
		return d.DestinationKind == NotificationDestinationKindSharedTarget
	case NotificationTransportSlackDM:
		return d.DestinationKind == NotificationDestinationKindSlackIdentity
	default:
		return false
	}
}

func NotificationSharedEmailTargetKey(targetID string) (NotificationDestinationKind, string, error) {
	trimmedTargetID := strings.TrimSpace(targetID)
	if trimmedTargetID == "" {
		return "", "", fmt.Errorf("shared email target id is required")
	}
	return NotificationDestinationKindSharedTarget, "email-target:" + trimmedTargetID, nil
}

func NotificationSharedSlackWebhookTargetKey(targetID string) (NotificationDestinationKind, string, error) {
	trimmedTargetID := strings.TrimSpace(targetID)
	if trimmedTargetID == "" {
		return "", "", fmt.Errorf("shared slack webhook target id is required")
	}
	return NotificationDestinationKindSharedTarget, "slack-webhook-target:" + trimmedTargetID, nil
}

func NotificationPersonalEmailTargetKey(targetID string) (NotificationDestinationKind, string, error) {
	trimmedTargetID := strings.TrimSpace(targetID)
	if trimmedTargetID == "" {
		return "", "", fmt.Errorf("personal email target id is required")
	}
	return NotificationDestinationKindPersonalEmail, "email-personal:" + trimmedTargetID, nil
}

func NotificationSlackDMDestinationKey(workspaceIntegrationID string, slackUserID string) (NotificationDestinationKind, string, error) {
	trimmedWorkspaceID := strings.TrimSpace(workspaceIntegrationID)
	trimmedSlackUserID := strings.TrimSpace(slackUserID)
	if trimmedWorkspaceID == "" {
		return "", "", fmt.Errorf("slack workspace integration id is required")
	}
	if trimmedSlackUserID == "" {
		return "", "", fmt.Errorf("slack user id is required")
	}
	return NotificationDestinationKindSlackIdentity, "slack-dm:" + trimmedWorkspaceID + ":" + trimmedSlackUserID, nil
}

func trimNotificationOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
