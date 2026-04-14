package webhook

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/observability"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

var ErrDeliveryRepoNotConfigured = errors.New("webhook delivery repository not configured")
var ErrDeliveryIDRequired = errors.New("webhook delivery_id is required")
var ErrDeliveryProviderRequired = errors.New("webhook provider is required")
var ErrInvalidDeliveryTransition = errors.New("invalid webhook delivery transition")

type DeliveryIngressService struct {
	deliveryRepo repository.WebhookDeliveryRepository
	jobService   DeliveryEventTriggerer
	metrics      observability.WebhookIngressMetrics
}

type DeliveryEventTriggerer interface {
	TriggerWebhookEvent(ctx context.Context, input WebhookTriggerInput) (WebhookTriggerResult, error)
}

type DeliveryIngressResult struct {
	Duplicate bool
	Delivery  domain.WebhookDelivery
	Trigger   WebhookTriggerResult
}

func NewDeliveryIngressService(deliveryRepo repository.WebhookDeliveryRepository, jobService DeliveryEventTriggerer) *DeliveryIngressService {
	return &DeliveryIngressService{deliveryRepo: deliveryRepo, jobService: jobService, metrics: observability.NewNoopWebhookIngressMetrics()}
}

func (s *DeliveryIngressService) SetMetrics(metrics observability.WebhookIngressMetrics) {
	if metrics == nil {
		s.metrics = observability.NewNoopWebhookIngressMetrics()
		return
	}
	s.metrics = metrics
}

func (s *DeliveryIngressService) RegisterReceived(ctx context.Context, provider string, deliveryID string, eventType string) (domain.WebhookDelivery, bool, error) {
	if s.deliveryRepo == nil {
		return domain.WebhookDelivery{}, false, ErrDeliveryRepoNotConfigured
	}
	provider = strings.ToLower(strings.TrimSpace(provider))
	deliveryID = strings.TrimSpace(deliveryID)
	if provider == "" {
		return domain.WebhookDelivery{}, false, ErrDeliveryProviderRequired
	}
	if deliveryID == "" {
		return domain.WebhookDelivery{}, false, ErrDeliveryIDRequired
	}

	now := time.Now().UTC()
	delivery := domain.WebhookDelivery{
		ID:         uuid.NewString(),
		Provider:   provider,
		DeliveryID: deliveryID,
		Status:     domain.WebhookDeliveryStatusReceived,
		ReceivedAt: now,
		UpdatedAt:  now,
	}
	if strings.TrimSpace(eventType) != "" {
		normalized := strings.ToLower(strings.TrimSpace(eventType))
		delivery.EventType = &normalized
	}

	created, err := s.deliveryRepo.Create(ctx, delivery)
	if err == nil {
		return created, false, nil
	}
	if !errors.Is(err, repository.ErrWebhookDeliveryDuplicate) {
		return domain.WebhookDelivery{}, false, err
	}

	existing, getErr := s.deliveryRepo.GetByProviderDeliveryID(ctx, provider, deliveryID)
	if getErr != nil {
		return domain.WebhookDelivery{}, false, getErr
	}

	updated, updateErr := s.updateDelivery(ctx, existing, domain.WebhookDeliveryStatusDuplicate, existing.Reason)
	if updateErr != nil {
		return existing, true, nil
	}
	return updated, true, nil
}

func (s *DeliveryIngressService) MarkFailed(ctx context.Context, delivery domain.WebhookDelivery, reason string) (domain.WebhookDelivery, error) {
	reasonPtr := optionalDeliveryString(reason)
	return s.updateDelivery(ctx, delivery, domain.WebhookDeliveryStatusFailed, reasonPtr)
}

func (s *DeliveryIngressService) MarkUnsupported(ctx context.Context, delivery domain.WebhookDelivery, reason string, trigger WebhookTriggerInput) (domain.WebhookDelivery, error) {
	delivery = applyDeliveryTriggerMetadata(delivery, trigger)
	reasonPtr := optionalDeliveryString(reason)
	return s.updateDelivery(ctx, delivery, domain.WebhookDeliveryStatusUnsupported, reasonPtr)
}

