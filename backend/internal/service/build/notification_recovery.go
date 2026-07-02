package build

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/observability"
	platformslack "github.com/radiation/coyote-ci/backend/internal/platform/slack"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

const (
	notificationRecoveryReasonInline     = "inline"
	notificationRecoveryReasonDueRetry   = "due_retry"
	notificationRecoveryReasonStaleClaim = "stale_claim"
)

type notificationContent struct {
	subject           string
	body              string
	slackText         string
	personalSlackText string
}

type notificationExecutionOutcome string

const (
	notificationExecutionOutcomeNone              notificationExecutionOutcome = "none"
	notificationExecutionOutcomeSent              notificationExecutionOutcome = "sent"
	notificationExecutionOutcomeRetryScheduled    notificationExecutionOutcome = "retry_scheduled"
	notificationExecutionOutcomePermanentlyFailed notificationExecutionOutcome = "permanently_failed"
	notificationExecutionOutcomeAttemptsExhausted notificationExecutionOutcome = "attempts_exhausted"
	notificationExecutionOutcomeLostClaim         notificationExecutionOutcome = "lost_claim"
)

type notificationExecutionFailure struct {
	category  domain.NotificationDeliveryFailureCategory
	reason    string
	retryable bool
	message   string
	cause     error
}

func (e *notificationExecutionFailure) Error() string {
	return e.message
}

func (e *notificationExecutionFailure) Unwrap() error {
	return e.cause
}

type notificationLogFields struct {
	recoveryReason  string
	claimOutcome    string
	claimOwner      string
	failureCategory string
	failureReason   string
}

type notificationRecoveryAttemptResult struct {
	claimOutcome      repository.NotificationDeliveryClaimOutcome
	executionOutcome  notificationExecutionOutcome
	rehydrationFailed bool
}

func deliveryMetricsOrNoop(metrics observability.NotificationDeliveryMetrics) observability.NotificationDeliveryMetrics {
	if metrics == nil {
		return observability.NewNoopNotificationDeliveryMetrics()
	}
	return metrics
}

func (s *BuildNotificationService) executeClaimedDelivery(ctx context.Context, delivery domain.NotificationDelivery, destination notificationDestination, content notificationContent, recoveryReason string) (notificationExecutionOutcome, error) {
	sendErr := s.sendDestination(ctx, destination, content.subject, content.body, content.slackText, content.personalSlackText)
	attemptedAt := s.now().UTC()
	if sendErr != nil {
		if errors.Is(sendErr, context.Canceled) || errors.Is(sendErr, context.DeadlineExceeded) {
			logNotificationDeliveryEvent("build notification claim left active for stale recovery", delivery, notificationLogFields{recoveryReason: recoveryReason, claimOwner: s.claimOwner, failureReason: "context_canceled"})
			return notificationExecutionOutcomeNone, sendErr
		}
		outcome, updateErr := s.markDeliveryFailed(ctx, delivery, sendErr, attemptedAt, recoveryReason)
		if updateErr != nil {
			return notificationExecutionOutcomeNone, errors.Join(sendErr, updateErr)
		}
		return outcome, sendErr
	}

	if s.deliveryRepo == nil {
		s.recordDeliveryMetric(delivery, recoveryReason, observability.NotificationDeliveryOutcomeSent)
		return notificationExecutionOutcomeSent, nil
	}

	outcome, updateErr := s.markDeliverySent(ctx, delivery, attemptedAt, recoveryReason)
	if updateErr != nil {
		persistErr := retryableNotificationExecutionFailure("delivery_state_persist_failed", "persist sent delivery state failed", updateErr)
		if _, markErr := s.markDeliveryFailed(ctx, delivery, persistErr, attemptedAt, recoveryReason); markErr != nil {
			return notificationExecutionOutcomeNone, errors.Join(persistErr, markErr)
		}
		return notificationExecutionOutcomeRetryScheduled, persistErr
	}
	return outcome, nil
}

