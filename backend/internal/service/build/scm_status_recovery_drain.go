package build

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/repository"
)

const (
	defaultSCMStatusRecoveryInterval  = 15 * time.Second
	defaultSCMStatusRecoveryBatchSize = 25
)

type SCMStatusRecoveryDrainConfig struct {
	Reporter  *SCMStatusReporter
	Interval  time.Duration
	BatchSize int
}

type SCMStatusRecoveryIterationResult struct {
	Scanned             int
	ClaimAcquired       int
	RetryClaimed        int
	StaleClaimReclaimed int
	Skipped             int
	SkippedContention   int
	SkippedNotDue       int
	SkippedTerminal     int
	Sent                int
	RetryScheduled      int
	PermanentlyFailed   int
	AttemptsExhausted   int
	LostClaim           int
	Superseded          int
	RehydrationFailed   int
	Errors              int
}

type SCMStatusRecoveryDrain struct {
	reporter  *SCMStatusReporter
	interval  time.Duration
	batchSize int
	now       func() time.Time
	mu        sync.Mutex
}

func NewSCMStatusRecoveryDrain(cfg SCMStatusRecoveryDrainConfig) (*SCMStatusRecoveryDrain, error) {
	if cfg.Reporter == nil {
		return nil, errors.New("scm status recovery drain requires a reporter")
	}
	if cfg.Reporter.deliveryRepo == nil {
		return nil, errors.New("scm status recovery drain requires a delivery repository")
	}
	if cfg.Interval <= 0 {
		return nil, errors.New("scm status recovery drain interval must be positive")
	}
	if cfg.BatchSize <= 0 {
		return nil, errors.New("scm status recovery drain batch size must be positive")
	}
	return &SCMStatusRecoveryDrain{reporter: cfg.Reporter, interval: cfg.Interval, batchSize: cfg.BatchSize, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (d *SCMStatusRecoveryDrain) Run(ctx context.Context) error {
	if _, err := d.RunIteration(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("scm status recovery iteration failed: err=%v", err)
	}
	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if _, err := d.RunIteration(ctx); err != nil && !errors.Is(err, context.Canceled) {
				log.Printf("scm status recovery iteration failed: err=%v", err)
			}
		}
	}
}

func (d *SCMStatusRecoveryDrain) RunIteration(ctx context.Context) (SCMStatusRecoveryIterationResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := d.now().UTC()
	candidates, err := d.reporter.deliveryRepo.ListRecoverable(ctx, repository.SCMStatusDeliveryRecoverableScanInput{Now: now, Limit: d.batchSize})
	if err != nil {
		return SCMStatusRecoveryIterationResult{}, err
	}
	result := SCMStatusRecoveryIterationResult{Scanned: len(candidates)}
	var iterationErrs []error
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		attempt, recoverErr := d.reporter.recoverDelivery(ctx, candidate, scmStatusRecoveryReasonDrain)
		d.applyAttemptResult(&result, attempt)
		if recoverErr != nil {
			result.Errors++
			iterationErrs = append(iterationErrs, fmt.Errorf("delivery %s: %w", candidate.ID, recoverErr))
		}
	}
	if result.Scanned > 0 || result.Errors > 0 {
		log.Printf("scm status recovery iteration completed: scanned=%d claim_acquired=%d retry_claimed=%d stale_claim_reclaimed=%d skipped=%d skipped_contention=%d skipped_not_due=%d skipped_terminal=%d sent=%d retry_scheduled=%d permanently_failed=%d attempts_exhausted=%d lost_claim=%d superseded=%d rehydration_failed=%d errors=%d", result.Scanned, result.ClaimAcquired, result.RetryClaimed, result.StaleClaimReclaimed, result.Skipped, result.SkippedContention, result.SkippedNotDue, result.SkippedTerminal, result.Sent, result.RetryScheduled, result.PermanentlyFailed, result.AttemptsExhausted, result.LostClaim, result.Superseded, result.RehydrationFailed, result.Errors)
	}
	return result, errors.Join(iterationErrs...)
}

func (d *SCMStatusRecoveryDrain) applyAttemptResult(result *SCMStatusRecoveryIterationResult, attempt scmStatusRecoveryAttemptResult) {
	switch attempt.claimOutcome {
	case repository.SCMStatusDeliveryClaimOutcomeCreatedClaimed:
		result.ClaimAcquired++
	case repository.SCMStatusDeliveryClaimOutcomeRetryClaimed:
		result.RetryClaimed++
	case repository.SCMStatusDeliveryClaimOutcomeStaleClaimReclaimed:
		result.StaleClaimReclaimed++
	case repository.SCMStatusDeliveryClaimOutcomeClaimedByOther:
		result.Skipped++
		result.SkippedContention++
	case repository.SCMStatusDeliveryClaimOutcomeRetryNotDue:
		result.Skipped++
		result.SkippedNotDue++
	case repository.SCMStatusDeliveryClaimOutcomeAlreadySent,
		repository.SCMStatusDeliveryClaimOutcomePermanentlyFailed,
		repository.SCMStatusDeliveryClaimOutcomeAttemptsExhausted,
		repository.SCMStatusDeliveryClaimOutcomeSuperseded:
		result.Skipped++
		result.SkippedTerminal++
	default:
		result.Skipped++
	}
	if attempt.rehydrationFailed {
		result.RehydrationFailed++
	}
	switch attempt.executionOutcome {
	case scmStatusExecutionOutcomeSent:
		result.Sent++
	case scmStatusExecutionOutcomeRetryScheduled:
		result.RetryScheduled++
	case scmStatusExecutionOutcomePermanentlyFailed:
		result.PermanentlyFailed++
	case scmStatusExecutionOutcomeAttemptsExhausted:
		result.AttemptsExhausted++
	case scmStatusExecutionOutcomeLostClaim:
		result.LostClaim++
	case scmStatusExecutionOutcomeSuperseded:
		result.Superseded++
	}
}
