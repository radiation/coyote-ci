package build

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
)

type timeoutSlackError struct{}

func (timeoutSlackError) Error() string   { return "i/o timeout" }
func (timeoutSlackError) Timeout() bool   { return true }
func (timeoutSlackError) Temporary() bool { return true }

var _ net.Error = timeoutSlackError{}

type recordingSlackHTTPDoer struct {
	response *http.Response
	err      error
	req      *http.Request
	body     string
}

func (d *recordingSlackHTTPDoer) Do(req *http.Request) (*http.Response, error) {
	d.req = req
	if req.Body != nil {
		payload, _ := io.ReadAll(req.Body)
		d.body = string(payload)
	}
	if d.err != nil {
		return nil, d.err
	}
	if d.response == nil {
		d.response = &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok"))}
	}
	return d.response, nil
}

func TestSlackWebhookSender_Send(t *testing.T) {
	doer := &recordingSlackHTTPDoer{response: &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok"))}}
	sender := NewSlackWebhookSender(doer)
	if err := sender.Send(context.Background(), "https://hooks.slack.example/services/T/B/X", SlackWebhookMessage{Text: "hello"}); err != nil {
		t.Fatalf("send failed: %v", err)
	}
	if doer.req == nil || doer.req.Method != http.MethodPost {
		t.Fatalf("expected POST request, got %#v", doer.req)
	}
	if got := doer.req.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("expected application/json content type, got %q", got)
	}
	if !strings.Contains(doer.body, `"text":"hello"`) {
		t.Fatalf("expected text payload, got %q", doer.body)
	}
}

func TestSlackWebhookSender_SendErrorCases(t *testing.T) {
	doer := &recordingSlackHTTPDoer{response: &http.Response{StatusCode: http.StatusBadGateway, Body: io.NopCloser(strings.NewReader("bad"))}}
	sender := NewSlackWebhookSender(doer)
	if err := sender.Send(context.Background(), "https://hooks.slack.example/services/T/B/X", SlackWebhookMessage{Text: "hello"}); err == nil || !strings.Contains(err.Error(), "status 502") {
		t.Fatalf("expected non-2xx error, got %v", err)
	}

	doer = &recordingSlackHTTPDoer{err: timeoutSlackError{}}
	sender = NewSlackWebhookSender(doer)
	if err := sender.Send(context.Background(), "https://hooks.slack.example/services/T/B/X", SlackWebhookMessage{Text: "hello"}); err == nil || err.Error() != "slack webhook request timed out" {
		t.Fatalf("expected sanitized timeout error, got %v", err)
	}

	secretToken := "SECRET_TOKEN_123"
	doer = &recordingSlackHTTPDoer{err: errors.New("Post \"https://hooks.slack.example/services/T/B/" + secretToken + "\": dial tcp: lookup hooks.slack.example: i/o timeout")}
	sender = NewSlackWebhookSender(doer)
	err := sender.Send(context.Background(), "https://hooks.slack.example/services/T/B/"+secretToken, SlackWebhookMessage{Text: "hello"})
	if err == nil || err.Error() != "slack webhook request failed" {
		t.Fatalf("expected sanitized network error, got %v", err)
	}
	if strings.Contains(err.Error(), secretToken) {
		t.Fatalf("expected sanitized error without token leak, got %q", err.Error())
	}
}