func (s *BuildNotificationService) recoverDelivery(ctx context.Context, candidate domain.NotificationDelivery, recoveryReason string) (notificationRecoveryAttemptResult, error) {
	claimResult, shouldSend, err := s.acquireDelivery(ctx, candidate, recoveryReason)
	if err != nil {
		return notificationRecoveryAttemptResult{}, err
	}
	claimed := claimResult.Delivery
	result := notificationRecoveryAttemptResult{claimOutcome: claimResult.Outcome}
	if !shouldSend {
		return result, nil
	}

	build, destination, content, rehydrateErr := s.rehydrateDelivery(ctx, claimed)
	_ = build
	if rehydrateErr != nil {
		if errors.Is(rehydrateErr, context.Canceled) || errors.Is(rehydrateErr, context.DeadlineExceeded) {
			return result, rehydrateErr
		}
		result.rehydrationFailed = true
		s.recordDeliveryMetric(claimed, recoveryReason, observability.NotificationDeliveryOutcomeRehydrationFailure)
		outcome, markErr := s.markDeliveryFailed(ctx, claimed, rehydrateErr, s.now().UTC(), recoveryReason)
		result.executionOutcome = outcome
		if markErr != nil {
			return result, markErr
		}
		return result, nil
	}

	executionOutcome, executeErr := s.executeClaimedDelivery(ctx, claimed, destination, content, recoveryReason)
	result.executionOutcome = executionOutcome
	return result, executeErr
}

func (s *BuildNotificationService) rehydrateDelivery(ctx context.Context, delivery domain.NotificationDelivery) (domain.Build, notificationDestination, notificationContent, error) {
	if s.buildRepo == nil {
		return domain.Build{}, notificationDestination{}, notificationContent{}, permanentNotificationExecutionFailure("delivery_metadata_invalid", "notification build repository is not configured", nil)
	}
	build, err := s.buildRepo.GetByID(ctx, delivery.BuildID)
	if err != nil {
		if errors.Is(err, repository.ErrBuildNotFound) {
			return domain.Build{}, notificationDestination{}, notificationContent{}, permanentNotificationExecutionFailure("build_unavailable", "notification build is no longer available", err)
		}
		return domain.Build{}, notificationDestination{}, notificationContent{}, retryableNotificationExecutionFailure("rehydration_failed", "notification build rehydration failed", err)
	}
	eventType, ok := buildStatusNotificationEventType(build.Status)
	if !ok || eventType != delivery.EventType {
		return domain.Build{}, notificationDestination{}, notificationContent{}, permanentNotificationExecutionFailure("build_event_mismatch", "notification build event no longer matches the persisted delivery", nil)
	}

	destination, destinationErr := s.rehydrateDestination(ctx, delivery)
	if destinationErr != nil {
		return domain.Build{}, notificationDestination{}, notificationContent{}, destinationErr
	}
	content := s.renderNotificationContent(ctx, build)
	return build, destination, content, nil
}

func (s *BuildNotificationService) renderNotificationContent(ctx context.Context, build domain.Build) notificationContent {
	details := s.buildNotificationDetails(ctx, build)
	subject, body := s.formatBuildStatusEmail(build, details)
	return notificationContent{
		subject:           subject,
		body:              body,
		slackText:         formatBuildStatusSlackText(details),
		personalSlackText: formatPersonalBuildStatusSlackText(details),
	}
}

func (s *BuildNotificationService) rehydrateDestination(ctx context.Context, delivery domain.NotificationDelivery) (notificationDestination, error) {
	switch delivery.Transport {
	case domain.NotificationTransportEmail:
		switch delivery.DestinationKind {
		case domain.NotificationDestinationKindSharedTarget:
			return s.rehydrateSharedEmailDestination(ctx, delivery)
		case domain.NotificationDestinationKindPersonalEmail:
			return s.rehydratePersonalEmailDestination(ctx, delivery)
		}
	case domain.NotificationTransportSlackWebhook:
		if delivery.DestinationKind == domain.NotificationDestinationKindSharedTarget {
			return s.rehydrateSlackWebhookDestination(ctx, delivery)
		}
	case domain.NotificationTransportSlackDM:
		if delivery.DestinationKind == domain.NotificationDestinationKindSlackIdentity {
			return s.rehydrateSlackDMDestination(ctx, delivery)
		}
	}
	return notificationDestination{}, permanentNotificationExecutionFailure("delivery_metadata_invalid", "notification delivery transport metadata is invalid", nil)
}

