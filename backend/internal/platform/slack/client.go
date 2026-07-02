package slack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const authTestEndpoint = "https://slack.com/api/auth.test"
const usersLookupByEmailEndpoint = "https://slack.com/api/users.lookupByEmail?email=%s"
const chatPostMessageEndpoint = "https://slack.com/api/chat.postMessage"

var ErrInvalidAuth = errors.New("slack invalid auth")
var ErrTokenRevoked = errors.New("slack token revoked")
var ErrAccountInactive = errors.New("slack account or workspace inactive")
var ErrRateLimited = errors.New("slack rate limited")
var ErrUpstreamFailure = errors.New("slack upstream failure")
var ErrMalformedResponse = errors.New("slack malformed response")
var ErrAuthTestFailed = errors.New("slack auth test failed")
var ErrUsersLookupByEmailFailed = errors.New("slack users.lookupByEmail failed")
var ErrUsersNotFound = errors.New("slack user not found")
var ErrMissingScope = errors.New("slack missing scope")
var ErrDeletedUser = errors.New("slack user is deleted")
var ErrBotUser = errors.New("slack user is a bot")
var ErrAppUser = errors.New("slack user is an app user")
var ErrPostMessageFailed = errors.New("slack chat.postMessage failed")
var ErrSlackUserIDInvalid = errors.New("slack user id is invalid")
var ErrChannelNotFound = errors.New("slack channel not found")
var ErrSlackUserNotFound = errors.New("slack user not found")
var ErrChannelArchived = errors.New("slack channel is archived")

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

type User struct {
	ID              string
	DisplayName     *string
	RealName        *string
	Handle          *string
	Email           *string
	ProfileImageURL *string
}

type Message struct {
	Text string
}

type PostMessageResult struct {
	ChannelID *string
	Timestamp *string
}

type MissingScopeError struct {
	Needed   string
	Provided string
}

func (e *MissingScopeError) Error() string {
	needed := strings.TrimSpace(e.Needed)
	if needed == "" {
		return ErrMissingScope.Error()
	}
	return fmt.Sprintf("%s: %s", ErrMissingScope.Error(), needed)
}

func (e *MissingScopeError) Unwrap() error {
	return ErrMissingScope
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

func (c *Client) LookupUserByEmail(ctx context.Context, token string, email string) (User, error) {
	trimmedToken := strings.TrimSpace(token)
	if trimmedToken == "" {
		return User{}, ErrInvalidAuth
	}
	trimmedEmail := strings.TrimSpace(email)
	if trimmedEmail == "" {
		return User{}, ErrUsersLookupByEmailFailed
	}

	endpoint := fmt.Sprintf(usersLookupByEmailEndpoint, url.QueryEscape(trimmedEmail))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return User{}, ErrUsersLookupByEmailFailed
	}
	req.Header.Set("Authorization", "Bearer "+trimmedToken)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return User{}, err
		}
		return User{}, ErrUpstreamFailure
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode == http.StatusTooManyRequests {
		return User{}, ErrRateLimited
	}
	if resp.StatusCode >= http.StatusInternalServerError {
		return User{}, ErrUpstreamFailure
	}
	if resp.StatusCode != http.StatusOK {
		return User{}, ErrUsersLookupByEmailFailed
	}

	var payload struct {
		OK       bool   `json:"ok"`
		Error    string `json:"error"`
		Needed   string `json:"needed"`
		Provided string `json:"provided"`
		User     struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			Deleted   bool   `json:"deleted"`
			IsBot     bool   `json:"is_bot"`
			IsAppUser bool   `json:"is_app_user"`
			Profile   struct {
				DisplayName string `json:"display_name"`
				RealName    string `json:"real_name"`
				Email       string `json:"email"`
				Image72     string `json:"image_72"`
			} `json:"profile"`
		} `json:"user"`
	}
	if decodeErr := json.NewDecoder(resp.Body).Decode(&payload); decodeErr != nil {
		return User{}, ErrMalformedResponse
	}
	if !payload.OK {
		switch strings.TrimSpace(payload.Error) {
		case "users_not_found":
			return User{}, ErrUsersNotFound
		case "missing_scope":
			return User{}, &MissingScopeError{Needed: payload.Needed, Provided: payload.Provided}
		case "invalid_auth", "not_authed":
			return User{}, ErrInvalidAuth
		case "token_revoked":
			return User{}, ErrTokenRevoked
		case "account_inactive", "team_disabled", "org_login_required":
			return User{}, ErrAccountInactive
		case "ratelimited":
			return User{}, ErrRateLimited
		default:
			return User{}, fmt.Errorf("%w: %s", ErrUsersLookupByEmailFailed, strings.TrimSpace(payload.Error))
		}
	}

	userID := strings.TrimSpace(payload.User.ID)
	if userID == "" {
		return User{}, ErrMalformedResponse
	}
	if payload.User.Deleted {
		return User{}, ErrDeletedUser
	}
	if payload.User.IsBot {
		return User{}, ErrBotUser
	}
	if payload.User.IsAppUser {
		return User{}, ErrAppUser
	}

	return User{
		ID:              userID,
		DisplayName:     optionalString(payload.User.Profile.DisplayName),
		RealName:        optionalString(payload.User.Profile.RealName),
		Handle:          optionalString(payload.User.Name),
		Email:           optionalString(payload.User.Profile.Email),
		ProfileImageURL: optionalString(payload.User.Profile.Image72),
	}, nil
}

