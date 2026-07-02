package build

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/observability"
	platformemail "github.com/radiation/coyote-ci/backend/internal/platform/email"
	platformslack "github.com/radiation/coyote-ci/backend/internal/platform/slack"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func (s *BuildNotificationService) sendTerminalNotification(ctx context.Context, buildID string, eventType domain.NotificationEventType, destinations []notificationDestination, subject string, body string, slackText string, personalSlackText string) error {
	var sendErrs []error
	content := notificationContent{
		subject:           subject,
		body:              body,
		slackText:         slackText,
		personalSlackText: personalSlackText,
	}

	for _, destination := range destinations {
		if s.deliveryRepo == nil {
			if sendErr := s.sendDestination(ctx, destination, content.subject, content.body, content.slackText, content.personalSlackText); sendErr != nil {
				sendErrs = append(sendErrs, sendErr)
			}
			continue
		}

		var delivery domain.NotificationDelivery
		var shouldSend bool
		var err error
		delivery, shouldSend, err = s.prepareDelivery(ctx, buildID, eventType, destination)
		if err != nil {
			return err
		}
		if !shouldSend {
			continue
		}
		if _, executeErr := s.executeClaimedDelivery(ctx, delivery, destination, content, notificationRecoveryReasonInline); executeErr != nil {
			sendErrs = append(sendErrs, executeErr)
			continue
		}
	}

	return errors.Join(sendErrs...)
}

func (s *BuildNotificationService) sendDestination(ctx context.Context, destination notificationDestination, subject string, body string, slackText string, personalSlackText string) error {
	if destination.transport == domain.NotificationTransportSlackWebhook {
		if s.slackSender == nil {
			return errors.New("slack sender is not configured")
		}
		return s.slackSender.Send(ctx, destination.webhookURL, SlackWebhookMessage{Text: slackText})
	}
	if destination.transport == domain.NotificationTransportSlackDM {
		if s.slackClient == nil {
			return errors.New("slack client is not configured")
		}
		_, err := s.slackClient.PostDirectMessage(ctx, destination.slackBotToken, destination.slackUserID, platformslack.Message{Text: personalSlackText})
		return err
	}
	if s.sender == nil {
		return errors.New("email sender is not configured")
	}
	return s.sender.SendText(ctx, platformemail.Message{
		To:      destination.emailRecipient,
		Subject: subject,
		Body:    body,
	})
}

func (s *BuildNotificationService) prepareDelivery(ctx context.Context, buildID string, eventType domain.NotificationEventType, destination notificationDestination) (domain.NotificationDelivery, bool, error) {
	result, shouldSend, err := s.acquireDelivery(ctx, domain.NotificationDelivery{
		BuildID:                     buildID,
		EventType:                   eventType,
		Transport:                   destination.transport,
		DestinationKind:             destination.destinationKind,
		DestinationKey:              destination.destinationKey,
		NotificationTargetID:        destination.notificationTargetID,
		RecipientUserID:             destination.recipientUserID,
		SlackWorkspaceIntegrationID: destination.slackWorkspaceIntegrationID,
		Recipient:                   destination.recipient,
	}, notificationRecoveryReasonInline)
	if err != nil {
		return domain.NotificationDelivery{}, false, err
	}
	return result.Delivery, shouldSend, nil
}

func (s *BuildNotificationService) acquireDelivery(ctx context.Context, delivery domain.NotificationDelivery, recoveryReason string) (repository.NotificationDeliveryClaimResult, bool, error) {
	result, err := s.deliveryRepo.AcquireForDelivery(ctx, repository.NotificationDeliveryClaimInput{
		Delivery: domain.NotificationDelivery{
			BuildID:                     delivery.BuildID,
			EventType:                   delivery.EventType,
			Transport:                   delivery.Transport,
			DestinationKind:             delivery.DestinationKind,
			DestinationKey:              delivery.DestinationKey,
			NotificationTargetID:        delivery.NotificationTargetID,
			RecipientUserID:             delivery.RecipientUserID,
			SlackWorkspaceIntegrationID: delivery.SlackWorkspaceIntegrationID,
			Recipient:                   delivery.Recipient,
		},
		ClaimOwner:    s.claimOwner,
		Now:           s.now().UTC(),
		ClaimDuration: s.claimDuration,
		MaxAttempts:   s.retryPolicy.maxAttempts,
	})
	if err != nil {
		return repository.NotificationDeliveryClaimResult{}, false, err
	}
	s.recordClaimMetric(result.Delivery, recoveryReason, result.Outcome)
	switch result.Outcome {
	case repository.NotificationDeliveryClaimOutcomeCreatedClaimed, repository.NotificationDeliveryClaimOutcomeRetryClaimed, repository.NotificationDeliveryClaimOutcomeStaleClaimReclaimed:
		logNotificationDeliveryEvent("build notification delivery claimed", result.Delivery, notificationLogFields{
			recoveryReason: recoveryReason,
			claimOutcome:   string(result.Outcome),
			claimOwner:     s.claimOwner,
		})
		return result, true, nil
	case repository.NotificationDeliveryClaimOutcomeAlreadySent:
		logNotificationDeliveryEvent("build notification skipped", result.Delivery, notificationLogFields{recoveryReason: recoveryReason, claimOutcome: string(result.Outcome)})
	default:
		logNotificationDeliveryEvent("build notification skipped", result.Delivery, notificationLogFields{recoveryReason: recoveryReason, claimOutcome: string(result.Outcome)})
	}
	return result, false, nil
}

