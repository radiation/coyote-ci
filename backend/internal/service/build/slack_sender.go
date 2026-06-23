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
		client = &http.Client{Timeout: 5 * time.Second}
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
		return errors.New("build slack webhook request failed")
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := s.client.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return errors.New("slack webhook request timed out")
		}
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return errors.New("slack webhook request timed out")
		}
		return errors.New("slack webhook request failed")
	}
	defer func() {
		_ = res.Body.Close()
	}()
	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("slack webhook returned status %d", res.StatusCode)
	}
	return nil
}