func (c *Client) PostDirectMessage(ctx context.Context, token string, slackUserID string, message Message) (PostMessageResult, error) {
	trimmedToken := strings.TrimSpace(token)
	if trimmedToken == "" {
		return PostMessageResult{}, ErrInvalidAuth
	}
	trimmedSlackUserID := strings.TrimSpace(slackUserID)
	if !IsSlackUserID(trimmedSlackUserID) {
		return PostMessageResult{}, ErrSlackUserIDInvalid
	}
	trimmedText := strings.TrimSpace(message.Text)
	if trimmedText == "" {
		return PostMessageResult{}, ErrPostMessageFailed
	}

	payload, err := json.Marshal(map[string]string{
		"channel": trimmedSlackUserID,
		"text":    trimmedText,
	})
	if err != nil {
		return PostMessageResult{}, ErrPostMessageFailed
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, chatPostMessageEndpoint, strings.NewReader(string(payload)))
	if err != nil {
		return PostMessageResult{}, ErrPostMessageFailed
	}
	req.Header.Set("Authorization", "Bearer "+trimmedToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return PostMessageResult{}, err
		}
		return PostMessageResult{}, ErrUpstreamFailure
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode == http.StatusTooManyRequests {
		return PostMessageResult{}, ErrRateLimited
	}
	if resp.StatusCode >= http.StatusInternalServerError {
		return PostMessageResult{}, ErrUpstreamFailure
	}
	if resp.StatusCode != http.StatusOK {
		return PostMessageResult{}, ErrPostMessageFailed
	}

	var payloadResponse struct {
		OK       bool   `json:"ok"`
		Error    string `json:"error"`
		Needed   string `json:"needed"`
		Provided string `json:"provided"`
		Channel  string `json:"channel"`
		TS       string `json:"ts"`
	}
	if decodeErr := json.NewDecoder(resp.Body).Decode(&payloadResponse); decodeErr != nil {
		return PostMessageResult{}, ErrMalformedResponse
	}
	if !payloadResponse.OK {
		errorCode, ok := normalizeSlackErrorCode(payloadResponse.Error)
		if !ok {
			return PostMessageResult{}, ErrPostMessageFailed
		}
		switch errorCode {
		case "missing_scope":
			return PostMessageResult{}, &MissingScopeError{Needed: payloadResponse.Needed, Provided: payloadResponse.Provided}
		case "invalid_auth", "not_authed":
			return PostMessageResult{}, ErrInvalidAuth
		case "token_revoked":
			return PostMessageResult{}, ErrTokenRevoked
		case "account_inactive", "team_disabled", "org_login_required":
			return PostMessageResult{}, ErrAccountInactive
		case "channel_not_found":
			return PostMessageResult{}, ErrChannelNotFound
		case "user_not_found":
			return PostMessageResult{}, ErrSlackUserNotFound
		case "is_archived":
			return PostMessageResult{}, ErrChannelArchived
		case "ratelimited":
			return PostMessageResult{}, ErrRateLimited
		default:
			return PostMessageResult{}, fmt.Errorf("%w: %s", ErrPostMessageFailed, errorCode)
		}
	}

	result := PostMessageResult{
		ChannelID: optionalString(payloadResponse.Channel),
		Timestamp: optionalString(payloadResponse.TS),
	}
	if result.ChannelID == nil || result.Timestamp == nil {
		return PostMessageResult{}, ErrMalformedResponse
	}
	return result, nil
}

func IsSlackUserID(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	if !strings.HasPrefix(trimmed, "U") && !strings.HasPrefix(trimmed, "W") {
		return false
	}
	for _, ch := range trimmed {
		if (ch < 'A' || ch > 'Z') && (ch < '0' || ch > '9') {
			return false
		}
	}
	return true
}

func normalizeSlackErrorCode(value string) (string, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || len(trimmed) > 64 {
		return "", false
	}
	for _, ch := range trimmed {
		if (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') && ch != '_' {
			return "", false
		}
	}
	return trimmed, true
}

func optionalString(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