func (s *BuildNotificationService) markDeliverySent(ctx context.Context, delivery domain.NotificationDelivery, sentAt time.Time, recoveryReason string) (notificationExecutionOutcome, error) {
	claimedAt, claimErr := claimedNotificationTimestamp(delivery)
	if claimErr != nil {
		return notificationExecutionOutcomeNone, claimErr
	}
	result, err := s.deliveryRepo.MarkSent(ctx, repository.NotificationDeliveryMarkSentInput{
		DeliveryID: delivery.ID,
		ClaimOwner: s.claimOwner,
		ClaimedAt:  claimedAt,
		SentAt:     sentAt,
	})
	if err != nil {
		return notificationExecutionOutcomeNone, err
	}
	if result.Outcome == repository.NotificationDeliveryUpdateOutcomeLostClaim {
		s.recordDeliveryMetric(delivery, recoveryReason, observability.NotificationDeliveryOutcomeLostClaim)
		logNotificationDeliveryEvent("build notification lost claim before marking sent", delivery, notificationLogFields{recoveryReason: recoveryReason, claimOwner: s.claimOwner})
		return notificationExecutionOutcomeLostClaim, nil
	}
	s.recordDeliveryMetric(delivery, recoveryReason, observability.NotificationDeliveryOutcomeSent)
	return notificationExecutionOutcomeSent, nil
}

func (s *BuildNotificationService) markDeliveryFailed(ctx context.Context, delivery domain.NotificationDelivery, sendErr error, attemptedAt time.Time, recoveryReason string) (notificationExecutionOutcome, error) {
	claimedAt, claimErr := claimedNotificationTimestamp(delivery)
	if claimErr != nil {
		return notificationExecutionOutcomeNone, claimErr
	}
	decision := classifyNotificationDeliveryFailure(delivery.Transport, sendErr)
	message := strings.TrimSpace(sendErr.Error())
	input := repository.NotificationDeliveryRecordFailureInput{
		DeliveryID:      delivery.ID,
		ClaimOwner:      s.claimOwner,
		ClaimedAt:       claimedAt,
		FailedAt:        attemptedAt,
		FailureCategory: decision.category,
		FailureReason:   decision.reason,
		LastError:       &message,
	}
	var (
		result repository.NotificationDeliveryUpdateResult
		err    error
	)
	if decision.retryable && delivery.Attempts < delivery.MaxAttempts {
		nextAttemptAt := attemptedAt.Add(s.retryPolicy.delayForAttempt(delivery.Attempts))
		input.NextAttemptAt = &nextAttemptAt
		result, err = s.deliveryRepo.RecordRetryableFailure(ctx, input)
		if err == nil {
			s.recordDeliveryMetric(delivery, recoveryReason, observability.NotificationDeliveryOutcomeRetryScheduled)
			logNotificationDeliveryEvent("build notification delivery scheduled retry", delivery, notificationLogFields{recoveryReason: recoveryReason, claimOwner: s.claimOwner, failureCategory: string(decision.category), failureReason: decision.reason})
		}
	} else if decision.retryable {
		result, err = s.deliveryRepo.RecordExhaustedFailure(ctx, input)
		if err == nil {
			s.recordDeliveryMetric(delivery, recoveryReason, observability.NotificationDeliveryOutcomeAttemptsExhausted)
			logNotificationDeliveryEvent("build notification delivery exhausted retries", delivery, notificationLogFields{recoveryReason: recoveryReason, claimOwner: s.claimOwner, failureCategory: string(decision.category), failureReason: decision.reason})
		}
	} else {
		result, err = s.deliveryRepo.RecordPermanentFailure(ctx, input)
		if err == nil {
			s.recordDeliveryMetric(delivery, recoveryReason, observability.NotificationDeliveryOutcomePermanentlyFailed)
			logNotificationDeliveryEvent("build notification delivery permanently failed", delivery, notificationLogFields{recoveryReason: recoveryReason, claimOwner: s.claimOwner, failureCategory: string(decision.category), failureReason: decision.reason})
		}
	}
	if err != nil {
		return notificationExecutionOutcomeNone, err
	}
	if result.Outcome == repository.NotificationDeliveryUpdateOutcomeLostClaim {
		s.recordDeliveryMetric(delivery, recoveryReason, observability.NotificationDeliveryOutcomeLostClaim)
		logNotificationDeliveryEvent("build notification lost claim before recording failure", delivery, notificationLogFields{recoveryReason: recoveryReason, claimOwner: s.claimOwner, failureCategory: string(decision.category), failureReason: decision.reason})
		return notificationExecutionOutcomeLostClaim, nil
	}
	if decision.retryable && delivery.Attempts < delivery.MaxAttempts {
		return notificationExecutionOutcomeRetryScheduled, nil
	}
	if decision.retryable {
		return notificationExecutionOutcomeAttemptsExhausted, nil
	}
	return notificationExecutionOutcomePermanentlyFailed, nil
}

func notificationClaimDuration(value time.Duration) time.Duration {
	if value > 0 {
		return value
	}
	return defaultNotificationDeliveryClaimDuration
}

func notificationClaimOwner(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed != "" {
		return trimmed
	}
	return "inline-notifier"
}

func claimedNotificationTimestamp(delivery domain.NotificationDelivery) (time.Time, error) {
	if delivery.ClaimedAt == nil || delivery.ClaimedAt.IsZero() {
		return time.Time{}, fmt.Errorf("notification delivery claim timestamp is required")
	}
	return delivery.ClaimedAt.UTC(), nil
}
