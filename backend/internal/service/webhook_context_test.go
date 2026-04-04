package service

import (
	"context"
	"testing"
)

func TestNewWebhookLogContext_UsesDeliveryIDAsCorrelation(t *testing.T) {
	ctx := NewWebhookLogContext("github", "delivery-123", "push")
	if ctx.CorrelationID != "delivery-123" {
		t.Fatalf("expected correlation id to use delivery id, got %q", ctx.CorrelationID)
	}
	if ctx.Provider != "github" {
		t.Fatalf("expected normalized provider github, got %q", ctx.Provider)
	}
	if ctx.EventType != "push" {
		t.Fatalf("expected normalized event_type push, got %q", ctx.EventType)
	}
}

func TestNewWebhookLogContext_GeneratesCorrelationWhenDeliveryIDMissing(t *testing.T) {
	ctx := NewWebhookLogContext("github", "", "push")
	if ctx.CorrelationID == "" {
		t.Fatal("expected generated correlation id")
	}
	if ctx.DeliveryID != "" {
		t.Fatalf("expected empty delivery id, got %q", ctx.DeliveryID)
	}
}

func TestWebhookLogContext_RoundTrip(t *testing.T) {
	fields := WebhookLogContext{CorrelationID: "corr-1", Provider: "github", DeliveryID: "delivery-1", EventType: "push"}
	ctx := WithWebhookLogContext(context.Background(), fields)
	got := WebhookLogContextFromContext(ctx)
	if got != fields {
		t.Fatalf("expected webhook context %+v, got %+v", fields, got)
	}
}