func (s *BuildNotificationService) rehydrateSharedEmailDestination(ctx context.Context, delivery domain.NotificationDelivery) (notificationDestination, error) {
	if delivery.NotificationTargetID == nil {
		return notificationDestination{}, permanentNotificationExecutionFailure("delivery_metadata_invalid", "shared email delivery target reference is missing", nil)
	}
	if s.subscriptionRepo == nil {
		return notificationDestination{}, retryableNotificationExecutionFailure("rehydration_failed", "notification subscription repository is not configured", nil)
	}
	target, err := s.subscriptionRepo.GetTargetByID(ctx, strings.TrimSpace(*delivery.NotificationTargetID))
	if err != nil {
		if errors.Is(err, repository.ErrNotificationTargetNotFound) {
			return notificationDestination{}, permanentNotificationExecutionFailure("shared_target_missing", "notification target no longer exists", err)
		}
		return notificationDestination{}, retryableNotificationExecutionFailure("rehydration_failed", "notification target lookup failed", err)
	}
	if !target.Enabled {
		return notificationDestination{}, permanentNotificationExecutionFailure("shared_target_disabled", "notification target is disabled", nil)
	}
	normalizedTarget, normalizeErr := domain.ValidateExplicitNotificationTarget(target)
	if normalizeErr != nil {
		return notificationDestination{}, permanentNotificationExecutionFailure("delivery_metadata_invalid", "notification target metadata is invalid", normalizeErr)
	}
	if normalizedTarget.Type != domain.NotificationTargetTypeEmail || normalizedTarget.OwnerUserID != nil {
		return notificationDestination{}, permanentNotificationExecutionFailure("delivery_metadata_invalid", "notification target type no longer matches the persisted delivery", nil)
	}
	destination, err := notificationTargetDestination(normalizedTarget)
	if err != nil {
		return notificationDestination{}, permanentNotificationExecutionFailure("delivery_metadata_invalid", "notification target metadata is invalid", err)
	}
	if destination.destinationKey != delivery.DestinationKey {
		return notificationDestination{}, permanentNotificationExecutionFailure("shared_target_mismatch", "notification target no longer matches the persisted logical destination", nil)
	}
	return destination, nil
}

func (s *BuildNotificationService) rehydratePersonalEmailDestination(ctx context.Context, delivery domain.NotificationDelivery) (notificationDestination, error) {
	if delivery.NotificationTargetID == nil {
		return notificationDestination{}, permanentNotificationExecutionFailure("delivery_metadata_invalid", "personal email delivery target reference is missing", nil)
	}
	if s.subscriptionRepo == nil {
		return notificationDestination{}, retryableNotificationExecutionFailure("rehydration_failed", "notification subscription repository is not configured", nil)
	}
	target, err := s.subscriptionRepo.GetTargetByID(ctx, strings.TrimSpace(*delivery.NotificationTargetID))
	if err != nil {
		if errors.Is(err, repository.ErrNotificationTargetNotFound) {
			return notificationDestination{}, permanentNotificationExecutionFailure("personal_email_target_missing", "personal notification target no longer exists", err)
		}
		return notificationDestination{}, retryableNotificationExecutionFailure("rehydration_failed", "personal notification target lookup failed", err)
	}
	if !target.Enabled {
		return notificationDestination{}, permanentNotificationExecutionFailure("personal_email_target_disabled", "personal notification target is disabled", nil)
	}
	if target.Type != domain.NotificationTargetTypeEmail || target.OwnerUserID == nil {
		return notificationDestination{}, permanentNotificationExecutionFailure("delivery_metadata_invalid", "personal notification target metadata is invalid", nil)
	}
	if delivery.RecipientUserID != nil && strings.TrimSpace(*delivery.RecipientUserID) != strings.TrimSpace(*target.OwnerUserID) {
		return notificationDestination{}, permanentNotificationExecutionFailure("personal_email_target_mismatch", "personal notification target no longer matches the persisted user", nil)
	}
	destination, err := personalEmailTargetDestination(target)
	if err != nil {
		return notificationDestination{}, permanentNotificationExecutionFailure("delivery_metadata_invalid", "personal notification target metadata is invalid", err)
	}
	if destination.destinationKey != delivery.DestinationKey {
		return notificationDestination{}, permanentNotificationExecutionFailure("personal_email_target_mismatch", "personal notification target no longer matches the persisted logical destination", nil)
	}
	return destination, nil
}

