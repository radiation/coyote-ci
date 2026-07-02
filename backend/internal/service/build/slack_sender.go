package build

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

var ErrSlackWebhookInvalidRequest = errors.New("slack webhook request is invalid")
var ErrSlackWebhookUpstreamFailure = errors.New("slack webhook upstream failure")

const defaultSlackWebhookTimeout = 5 * time.Second

type slackWebhookUpstreamError struct {
	timeout bool
}

func (e *slackWebhookUpstreamError) Error() string {
	if e.timeout {
		return "slack webhook request timed out"
	}
	return "slack webhook request failed"
}

func (e *slackWebhookUpstreamError) Unwrap() error {
	return ErrSlackWebhookUpstreamFailure
}

type SlackWebhookHTTPError struct {
	StatusCode int
}

func (e *SlackWebhookHTTPError) Error() string {
	return fmt.Sprintf("slack webhook returned status %d", e.StatusCode)
}

type SlackWebhookMessage struct {
	Text string `json:"text"`
}

type SlackWebhookSender interface {
	Send(ctx context.Context, webhookURL string, message SlackWebhookMessage) error
}

type slackHTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

type slackWebhookSender struct {
	client slackHTTPDoer
}

func NewSlackWebhookSender(client slackHTTPDoer) SlackWebhookSender {
	if client == nil {
		client = &http.Client{Timeout: defaultSlackWebhookTimeout}
	}
	return &slackWebhookSender{client: client}
}

func (s *slackWebhookSender) Send(ctx context.Context, webhookURL string, message SlackWebhookMessage) error {
	payload, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("encode slack webhook payload failed: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSpace(webhookURL), bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrSlackWebhookInvalidRequest, err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := s.client.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return err
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return &slackWebhookUpstreamError{timeout: true}
		}
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return &slackWebhookUpstreamError{timeout: true}
		}
		return &slackWebhookUpstreamError{}
	}
	defer func() {
		_ = res.Body.Close()
	}()
	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		return &SlackWebhookHTTPError{StatusCode: res.StatusCode}
	}
	return nil
}
