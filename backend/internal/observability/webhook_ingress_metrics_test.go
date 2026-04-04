package observability

import (
	"testing"
	"time"
)

func TestInMemoryWebhookIngressMetrics_RecordsOutcomeAndDuration(t *testing.T) {
	m := NewInMemoryWebhookIngressMetrics()

	m.IncOutcome("github", "push", WebhookOutcomeDeliveriesReceived)
	m.IncOutcome("github", "push", WebhookOutcomeDeliveriesReceived)
	m.ObserveIngressDuration("github", "push", WebhookOutcomeBuildQueued, 42*time.Millisecond)

	if got := m.OutcomeCount("github", "push", WebhookOutcomeDeliveriesReceived); got != 2 {
		t.Fatalf("expected 2 deliveries_received, got %d", got)
	}
	if got := m.DurationSampleCount("github", "push", WebhookOutcomeBuildQueued); got != 1 {
		t.Fatalf("expected 1 duration sample, got %d", got)
	}
}

func TestMetricLabelNormalization(t *testing.T) {
	m := NewInMemoryWebhookIngressMetrics()
	m.IncOutcome(" GITHUB ", "", " BUILD_QUEUED ")

	if got := m.OutcomeCount("github", "unknown", "build_queued"); got != 1 {
		t.Fatalf("expected normalized labels to map to one counter, got %d", got)
	}
}
