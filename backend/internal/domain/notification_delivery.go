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
	NotificationDeliveryStatusPending         NotificationDeliveryStatus = "pending"
	NotificationDeliveryStatusSending         NotificationDeliveryStatus = "sending"
	NotificationDeliveryStatusRetryWaiting    NotificationDeliveryStatus = "retry_waiting"
	NotificationDeliveryStatusSent            NotificationDeliveryStatus = "sent"
	NotificationDeliveryStatusFailedPermanent NotificationDeliveryStatus = "failed_permanent"
	NotificationDeliveryStatusFailedExhausted NotificationDeliveryStatus = "failed_exhausted"
	NotificationDeliveryStatusFailed          NotificationDeliveryStatus = NotificationDeliveryStatusFailedPermanent
)

type NotificationDeliveryFailureCategory string

const (
	NotificationDeliveryFailureCategoryRetryable NotificationDeliveryFailureCategory = "retryable"
	NotificationDeliveryFailureCategoryPermanent NotificationDeliveryFailureCategory = "permanent"
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
	MaxAttempts                 int
	LastAttemptAt               *time.Time
	NextAttemptAt               *time.Time
	ClaimedAt                   *time.Time
	ClaimExpiresAt              *time.Time
	ClaimedBy                   *string
	FailureCategory             *NotificationDeliveryFailureCategory
	FailureReason               *string
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
	d.MaxAttempts = maxNotificationAttempts(d.MaxAttempts)
	d.ClaimedBy = trimNotificationOptionalString(d.ClaimedBy)
	d.FailureReason = trimNotificationOptionalString(d.FailureReason)
	d.LastError = trimNotificationOptionalString(d.LastError)
	d.LastAttemptAt = normalizeNotificationOptionalTime(d.LastAttemptAt)
	d.NextAttemptAt = normalizeNotificationOptionalTime(d.NextAttemptAt)
	d.ClaimedAt = normalizeNotificationOptionalTime(d.ClaimedAt)
	d.ClaimExpiresAt = normalizeNotificationOptionalTime(d.ClaimExpiresAt)
	d.SentAt = normalizeNotificationOptionalTime(d.SentAt)
	if d.FailureCategory != nil {
		trimmed := NotificationDeliveryFailureCategory(strings.TrimSpace(string(*d.FailureCategory)))
		if trimmed == "" {
			d.FailureCategory = nil
		} else {
			d.FailureCategory = &trimmed
		}
	}
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

func (d NotificationDelivery) Validate() error {
	d = d.Normalize()
	if err := d.ValidateIdentity(); err != nil {
		return err
	}
	if d.Attempts < 0 {
		return fmt.Errorf("notification delivery attempts cannot be negative")
	}
	if d.MaxAttempts <= 0 {
		return fmt.Errorf("notification delivery max attempts must be positive")
	}
	if d.Attempts > d.MaxAttempts {
		return fmt.Errorf("notification delivery attempts cannot exceed max attempts")
	}
	if !d.Status.IsValid() {
		return fmt.Errorf("unsupported notification delivery status %q", d.Status)
	}
	if d.FailureCategory != nil && !d.FailureCategory.IsValid() {
		return fmt.Errorf("unsupported notification delivery failure category %q", *d.FailureCategory)
	}
	if d.SentAt != nil && d.Status != NotificationDeliveryStatusSent {
		return fmt.Errorf("notification delivery sent_at requires sent status")
	}
	if d.Status == NotificationDeliveryStatusSent && d.SentAt == nil {
		return fmt.Errorf("sent notification delivery requires sent_at")
	}
	if d.IsTerminal() && d.ClaimedBy != nil {
		return fmt.Errorf("terminal notification delivery cannot retain an active claim owner")
	}
	if d.IsTerminal() && d.ClaimedAt != nil {
		return fmt.Errorf("terminal notification delivery cannot retain claimed_at")
	}
	if d.IsTerminal() && d.ClaimExpiresAt != nil {
		return fmt.Errorf("terminal notification delivery cannot retain claim_expires_at")
	}
	if d.IsTerminal() && d.NextAttemptAt != nil {
		return fmt.Errorf("terminal notification delivery cannot retain next_attempt_at")
	}
	if d.Status == NotificationDeliveryStatusPending {
		if d.NextAttemptAt != nil {
			return fmt.Errorf("pending notification delivery cannot retain next_attempt_at")
		}
		if d.ClaimedBy != nil || d.ClaimedAt != nil || d.ClaimExpiresAt != nil {
			return fmt.Errorf("pending notification delivery cannot retain active claim metadata")
		}
	}
	if d.Status == NotificationDeliveryStatusRetryWaiting {
		if d.Attempts >= d.MaxAttempts {
			return fmt.Errorf("retry-waiting notification delivery requires attempts below max attempts")
		}
		if d.NextAttemptAt == nil {
			return fmt.Errorf("retry-waiting notification delivery requires next_attempt_at")
		}
		if d.ClaimedBy != nil || d.ClaimedAt != nil || d.ClaimExpiresAt != nil {
			return fmt.Errorf("retry-waiting notification delivery cannot retain active claim metadata")
		}
		if d.FailureCategory == nil || *d.FailureCategory != NotificationDeliveryFailureCategoryRetryable {
			return fmt.Errorf("retry-waiting notification delivery requires retryable failure category")
		}
	}
	if d.Status == NotificationDeliveryStatusSending {
		if d.Attempts < 1 {
			return fmt.Errorf("sending notification delivery requires at least one attempt")
		}
		if d.ClaimedBy == nil || d.ClaimedAt == nil || d.ClaimExpiresAt == nil {
			return fmt.Errorf("sending notification delivery requires claim owner and expiry")
		}
		if !d.ClaimExpiresAt.After(*d.ClaimedAt) {
			return fmt.Errorf("notification delivery claim expiry must be after claim acquisition")
		}
		if d.NextAttemptAt != nil {
			return fmt.Errorf("sending notification delivery cannot retain next_attempt_at")
		}
	}
	if d.Status == NotificationDeliveryStatusFailedPermanent {
		if d.FailureCategory == nil || *d.FailureCategory != NotificationDeliveryFailureCategoryPermanent {
			return fmt.Errorf("permanently failed notification delivery requires permanent failure category")
		}
	}
	if d.Status == NotificationDeliveryStatusFailedExhausted {
		if d.Attempts != d.MaxAttempts {
			return fmt.Errorf("exhausted notification delivery requires attempts to equal max attempts")
		}
		if d.FailureCategory == nil || *d.FailureCategory != NotificationDeliveryFailureCategoryRetryable {
			return fmt.Errorf("exhausted notification delivery requires retryable failure category")
		}
	}
	if d.Status == NotificationDeliveryStatusSent {
		if d.Attempts < 1 {
			return fmt.Errorf("sent notification delivery requires at least one attempt")
		}
	}
	return nil
}

func (s NotificationDeliveryStatus) IsValid() bool {
	switch s {
	case NotificationDeliveryStatusPending,
		NotificationDeliveryStatusSending,
		NotificationDeliveryStatusRetryWaiting,
		NotificationDeliveryStatusSent,
		NotificationDeliveryStatusFailedPermanent,
		NotificationDeliveryStatusFailedExhausted:
		return true
	default:
		return false
	}
}

func (c NotificationDeliveryFailureCategory) IsValid() bool {
	switch c {
	case NotificationDeliveryFailureCategoryRetryable,
		NotificationDeliveryFailureCategoryPermanent:
		return true
	default:
		return false
	}
}

func (d NotificationDelivery) IsTerminal() bool {
	switch d.Status {
	case NotificationDeliveryStatusSent, NotificationDeliveryStatusFailedPermanent, NotificationDeliveryStatusFailedExhausted:
		return true
	default:
		return false
	}
}

func (d NotificationDelivery) CanAttempt(now time.Time) bool {
	if d.IsTerminal() || d.Attempts >= d.MaxAttempts {
		return false
	}
	switch d.Status {
	case NotificationDeliveryStatusPending:
		return true
	case NotificationDeliveryStatusRetryWaiting:
		return d.NextAttemptAt != nil && !now.UTC().Before(d.NextAttemptAt.UTC())
	case NotificationDeliveryStatusSending:
		return d.ClaimExpiresAt != nil && !now.UTC().Before(d.ClaimExpiresAt.UTC())
	default:
		return false
	}
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

func normalizeNotificationOptionalTime(value *time.Time) *time.Time {
	if value == nil || value.IsZero() {
		return nil
	}
	utc := value.UTC()
	return &utc
}

func maxNotificationAttempts(value int) int {
	if value <= 0 {
		return 0
	}
	return value
}
