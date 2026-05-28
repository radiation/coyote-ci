package observability

import (
	"expvar"
	"testing"
	"time"
)

func TestNoopWebhookIngressMetrics_DropsObservations(t *testing.T) {
	metrics := NewNoopWebhookIngressMetrics()
	metrics.IncOutcome("github", "push", WebhookOutcomeDeliveriesReceived)
	metrics.ObserveIngressDuration("github", "push", WebhookOutcomeBuildQueued, 25*time.Millisecond)
}

func TestExpvarWebhookIngressMetrics_RecordsOutcomeAndDuration(t *testing.T) {
	if expvar.Get("webhook_ingress_outcome_total") != nil {
		t.Skip("expvar metrics are already registered in this test process")
	}
	metrics, ok := NewExpvarWebhookIngressMetrics().(*ExpvarWebhookIngressMetrics)
	if !ok {
		t.Fatal("expected expvar webhook ingress metrics")
	}

	metrics.IncOutcome(" GitHub ", " Push ", WebhookOutcomeDeliveriesReceived)
	metrics.ObserveIngressDuration(" GitHub ", " Push ", WebhookOutcomeBuildQueued, 75*time.Millisecond)

	outcomeKey := metricLabelKey("github", "push", WebhookOutcomeDeliveriesReceived)
	if got := metrics.outcomeTotal.Get(outcomeKey); got == nil || got.String() != "1" {
		t.Fatalf("expected outcome count 1, got %v", got)
	}
	durationKey := metricLabelKey("github", "push", WebhookOutcomeBuildQueued)
	if got := metrics.durationCount.Get(durationKey); got == nil || got.String() != "1" {
		t.Fatalf("expected duration count 1, got %v", got)
	}
	if got := metrics.durationMsSum.Get(durationKey); got == nil || got.String() != "75" {
		t.Fatalf("expected duration sum 75, got %v", got)
	}
	if got := metrics.durationBucket.Get(durationKey + ",le=100"); got == nil || got.String() != "1" {
		t.Fatalf("expected le=100 bucket count 1, got %v", got)
	}
}

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
