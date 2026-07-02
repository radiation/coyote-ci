package domain

import (
	"fmt"
	"net/mail"
	"net/url"
	"strings"
	"time"
)

type NotificationTargetType string

type NotificationTargetOrigin string

const (
	NotificationTargetTypeEmail        NotificationTargetType = "email"
	NotificationTargetTypeSlackWebhook NotificationTargetType = "slack_webhook"

	NotificationTargetOriginManual        NotificationTargetOrigin = "manual"
	NotificationTargetOriginConfigDefault NotificationTargetOrigin = "config_default"
)

type NotificationTarget struct {
	ID          string
	OwnerUserID *string
	Type        NotificationTargetType
	Origin      NotificationTargetOrigin
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

func NormalizeNotificationTarget(target NotificationTarget) (NotificationTarget, error) {
	target.Type = NotificationTargetType(strings.TrimSpace(string(target.Type)))
	if target.Type == "" {
		target.Type = NotificationTargetTypeEmail
	}
	if target.Type != NotificationTargetTypeEmail && target.Type != NotificationTargetTypeSlackWebhook {
		return NotificationTarget{}, fmt.Errorf("unsupported notification target type %q", target.Type)
	}

	normalizedRecipient, err := normalizeNotificationTargetRecipient(target.Type, target.Recipient)
	if err != nil {
		return NotificationTarget{}, err
	}
	target.Recipient = normalizedRecipient
	target.Name = strings.TrimSpace(target.Name)
	target.OwnerUserID = trimNotificationTargetOptionalString(target.OwnerUserID)

	origin, err := normalizeNotificationTargetOrigin(target.Type, target.OwnerUserID, target.Origin)
	if err != nil {
		return NotificationTarget{}, err
	}
	target.Origin = origin

	return target, nil
}

func ValidateExplicitNotificationTarget(target NotificationTarget) (NotificationTarget, error) {
	if strings.TrimSpace(string(target.Origin)) == "" {
		return NotificationTarget{}, fmt.Errorf("notification target origin is required")
	}
	return NormalizeNotificationTarget(target)
}

func normalizeNotificationTargetOrigin(targetType NotificationTargetType, ownerUserID *string, origin NotificationTargetOrigin) (NotificationTargetOrigin, error) {
	trimmedOrigin := NotificationTargetOrigin(strings.TrimSpace(string(origin)))
	if trimmedOrigin == "" {
		trimmedOrigin = NotificationTargetOriginManual
	}
	if trimmedOrigin == NotificationTargetOriginManual {
		return trimmedOrigin, nil
	}
	if trimmedOrigin == NotificationTargetOriginConfigDefault {
		if targetType != NotificationTargetTypeEmail {
			return "", fmt.Errorf("config-default notification targets must be email targets")
		}
		if ownerUserID != nil {
			return "", fmt.Errorf("config-default notification targets must be ownerless")
		}
		return trimmedOrigin, nil
	}
	return "", fmt.Errorf("unsupported notification target origin %q", trimmedOrigin)
}

func normalizeNotificationTargetRecipient(targetType NotificationTargetType, value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	switch targetType {
	case NotificationTargetTypeEmail:
		parsed, err := mail.ParseAddress(trimmed)
		if err != nil {
			return "", fmt.Errorf("invalid notification target recipient %q: %w", trimmed, err)
		}
		return parsed.String(), nil
	case NotificationTargetTypeSlackWebhook:
		parsed, err := url.Parse(trimmed)
		if err != nil || parsed == nil || !parsed.IsAbs() || strings.ToLower(parsed.Scheme) != "https" || strings.TrimSpace(parsed.Host) == "" {
			return "", fmt.Errorf("notification target webhook_url must be a valid https URL")
		}
		return parsed.String(), nil
	default:
		return "", fmt.Errorf("unsupported notification target type %q", targetType)
	}
}

func trimNotificationTargetOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
