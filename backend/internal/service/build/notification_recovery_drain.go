package build

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/observability"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

const (
	defaultNotificationRecoveryInterval  = 15 * time.Second
	defaultNotificationRecoveryBatchSize = 25
)

type NotificationRecoveryDrainConfig struct {
	Notifier  *BuildNotificationService
	Interval  time.Duration
	BatchSize int
}

type NotificationRecoveryIterationResult struct {
	Scanned             int
	ClaimAcquired       int
	RetryClaimed        int
	StaleClaimReclaimed int
	Skipped             int
	Sent                int
	RetryScheduled      int
	PermanentlyFailed   int
	AttemptsExhausted   int
	LostClaim           int
	RehydrationFailed   int
	Errors              int
}

type NotificationRecoveryDrain struct {
	notifier  *BuildNotificationService
	interval  time.Duration
	batchSize int
	now       func() time.Time
	mu        sync.Mutex
}

func NewNotificationRecoveryDrain(cfg NotificationRecoveryDrainConfig) (*NotificationRecoveryDrain, error) {
	if cfg.Notifier == nil {
		return nil, errors.New("notification recovery drain requires a notifier")
	}
	if cfg.Notifier.deliveryRepo == nil {
		return nil, errors.New("notification recovery drain requires a delivery repository")
	}
	if cfg.Notifier.buildRepo == nil {
		return nil, errors.New("notification recovery drain requires a build repository")
	}
	if cfg.Interval <= 0 {
		return nil, errors.New("notification recovery drain interval must be positive")
	}
	if cfg.BatchSize <= 0 {
		return nil, errors.New("notification recovery drain batch size must be positive")
	}
	return &NotificationRecoveryDrain{
		notifier:  cfg.Notifier,
		interval:  cfg.Interval,
		batchSize: cfg.BatchSize,
		now:       func() time.Time { return time.Now().UTC() },
	}, nil
}

func (d *NotificationRecoveryDrain) Run(ctx context.Context) error {
	if _, err := d.RunIteration(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("notification recovery iteration failed: err=%v", err)
	}
	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if _, err := d.RunIteration(ctx); err != nil && !errors.Is(err, context.Canceled) {
				log.Printf("notification recovery iteration failed: err=%v", err)
			}
		}
	}
}

func (d *NotificationRecoveryDrain) RunIteration(ctx context.Context) (NotificationRecoveryIterationResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := d.now().UTC()
	candidates, err := d.notifier.deliveryRepo.ListRecoverable(ctx, repository.NotificationDeliveryRecoverableScanInput{Now: now, Limit: d.batchSize})
	if err != nil {
		return NotificationRecoveryIterationResult{}, err
	}

	result := NotificationRecoveryIterationResult{Scanned: len(candidates)}
	var iterationErrs []error
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		recoveryReason := candidateRecoveryReason(candidate.Status)
		d.notifier.recordDeliveryMetric(candidate, recoveryReason, observability.NotificationDeliveryOutcomeScanned)
		attemptResult, recoverErr := d.notifier.recoverDelivery(ctx, candidate, recoveryReason)
		d.applyAttemptResult(&result, attemptResult)
		if recoverErr != nil {
			result.Errors++
			iterationErrs = append(iterationErrs, fmt.Errorf("delivery %s: %w", candidate.ID, recoverErr))
		}
	}
	if result.Scanned > 0 || result.Errors > 0 {
		log.Printf("notification recovery iteration completed: scanned=%d claim_acquired=%d retry_claimed=%d stale_claim_reclaimed=%d skipped=%d sent=%d retry_scheduled=%d permanently_failed=%d attempts_exhausted=%d lost_claim=%d rehydration_failed=%d errors=%d", result.Scanned, result.ClaimAcquired, result.RetryClaimed, result.StaleClaimReclaimed, result.Skipped, result.Sent, result.RetryScheduled, result.PermanentlyFailed, result.AttemptsExhausted, result.LostClaim, result.RehydrationFailed, result.Errors)
	}
	return result, errors.Join(iterationErrs...)
}

func (d *NotificationRecoveryDrain) applyAttemptResult(result *NotificationRecoveryIterationResult, attempt notificationRecoveryAttemptResult) {
	switch attempt.claimOutcome {
	case repository.NotificationDeliveryClaimOutcomeCreatedClaimed:
		result.ClaimAcquired++
	case repository.NotificationDeliveryClaimOutcomeRetryClaimed:
		result.RetryClaimed++
	case repository.NotificationDeliveryClaimOutcomeStaleClaimReclaimed:
		result.StaleClaimReclaimed++
	default:
		result.Skipped++
	}
	if attempt.rehydrationFailed {
		result.RehydrationFailed++
	}
	switch attempt.executionOutcome {
	case notificationExecutionOutcomeSent:
		result.Sent++
	case notificationExecutionOutcomeRetryScheduled:
		result.RetryScheduled++
	case notificationExecutionOutcomePermanentlyFailed:
		result.PermanentlyFailed++
	case notificationExecutionOutcomeAttemptsExhausted:
		result.AttemptsExhausted++
	case notificationExecutionOutcomeLostClaim:
		result.LostClaim++
	}
}
