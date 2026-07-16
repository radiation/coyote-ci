package repository

import (
	"context"
	"errors"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

var ErrSCMStatusDeliveryNotFound = errors.New("scm status delivery not found")

type SCMStatusDeliveryClaimOutcome string

const (
	SCMStatusDeliveryClaimOutcomeCreatedClaimed      SCMStatusDeliveryClaimOutcome = "created_claimed"
	SCMStatusDeliveryClaimOutcomeRetryClaimed        SCMStatusDeliveryClaimOutcome = "retry_claimed"
	SCMStatusDeliveryClaimOutcomeStaleClaimReclaimed SCMStatusDeliveryClaimOutcome = "stale_claim_reclaimed"
	SCMStatusDeliveryClaimOutcomeAlreadySent         SCMStatusDeliveryClaimOutcome = "already_sent"
	SCMStatusDeliveryClaimOutcomePermanentlyFailed   SCMStatusDeliveryClaimOutcome = "permanently_failed"
	SCMStatusDeliveryClaimOutcomeAttemptsExhausted   SCMStatusDeliveryClaimOutcome = "attempts_exhausted"
	SCMStatusDeliveryClaimOutcomeClaimedByOther      SCMStatusDeliveryClaimOutcome = "claimed_by_other"
	SCMStatusDeliveryClaimOutcomeRetryNotDue         SCMStatusDeliveryClaimOutcome = "retry_not_due"
	SCMStatusDeliveryClaimOutcomeSuperseded          SCMStatusDeliveryClaimOutcome = "superseded"
)

type SCMStatusDeliveryClaimInput struct {
	Delivery      domain.SCMStatusDelivery
	ClaimOwner    string
	Now           time.Time
	ClaimDuration time.Duration
	MaxAttempts   int
}

type SCMStatusDeliveryClaimResult struct {
	Delivery domain.SCMStatusDelivery
	Outcome  SCMStatusDeliveryClaimOutcome
}

type SCMStatusDeliveryRecoverableScanInput struct {
	Now   time.Time
	Limit int
}

type SCMStatusDeliveryUpdateOutcome string

const (
	SCMStatusDeliveryUpdateOutcomeUpdated   SCMStatusDeliveryUpdateOutcome = "updated"
	SCMStatusDeliveryUpdateOutcomeLostClaim SCMStatusDeliveryUpdateOutcome = "lost_claim"
)

type SCMStatusDeliveryUpdateResult struct {
	Delivery domain.SCMStatusDelivery
	Outcome  SCMStatusDeliveryUpdateOutcome
}

type SCMStatusDeliveryMarkSentInput struct {
	DeliveryID string
	ClaimOwner string
	ClaimedAt  time.Time
	SentAt     time.Time
	State      domain.SCMCommitStatusState
}

type SCMStatusDeliveryRecordFailureInput struct {
	DeliveryID      string
	ClaimOwner      string
	ClaimedAt       time.Time
	FailedAt        time.Time
	NextAttemptAt   *time.Time
	FailureCategory domain.SCMStatusDeliveryFailureCategory
	FailureReason   string
	LastError       *string
}

type SCMStatusDeliveryMarkSupersededInput struct {
	DeliveryID   string
	ClaimOwner   *string
	ClaimedAt    *time.Time
	SupersededAt time.Time
	Reason       string
	LastError    *string
}

type SCMStatusDeliveryRepository interface {
	AcquireForDelivery(ctx context.Context, input SCMStatusDeliveryClaimInput) (SCMStatusDeliveryClaimResult, error)
	ListRecoverable(ctx context.Context, input SCMStatusDeliveryRecoverableScanInput) ([]domain.SCMStatusDelivery, error)
	MarkSent(ctx context.Context, input SCMStatusDeliveryMarkSentInput) (SCMStatusDeliveryUpdateResult, error)
	RecordRetryableFailure(ctx context.Context, input SCMStatusDeliveryRecordFailureInput) (SCMStatusDeliveryUpdateResult, error)
	RecordPermanentFailure(ctx context.Context, input SCMStatusDeliveryRecordFailureInput) (SCMStatusDeliveryUpdateResult, error)
	RecordExhaustedFailure(ctx context.Context, input SCMStatusDeliveryRecordFailureInput) (SCMStatusDeliveryUpdateResult, error)
	MarkSuperseded(ctx context.Context, input SCMStatusDeliveryMarkSupersededInput) (SCMStatusDeliveryUpdateResult, error)
	GetByKey(ctx context.Context, provider string, repositoryOwner string, repositoryName string, commitSHA string, contextName string) (domain.SCMStatusDelivery, error)
}

func SCMStatusDeliveryClaimOutcomeFromExisting(delivery domain.SCMStatusDelivery, now time.Time) SCMStatusDeliveryClaimOutcome {
	switch delivery.Status {
	case domain.SCMStatusDeliveryStatusSent:
		return SCMStatusDeliveryClaimOutcomeAlreadySent
	case domain.SCMStatusDeliveryStatusFailedPermanent:
		return SCMStatusDeliveryClaimOutcomePermanentlyFailed
	case domain.SCMStatusDeliveryStatusFailedExhausted:
		return SCMStatusDeliveryClaimOutcomeAttemptsExhausted
	case domain.SCMStatusDeliveryStatusSuperseded:
		return SCMStatusDeliveryClaimOutcomeSuperseded
	case domain.SCMStatusDeliveryStatusSending:
		if delivery.Attempts >= delivery.MaxAttempts {
			return SCMStatusDeliveryClaimOutcomeAttemptsExhausted
		}
		if delivery.ClaimExpiresAt != nil && !now.UTC().Before(delivery.ClaimExpiresAt.UTC()) {
			return SCMStatusDeliveryClaimOutcomeStaleClaimReclaimed
		}
		return SCMStatusDeliveryClaimOutcomeClaimedByOther
	case domain.SCMStatusDeliveryStatusRetryWaiting:
		if delivery.NextAttemptAt != nil && now.UTC().Before(delivery.NextAttemptAt.UTC()) {
			return SCMStatusDeliveryClaimOutcomeRetryNotDue
		}
		if delivery.Attempts >= delivery.MaxAttempts {
			return SCMStatusDeliveryClaimOutcomeAttemptsExhausted
		}
		return SCMStatusDeliveryClaimOutcomeRetryClaimed
	case domain.SCMStatusDeliveryStatusPending:
		if delivery.Attempts >= delivery.MaxAttempts {
			return SCMStatusDeliveryClaimOutcomeAttemptsExhausted
		}
		return SCMStatusDeliveryClaimOutcomeRetryClaimed
	default:
		return SCMStatusDeliveryClaimOutcomeClaimedByOther
	}
}
