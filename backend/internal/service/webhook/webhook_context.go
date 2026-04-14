package webhook

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type WebhookLogContext struct {
	CorrelationID string
	Provider      string
	DeliveryID    string
	EventType     string
}

type webhookLogContextKey struct{}

func NewWebhookLogContext(provider string, deliveryID string, eventType string) WebhookLogContext {
	normalizedProvider := strings.ToLower(strings.TrimSpace(provider))
	normalizedDeliveryID := strings.TrimSpace(deliveryID)
	normalizedEventType := strings.ToLower(strings.TrimSpace(eventType))

	correlationID := normalizedDeliveryID
	if correlationID == "" {
		correlationID = uuid.NewString()
	}

	return WebhookLogContext{
		CorrelationID: correlationID,
		Provider:      normalizedProvider,
		DeliveryID:    normalizedDeliveryID,
		EventType:     normalizedEventType,
	}
}

func WithWebhookLogContext(ctx context.Context, fields WebhookLogContext) context.Context {
	return context.WithValue(ctx, webhookLogContextKey{}, fields)
}

func WebhookLogContextFromContext(ctx context.Context) WebhookLogContext {
	if ctx == nil {
		return WebhookLogContext{}
	}
	value := ctx.Value(webhookLogContextKey{})
	fields, ok := value.(WebhookLogContext)
	if !ok {
		return WebhookLogContext{}
	}
	return fields
}

func WebhookLogFields(ctx context.Context) string {
	fields := WebhookLogContextFromContext(ctx)
	return fmt.Sprintf(
		"correlation_id=%s provider=%s delivery_id=%s event_type=%s",
		logFieldOrUnknown(fields.CorrelationID),
		logFieldOrUnknown(fields.Provider),
		logFieldOrUnknown(fields.DeliveryID),
		logFieldOrUnknown(fields.EventType),
	)
}

func logFieldOrUnknown(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "unknown"
	}
	return trimmed
}
