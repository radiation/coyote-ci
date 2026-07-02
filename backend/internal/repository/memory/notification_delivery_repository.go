package memory

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type NotificationDeliveryRepository struct {
	mu         sync.RWMutex
	deliveries map[string]domain.NotificationDelivery
	index      map[string]string
	recipient  map[string]string
}

func NewNotificationDeliveryRepository() *NotificationDeliveryRepository {
	return &NotificationDeliveryRepository{
		deliveries: make(map[string]domain.NotificationDelivery),
		index:      make(map[string]string),
		recipient:  make(map[string]string),
	}
}

func (r *NotificationDeliveryRepository) Create(ctx context.Context, delivery domain.NotificationDelivery) (domain.NotificationDelivery, error) {
	if err := ctx.Err(); err != nil {
		return domain.NotificationDelivery{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	delivery = delivery.Normalize()
	if err := delivery.ValidateIdentity(); err != nil {
		return domain.NotificationDelivery{}, err
	}
	key := notificationDeliveryLogicalKey(delivery.BuildID, delivery.EventType, delivery.Transport, delivery.DestinationKey)
	if _, exists := r.index[key]; exists {
		return domain.NotificationDelivery{}, repository.ErrNotificationDeliveryDuplicate
	}
	now := time.Now().UTC()
	if strings.TrimSpace(delivery.ID) == "" {
		delivery.ID = uuid.NewString()
	}
	if delivery.CreatedAt.IsZero() {
		delivery.CreatedAt = now
	}
	if delivery.UpdatedAt.IsZero() {
		delivery.UpdatedAt = delivery.CreatedAt
	}
	if delivery.MaxAttempts <= 0 {
		delivery.MaxAttempts = 1
	}
	if strings.TrimSpace(string(delivery.Status)) == "" {
		delivery.Status = domain.NotificationDeliveryStatusPending
	}
	if err := delivery.Validate(); err != nil {
		return domain.NotificationDelivery{}, err
	}
	r.deliveries[delivery.ID] = delivery
	r.index[key] = delivery.ID
	recipientKey := notificationDeliveryRecipientKey(delivery.BuildID, delivery.EventType, delivery.Recipient)
	if recipientKey != "||" {
		r.recipient[recipientKey] = delivery.ID
	}
	return delivery, nil
}

func (r *NotificationDeliveryRepository) AcquireForDelivery(ctx context.Context, input repository.NotificationDeliveryClaimInput) (repository.NotificationDeliveryClaimResult, error) {
	if err := ctx.Err(); err != nil {
		return repository.NotificationDeliveryClaimResult{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	delivery, now, claimOwner, claimDuration, err := normalizeNotificationClaimInput(input)
	if err != nil {
		return repository.NotificationDeliveryClaimResult{}, err
	}

	key := notificationDeliveryLogicalKey(delivery.BuildID, delivery.EventType, delivery.Transport, delivery.DestinationKey)
	if existingID, exists := r.index[key]; exists {
		existing := r.deliveries[existingID]
		claimed, outcome := claimNotificationDelivery(existing, now, claimOwner, claimDuration)
		if outcome == repository.NotificationDeliveryClaimOutcomeRetryClaimed || outcome == repository.NotificationDeliveryClaimOutcomeStaleClaimReclaimed {
			r.deliveries[existingID] = claimed
			return repository.NotificationDeliveryClaimResult{Delivery: claimed, Outcome: outcome}, nil
		}
		return repository.NotificationDeliveryClaimResult{Delivery: claimed, Outcome: outcome}, nil
	}

	if strings.TrimSpace(delivery.ID) == "" {
		delivery.ID = uuid.NewString()
	}
	if delivery.CreatedAt.IsZero() {
		delivery.CreatedAt = now
	}
	delivery.UpdatedAt = now
	delivery.Status = domain.NotificationDeliveryStatusSending
	delivery.Attempts = 1
	delivery.MaxAttempts = input.MaxAttempts
	delivery.LastAttemptAt = &now
	delivery.NextAttemptAt = nil
	delivery.ClaimedAt = &now
	claimExpiresAt := now.Add(claimDuration)
	delivery.ClaimExpiresAt = &claimExpiresAt
	delivery.ClaimedBy = &claimOwner
	delivery.FailureCategory = nil
	delivery.FailureReason = nil
	delivery.LastError = nil
	delivery.SentAt = nil
	if err := delivery.Validate(); err != nil {
		return repository.NotificationDeliveryClaimResult{}, err
	}

	r.deliveries[delivery.ID] = delivery
	r.index[key] = delivery.ID
	recipientKey := notificationDeliveryRecipientKey(delivery.BuildID, delivery.EventType, delivery.Recipient)
	if recipientKey != "||" {
		r.recipient[recipientKey] = delivery.ID
	}
	return repository.NotificationDeliveryClaimResult{
		Delivery: delivery,
		Outcome:  repository.NotificationDeliveryClaimOutcomeCreatedClaimed,
	}, nil
}

func (r *NotificationDeliveryRepository) GetByBuildEventRecipient(ctx context.Context, buildID string, eventType domain.NotificationEventType, recipient string) (domain.NotificationDelivery, error) {
	if err := ctx.Err(); err != nil {
		return domain.NotificationDelivery{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	key := notificationDeliveryRecipientKey(buildID, eventType, recipient)
	id, ok := r.recipient[key]
	if !ok {
		return domain.NotificationDelivery{}, repository.ErrNotificationDeliveryNotFound
	}

	delivery, ok := r.deliveries[id]
	if !ok {
		return domain.NotificationDelivery{}, repository.ErrNotificationDeliveryNotFound
	}

	return delivery, nil
}

func (r *NotificationDeliveryRepository) MarkSent(ctx context.Context, input repository.NotificationDeliveryMarkSentInput) (repository.NotificationDeliveryUpdateResult, error) {
	if err := ctx.Err(); err != nil {
		return repository.NotificationDeliveryUpdateResult{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	current, ok := r.deliveries[strings.TrimSpace(input.DeliveryID)]
	if !ok {
		return repository.NotificationDeliveryUpdateResult{}, repository.ErrNotificationDeliveryNotFound
	}
	if !notificationDeliveryClaimMatches(current, strings.TrimSpace(input.ClaimOwner), input.ClaimedAt) {
		return repository.NotificationDeliveryUpdateResult{Delivery: current, Outcome: repository.NotificationDeliveryUpdateOutcomeLostClaim}, nil
	}
	sentAt := input.SentAt.UTC()
	current.Status = domain.NotificationDeliveryStatusSent
	current.UpdatedAt = sentAt
	current.SentAt = &sentAt
	current.NextAttemptAt = nil
	current.ClaimedAt = nil
	current.ClaimExpiresAt = nil
	current.ClaimedBy = nil
	current.FailureCategory = nil
	current.FailureReason = nil
	current.LastError = nil
	if err := current.Validate(); err != nil {
		return repository.NotificationDeliveryUpdateResult{}, err
	}
	r.deliveries[current.ID] = current
	return repository.NotificationDeliveryUpdateResult{Delivery: current, Outcome: repository.NotificationDeliveryUpdateOutcomeUpdated}, nil
}

func (r *NotificationDeliveryRepository) Update(ctx context.Context, delivery domain.NotificationDelivery) (domain.NotificationDelivery, error) {
	if err := ctx.Err(); err != nil {
		return domain.NotificationDelivery{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	current, ok := r.deliveries[strings.TrimSpace(delivery.ID)]
	if !ok {
		return domain.NotificationDelivery{}, repository.ErrNotificationDeliveryNotFound
	}
	delivery = delivery.Normalize()
	if delivery.BuildID == "" {
		delivery.BuildID = current.BuildID
	}
	if delivery.EventType == "" {
		delivery.EventType = current.EventType
	}
	if delivery.Transport == "" {
		delivery.Transport = current.Transport
	}
	if delivery.DestinationKind == "" {
		delivery.DestinationKind = current.DestinationKind
	}
	if delivery.DestinationKey == "" {
		delivery.DestinationKey = current.DestinationKey
	}
	if delivery.NotificationTargetID == nil {
		delivery.NotificationTargetID = current.NotificationTargetID
	}
	if delivery.RecipientUserID == nil {
		delivery.RecipientUserID = current.RecipientUserID
	}
	if delivery.SlackWorkspaceIntegrationID == nil {
		delivery.SlackWorkspaceIntegrationID = current.SlackWorkspaceIntegrationID
	}
	if delivery.Recipient == "" {
		delivery.Recipient = current.Recipient
	}
	if delivery.CreatedAt.IsZero() {
		delivery.CreatedAt = current.CreatedAt
	}
	if delivery.UpdatedAt.IsZero() {
		delivery.UpdatedAt = time.Now().UTC()
	}
	if delivery.MaxAttempts <= 0 {
		delivery.MaxAttempts = current.MaxAttempts
	}
	if err := delivery.Validate(); err != nil {
		return domain.NotificationDelivery{}, err
	}
	r.deliveries[delivery.ID] = delivery
	return delivery, nil
}

func (r *NotificationDeliveryRepository) RecordRetryableFailure(ctx context.Context, input repository.NotificationDeliveryRecordFailureInput) (repository.NotificationDeliveryUpdateResult, error) {
	return r.recordFailure(ctx, input, domain.NotificationDeliveryStatusRetryWaiting)
}

func (r *NotificationDeliveryRepository) RecordPermanentFailure(ctx context.Context, input repository.NotificationDeliveryRecordFailureInput) (repository.NotificationDeliveryUpdateResult, error) {
	return r.recordFailure(ctx, input, domain.NotificationDeliveryStatusFailedPermanent)
}

func (r *NotificationDeliveryRepository) RecordExhaustedFailure(ctx context.Context, input repository.NotificationDeliveryRecordFailureInput) (repository.NotificationDeliveryUpdateResult, error) {
	return r.recordFailure(ctx, input, domain.NotificationDeliveryStatusFailedExhausted)
}

func (r *NotificationDeliveryRepository) recordFailure(ctx context.Context, input repository.NotificationDeliveryRecordFailureInput, status domain.NotificationDeliveryStatus) (repository.NotificationDeliveryUpdateResult, error) {
	if err := ctx.Err(); err != nil {
		return repository.NotificationDeliveryUpdateResult{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	current, ok := r.deliveries[strings.TrimSpace(input.DeliveryID)]
	if !ok {
		return repository.NotificationDeliveryUpdateResult{}, repository.ErrNotificationDeliveryNotFound
	}
	if !notificationDeliveryClaimMatches(current, strings.TrimSpace(input.ClaimOwner), input.ClaimedAt) {
		return repository.NotificationDeliveryUpdateResult{Delivery: current, Outcome: repository.NotificationDeliveryUpdateOutcomeLostClaim}, nil
	}
	failedAt := input.FailedAt.UTC()
	current.Status = status
	current.UpdatedAt = failedAt
	current.NextAttemptAt = normalizeNotificationRecordFailureTime(input.NextAttemptAt)
	current.ClaimedAt = nil
	current.ClaimExpiresAt = nil
	current.ClaimedBy = nil
	category := input.FailureCategory
	current.FailureCategory = &category
	reason := strings.TrimSpace(input.FailureReason)
	if reason == "" {
		current.FailureReason = nil
	} else {
		current.FailureReason = &reason
	}
	current.LastError = trimMemoryNotificationOptionalString(input.LastError)
	if status != domain.NotificationDeliveryStatusRetryWaiting {
		current.NextAttemptAt = nil
	}
	if status == domain.NotificationDeliveryStatusFailedExhausted && current.Attempts < current.MaxAttempts {
		current.Attempts = current.MaxAttempts
	}
	if err := current.Validate(); err != nil {
		return repository.NotificationDeliveryUpdateResult{}, err
	}
	r.deliveries[current.ID] = current
	return repository.NotificationDeliveryUpdateResult{Delivery: current, Outcome: repository.NotificationDeliveryUpdateOutcomeUpdated}, nil
}

func notificationDeliveryLogicalKey(buildID string, eventType domain.NotificationEventType, transport domain.NotificationTransport, destinationKey string) string {
	return strings.TrimSpace(buildID) + "|" + strings.TrimSpace(string(eventType)) + "|" + strings.TrimSpace(string(transport)) + "|" + strings.TrimSpace(destinationKey)
}

func notificationDeliveryRecipientKey(buildID string, eventType domain.NotificationEventType, recipient string) string {
	return strings.TrimSpace(buildID) + "|" + strings.TrimSpace(string(eventType)) + "|" + strings.TrimSpace(recipient)
}

func normalizeNotificationClaimInput(input repository.NotificationDeliveryClaimInput) (domain.NotificationDelivery, time.Time, string, time.Duration, error) {
	delivery := input.Delivery.Normalize()
	if err := delivery.ValidateIdentity(); err != nil {
		return domain.NotificationDelivery{}, time.Time{}, "", 0, err
	}
	now := input.Now.UTC()
	if now.IsZero() {
		return domain.NotificationDelivery{}, time.Time{}, "", 0, context.Canceled
	}
	claimOwner := strings.TrimSpace(input.ClaimOwner)
	if claimOwner == "" {
		return domain.NotificationDelivery{}, time.Time{}, "", 0, errors.New("notification delivery claim owner is required")
	}
	if input.ClaimDuration <= 0 {
		return domain.NotificationDelivery{}, time.Time{}, "", 0, errors.New("notification delivery claim duration must be positive")
	}
	if input.MaxAttempts <= 0 {
		return domain.NotificationDelivery{}, time.Time{}, "", 0, errors.New("notification delivery max attempts must be positive")
	}
	return delivery, now, claimOwner, input.ClaimDuration, nil
}

func claimNotificationDelivery(existing domain.NotificationDelivery, now time.Time, claimOwner string, claimDuration time.Duration) (domain.NotificationDelivery, repository.NotificationDeliveryClaimOutcome) {
	existing = existing.Normalize()
	if existing.IsTerminal() {
		return existing, repository.NotificationDeliveryClaimOutcomeFromExisting(existing, now)
	}
	if existing.Attempts >= existing.MaxAttempts {
		existing.Status = domain.NotificationDeliveryStatusFailedExhausted
		return existing, repository.NotificationDeliveryClaimOutcomeAttemptsExhausted
	}
	switch existing.Status {
	case domain.NotificationDeliveryStatusSending:
		if existing.ClaimExpiresAt != nil && now.Before(existing.ClaimExpiresAt.UTC()) {
			return existing, repository.NotificationDeliveryClaimOutcomeClaimedByOther
		}
		existing.Attempts++
		existing.Status = domain.NotificationDeliveryStatusSending
		existing.LastAttemptAt = &now
		existing.NextAttemptAt = nil
		existing.ClaimedAt = &now
		claimExpiresAt := now.Add(claimDuration)
		existing.ClaimExpiresAt = &claimExpiresAt
		existing.ClaimedBy = &claimOwner
		existing.UpdatedAt = now
		return existing, repository.NotificationDeliveryClaimOutcomeStaleClaimReclaimed
	case domain.NotificationDeliveryStatusRetryWaiting, domain.NotificationDeliveryStatusPending:
		if existing.Status == domain.NotificationDeliveryStatusRetryWaiting && existing.NextAttemptAt != nil && now.Before(existing.NextAttemptAt.UTC()) {
			return existing, repository.NotificationDeliveryClaimOutcomeRetryNotDue
		}
		existing.Attempts++
		existing.Status = domain.NotificationDeliveryStatusSending
		existing.LastAttemptAt = &now
		existing.NextAttemptAt = nil
		existing.ClaimedAt = &now
		claimExpiresAt := now.Add(claimDuration)
		existing.ClaimExpiresAt = &claimExpiresAt
		existing.ClaimedBy = &claimOwner
		existing.UpdatedAt = now
		return existing, repository.NotificationDeliveryClaimOutcomeRetryClaimed
	default:
		return existing, repository.NotificationDeliveryClaimOutcomeFromExisting(existing, now)
	}
}

func notificationDeliveryClaimMatches(delivery domain.NotificationDelivery, claimOwner string, claimedAt time.Time) bool {
	if delivery.Status != domain.NotificationDeliveryStatusSending {
		return false
	}
	if delivery.ClaimedBy == nil || delivery.ClaimedAt == nil {
		return false
	}
	return strings.TrimSpace(*delivery.ClaimedBy) == claimOwner && delivery.ClaimedAt.UTC().Equal(claimedAt.UTC())
}

func normalizeNotificationRecordFailureTime(value *time.Time) *time.Time {
	if value == nil || value.IsZero() {
		return nil
	}
	utc := value.UTC()
	return &utc
}

func trimMemoryNotificationOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
