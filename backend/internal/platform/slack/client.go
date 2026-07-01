package slack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const authTestEndpoint = "https://slack.com/api/auth.test"

var ErrInvalidAuth = errors.New("slack invalid auth")
var ErrTokenRevoked = errors.New("slack token revoked")
var ErrAccountInactive = errors.New("slack account or workspace inactive")
var ErrRateLimited = errors.New("slack rate limited")
var ErrUpstreamFailure = errors.New("slack upstream failure")
var ErrMalformedResponse = errors.New("slack malformed response")
var ErrAuthTestFailed = errors.New("slack auth test failed")

type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

type AuthTestResult struct {
	WorkspaceID   string
	WorkspaceName *string
	WorkspaceURL  *string
	BotID         *string
	AuthedUserID  *string
	AppID         *string
}

type Client struct {
	httpClient HTTPDoer
}

func NewClient(httpClient HTTPDoer) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Second}
	}
	return &Client{httpClient: httpClient}
}

func (c *Client) TestAuthentication(ctx context.Context, token string) (AuthTestResult, error) {
	trimmedToken := strings.TrimSpace(token)
	if trimmedToken == "" {
		return AuthTestResult{}, ErrInvalidAuth
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, authTestEndpoint, nil)
	if err != nil {
		return AuthTestResult{}, ErrAuthTestFailed
	}
	req.Header.Set("Authorization", "Bearer "+trimmedToken)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return AuthTestResult{}, err
		}
		return AuthTestResult{}, ErrUpstreamFailure
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode == http.StatusTooManyRequests {
		return AuthTestResult{}, ErrRateLimited
	}
	if resp.StatusCode >= http.StatusInternalServerError {
		return AuthTestResult{}, ErrUpstreamFailure
	}
	if resp.StatusCode != http.StatusOK {
		return AuthTestResult{}, ErrAuthTestFailed
	}

	var payload struct {
		OK     bool   `json:"ok"`
		Error  string `json:"error"`
		URL    string `json:"url"`
		Team   string `json:"team"`
		User   string `json:"user"`
		TeamID string `json:"team_id"`
		UserID string `json:"user_id"`
		BotID  string `json:"bot_id"`
		AppID  string `json:"app_id"`
	}
	if decodeErr := json.NewDecoder(resp.Body).Decode(&payload); decodeErr != nil {
		return AuthTestResult{}, ErrMalformedResponse
	}
	if !payload.OK {
		switch strings.TrimSpace(payload.Error) {
		case "invalid_auth", "not_authed":
			return AuthTestResult{}, ErrInvalidAuth
		case "token_revoked":
			return AuthTestResult{}, ErrTokenRevoked
		case "account_inactive", "team_disabled", "org_login_required":
			return AuthTestResult{}, ErrAccountInactive
		case "ratelimited":
			return AuthTestResult{}, ErrRateLimited
		default:
			return AuthTestResult{}, fmt.Errorf("%w: %s", ErrAuthTestFailed, strings.TrimSpace(payload.Error))
		}
	}

	workspaceID := strings.TrimSpace(payload.TeamID)
	if workspaceID == "" {
		return AuthTestResult{}, ErrMalformedResponse
	}

	result := AuthTestResult{
		WorkspaceID:   workspaceID,
		WorkspaceName: optionalString(payload.Team),
		WorkspaceURL:  optionalString(payload.URL),
		BotID:         optionalString(payload.BotID),
		AuthedUserID:  optionalString(payload.UserID),
		AppID:         optionalString(payload.AppID),
	}
	return result, nil
}

func optionalString(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
