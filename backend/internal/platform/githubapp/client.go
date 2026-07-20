package githubapp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const defaultAPIVersion = "2022-11-28"

var defaultTokenRefreshSkew = time.Minute

const maxGitHubProbeResponseBytes = 64 << 10
const maxGitHubTokenResponseBytes = 64 << 10

type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

type InstallationTokenRequest struct {
	AppRegistrationID string
	AppID             string
	InstallationID    string
	APIBaseURL        string
	PrivateKeyPEM     string
}

type InstallationToken struct {
	Value     string
	ExpiresAt time.Time
}

type InstallationProbeResult struct {
	InstallationID string
	AccountLogin   string
	Suspended      bool
}

type installationProbeResponse struct {
	TotalCount   *int `json:"total_count"`
	Repositories []struct {
		ID int64 `json:"id"`
	} `json:"repositories"`
}

type installationDetailsResponse struct {
	ID          json.Number `json:"id"`
	SuspendedAt *string     `json:"suspended_at"`
	Account     struct {
		Login string `json:"login"`
	} `json:"account"`
}

type Client struct {
	httpClient  httpDoer
	signer      *JWTSigner
	now         func() time.Time
	refreshSkew time.Duration

	mu    sync.Mutex
	cache map[string]cachedInstallationToken
	wait  map[string]chan struct{}
}

type cachedInstallationToken struct {
	token InstallationToken
}

func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{
		httpClient:  httpClient,
		signer:      NewJWTSigner(),
		now:         time.Now,
		refreshSkew: defaultTokenRefreshSkew,
		cache:       map[string]cachedInstallationToken{},
		wait:        map[string]chan struct{}{},
	}
}

func (c *Client) GetInstallationToken(ctx context.Context, input InstallationTokenRequest) (InstallationToken, error) {
	token, _, err := c.getInstallationToken(ctx, input)
	return token, err
}

func (c *Client) getInstallationToken(ctx context.Context, input InstallationTokenRequest) (InstallationToken, bool, error) {
	key := installationCacheKey(input)
	for {
		now := c.now().UTC()
		c.mu.Lock()
		if entry, ok := c.cache[key]; ok && entry.token.Value != "" && now.Before(entry.token.ExpiresAt.Add(-c.refreshSkew)) {
			token := entry.token
			c.mu.Unlock()
			return token, true, nil
		}
		if waitCh, ok := c.wait[key]; ok {
			c.mu.Unlock()
			select {
			case <-ctx.Done():
				return InstallationToken{}, false, ctx.Err()
			case <-waitCh:
				continue
			}
		}
		waitCh := make(chan struct{})
		c.wait[key] = waitCh
		c.mu.Unlock()

		token, err := c.exchangeInstallationToken(ctx, input)

		c.mu.Lock()
		if err == nil {
			c.cache[key] = cachedInstallationToken{token: token}
		}
		waitCh = c.wait[key]
		delete(c.wait, key)
		close(waitCh)
		c.mu.Unlock()

		if err != nil {
			return InstallationToken{}, false, err
		}
		return token, false, nil
	}
}

func (c *Client) ProbeInstallation(ctx context.Context, input InstallationTokenRequest) (InstallationProbeResult, error) {
	token, fromCache, err := c.getInstallationToken(ctx, input)
	if err != nil {
		return InstallationProbeResult{}, err
	}
	result, err := c.probeInstallationWithToken(ctx, input, token)
	if err == nil {
		return result, nil
	}
	if err == ErrAuthentication && fromCache {
		c.invalidateCachedToken(input, token)
		refreshedToken, _, refreshErr := c.getInstallationToken(ctx, input)
		if refreshErr != nil {
			return InstallationProbeResult{}, refreshErr
		}
		return c.probeInstallationWithToken(ctx, input, refreshedToken)
	}
	return InstallationProbeResult{}, err
}

func (c *Client) exchangeInstallationToken(ctx context.Context, input InstallationTokenRequest) (InstallationToken, error) {
	jwtToken, err := c.signer.Sign(input.AppID, input.PrivateKeyPEM)
	if err != nil {
		return InstallationToken{}, err
	}
	exchangeURL := strings.TrimRight(strings.TrimSpace(input.APIBaseURL), "/") + "/app/installations/" + url.PathEscape(strings.TrimSpace(input.InstallationID)) + "/access_tokens"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, exchangeURL, bytes.NewBufferString("{}"))
	if err != nil {
		return InstallationToken{}, err
	}
	setGitHubHeaders(req, jwtToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return InstallationToken{}, ctx.Err()
		}
		return InstallationToken{}, ErrProviderUnavailable
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		return InstallationToken{}, classifyGitHubResponse(resp)
	}
	var payload struct {
		Token     string `json:"token"`
		ExpiresAt string `json:"expires_at"`
	}
	decodeErr := decodeGitHubJSON(resp.Body, maxGitHubTokenResponseBytes, &payload)
	if decodeErr != nil {
		return InstallationToken{}, ErrMalformedResponse
	}
	expiresAt, err := time.Parse(time.RFC3339, strings.TrimSpace(payload.ExpiresAt))
	if err != nil {
		return InstallationToken{}, ErrMalformedResponse
	}
	if strings.TrimSpace(payload.Token) == "" {
		return InstallationToken{}, ErrMalformedResponse
	}
	return InstallationToken{Value: strings.TrimSpace(payload.Token), ExpiresAt: expiresAt.UTC()}, nil
}

