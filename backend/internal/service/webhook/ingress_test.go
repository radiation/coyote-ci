package webhook

import (
	"context"
	"errors"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	memoryrepo "github.com/radiation/coyote-ci/backend/internal/repository/memory"
)

func TestDeliveryIngressService_RegisterReceived_NormalizesAndMarksDuplicate(t *testing.T) {
	ctx := context.Background()
	deliveryRepo := memoryrepo.NewWebhookDeliveryRepository()
	service := NewDeliveryIngressService(deliveryRepo, &recordingWebhookTriggerer{})

	delivery, duplicate, registerErr := service.RegisterReceived(ctx, " GitHub ", " delivery-1 ", " Push ")
	if registerErr != nil {
		t.Fatalf("register delivery: %v", registerErr)
	}
	if duplicate {
		t.Fatal("first delivery should not be duplicate")
	}
	if delivery.Provider != "github" || delivery.DeliveryID != "delivery-1" || readOptionalDeliveryString(delivery.EventType) != "push" {
		t.Fatalf("expected normalized delivery, got %+v", delivery)
	}

	duplicateDelivery, duplicate, duplicateErr := service.RegisterReceived(ctx, "github", "delivery-1", "push")
	if duplicateErr != nil {
		t.Fatalf("register duplicate delivery: %v", duplicateErr)
	}
	if !duplicate {
		t.Fatal("second delivery should be duplicate")
	}
	if duplicateDelivery.ID != delivery.ID {
		t.Fatalf("expected duplicate to return existing delivery ID %q, got %q", delivery.ID, duplicateDelivery.ID)
	}
	if duplicateDelivery.Status != domain.WebhookDeliveryStatusDuplicate {
		t.Fatalf("expected duplicate status, got %q", duplicateDelivery.Status)
	}
}

func TestDeliveryIngressService_ProcessVerifiedEvent_NoMatchMarksIgnored(t *testing.T) {
	ctx := context.Background()
	deliveryRepo := memoryrepo.NewWebhookDeliveryRepository()
	noMatchReason := "no configured job matched ref"
	triggerer := &recordingWebhookTriggerer{result: WebhookTriggerResult{MatchedJobs: 0, NoMatchReason: &noMatchReason}}
	service := NewDeliveryIngressService(deliveryRepo, triggerer)
	delivery := seedReceivedDelivery(t, ctx, deliveryRepo)

	result, processErr := service.ProcessVerifiedEvent(ctx, delivery, sampleWebhookTrigger())
	if processErr != nil {
		t.Fatalf("process verified event: %v", processErr)
	}
	if result.Delivery.Status != domain.WebhookDeliveryStatusIgnoredNoMatch {
		t.Fatalf("expected ignored_no_match status, got %q", result.Delivery.Status)
	}
	if readOptionalDeliveryString(result.Delivery.Reason) != noMatchReason {
		t.Fatalf("expected no-match reason, got %q", readOptionalDeliveryString(result.Delivery.Reason))
	}
	if readOptionalDeliveryString(result.Delivery.RepositoryOwner) != "radiation" || readOptionalDeliveryString(result.Delivery.RefName) != "main" {
		t.Fatalf("expected trigger metadata on delivery, got %+v", result.Delivery)
	}
	if triggerer.calls != 1 {
		t.Fatalf("expected one trigger call, got %d", triggerer.calls)
	}
}

func TestDeliveryIngressService_ProcessVerifiedEvent_MatchedBuildMarksQueued(t *testing.T) {
	ctx := context.Background()
	deliveryRepo := memoryrepo.NewWebhookDeliveryRepository()
	triggerer := &recordingWebhookTriggerer{result: WebhookTriggerResult{
		MatchedJobs: 1,
		Builds: []WebhookMatchedBuild{{
			Job:   domain.Job{ID: "job-1"},
			Build: domain.Build{ID: "build-1"},
		}},
	}}
	service := NewDeliveryIngressService(deliveryRepo, triggerer)
	delivery := seedReceivedDelivery(t, ctx, deliveryRepo)

	result, processErr := service.ProcessVerifiedEvent(ctx, delivery, sampleWebhookTrigger())
	if processErr != nil {
		t.Fatalf("process verified event: %v", processErr)
	}
	if result.Delivery.Status != domain.WebhookDeliveryStatusQueued {
		t.Fatalf("expected queued status, got %q", result.Delivery.Status)
	}
	if readOptionalDeliveryString(result.Delivery.MatchedJobID) != "job-1" || readOptionalDeliveryString(result.Delivery.QueuedBuildID) != "build-1" {
		t.Fatalf("expected matched IDs on delivery, got %+v", result.Delivery)
	}
	if result.Trigger.MatchedJobs != 1 {
		t.Fatalf("expected trigger result to be preserved, got %+v", result.Trigger)
	}
}

