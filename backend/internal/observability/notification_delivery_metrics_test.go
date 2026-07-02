package observability

import (
	"expvar"
	"testing"
)

func TestNoopNotificationDeliveryMetrics_DropsObservations(t *testing.T) {
	metrics := NewNoopNotificationDeliveryMetrics()
	metrics.IncOutcome("build_failed", "email", "shared_target", "inline", NotificationDeliveryOutcomeSent)
}

func TestInMemoryNotificationDeliveryMetrics_RecordsAndNormalizes(t *testing.T) {
	m := NewInMemoryNotificationDeliveryMetrics()

	m.IncOutcome(" Build_Failed ", " Email ", " Shared_Target ", " due_retry ", NotificationDeliveryOutcomeRetryClaimed)
	m.IncOutcome(" Build_Failed ", " Email ", " Shared_Target ", " due_retry ", NotificationDeliveryOutcomeRetryClaimed)
	m.IncOutcome("", "", "", "   ", "")

	if got := m.OutcomeCount("build_failed", "email", "shared_target", "due_retry", NotificationDeliveryOutcomeRetryClaimed); got != 2 {
		t.Fatalf("expected 2 retry-claimed outcomes, got %d", got)
	}
	if got := m.OutcomeCount("unknown", "unknown", "unknown", "inline", "unknown"); got != 1 {
		t.Fatalf("expected normalized default labels to count once, got %d", got)
	}
	if key := notificationMetricLabelKey("", "", "", "", ""); key != "event_type=unknown,transport=unknown,destination_kind=unknown,recovery_reason=inline,outcome=unknown" {
		t.Fatalf("unexpected normalized metric key %q", key)
	}
}

func TestExpvarNotificationDeliveryMetrics_RecordsOutcome(t *testing.T) {
	if expvar.Get("notification_delivery_outcome_total") != nil {
		t.Skip("expvar metrics are already registered in this test process")
	}
	metrics, ok := NewExpvarNotificationDeliveryMetrics().(*ExpvarNotificationDeliveryMetrics)
	if !ok {
		t.Fatal("expected expvar notification delivery metrics")
	}

	metrics.IncOutcome(" Build_Failed ", " Email ", " Shared_Target ", " due_retry ", NotificationDeliveryOutcomeRetryClaimed)
	key := notificationMetricLabelKey("build_failed", "email", "shared_target", "due_retry", NotificationDeliveryOutcomeRetryClaimed)
	if got := metrics.outcomeTotal.Get(key); got == nil || got.String() != "1" {
		t.Fatalf("expected expvar count 1, got %v", got)
	}
}
