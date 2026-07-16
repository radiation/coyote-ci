package memory

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type SCMStatusDeliveryRepository struct {
	mu         sync.RWMutex
	deliveries map[string]domain.SCMStatusDelivery
	index      map[string]string
}

func NewSCMStatusDeliveryRepository() *SCMStatusDeliveryRepository {
	return &SCMStatusDeliveryRepository{
		deliveries: make(map[string]domain.SCMStatusDelivery),
		index:      make(map[string]string),
	}
}

func (r *SCMStatusDeliveryRepository) AcquireForDelivery(ctx context.Context, input repository.SCMStatusDeliveryClaimInput) (repository.SCMStatusDeliveryClaimResult, error) {
	if err := ctx.Err(); err != nil {
		return repository.SCMStatusDeliveryClaimResult{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	delivery, now, claimOwner, claimDuration, err := normalizeSCMClaimInput(input)
	if err != nil {
		return repository.SCMStatusDeliveryClaimResult{}, err
	}

	key := scmStatusDeliveryLogicalKey(delivery.BuildID, delivery.Provider, delivery.Context, delivery.DesiredState)
	if existingID, exists := r.index[key]; exists {
		existing := r.deliveries[existingID]
		claimed, outcome := claimSCMStatusDelivery(existing, now, claimOwner, claimDuration)
		if outcome == repository.SCMStatusDeliveryClaimOutcomeRetryClaimed || outcome == repository.SCMStatusDeliveryClaimOutcomeStaleClaimReclaimed {
			r.deliveries[existingID] = claimed
		}
		return repository.SCMStatusDeliveryClaimResult{Delivery: claimed, Outcome: outcome}, nil
	}

	if strings.TrimSpace(delivery.ID) == "" {
		delivery.ID = uuid.NewString()
	}
	if delivery.CreatedAt.IsZero() {
		delivery.CreatedAt = now
	}
	delivery.UpdatedAt = now
	delivery.Status = domain.SCMStatusDeliveryStatusSending
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
	delivery.SupersededAt = nil
	delivery.LastSentState = nil
	if err := delivery.Validate(); err != nil {
		return repository.SCMStatusDeliveryClaimResult{}, err
	}

	r.deliveries[delivery.ID] = delivery
	r.index[key] = delivery.ID
	return repository.SCMStatusDeliveryClaimResult{Delivery: delivery, Outcome: repository.SCMStatusDeliveryClaimOutcomeCreatedClaimed}, nil
}

func (r *SCMStatusDeliveryRepository) ListRecoverable(ctx context.Context, input repository.SCMStatusDeliveryRecoverableScanInput) ([]domain.SCMStatusDelivery, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	now := input.Now.UTC()
	if now.IsZero() {
		return nil, errors.New("scm status delivery recoverable scan time is required")
	}
	if input.Limit <= 0 {
		return nil, errors.New("scm status delivery recoverable scan limit must be positive")
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	type candidate struct {
		delivery domain.SCMStatusDelivery
		dueAt    time.Time
	}
	candidates := make([]candidate, 0, len(r.deliveries))
	for _, delivery := range r.deliveries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		dueAt, ok := scmStatusDeliveryRecoverableDueAt(delivery, now)
		if !ok {
			continue
		}
		candidates = append(candidates, candidate{delivery: delivery, dueAt: dueAt})
	}

	sort.Slice(candidates, func(i int, j int) bool {
		if !candidates[i].dueAt.Equal(candidates[j].dueAt) {
			return candidates[i].dueAt.Before(candidates[j].dueAt)
		}
		return candidates[i].delivery.ID < candidates[j].delivery.ID
	})
	if len(candidates) > input.Limit {
		candidates = candidates[:input.Limit]
	}
	result := make([]domain.SCMStatusDelivery, 0, len(candidates))
	for _, candidate := range candidates {
		result = append(result, candidate.delivery)
	}
	return result, nil
}

func (r *SCMStatusDeliveryRepository) MarkSent(ctx context.Context, input repository.SCMStatusDeliveryMarkSentInput) (repository.SCMStatusDeliveryUpdateResult, error) {
	if err := ctx.Err(); err != nil {
		return repository.SCMStatusDeliveryUpdateResult{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	current, ok := r.deliveries[strings.TrimSpace(input.DeliveryID)]
	if !ok {
		return repository.SCMStatusDeliveryUpdateResult{}, repository.ErrSCMStatusDeliveryNotFound
	}
	if !scmStatusDeliveryClaimMatches(current, strings.TrimSpace(input.ClaimOwner), input.ClaimedAt) {
		return repository.SCMStatusDeliveryUpdateResult{Delivery: current, Outcome: repository.SCMStatusDeliveryUpdateOutcomeLostClaim}, nil
	}
	sentAt := input.SentAt.UTC()
	state := input.State
	current.Status = domain.SCMStatusDeliveryStatusSent
	current.UpdatedAt = sentAt
	current.SentAt = &sentAt
	current.LastSentState = &state
	current.NextAttemptAt = nil
	current.ClaimedAt = nil
	current.ClaimExpiresAt = nil
	current.ClaimedBy = nil
	current.FailureCategory = nil
	current.FailureReason = nil
	current.LastError = nil
	current.SupersededAt = nil
	if err := current.Validate(); err != nil {
		return repository.SCMStatusDeliveryUpdateResult{}, err
	}
	r.deliveries[current.ID] = current
	return repository.SCMStatusDeliveryUpdateResult{Delivery: current, Outcome: repository.SCMStatusDeliveryUpdateOutcomeUpdated}, nil
}

func (r *SCMStatusDeliveryRepository) RecordRetryableFailure(ctx context.Context, input repository.SCMStatusDeliveryRecordFailureInput) (repository.SCMStatusDeliveryUpdateResult, error) {
	return r.recordFailure(ctx, input, domain.SCMStatusDeliveryStatusRetryWaiting)
}

func (r *SCMStatusDeliveryRepository) RecordPermanentFailure(ctx context.Context, input repository.SCMStatusDeliveryRecordFailureInput) (repository.SCMStatusDeliveryUpdateResult, error) {
	return r.recordFailure(ctx, input, domain.SCMStatusDeliveryStatusFailedPermanent)
}

func (r *SCMStatusDeliveryRepository) RecordExhaustedFailure(ctx context.Context, input repository.SCMStatusDeliveryRecordFailureInput) (repository.SCMStatusDeliveryUpdateResult, error) {
	return r.recordFailure(ctx, input, domain.SCMStatusDeliveryStatusFailedExhausted)
}

func (r *SCMStatusDeliveryRepository) MarkSuperseded(ctx context.Context, input repository.SCMStatusDeliveryMarkSupersededInput) (repository.SCMStatusDeliveryUpdateResult, error) {
	if err := ctx.Err(); err != nil {
		return repository.SCMStatusDeliveryUpdateResult{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	current, ok := r.deliveries[strings.TrimSpace(input.DeliveryID)]
	if !ok {
		return repository.SCMStatusDeliveryUpdateResult{}, repository.ErrSCMStatusDeliveryNotFound
	}
	if input.ClaimOwner != nil || input.ClaimedAt != nil {
		if input.ClaimOwner == nil || input.ClaimedAt == nil || !scmStatusDeliveryClaimMatches(current, strings.TrimSpace(*input.ClaimOwner), *input.ClaimedAt) {
			return repository.SCMStatusDeliveryUpdateResult{Delivery: current, Outcome: repository.SCMStatusDeliveryUpdateOutcomeLostClaim}, nil
		}
	}
	supersededAt := input.SupersededAt.UTC()
	current.Status = domain.SCMStatusDeliveryStatusSuperseded
	current.UpdatedAt = supersededAt
	current.SupersededAt = &supersededAt
	current.NextAttemptAt = nil
	current.ClaimedAt = nil
	current.ClaimExpiresAt = nil
	current.ClaimedBy = nil
	current.FailureCategory = scmFailureCategoryPtr(domain.SCMStatusDeliveryFailureCategoryPermanent)
	current.FailureReason = scmOptionalTrimmedString(input.Reason)
	current.LastError = input.LastError
	if err := current.Validate(); err != nil {
		return repository.SCMStatusDeliveryUpdateResult{}, err
	}
	r.deliveries[current.ID] = current
	return repository.SCMStatusDeliveryUpdateResult{Delivery: current, Outcome: repository.SCMStatusDeliveryUpdateOutcomeUpdated}, nil
}

func (r *SCMStatusDeliveryRepository) GetByBuildContextState(ctx context.Context, buildID string, provider string, contextName string, desiredState domain.SCMCommitStatusState) (domain.SCMStatusDelivery, error) {
	if err := ctx.Err(); err != nil {
		return domain.SCMStatusDelivery{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	key := scmStatusDeliveryLogicalKey(strings.TrimSpace(buildID), strings.ToLower(strings.TrimSpace(provider)), strings.TrimSpace(contextName), desiredState)
	id, ok := r.index[key]
	if !ok {
		return domain.SCMStatusDelivery{}, repository.ErrSCMStatusDeliveryNotFound
	}
	delivery, ok := r.deliveries[id]
	if !ok {
		return domain.SCMStatusDelivery{}, repository.ErrSCMStatusDeliveryNotFound
	}
	return delivery, nil
}

func (r *SCMStatusDeliveryRepository) recordFailure(ctx context.Context, input repository.SCMStatusDeliveryRecordFailureInput, status domain.SCMStatusDeliveryStatus) (repository.SCMStatusDeliveryUpdateResult, error) {
	if err := ctx.Err(); err != nil {
		return repository.SCMStatusDeliveryUpdateResult{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	current, ok := r.deliveries[strings.TrimSpace(input.DeliveryID)]
	if !ok {
		return repository.SCMStatusDeliveryUpdateResult{}, repository.ErrSCMStatusDeliveryNotFound
	}
	if !scmStatusDeliveryClaimMatches(current, strings.TrimSpace(input.ClaimOwner), input.ClaimedAt) {
		return repository.SCMStatusDeliveryUpdateResult{Delivery: current, Outcome: repository.SCMStatusDeliveryUpdateOutcomeLostClaim}, nil
	}
	failedAt := input.FailedAt.UTC()
	current.Status = status
	current.UpdatedAt = failedAt
	current.NextAttemptAt = normalizeSCMRecordFailureTime(input.NextAttemptAt)
	current.ClaimedAt = nil
	current.ClaimExpiresAt = nil
	current.ClaimedBy = nil
	current.FailureCategory = scmFailureCategoryPtr(input.FailureCategory)
	current.FailureReason = scmOptionalTrimmedString(input.FailureReason)
	current.LastError = scmNormalizeOptionalString(input.LastError)
	current.SupersededAt = nil
	if status == domain.SCMStatusDeliveryStatusFailedExhausted && current.Attempts < current.MaxAttempts {
		current.Attempts = current.MaxAttempts
	}
	if err := current.Validate(); err != nil {
		return repository.SCMStatusDeliveryUpdateResult{}, err
	}
	r.deliveries[current.ID] = current
	return repository.SCMStatusDeliveryUpdateResult{Delivery: current, Outcome: repository.SCMStatusDeliveryUpdateOutcomeUpdated}, nil
}

func normalizeSCMClaimInput(input repository.SCMStatusDeliveryClaimInput) (domain.SCMStatusDelivery, time.Time, string, time.Duration, error) {
	delivery := input.Delivery.Normalize()
	if err := delivery.ValidateIdentity(); err != nil {
		return domain.SCMStatusDelivery{}, time.Time{}, "", 0, err
	}
	now := input.Now.UTC()
	if now.IsZero() {
		return domain.SCMStatusDelivery{}, time.Time{}, "", 0, errors.New("scm status delivery claim time is required")
	}
	claimOwner := strings.TrimSpace(input.ClaimOwner)
	if claimOwner == "" {
		return domain.SCMStatusDelivery{}, time.Time{}, "", 0, errors.New("scm status delivery claim owner is required")
	}
	if input.ClaimDuration <= 0 {
		return domain.SCMStatusDelivery{}, time.Time{}, "", 0, errors.New("scm status delivery claim duration must be positive")
	}
	if input.MaxAttempts <= 0 {
		return domain.SCMStatusDelivery{}, time.Time{}, "", 0, errors.New("scm status delivery max attempts must be positive")
	}
	return delivery, now, claimOwner, input.ClaimDuration, nil
}

func claimSCMStatusDelivery(delivery domain.SCMStatusDelivery, now time.Time, claimOwner string, claimDuration time.Duration) (domain.SCMStatusDelivery, repository.SCMStatusDeliveryClaimOutcome) {
	delivery = delivery.Normalize()
	outcome := repository.SCMStatusDeliveryClaimOutcomeFromExisting(delivery, now)
	if outcome != repository.SCMStatusDeliveryClaimOutcomeRetryClaimed && outcome != repository.SCMStatusDeliveryClaimOutcomeStaleClaimReclaimed {
		return delivery, outcome
	}
	delivery.Status = domain.SCMStatusDeliveryStatusSending
	delivery.Attempts++
	delivery.LastAttemptAt = &now
	delivery.NextAttemptAt = nil
	delivery.ClaimedAt = &now
	claimExpiresAt := now.Add(claimDuration)
	delivery.ClaimExpiresAt = &claimExpiresAt
	delivery.ClaimedBy = &claimOwner
	delivery.UpdatedAt = now
	return delivery, outcome
}

func scmStatusDeliveryRecoverableDueAt(delivery domain.SCMStatusDelivery, now time.Time) (time.Time, bool) {
	delivery = delivery.Normalize()
	switch delivery.Status {
	case domain.SCMStatusDeliveryStatusRetryWaiting:
		if delivery.NextAttemptAt == nil || delivery.NextAttemptAt.After(now) {
			return time.Time{}, false
		}
		return delivery.NextAttemptAt.UTC(), true
	case domain.SCMStatusDeliveryStatusSending:
		if delivery.ClaimExpiresAt == nil || delivery.ClaimExpiresAt.After(now) {
			return time.Time{}, false
		}
		return delivery.ClaimExpiresAt.UTC(), true
	default:
		return time.Time{}, false
	}
}

func scmStatusDeliveryClaimMatches(delivery domain.SCMStatusDelivery, claimOwner string, claimedAt time.Time) bool {
	if delivery.ClaimedBy == nil || strings.TrimSpace(*delivery.ClaimedBy) != strings.TrimSpace(claimOwner) {
		return false
	}
	if delivery.ClaimedAt == nil || delivery.ClaimedAt.IsZero() {
		return false
	}
	return delivery.ClaimedAt.UTC().Equal(claimedAt.UTC())
}

func scmStatusDeliveryLogicalKey(buildID string, provider string, contextName string, desiredState domain.SCMCommitStatusState) string {
	return strings.TrimSpace(buildID) + "|" + strings.ToLower(strings.TrimSpace(provider)) + "|" + strings.TrimSpace(contextName) + "|" + strings.TrimSpace(string(desiredState))
}

func normalizeSCMRecordFailureTime(value *time.Time) *time.Time {
	if value == nil || value.IsZero() {
		return nil
	}
	utc := value.UTC()
	return &utc
}

func scmFailureCategoryPtr(value domain.SCMStatusDeliveryFailureCategory) *domain.SCMStatusDeliveryFailureCategory {
	return &value
}

func scmOptionalTrimmedString(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func scmNormalizeOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