func TestDeliveryIngressService_ProcessVerifiedEvent_TriggerFailureMarksFailed(t *testing.T) {
	ctx := context.Background()
	deliveryRepo := memoryrepo.NewWebhookDeliveryRepository()
	triggerErr := errors.New("queue unavailable")
	service := NewDeliveryIngressService(deliveryRepo, &recordingWebhookTriggerer{err: triggerErr})
	delivery := seedReceivedDelivery(t, ctx, deliveryRepo)

	result, processErr := service.ProcessVerifiedEvent(ctx, delivery, sampleWebhookTrigger())
	if !errors.Is(processErr, triggerErr) {
		t.Fatalf("expected trigger error %v, got %v", triggerErr, processErr)
	}
	if result.Delivery.Status != domain.WebhookDeliveryStatusFailed {
		t.Fatalf("expected failed delivery, got %+v", result.Delivery)
	}
	if readOptionalDeliveryString(result.Delivery.Reason) != triggerErr.Error() {
		t.Fatalf("expected trigger error reason, got %q", readOptionalDeliveryString(result.Delivery.Reason))
	}
}

func TestDeliveryIngressService_MarkUnsupportedRecordsTriggerMetadata(t *testing.T) {
	ctx := context.Background()
	deliveryRepo := memoryrepo.NewWebhookDeliveryRepository()
	service := NewDeliveryIngressService(deliveryRepo, &recordingWebhookTriggerer{})
	delivery := seedReceivedDelivery(t, ctx, deliveryRepo)

	updated, markErr := service.MarkUnsupported(ctx, delivery, "unsupported event", sampleWebhookTrigger())
	if markErr != nil {
		t.Fatalf("mark unsupported: %v", markErr)
	}
	if updated.Status != domain.WebhookDeliveryStatusUnsupported {
		t.Fatalf("expected unsupported status, got %q", updated.Status)
	}
	if readOptionalDeliveryString(updated.Reason) != "unsupported event" || readOptionalDeliveryString(updated.EventType) != "push" {
		t.Fatalf("expected unsupported metadata, got %+v", updated)
	}
}

func seedReceivedDelivery(t *testing.T, ctx context.Context, repo *memoryrepo.WebhookDeliveryRepository) domain.WebhookDelivery {
	t.Helper()
	delivery, createErr := repo.Create(ctx, domain.WebhookDelivery{
		ID:         "delivery-row-1",
		Provider:   "github",
		DeliveryID: "delivery-1",
		Status:     domain.WebhookDeliveryStatusReceived,
	})
	if createErr != nil {
		t.Fatalf("seed delivery: %v", createErr)
	}
	return delivery
}

func sampleWebhookTrigger() WebhookTriggerInput {
	return WebhookTriggerInput{
		SCMProvider:     "github",
		EventType:       "push",
		RepositoryOwner: "radiation",
		RepositoryName:  "coyote-ci",
		RepositoryURL:   "https://github.com/radiation/coyote-ci.git",
		RawRef:          "refs/heads/main",
		Ref:             "refs/heads/main",
		RefType:         "branch",
		RefName:         "main",
		CommitSHA:       "abc123",
		DeliveryID:      "delivery-1",
		Actor:           "octocat",
	}
}

type recordingWebhookTriggerer struct {
	result WebhookTriggerResult
	err    error
	calls  int
}

func (t *recordingWebhookTriggerer) TriggerWebhookEvent(context.Context, WebhookTriggerInput) (WebhookTriggerResult, error) {
	t.calls++
	if t.err != nil {
		return WebhookTriggerResult{}, t.err
	}
	return t.result, nil
}
