package build

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	platformemail "github.com/radiation/coyote-ci/backend/internal/platform/email"
	platformslack "github.com/radiation/coyote-ci/backend/internal/platform/slack"
)

func TestClassifyNotificationSendError(t *testing.T) {
	tests := []struct {
		name      string
		transport domain.NotificationTransport
		err       error
		want      notificationFailureDecision
	}{
		{name: "context canceled", transport: domain.NotificationTransportEmail, err: context.Canceled, want: notificationFailureDecision{reason: "context_canceled"}},
		{name: "context deadline exceeded", transport: domain.NotificationTransportEmail, err: context.DeadlineExceeded, want: notificationFailureDecision{reason: "context_canceled"}},
		{name: "network timeout", transport: domain.NotificationTransportEmail, err: timeoutNetError{}, want: notificationFailureDecision{category: domain.NotificationDeliveryFailureCategoryRetryable, reason: "network_timeout", retryable: true}},
		{name: "invalid email permanent", transport: domain.NotificationTransportEmail, err: platformemail.ErrInvalidMessage, want: notificationFailureDecision{category: domain.NotificationDeliveryFailureCategoryPermanent, reason: "invalid_email_message"}},
		{name: "email default retryable", transport: domain.NotificationTransportEmail, err: errors.New("smtp boom"), want: notificationFailureDecision{category: domain.NotificationDeliveryFailureCategoryRetryable, reason: "email_send_failed", retryable: true}},
		{name: "slack webhook invalid request", transport: domain.NotificationTransportSlackWebhook, err: ErrSlackWebhookInvalidRequest, want: notificationFailureDecision{category: domain.NotificationDeliveryFailureCategoryPermanent, reason: "invalid_slack_webhook_request"}},
		{name: "slack webhook upstream retryable", transport: domain.NotificationTransportSlackWebhook, err: ErrSlackWebhookUpstreamFailure, want: notificationFailureDecision{category: domain.NotificationDeliveryFailureCategoryRetryable, reason: "slack_webhook_upstream_failure", retryable: true}},
		{name: "slack webhook retryable http", transport: domain.NotificationTransportSlackWebhook, err: &SlackWebhookHTTPError{StatusCode: 429}, want: notificationFailureDecision{category: domain.NotificationDeliveryFailureCategoryRetryable, reason: "slack_webhook_http_retryable", retryable: true}},
		{name: "slack webhook permanent http", transport: domain.NotificationTransportSlackWebhook, err: &SlackWebhookHTTPError{StatusCode: 400}, want: notificationFailureDecision{category: domain.NotificationDeliveryFailureCategoryPermanent, reason: "slack_webhook_http_permanent"}},
		{name: "slack webhook default retryable", transport: domain.NotificationTransportSlackWebhook, err: errors.New("webhook boom"), want: notificationFailureDecision{category: domain.NotificationDeliveryFailureCategoryRetryable, reason: "slack_webhook_send_failed", retryable: true}},
		{name: "slack dm retryable upstream", transport: domain.NotificationTransportSlackDM, err: platformslack.ErrUpstreamFailure, want: notificationFailureDecision{category: domain.NotificationDeliveryFailureCategoryRetryable, reason: "slack_dm_retryable", retryable: true}},
		{name: "slack dm permanent invalid auth", transport: domain.NotificationTransportSlackDM, err: platformslack.ErrInvalidAuth, want: notificationFailureDecision{category: domain.NotificationDeliveryFailureCategoryPermanent, reason: "slack_dm_permanent"}},
		{name: "slack dm default retryable", transport: domain.NotificationTransportSlackDM, err: errors.New("dm boom"), want: notificationFailureDecision{category: domain.NotificationDeliveryFailureCategoryRetryable, reason: "slack_dm_send_failed", retryable: true}},
		{name: "unsupported transport permanent", transport: domain.NotificationTransport("pager"), err: errors.New("boom"), want: notificationFailureDecision{category: domain.NotificationDeliveryFailureCategoryPermanent, reason: "unsupported_transport"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyNotificationSendError(tc.transport, tc.err); got != tc.want {
				t.Fatalf("expected %+v, got %+v", tc.want, got)
			}
		})
	}
}

type timeoutNetError struct{}

func (timeoutNetError) Error() string   { return "i/o timeout" }
func (timeoutNetError) Timeout() bool   { return true }
func (timeoutNetError) Temporary() bool { return false }

var _ net.Error = timeoutNetError{}
