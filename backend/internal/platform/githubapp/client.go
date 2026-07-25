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
const maxGitHubRepositoryResponseBytes = 128 << 10

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

type Repository struct {
	ID            string
	Owner         string
	Name          string
	FullName      string
	CloneURL      string
	WebURL        string
	DefaultBranch *string
	Archived      bool
	Disabled      bool
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

type repositoryResponse struct {
	ID            json.Number `json:"id"`
	Name          string      `json:"name"`
	FullName      string      `json:"full_name"`
	CloneURL      string      `json:"clone_url"`
	HTMLURL       string      `json:"html_url"`
	DefaultBranch string      `json:"default_branch"`
	Archived      bool        `json:"archived"`
	Disabled      bool        `json:"disabled"`
	Owner         struct {
		Login string `json:"login"`
	} `json:"owner"`
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

func (c *Client) GetRepositoryByID(ctx context.Context, input InstallationTokenRequest, repositoryID string) (Repository, error) {
	token, fromCache, err := c.getInstallationToken(ctx, input)
	if err != nil {
		return Repository{}, err
	}
	repository, err := c.getRepositoryByIDWithToken(ctx, input, token, repositoryID)
	if err == nil {
		return repository, nil
	}
	if err == ErrAuthentication && fromCache {
		c.invalidateCachedToken(input, token)
		refreshedToken, _, refreshErr := c.getInstallationToken(ctx, input)
		if refreshErr != nil {
			return Repository{}, refreshErr
		}
		return c.getRepositoryByIDWithToken(ctx, input, refreshedToken, repositoryID)
	}
	return Repository{}, err
}

func (c *Client) GetRepositoryByOwnerAndName(ctx context.Context, input InstallationTokenRequest, owner string, name string) (Repository, error) {
	token, fromCache, err := c.getInstallationToken(ctx, input)
	if err != nil {
		return Repository{}, err
	}
	repository, err := c.getRepositoryByOwnerAndNameWithToken(ctx, input, token, owner, name)
	if err == nil {
		return repository, nil
	}
	if err == ErrAuthentication && fromCache {
		c.invalidateCachedToken(input, token)
		refreshedToken, _, refreshErr := c.getInstallationToken(ctx, input)
		if refreshErr != nil {
			return Repository{}, refreshErr
		}
		return c.getRepositoryByOwnerAndNameWithToken(ctx, input, refreshedToken, owner, name)
	}
	return Repository{}, err
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

func classifyGitHubRepositoryResponse(resp *http.Response) error {
	message := readGitHubMessage(resp.Body)
	if isRateLimited(resp, message) {
		return ErrRateLimited
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return ErrAuthentication
	}
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound {
		return ErrRepositoryInaccessible
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

func (c *Client) getRepositoryByIDWithToken(ctx context.Context, input InstallationTokenRequest, token InstallationToken, repositoryID string) (Repository, error) {
	repositoryURL := strings.TrimRight(strings.TrimSpace(input.APIBaseURL), "/") + "/repositories/" + url.PathEscape(strings.TrimSpace(repositoryID))
	return c.getRepositoryWithToken(ctx, repositoryURL, token)
}

func (c *Client) getRepositoryByOwnerAndNameWithToken(ctx context.Context, input InstallationTokenRequest, token InstallationToken, owner string, name string) (Repository, error) {
	repositoryURL := strings.TrimRight(strings.TrimSpace(input.APIBaseURL), "/") + "/repos/" + url.PathEscape(strings.TrimSpace(owner)) + "/" + url.PathEscape(strings.TrimSpace(name))
	return c.getRepositoryWithToken(ctx, repositoryURL, token)
}

func (c *Client) getRepositoryWithToken(ctx context.Context, repositoryURL string, token InstallationToken) (Repository, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, repositoryURL, nil)
	if err != nil {
		return Repository{}, err
	}
	setGitHubHeaders(req, token.Value)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return Repository{}, ctx.Err()
		}
		return Repository{}, ErrProviderUnavailable
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return Repository{}, classifyGitHubRepositoryResponse(resp)
	}
	var payload repositoryResponse
	if err := decodeGitHubJSON(resp.Body, maxGitHubRepositoryResponseBytes, &payload); err != nil {
		return Repository{}, ErrMalformedResponse
	}
	return mapRepositoryResponse(payload)
}

func mapRepositoryResponse(payload repositoryResponse) (Repository, error) {
	repositoryID := strings.TrimSpace(payload.ID.String())
	owner := strings.TrimSpace(payload.Owner.Login)
	name := strings.TrimSpace(payload.Name)
	fullName := strings.TrimSpace(payload.FullName)
	cloneURL := strings.TrimSpace(payload.CloneURL)
	webURL := strings.TrimSpace(payload.HTMLURL)
	if repositoryID == "" || owner == "" || name == "" || fullName == "" || cloneURL == "" || webURL == "" {
		return Repository{}, ErrMalformedResponse
	}
	var defaultBranch *string
	if trimmedBranch := strings.TrimSpace(payload.DefaultBranch); trimmedBranch != "" {
		defaultBranch = &trimmedBranch
	}
	return Repository{
		ID:            repositoryID,
		Owner:         owner,
		Name:          name,
		FullName:      fullName,
		CloneURL:      cloneURL,
		WebURL:        webURL,
		DefaultBranch: defaultBranch,
		Archived:      payload.Archived,
		Disabled:      payload.Disabled,
	}, nil
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