func (s *DeliveryIngressService) ProcessVerifiedEvent(ctx context.Context, delivery domain.WebhookDelivery, trigger WebhookTriggerInput) (DeliveryIngressResult, error) {
	if s.deliveryRepo == nil {
		return DeliveryIngressResult{}, ErrDeliveryRepoNotConfigured
	}

	delivery = applyDeliveryTriggerMetadata(delivery, trigger)
	verified, err := s.updateDelivery(ctx, delivery, domain.WebhookDeliveryStatusVerified, nil)
	if err != nil {
		return DeliveryIngressResult{}, err
	}
	s.metrics.IncOutcome(verified.Provider, readOptionalDeliveryString(verified.EventType), observability.WebhookOutcomeDeliveriesVerified)

	triggerResult, triggerErr := s.jobService.TriggerWebhookEvent(ctx, trigger)
	if triggerErr != nil {
		failed, _ := s.MarkFailed(ctx, verified, triggerErr.Error())
		return DeliveryIngressResult{Delivery: failed}, triggerErr
	}

	if triggerResult.MatchedJobs == 0 {
		ignored, ignoredErr := s.updateDelivery(ctx, verified, domain.WebhookDeliveryStatusIgnoredNoMatch, triggerResult.NoMatchReason)
		if ignoredErr != nil {
			return DeliveryIngressResult{}, ignoredErr
		}
		return DeliveryIngressResult{Delivery: ignored, Trigger: triggerResult}, nil
	}

	matched := verified
	firstMatchedJobID, firstBuildID := firstMatchedIDs(triggerResult)
	matched.MatchedJobID = firstMatchedJobID
	matched.QueuedBuildID = firstBuildID
	matched, err = s.updateDelivery(ctx, matched, domain.WebhookDeliveryStatusMatched, nil)
	if err != nil {
		return DeliveryIngressResult{}, err
	}

	queued, err := s.updateDelivery(ctx, matched, domain.WebhookDeliveryStatusQueued, nil)
	if err != nil {
		return DeliveryIngressResult{}, err
	}

	return DeliveryIngressResult{Delivery: queued, Trigger: triggerResult}, nil
}

func (s *DeliveryIngressService) updateDelivery(ctx context.Context, delivery domain.WebhookDelivery, next domain.WebhookDeliveryStatus, reason *string) (domain.WebhookDelivery, error) {
	if s.deliveryRepo == nil {
		return domain.WebhookDelivery{}, ErrDeliveryRepoNotConfigured
	}
	if !isAllowedDeliveryTransition(delivery.Status, next) {
		return domain.WebhookDelivery{}, ErrInvalidDeliveryTransition
	}
	delivery.Status = next
	delivery.Reason = reason
	delivery.UpdatedAt = time.Now().UTC()
	return s.deliveryRepo.Update(ctx, delivery)
}

func isAllowedDeliveryTransition(from domain.WebhookDeliveryStatus, to domain.WebhookDeliveryStatus) bool {
	allowed := map[domain.WebhookDeliveryStatus]map[domain.WebhookDeliveryStatus]bool{
		domain.WebhookDeliveryStatusReceived: {
			domain.WebhookDeliveryStatusVerified:    true,
			domain.WebhookDeliveryStatusUnsupported: true,
			domain.WebhookDeliveryStatusDuplicate:   true,
			domain.WebhookDeliveryStatusFailed:      true,
		},
		domain.WebhookDeliveryStatusVerified: {
			domain.WebhookDeliveryStatusMatched:        true,
			domain.WebhookDeliveryStatusIgnoredNoMatch: true,
			domain.WebhookDeliveryStatusQueued:         true,
			domain.WebhookDeliveryStatusFailed:         true,
			domain.WebhookDeliveryStatusDuplicate:      true,
		},
		domain.WebhookDeliveryStatusMatched: {
			domain.WebhookDeliveryStatusQueued:    true,
			domain.WebhookDeliveryStatusFailed:    true,
			domain.WebhookDeliveryStatusDuplicate: true,
		},
		domain.WebhookDeliveryStatusQueued: {
			domain.WebhookDeliveryStatusDuplicate: true,
		},
		domain.WebhookDeliveryStatusUnsupported: {
			domain.WebhookDeliveryStatusDuplicate: true,
		},
		domain.WebhookDeliveryStatusIgnoredNoMatch: {
			domain.WebhookDeliveryStatusDuplicate: true,
		},
		domain.WebhookDeliveryStatusFailed: {
			domain.WebhookDeliveryStatusDuplicate: true,
		},
		domain.WebhookDeliveryStatusDuplicate: {
			domain.WebhookDeliveryStatusDuplicate: true,
		},
	}
	if _, ok := allowed[from]; !ok {
		return from == to
	}
	return allowed[from][to]
}

func applyDeliveryTriggerMetadata(delivery domain.WebhookDelivery, trigger WebhookTriggerInput) domain.WebhookDelivery {
	delivery.EventType = optionalDeliveryString(strings.ToLower(strings.TrimSpace(trigger.EventType)))
	delivery.RepositoryOwner = optionalDeliveryString(trigger.RepositoryOwner)
	delivery.RepositoryName = optionalDeliveryString(trigger.RepositoryName)
	delivery.RawRef = optionalDeliveryString(trigger.RawRef)
	delivery.RefType = optionalDeliveryString(trigger.RefType)
	delivery.RefName = optionalDeliveryString(trigger.RefName)
	delivery.TriggerRef = optionalDeliveryString(trigger.Ref)
	delivery.Deleted = &trigger.Deleted
	delivery.CommitSHA = optionalDeliveryString(trigger.CommitSHA)
	delivery.Actor = optionalDeliveryString(trigger.Actor)
	return delivery
}

func firstMatchedIDs(result WebhookTriggerResult) (*string, *string) {
	if len(result.Builds) == 0 {
		return nil, nil
	}
	jobID := strings.TrimSpace(result.Builds[0].Job.ID)
	buildID := strings.TrimSpace(result.Builds[0].Build.ID)
	return optionalDeliveryString(jobID), optionalDeliveryString(buildID)
}

func optionalDeliveryString(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func readOptionalDeliveryString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