func (s *BuildNotificationService) rehydrateSlackWebhookDestination(ctx context.Context, delivery domain.NotificationDelivery) (notificationDestination, error) {
	if delivery.NotificationTargetID == nil {
		return notificationDestination{}, permanentNotificationExecutionFailure("delivery_metadata_invalid", "slack webhook delivery target reference is missing", nil)
	}
	if s.subscriptionRepo == nil {
		return notificationDestination{}, retryableNotificationExecutionFailure("rehydration_failed", "notification subscription repository is not configured", nil)
	}
	target, err := s.subscriptionRepo.GetTargetByID(ctx, strings.TrimSpace(*delivery.NotificationTargetID))
	if err != nil {
		if errors.Is(err, repository.ErrNotificationTargetNotFound) {
			return notificationDestination{}, permanentNotificationExecutionFailure("shared_target_missing", "slack webhook target no longer exists", err)
		}
		return notificationDestination{}, retryableNotificationExecutionFailure("rehydration_failed", "slack webhook target lookup failed", err)
	}
	if !target.Enabled {
		return notificationDestination{}, permanentNotificationExecutionFailure("shared_target_disabled", "slack webhook target is disabled", nil)
	}
	if target.Type != domain.NotificationTargetTypeSlackWebhook {
		return notificationDestination{}, permanentNotificationExecutionFailure("delivery_metadata_invalid", "slack webhook target type no longer matches the persisted delivery", nil)
	}
	destination, err := notificationTargetDestination(target)
	if err != nil {
		return notificationDestination{}, permanentNotificationExecutionFailure("delivery_metadata_invalid", "slack webhook target metadata is invalid", err)
	}
	if destination.destinationKey != delivery.DestinationKey {
		return notificationDestination{}, permanentNotificationExecutionFailure("shared_target_mismatch", "slack webhook target no longer matches the persisted logical destination", nil)
	}
	return destination, nil
}

func (s *BuildNotificationService) rehydrateSlackDMDestination(ctx context.Context, delivery domain.NotificationDelivery) (notificationDestination, error) {
	if delivery.RecipientUserID == nil || delivery.SlackWorkspaceIntegrationID == nil {
		return notificationDestination{}, permanentNotificationExecutionFailure("delivery_metadata_invalid", "slack identity delivery references are missing", nil)
	}
	if s.identityRepo == nil || s.workspaceRepo == nil || s.slackClient == nil {
		return notificationDestination{}, retryableNotificationExecutionFailure("rehydration_failed", "slack delivery dependencies are not configured", nil)
	}
	identity, err := s.identityRepo.GetByUserID(ctx, strings.TrimSpace(*delivery.RecipientUserID))
	if err != nil {
		if errors.Is(err, repository.ErrUserSlackIdentityNotFound) {
			return notificationDestination{}, permanentNotificationExecutionFailure("slack_identity_missing", "slack identity no longer exists", err)
		}
		return notificationDestination{}, retryableNotificationExecutionFailure("rehydration_failed", "slack identity lookup failed", err)
	}
	if !identity.Enabled {
		return notificationDestination{}, permanentNotificationExecutionFailure("slack_identity_disabled", "slack identity is disabled", nil)
	}
	if strings.TrimSpace(identity.SlackWorkspaceIntegrationID) != strings.TrimSpace(*delivery.SlackWorkspaceIntegrationID) {
		return notificationDestination{}, permanentNotificationExecutionFailure("slack_identity_mismatch", "slack identity no longer matches the persisted workspace", nil)
	}
	integration, err := s.workspaceRepo.Get(ctx)
	if err != nil {
		if errors.Is(err, repository.ErrSlackWorkspaceIntegrationNotFound) {
			return notificationDestination{}, permanentNotificationExecutionFailure("slack_workspace_integration_missing", "slack workspace integration is unavailable", err)
		}
		return notificationDestination{}, retryableNotificationExecutionFailure("rehydration_failed", "slack workspace integration lookup failed", err)
	}
	if !integration.Enabled {
		return notificationDestination{}, permanentNotificationExecutionFailure("slack_workspace_integration_disabled", "slack workspace integration is disabled", nil)
	}
	if strings.TrimSpace(integration.ID) != strings.TrimSpace(*delivery.SlackWorkspaceIntegrationID) {
		return notificationDestination{}, permanentNotificationExecutionFailure("slack_workspace_integration_mismatch", "slack workspace integration no longer matches the persisted delivery", nil)
	}
	if strings.TrimSpace(integration.BotTokenSecret) == "" || !platformslack.IsSlackUserID(strings.TrimSpace(identity.SlackUserID)) {
		return notificationDestination{}, permanentNotificationExecutionFailure("slack_workspace_credentials_missing", "slack delivery credentials are unavailable", nil)
	}
	destination, err := slackDMDestination(identity.UserID, integration.ID, identity.SlackUserID, integration.BotTokenSecret)
	if err != nil {
		return notificationDestination{}, permanentNotificationExecutionFailure("delivery_metadata_invalid", "slack identity delivery metadata is invalid", err)
	}
	if destination.destinationKey != delivery.DestinationKey {
		return notificationDestination{}, permanentNotificationExecutionFailure("slack_identity_mismatch", "slack identity no longer matches the persisted logical destination", nil)
	}
	return destination, nil
}

