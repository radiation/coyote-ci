package observability

import (
	"expvar"
	"fmt"
	"strings"
	"sync"
)

const (
	NotificationDeliveryOutcomeScanned             = "scanned"
	NotificationDeliveryOutcomeClaimAcquired       = "claim_acquired"
	NotificationDeliveryOutcomeRetryClaimed        = "retry_claimed"
	NotificationDeliveryOutcomeStaleClaimReclaimed = "stale_claim_reclaimed"
	NotificationDeliveryOutcomeSkippedContention   = "skipped_contention"
	NotificationDeliveryOutcomeSkippedNotDue       = "skipped_not_due"
	NotificationDeliveryOutcomeSkippedTerminal     = "skipped_terminal"
	NotificationDeliveryOutcomeSkippedIneligible   = "skipped_ineligible"
	NotificationDeliveryOutcomeSent                = "sent"
	NotificationDeliveryOutcomeRetryScheduled      = "retry_scheduled"
	NotificationDeliveryOutcomePermanentlyFailed   = "permanently_failed"
	NotificationDeliveryOutcomeAttemptsExhausted   = "attempts_exhausted"
	NotificationDeliveryOutcomeLostClaim           = "lost_claim"
	NotificationDeliveryOutcomeRehydrationFailure  = "rehydration_failure"
)

type NotificationDeliveryMetrics interface {
	IncOutcome(eventType string, transport string, destinationKind string, recoveryReason string, outcome string)
}

type NoopNotificationDeliveryMetrics struct{}

func NewNoopNotificationDeliveryMetrics() NotificationDeliveryMetrics {
	return NoopNotificationDeliveryMetrics{}
}

func (NoopNotificationDeliveryMetrics) IncOutcome(eventType string, transport string, destinationKind string, recoveryReason string, outcome string) {
}

type ExpvarNotificationDeliveryMetrics struct {
	outcomeTotal *expvar.Map
}

func NewExpvarNotificationDeliveryMetrics() NotificationDeliveryMetrics {
	return &ExpvarNotificationDeliveryMetrics{outcomeTotal: expvar.NewMap("notification_delivery_outcome_total")}
}

func (m *ExpvarNotificationDeliveryMetrics) IncOutcome(eventType string, transport string, destinationKind string, recoveryReason string, outcome string) {
	m.outcomeTotal.Add(notificationMetricLabelKey(eventType, transport, destinationKind, recoveryReason, outcome), 1)
}

type InMemoryNotificationDeliveryMetrics struct {
	mu            sync.RWMutex
	outcomeTotals map[string]int64
}

func NewInMemoryNotificationDeliveryMetrics() *InMemoryNotificationDeliveryMetrics {
	return &InMemoryNotificationDeliveryMetrics{outcomeTotals: make(map[string]int64)}
}

func (m *InMemoryNotificationDeliveryMetrics) IncOutcome(eventType string, transport string, destinationKind string, recoveryReason string, outcome string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.outcomeTotals[notificationMetricLabelKey(eventType, transport, destinationKind, recoveryReason, outcome)]++
}

func (m *InMemoryNotificationDeliveryMetrics) OutcomeCount(eventType string, transport string, destinationKind string, recoveryReason string, outcome string) int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.outcomeTotals[notificationMetricLabelKey(eventType, transport, destinationKind, recoveryReason, outcome)]
}

func notificationMetricLabelKey(eventType string, transport string, destinationKind string, recoveryReason string, outcome string) string {
	return fmt.Sprintf("event_type=%s,transport=%s,destination_kind=%s,recovery_reason=%s,outcome=%s",
		normalizeMetricLabel(eventType, "unknown"),
		normalizeMetricLabel(transport, "unknown"),
		normalizeMetricLabel(destinationKind, "unknown"),
		normalizeMetricLabel(strings.TrimSpace(recoveryReason), "inline"),
		normalizeMetricLabel(outcome, "unknown"),
	)
}
