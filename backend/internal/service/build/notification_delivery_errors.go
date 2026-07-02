package build

import (
	"context"
	"errors"
	"net"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	platformemail "github.com/radiation/coyote-ci/backend/internal/platform/email"
	platformslack "github.com/radiation/coyote-ci/backend/internal/platform/slack"
)

type notificationFailureDecision struct {
	category  domain.NotificationDeliveryFailureCategory
	reason    string
	retryable bool
}

func classifyNotificationSendError(transport domain.NotificationTransport, err error) notificationFailureDecision {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return notificationFailureDecision{reason: "context_canceled"}
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return notificationFailureDecision{category: domain.NotificationDeliveryFailureCategoryRetryable, reason: "network_timeout", retryable: true}
	}

	switch transport {
	case domain.NotificationTransportEmail:
		return classifyEmailNotificationError(err)
	case domain.NotificationTransportSlackWebhook:
		return classifySlackWebhookNotificationError(err)
	case domain.NotificationTransportSlackDM:
		return classifySlackDMNotificationError(err)
	default:
		return notificationFailureDecision{category: domain.NotificationDeliveryFailureCategoryPermanent, reason: "unsupported_transport"}
	}
}

func classifyNotificationDeliveryFailure(transport domain.NotificationTransport, err error) notificationFailureDecision {
	var executionErr *notificationExecutionFailure
	if errors.As(err, &executionErr) {
		return notificationFailureDecision{
			category:  executionErr.category,
			reason:    executionErr.reason,
			retryable: executionErr.retryable,
		}
	}
	return classifyNotificationSendError(transport, err)
}

func classifyEmailNotificationError(err error) notificationFailureDecision {
	if errors.Is(err, platformemail.ErrInvalidMessage) {
		return notificationFailureDecision{category: domain.NotificationDeliveryFailureCategoryPermanent, reason: "invalid_email_message"}
	}
	return notificationFailureDecision{category: domain.NotificationDeliveryFailureCategoryRetryable, reason: "email_send_failed", retryable: true}
}

func classifySlackWebhookNotificationError(err error) notificationFailureDecision {
	if errors.Is(err, ErrSlackWebhookInvalidRequest) {
		return notificationFailureDecision{category: domain.NotificationDeliveryFailureCategoryPermanent, reason: "invalid_slack_webhook_request"}
	}
	if errors.Is(err, ErrSlackWebhookUpstreamFailure) {
		return notificationFailureDecision{category: domain.NotificationDeliveryFailureCategoryRetryable, reason: "slack_webhook_upstream_failure", retryable: true}
	}
	var httpErr *SlackWebhookHTTPError
	if errors.As(err, &httpErr) {
		if httpErr.StatusCode == 429 || httpErr.StatusCode >= 500 {
			return notificationFailureDecision{category: domain.NotificationDeliveryFailureCategoryRetryable, reason: "slack_webhook_http_retryable", retryable: true}
		}
		return notificationFailureDecision{category: domain.NotificationDeliveryFailureCategoryPermanent, reason: "slack_webhook_http_permanent"}
	}
	return notificationFailureDecision{category: domain.NotificationDeliveryFailureCategoryRetryable, reason: "slack_webhook_send_failed", retryable: true}
}

func classifySlackDMNotificationError(err error) notificationFailureDecision {
	switch {
	case errors.Is(err, platformslack.ErrRateLimited), errors.Is(err, platformslack.ErrUpstreamFailure):
		return notificationFailureDecision{category: domain.NotificationDeliveryFailureCategoryRetryable, reason: "slack_dm_retryable", retryable: true}
	case errors.Is(err, platformslack.ErrInvalidAuth),
		errors.Is(err, platformslack.ErrTokenRevoked),
		errors.Is(err, platformslack.ErrAccountInactive),
		errors.Is(err, platformslack.ErrMissingScope),
		errors.Is(err, platformslack.ErrChannelNotFound),
		errors.Is(err, platformslack.ErrSlackUserNotFound),
		errors.Is(err, platformslack.ErrChannelArchived),
		errors.Is(err, platformslack.ErrSlackUserIDInvalid),
		errors.Is(err, platformslack.ErrMalformedResponse),
		errors.Is(err, platformslack.ErrPostMessageFailed):
		return notificationFailureDecision{category: domain.NotificationDeliveryFailureCategoryPermanent, reason: "slack_dm_permanent"}
	default:
		return notificationFailureDecision{category: domain.NotificationDeliveryFailureCategoryRetryable, reason: "slack_dm_send_failed", retryable: true}
	}
}
