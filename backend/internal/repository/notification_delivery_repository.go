package repository

import (
	"context"
	"errors"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

var ErrNotificationDeliveryNotFound = errors.New("notification delivery not found")
var ErrNotificationDeliveryDuplicate = errors.New("notification delivery already exists")

type NotificationDeliveryClaimOutcome string

const NotificationDeliveryFinalClaimExpiredReason = "final delivery claim expired"

const (
	NotificationDeliveryClaimOutcomeCreatedClaimed      NotificationDeliveryClaimOutcome = "created_claimed"
	NotificationDeliveryClaimOutcomeRetryClaimed        NotificationDeliveryClaimOutcome = "retry_claimed"
	NotificationDeliveryClaimOutcomeStaleClaimReclaimed NotificationDeliveryClaimOutcome = "stale_claim_reclaimed"
	NotificationDeliveryClaimOutcomeAlreadySent         NotificationDeliveryClaimOutcome = "already_sent"
	NotificationDeliveryClaimOutcomePermanentlyFailed   NotificationDeliveryClaimOutcome = "permanently_failed"
	NotificationDeliveryClaimOutcomeAttemptsExhausted   NotificationDeliveryClaimOutcome = "attempts_exhausted"
	NotificationDeliveryClaimOutcomeClaimedByOther      NotificationDeliveryClaimOutcome = "claimed_by_other"
	NotificationDeliveryClaimOutcomeRetryNotDue         NotificationDeliveryClaimOutcome = "retry_not_due"
)

type NotificationDeliveryClaimInput struct {
	ExistingOnly  bool
	Delivery      domain.NotificationDelivery
	ClaimOwner    string
	Now           time.Time
	ClaimDuration time.Duration
	MaxAttempts   int
}

type NotificationDeliveryClaimResult struct {
	Delivery domain.NotificationDelivery
	Outcome  NotificationDeliveryClaimOutcome
}

type NotificationDeliveryRecoverableScanInput struct {
	Now   time.Time
	Limit int
}

type NotificationDeliveryUpdateOutcome string

const (
	NotificationDeliveryUpdateOutcomeUpdated   NotificationDeliveryUpdateOutcome = "updated"
	NotificationDeliveryUpdateOutcomeLostClaim NotificationDeliveryUpdateOutcome = "lost_claim"
)

type NotificationDeliveryUpdateResult struct {
	Delivery domain.NotificationDelivery
	Outcome  NotificationDeliveryUpdateOutcome
}

type NotificationDeliveryMarkSentInput struct {
	DeliveryID string
	ClaimOwner string
	ClaimedAt  time.Time
	SentAt     time.Time
}

type NotificationDeliveryRecordFailureInput struct {
	DeliveryID      string
	ClaimOwner      string
	ClaimedAt       time.Time
	FailedAt        time.Time
	NextAttemptAt   *time.Time
	FailureCategory domain.NotificationDeliveryFailureCategory
	FailureReason   string
	LastError       *string
}

type NotificationDeliveryRepository interface {
	AcquireForDelivery(ctx context.Context, input NotificationDeliveryClaimInput) (NotificationDeliveryClaimResult, error)
	ListRecoverable(ctx context.Context, input NotificationDeliveryRecoverableScanInput) ([]domain.NotificationDelivery, error)
	MarkSent(ctx context.Context, input NotificationDeliveryMarkSentInput) (NotificationDeliveryUpdateResult, error)
	RecordRetryableFailure(ctx context.Context, input NotificationDeliveryRecordFailureInput) (NotificationDeliveryUpdateResult, error)
	RecordPermanentFailure(ctx context.Context, input NotificationDeliveryRecordFailureInput) (NotificationDeliveryUpdateResult, error)
	RecordExhaustedFailure(ctx context.Context, input NotificationDeliveryRecordFailureInput) (NotificationDeliveryUpdateResult, error)
	GetByBuildEventRecipient(ctx context.Context, buildID string, eventType domain.NotificationEventType, recipient string) (domain.NotificationDelivery, error)
}

type NotificationPlanningRepository interface {
	ListUnplannedBuildIDs(ctx context.Context, after string, limit int) ([]string, error)
	CommitNotificationPlan(ctx context.Context, buildID string, status domain.BuildStatus, deliveries []domain.NotificationDelivery) (bool, error)
}

func ValidatePlannedNotification(buildID string, status domain.BuildStatus, delivery domain.NotificationDelivery) error {
	eventMatches := status == domain.BuildStatusSuccess && delivery.EventType == domain.NotificationEventTypeBuildSucceeded ||
		status == domain.BuildStatusFailed && delivery.EventType == domain.NotificationEventTypeBuildFailed
	if !eventMatches || delivery.BuildID != buildID || delivery.Status != domain.NotificationDeliveryStatusPending || delivery.Attempts != 0 || delivery.MaxAttempts <= 0 {
		return errors.New("notification plan requires an unattempted delivery for the terminal build event")
	}
	return delivery.Validate()
}

func NotificationDeliveryClaimOutcomeFromExisting(delivery domain.NotificationDelivery, now time.Time) NotificationDeliveryClaimOutcome {
	switch delivery.Status {
	case domain.NotificationDeliveryStatusSent:
		return NotificationDeliveryClaimOutcomeAlreadySent
	case domain.NotificationDeliveryStatusFailedPermanent:
		return NotificationDeliveryClaimOutcomePermanentlyFailed
	case domain.NotificationDeliveryStatusFailedExhausted:
		return NotificationDeliveryClaimOutcomeAttemptsExhausted
	case domain.NotificationDeliveryStatusSending:
		if delivery.ClaimExpiresAt == nil || now.Before(*delivery.ClaimExpiresAt) {
			return NotificationDeliveryClaimOutcomeClaimedByOther
		}
		if delivery.Attempts >= delivery.MaxAttempts {
			return NotificationDeliveryClaimOutcomeAttemptsExhausted
		}
		return NotificationDeliveryClaimOutcomeStaleClaimReclaimed
	case domain.NotificationDeliveryStatusRetryWaiting:
		if delivery.NextAttemptAt != nil && now.UTC().Before(delivery.NextAttemptAt.UTC()) {
			return NotificationDeliveryClaimOutcomeRetryNotDue
		}
		if delivery.Attempts >= delivery.MaxAttempts {
			return NotificationDeliveryClaimOutcomeAttemptsExhausted
		}
		return NotificationDeliveryClaimOutcomeRetryClaimed
	case domain.NotificationDeliveryStatusPending:
		if delivery.Attempts >= delivery.MaxAttempts {
			return NotificationDeliveryClaimOutcomeAttemptsExhausted
		}
		if delivery.Attempts == 0 {
			return NotificationDeliveryClaimOutcomeCreatedClaimed
		}
		return NotificationDeliveryClaimOutcomeRetryClaimed
	default:
		return NotificationDeliveryClaimOutcomeClaimedByOther
	}
}