func installationCacheKey(input InstallationTokenRequest) string {
	return strings.TrimSpace(input.AppRegistrationID) + "|" + strings.TrimSpace(input.InstallationID) + "|" + strings.TrimRight(strings.TrimSpace(input.APIBaseURL), "/")
}

func (c *Client) invalidateCachedToken(input InstallationTokenRequest, failedToken InstallationToken) {
	key := installationCacheKey(input)
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.cache[key]
	if !ok {
		return
	}
	if entry.token == failedToken {
		delete(c.cache, key)
	}
}

func setGitHubHeaders(req *http.Request, bearerToken string) {
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(bearerToken))
	req.Header.Set("X-GitHub-Api-Version", defaultAPIVersion)
}

func classifyGitHubResponse(resp *http.Response) error {
	message := readGitHubMessage(resp.Body)
	if isRateLimited(resp, message) {
		return ErrRateLimited
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return ErrAuthentication
	}
	if resp.StatusCode == http.StatusForbidden {
		if strings.Contains(message, "suspend") || strings.Contains(message, "revok") || strings.Contains(message, "removed") {
			return ErrInstallationUnavailable
		}
		return ErrAuthentication
	}
	if resp.StatusCode == http.StatusNotFound {
		return ErrInstallationUnavailable
	}
	if resp.StatusCode >= http.StatusInternalServerError {
		return ErrProviderUnavailable
	}
	return ErrProviderUnavailable
}

func isRateLimited(resp *http.Response, message string) bool {
	if resp.StatusCode == http.StatusTooManyRequests {
		return true
	}
	if strings.TrimSpace(resp.Header.Get("Retry-After")) != "" {
		return true
	}
	if strings.TrimSpace(resp.Header.Get("X-RateLimit-Remaining")) == "0" {
		return true
	}
	return strings.Contains(message, "rate limit")
}

func readGitHubMessage(body io.Reader) string {
	data, err := io.ReadAll(io.LimitReader(body, 4096))
	if err != nil {
		return ""
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return ""
	}
	var payload struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(data, &payload); err == nil && strings.TrimSpace(payload.Message) != "" {
		return strings.ToLower(strings.TrimSpace(payload.Message))
	}
	if json.Valid(data) {
		return ""
	}
	return strings.ToLower(trimmed)
}

func installationIDString(value int64) string {
	return strconv.FormatInt(value, 10)
}

func decodeGitHubJSON(body io.Reader, maxBytes int64, target any) error {
	return json.NewDecoder(io.LimitReader(body, maxBytes)).Decode(target)
}

func (c *Client) probeInstallationWithToken(ctx context.Context, input InstallationTokenRequest, token InstallationToken) (InstallationProbeResult, error) {
	probeURL := strings.TrimRight(strings.TrimSpace(input.APIBaseURL), "/") + "/installation/repositories?per_page=1"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
	if err != nil {
		return InstallationProbeResult{}, err
	}
	setGitHubHeaders(req, token.Value)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return InstallationProbeResult{}, ctx.Err()
		}
		return InstallationProbeResult{}, ErrProviderUnavailable
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return InstallationProbeResult{}, classifyGitHubResponse(resp)
	}
	var payload installationProbeResponse
	if err := decodeGitHubJSON(resp.Body, maxGitHubProbeResponseBytes, &payload); err != nil {
		return InstallationProbeResult{}, ErrMalformedResponse
	}
	if payload.TotalCount == nil || payload.Repositories == nil {
		return InstallationProbeResult{}, ErrMalformedResponse
	}
	return c.fetchInstallationDetails(ctx, input)
}

func (c *Client) fetchInstallationDetails(ctx context.Context, input InstallationTokenRequest) (InstallationProbeResult, error) {
	jwtToken, err := c.signer.Sign(input.AppID, input.PrivateKeyPEM)
	if err != nil {
		return InstallationProbeResult{}, err
	}
	probeURL := strings.TrimRight(strings.TrimSpace(input.APIBaseURL), "/") + "/app/installations/" + url.PathEscape(strings.TrimSpace(input.InstallationID))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
	if err != nil {
		return InstallationProbeResult{}, err
	}
	setGitHubHeaders(req, jwtToken)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return InstallationProbeResult{}, ctx.Err()
		}
		return InstallationProbeResult{}, ErrProviderUnavailable
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return InstallationProbeResult{}, classifyGitHubResponse(resp)
	}
	var payload installationDetailsResponse
	if err := decodeGitHubJSON(resp.Body, maxGitHubProbeResponseBytes, &payload); err != nil {
		return InstallationProbeResult{}, ErrMalformedResponse
	}
	installationID := strings.TrimSpace(payload.ID.String())
	if installationID == "" {
		return InstallationProbeResult{}, ErrMalformedResponse
	}
	if installationID != strings.TrimSpace(input.InstallationID) {
		return InstallationProbeResult{}, ErrInstallationUnavailable
	}
	accountLogin := strings.TrimSpace(payload.Account.Login)
	if accountLogin == "" {
		return InstallationProbeResult{}, ErrMalformedResponse
	}
	suspended := payload.SuspendedAt != nil && strings.TrimSpace(*payload.SuspendedAt) != ""
	if suspended {
		return InstallationProbeResult{}, ErrInstallationUnavailable
	}
	return InstallationProbeResult{InstallationID: installationID, AccountLogin: accountLogin, Suspended: false}, nil
}
