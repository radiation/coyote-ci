package observability

import (
	"expvar"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	WebhookOutcomeDeliveriesReceived = "deliveries_received"
	WebhookOutcomeDeliveriesVerified = "deliveries_verified"
	WebhookOutcomeInvalidSignature   = "invalid_signature"
	WebhookOutcomeDuplicate          = "duplicate"
	WebhookOutcomeUnsupportedEvent   = "unsupported_event"
	WebhookOutcomeNoMatchingJob      = "no_matching_job"
	WebhookOutcomeBuildQueued        = "build_queued"
	WebhookOutcomeFailedProcessing   = "failed_processing"
)

type WebhookIngressMetrics interface {
	IncOutcome(provider string, eventType string, outcome string)
	ObserveIngressDuration(provider string, eventType string, outcome string, d time.Duration)
}

type NoopWebhookIngressMetrics struct{}

func NewNoopWebhookIngressMetrics() WebhookIngressMetrics {
	return NoopWebhookIngressMetrics{}
}

func (NoopWebhookIngressMetrics) IncOutcome(provider string, eventType string, outcome string) {}

func (NoopWebhookIngressMetrics) ObserveIngressDuration(provider string, eventType string, outcome string, d time.Duration) {
}

type ExpvarWebhookIngressMetrics struct {
	outcomeTotal   *expvar.Map
	durationCount  *expvar.Map
	durationMsSum  *expvar.Map
	durationBucket *expvar.Map
}

func NewExpvarWebhookIngressMetrics() WebhookIngressMetrics {
	return &ExpvarWebhookIngressMetrics{
		outcomeTotal:   expvar.NewMap("webhook_ingress_outcome_total"),
		durationCount:  expvar.NewMap("webhook_ingress_duration_count"),
		durationMsSum:  expvar.NewMap("webhook_ingress_duration_ms_sum"),
		durationBucket: expvar.NewMap("webhook_ingress_duration_bucket_ms"),
	}
}

func (m *ExpvarWebhookIngressMetrics) IncOutcome(provider string, eventType string, outcome string) {
	m.outcomeTotal.Add(metricLabelKey(provider, eventType, outcome), 1)
}

func (m *ExpvarWebhookIngressMetrics) ObserveIngressDuration(provider string, eventType string, outcome string, d time.Duration) {
	key := metricLabelKey(provider, eventType, outcome)
	m.durationCount.Add(key, 1)
	m.durationMsSum.Add(key, d.Milliseconds())
	for _, boundary := range durationBucketBoundariesMs {
		if d.Milliseconds() <= boundary {
			bucketKey := fmt.Sprintf("%s,le=%d", key, boundary)
			m.durationBucket.Add(bucketKey, 1)
		}
	}
}

var durationBucketBoundariesMs = []int64{50, 100, 250, 500, 1000, 3000, 10000}

func metricLabelKey(provider string, eventType string, outcome string) string {
	return "provider=" + normalizeMetricLabel(provider, "unknown") + ",event_type=" + normalizeMetricLabel(eventType, "unknown") + ",outcome=" + normalizeMetricLabel(outcome, "unknown")
}

func normalizeMetricLabel(value string, fallback string) string {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	if trimmed == "" {
		return fallback
	}
	return trimmed
}

type InMemoryWebhookIngressMetrics struct {
	mu             sync.RWMutex
	outcomeTotals  map[string]int64
	durationCounts map[string]int64
}

func NewInMemoryWebhookIngressMetrics() *InMemoryWebhookIngressMetrics {
	return &InMemoryWebhookIngressMetrics{
		outcomeTotals:  make(map[string]int64),
		durationCounts: make(map[string]int64),
	}
}

func (m *InMemoryWebhookIngressMetrics) IncOutcome(provider string, eventType string, outcome string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := metricLabelKey(provider, eventType, outcome)
	m.outcomeTotals[key]++
}

func (m *InMemoryWebhookIngressMetrics) ObserveIngressDuration(provider string, eventType string, outcome string, d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := metricLabelKey(provider, eventType, outcome)
	m.durationCounts[key]++
}

func (m *InMemoryWebhookIngressMetrics) OutcomeCount(provider string, eventType string, outcome string) int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.outcomeTotals[metricLabelKey(provider, eventType, outcome)]
}

func (m *InMemoryWebhookIngressMetrics) DurationSampleCount(provider string, eventType string, outcome string) int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.durationCounts[metricLabelKey(provider, eventType, outcome)]
}
