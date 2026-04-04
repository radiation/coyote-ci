package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

var ErrWebhookDeliveryRepoNotConfigured = errors.New("webhook delivery repository not configured")
var ErrWebhookDeliveryIDRequired = errors.New("webhook delivery_id is required")
var ErrWebhookProviderRequired = errors.New("webhook provider is required")
var ErrWebhookInvalidDeliveryTransition = errors.New("invalid webhook delivery transition")

type WebhookIngressService struct {
	deliveryRepo repository.WebhookDeliveryRepository
	jobService   *JobService
}

type WebhookIngressResult struct {
	Duplicate bool
	Delivery  domain.WebhookDelivery
	Trigger   WebhookTriggerResult
}

func NewWebhookIngressService(deliveryRepo repository.WebhookDeliveryRepository, jobService *JobService) *WebhookIngressService {
	return &WebhookIngressService{deliveryRepo: deliveryRepo, jobService: jobService}
}

func (s *WebhookIngressService) RegisterReceived(ctx context.Context, provider string, deliveryID string, eventType string) (domain.WebhookDelivery, bool, error) {
	if s.deliveryRepo == nil {
		return domain.WebhookDelivery{}, false, ErrWebhookDeliveryRepoNotConfigured
	}
	provider = strings.ToLower(strings.TrimSpace(provider))
	deliveryID = strings.TrimSpace(deliveryID)
	if provider == "" {
		return domain.WebhookDelivery{}, false, ErrWebhookProviderRequired
	}
	if deliveryID == "" {
		return domain.WebhookDelivery{}, false, ErrWebhookDeliveryIDRequired
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

func (s *WebhookIngressService) MarkFailed(ctx context.Context, delivery domain.WebhookDelivery, reason string) (domain.WebhookDelivery, error) {
	reasonPtr := optionalString(reason)
	return s.updateDelivery(ctx, delivery, domain.WebhookDeliveryStatusFailed, reasonPtr)
}

func (s *WebhookIngressService) MarkUnsupported(ctx context.Context, delivery domain.WebhookDelivery, reason string, trigger WebhookTriggerInput) (domain.WebhookDelivery, error) {
	delivery = applyTriggerMetadata(delivery, trigger)
	reasonPtr := optionalString(reason)
	return s.updateDelivery(ctx, delivery, domain.WebhookDeliveryStatusUnsupported, reasonPtr)
}

func (s *WebhookIngressService) ProcessVerifiedEvent(ctx context.Context, delivery domain.WebhookDelivery, trigger WebhookTriggerInput) (WebhookIngressResult, error) {
	if s.deliveryRepo == nil {
		return WebhookIngressResult{}, ErrWebhookDeliveryRepoNotConfigured
	}

	delivery = applyTriggerMetadata(delivery, trigger)
	verified, err := s.updateDelivery(ctx, delivery, domain.WebhookDeliveryStatusVerified, nil)
	if err != nil {
		return WebhookIngressResult{}, err
	}

	triggerResult, triggerErr := s.jobService.TriggerWebhookEvent(ctx, trigger)
	if triggerErr != nil {
		failed, _ := s.MarkFailed(ctx, verified, triggerErr.Error())
		return WebhookIngressResult{Delivery: failed}, triggerErr
	}

	if triggerResult.MatchedJobs == 0 {
		ignored, ignoredErr := s.updateDelivery(ctx, verified, domain.WebhookDeliveryStatusIgnoredNoMatch, nil)
		if ignoredErr != nil {
			return WebhookIngressResult{}, ignoredErr
		}
		return WebhookIngressResult{Delivery: ignored, Trigger: triggerResult}, nil
	}

	matched := verified
	firstMatchedJobID, firstBuildID := firstMatchIDs(triggerResult)
	matched.MatchedJobID = firstMatchedJobID
	matched.QueuedBuildID = firstBuildID
	matched, err = s.updateDelivery(ctx, matched, domain.WebhookDeliveryStatusMatched, nil)
	if err != nil {
		return WebhookIngressResult{}, err
	}

	queued, err := s.updateDelivery(ctx, matched, domain.WebhookDeliveryStatusQueued, nil)
	if err != nil {
		return WebhookIngressResult{}, err
	}

	return WebhookIngressResult{Delivery: queued, Trigger: triggerResult}, nil
}

func (s *WebhookIngressService) updateDelivery(ctx context.Context, delivery domain.WebhookDelivery, next domain.WebhookDeliveryStatus, reason *string) (domain.WebhookDelivery, error) {
	if s.deliveryRepo == nil {
		return domain.WebhookDelivery{}, ErrWebhookDeliveryRepoNotConfigured
	}
	if !isAllowedWebhookTransition(delivery.Status, next) {
		return domain.WebhookDelivery{}, ErrWebhookInvalidDeliveryTransition
	}
	delivery.Status = next
	delivery.Reason = reason
	delivery.UpdatedAt = time.Now().UTC()
	return s.deliveryRepo.Update(ctx, delivery)
}

func isAllowedWebhookTransition(from domain.WebhookDeliveryStatus, to domain.WebhookDeliveryStatus) bool {
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

func applyTriggerMetadata(delivery domain.WebhookDelivery, trigger WebhookTriggerInput) domain.WebhookDelivery {
	delivery.EventType = optionalString(strings.ToLower(strings.TrimSpace(trigger.EventType)))
	delivery.RepositoryOwner = optionalString(trigger.RepositoryOwner)
	delivery.RepositoryName = optionalString(trigger.RepositoryName)
	delivery.TriggerRef = optionalString(trigger.Ref)
	delivery.CommitSHA = optionalString(trigger.CommitSHA)
	delivery.Actor = optionalString(trigger.Actor)
	return delivery
}

func firstMatchIDs(result WebhookTriggerResult) (*string, *string) {
	if len(result.Builds) == 0 {
		return nil, nil
	}
	jobID := strings.TrimSpace(result.Builds[0].Job.ID)
	buildID := strings.TrimSpace(result.Builds[0].Build.ID)
	return optionalString(jobID), optionalString(buildID)
}

func optionalString(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