func candidateRecoveryReason(status domain.NotificationDeliveryStatus) string {
	if status == domain.NotificationDeliveryStatusSending {
		return notificationRecoveryReasonStaleClaim
	}
	return notificationRecoveryReasonDueRetry
}

func permanentNotificationExecutionFailure(reason string, message string, cause error) error {
	return &notificationExecutionFailure{category: domain.NotificationDeliveryFailureCategoryPermanent, reason: reason, message: message, cause: cause}
}

func retryableNotificationExecutionFailure(reason string, message string, cause error) error {
	return &notificationExecutionFailure{category: domain.NotificationDeliveryFailureCategoryRetryable, reason: reason, retryable: true, message: message, cause: cause}
}

func (s *BuildNotificationService) recordClaimMetric(delivery domain.NotificationDelivery, recoveryReason string, outcome repository.NotificationDeliveryClaimOutcome) {
	switch outcome {
	case repository.NotificationDeliveryClaimOutcomeCreatedClaimed:
		s.recordDeliveryMetric(delivery, recoveryReason, observability.NotificationDeliveryOutcomeClaimAcquired)
	case repository.NotificationDeliveryClaimOutcomeRetryClaimed:
		s.recordDeliveryMetric(delivery, recoveryReason, observability.NotificationDeliveryOutcomeRetryClaimed)
	case repository.NotificationDeliveryClaimOutcomeStaleClaimReclaimed:
		s.recordDeliveryMetric(delivery, recoveryReason, observability.NotificationDeliveryOutcomeStaleClaimReclaimed)
	case repository.NotificationDeliveryClaimOutcomeClaimedByOther:
		s.recordDeliveryMetric(delivery, recoveryReason, observability.NotificationDeliveryOutcomeSkippedContention)
	case repository.NotificationDeliveryClaimOutcomeRetryNotDue:
		s.recordDeliveryMetric(delivery, recoveryReason, observability.NotificationDeliveryOutcomeSkippedNotDue)
	case repository.NotificationDeliveryClaimOutcomeAlreadySent, repository.NotificationDeliveryClaimOutcomePermanentlyFailed, repository.NotificationDeliveryClaimOutcomeAttemptsExhausted:
		s.recordDeliveryMetric(delivery, recoveryReason, observability.NotificationDeliveryOutcomeSkippedTerminal)
	default:
		s.recordDeliveryMetric(delivery, recoveryReason, observability.NotificationDeliveryOutcomeSkippedIneligible)
	}
}

func (s *BuildNotificationService) recordDeliveryMetric(delivery domain.NotificationDelivery, recoveryReason string, outcome string) {
	if s == nil || s.deliveryMetrics == nil {
		return
	}
	s.deliveryMetrics.IncOutcome(string(delivery.EventType), string(delivery.Transport), string(delivery.DestinationKind), recoveryReason, outcome)
}

func logNotificationDeliveryEvent(message string, delivery domain.NotificationDelivery, fields notificationLogFields) {
	parts := []string{
		fmt.Sprintf("delivery_id=%s", delivery.ID),
		fmt.Sprintf("build_id=%s", delivery.BuildID),
		fmt.Sprintf("event_type=%s", delivery.EventType),
		fmt.Sprintf("transport=%s", delivery.Transport),
		fmt.Sprintf("destination_kind=%s", delivery.DestinationKind),
		fmt.Sprintf("attempt=%d", delivery.Attempts),
	}
	if trimmed := strings.TrimSpace(fields.recoveryReason); trimmed != "" {
		parts = append(parts, fmt.Sprintf("recovery_reason=%s", trimmed))
	}
	if trimmed := strings.TrimSpace(fields.claimOutcome); trimmed != "" {
		parts = append(parts, fmt.Sprintf("claim_outcome=%s", trimmed))
	}
	if trimmed := strings.TrimSpace(fields.claimOwner); trimmed != "" {
		parts = append(parts, fmt.Sprintf("claim_owner=%s", trimmed))
	}
	if trimmed := strings.TrimSpace(fields.failureCategory); trimmed != "" {
		parts = append(parts, fmt.Sprintf("failure_category=%s", trimmed))
	}
	if trimmed := strings.TrimSpace(fields.failureReason); trimmed != "" {
		parts = append(parts, fmt.Sprintf("failure_reason=%s", trimmed))
	}
	log.Printf("%s: %s", message, strings.Join(parts, " "))
}
