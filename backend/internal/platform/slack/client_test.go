package slack

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type recordingDoer struct {
	response *http.Response
	err      error
	req      *http.Request
}

func (d *recordingDoer) Do(req *http.Request) (*http.Response, error) {
	d.req = req
	if d.err != nil {
		return nil, d.err
	}
	if d.response == nil {
		d.response = &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"ok":true,"team_id":"T1"}`))}
	}
	return d.response, nil
}

func TestClient_TestAuthentication_Success(t *testing.T) {
	doer := &recordingDoer{response: &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"ok":true,"team_id":"T123","team":"Coyote","url":"https://example.slack.com/","bot_id":"B123","user_id":"U123","app_id":"A123"}`))}}
	client := NewClient(doer)

	result, err := client.TestAuthentication(context.Background(), "xoxb-secret")
	if err != nil {
		t.Fatalf("test authentication: %v", err)
	}
	if result.WorkspaceID != "T123" {
		t.Fatalf("expected workspace id T123, got %q", result.WorkspaceID)
	}
	if doer.req == nil {
		t.Fatal("expected request")
	}
	if got := doer.req.Header.Get("Authorization"); got != "Bearer xoxb-secret" {
		t.Fatalf("expected bearer header, got %q", got)
	}
}

func TestClient_TestAuthentication_Errors(t *testing.T) {
	tests := []struct {
		name      string
		response  *http.Response
		err       error
		expectErr error
	}{
		{name: "invalid auth", response: &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"ok":false,"error":"invalid_auth"}`))}, expectErr: ErrInvalidAuth},
		{name: "token revoked", response: &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"ok":false,"error":"token_revoked"}`))}, expectErr: ErrTokenRevoked},
		{name: "inactive", response: &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"ok":false,"error":"account_inactive"}`))}, expectErr: ErrAccountInactive},
		{name: "rate limited status", response: &http.Response{StatusCode: http.StatusTooManyRequests, Body: io.NopCloser(strings.NewReader(""))}, expectErr: ErrRateLimited},
		{name: "upstream 5xx", response: &http.Response{StatusCode: http.StatusBadGateway, Body: io.NopCloser(strings.NewReader(""))}, expectErr: ErrUpstreamFailure},
		{name: "malformed response", response: &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"ok":true}`))}, expectErr: ErrMalformedResponse},
		{name: "network", err: errors.New("dial error"), expectErr: ErrUpstreamFailure},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := NewClient(&recordingDoer{response: tc.response, err: tc.err})
			_, err := client.TestAuthentication(context.Background(), "xoxb-secret")
			if !errors.Is(err, tc.expectErr) {
				t.Fatalf("expected %v, got %v", tc.expectErr, err)
			}
			if strings.Contains(err.Error(), "xoxb-secret") {
				t.Fatalf("token leaked in error: %q", err.Error())
			}
		})
	}
}

func TestClient_TestAuthentication_ContextCancellation(t *testing.T) {
	client := NewClient(&recordingDoer{err: context.DeadlineExceeded})
	_, err := client.TestAuthentication(context.Background(), "xoxb-secret")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
}
