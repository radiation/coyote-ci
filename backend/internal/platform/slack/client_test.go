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
	if result.BotID == nil || *result.BotID != "B123" {
		t.Fatalf("expected bot id B123, got %v", result.BotID)
	}
	if result.AuthedUserID == nil || *result.AuthedUserID != "U123" {
		t.Fatalf("expected authed user id U123, got %v", result.AuthedUserID)
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

func TestClient_TestAuthentication_RejectsBlankToken(t *testing.T) {
	client := NewClient(nil)

	_, err := client.TestAuthentication(context.Background(), "   ")
	if !errors.Is(err, ErrInvalidAuth) {
		t.Fatalf("expected invalid auth for blank token, got %v", err)
	}
}

func TestClient_TestAuthentication_AuthFailureFallbacks(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		expectErr error
	}{
		{name: "not authed", body: `{"ok":false,"error":"not_authed"}`, expectErr: ErrInvalidAuth},
		{name: "payload rate limited", body: `{"ok":false,"error":"ratelimited"}`, expectErr: ErrRateLimited},
		{name: "unknown auth failure", body: `{"ok":false,"error":"something_else"}`, expectErr: ErrAuthTestFailed},
		{name: "non ok status", body: `{"ok":false}`, expectErr: ErrAuthTestFailed},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := NewClient(&recordingDoer{response: &http.Response{StatusCode: http.StatusBadRequest, Body: io.NopCloser(strings.NewReader(tc.body))}})
			if tc.name != "non ok status" {
				client = NewClient(&recordingDoer{response: &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(tc.body))}})
			}

			_, err := client.TestAuthentication(context.Background(), "xoxb-secret")
			if !errors.Is(err, tc.expectErr) {
				t.Fatalf("expected %v, got %v", tc.expectErr, err)
			}
		})
	}
}

func TestClient_LookupUserByEmail_Success(t *testing.T) {
	doer := &recordingDoer{response: &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"ok":true,"user":{"id":"U123","name":"bryan","profile":{"display_name":"Bryan","real_name":"Bryan Choate","email":"bryan@example.com","image_72":"https://images.example/avatar.png"}}}`))}}
	client := NewClient(doer)

	user, err := client.LookupUserByEmail(context.Background(), "xoxb-secret", " bryan@example.com ")
	if err != nil {
		t.Fatalf("lookup user by email: %v", err)
	}
	if user.ID != "U123" {
		t.Fatalf("expected slack user id U123, got %q", user.ID)
	}
	if user.Handle == nil || *user.Handle != "bryan" {
		t.Fatalf("expected handle bryan, got %v", user.Handle)
	}
	if doer.req == nil {
		t.Fatal("expected request")
	}
	if got := doer.req.Header.Get("Authorization"); got != "Bearer xoxb-secret" {
		t.Fatalf("expected bearer header, got %q", got)
	}
	if got := doer.req.URL.Query().Get("email"); got != "bryan@example.com" {
		t.Fatalf("expected email query bryan@example.com, got %q", got)
	}
}

func TestClient_LookupUserByEmail_Errors(t *testing.T) {
	tests := []struct {
		name      string
		response  *http.Response
		err       error
		expectErr error
	}{
		{name: "users not found", response: &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"ok":false,"error":"users_not_found"}`))}, expectErr: ErrUsersNotFound},
		{name: "missing scope", response: &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"ok":false,"error":"missing_scope","needed":"users:read.email","provided":"chat:write"}`))}, expectErr: ErrMissingScope},
		{name: "invalid auth", response: &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"ok":false,"error":"invalid_auth"}`))}, expectErr: ErrInvalidAuth},
		{name: "token revoked", response: &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"ok":false,"error":"token_revoked"}`))}, expectErr: ErrTokenRevoked},
		{name: "deleted user", response: &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"ok":true,"user":{"id":"U123","deleted":true}}`))}, expectErr: ErrDeletedUser},
		{name: "bot user", response: &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"ok":true,"user":{"id":"U123","is_bot":true}}`))}, expectErr: ErrBotUser},
		{name: "app user", response: &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"ok":true,"user":{"id":"U123","is_app_user":true}}`))}, expectErr: ErrAppUser},
		{name: "malformed user", response: &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"ok":true,"user":{}}`))}, expectErr: ErrMalformedResponse},
		{name: "network", err: errors.New("dial error"), expectErr: ErrUpstreamFailure},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := NewClient(&recordingDoer{response: tc.response, err: tc.err})
			_, err := client.LookupUserByEmail(context.Background(), "xoxb-secret", "user@example.com")
			if !errors.Is(err, tc.expectErr) {
				t.Fatalf("expected %v, got %v", tc.expectErr, err)
			}
			if strings.Contains(err.Error(), "xoxb-secret") {
				t.Fatalf("token leaked in error: %q", err.Error())
			}
			var missingScopeErr *MissingScopeError
			if errors.As(err, &missingScopeErr) {
				if missingScopeErr.Needed != "users:read.email" {
					t.Fatalf("expected needed scope users:read.email, got %q", missingScopeErr.Needed)
				}
			}
		})
	}
}
