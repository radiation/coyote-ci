package build

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

const defaultGitHubCommitStatusAPIBaseURL = "https://api.github.com"
const defaultGitHubCommitStatusTimeout = 30 * time.Second
const maxGitHubCommitStatusResponseBodyBytes = 4096

type GitHubCommitStatusClient struct {
	baseURL    string
	httpClient *http.Client
	token      string
}

type GitHubCommitStatusError struct {
	statusCode int
	retryable  bool
	reason     string
	message    string
	cause      error
}

func NewGitHubCommitStatusClient(baseURL string, httpClient *http.Client, token string) *GitHubCommitStatusClient {
	trimmedBaseURL := strings.TrimSpace(baseURL)
	if trimmedBaseURL == "" {
		trimmedBaseURL = defaultGitHubCommitStatusAPIBaseURL
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultGitHubCommitStatusTimeout}
	}
	return &GitHubCommitStatusClient{baseURL: strings.TrimRight(trimmedBaseURL, "/"), httpClient: httpClient, token: strings.TrimSpace(token)}
}

func (c *GitHubCommitStatusClient) PublishCommitStatus(ctx context.Context, req SCMCommitStatusPublishRequest) error {
	if strings.TrimSpace(c.token) == "" {
		return &GitHubCommitStatusError{statusCode: http.StatusUnauthorized, reason: "github_status_token_missing", message: "github status token is not configured"}
	}
	return c.PublishCommitStatusWithToken(ctx, req, c.token)
}

func (c *GitHubCommitStatusClient) PublishCommitStatusWithToken(ctx context.Context, req SCMCommitStatusPublishRequest, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return &GitHubCommitStatusError{statusCode: http.StatusUnauthorized, reason: "github_status_token_missing", message: "github status token is not configured"}
	}
	if inputErr := validateGitHubCommitStatusRequest(req); inputErr != nil {
		return inputErr
	}
	body := map[string]string{
		"state":       strings.TrimSpace(string(req.State)),
		"context":     truncateSCMStatusText(req.Context, maxSCMStatusContextLength),
		"description": truncateSCMStatusText(req.Description, maxSCMStatusDescriptionLength),
	}
	if req.DetailsURL != nil && strings.TrimSpace(*req.DetailsURL) != "" {
		body["target_url"] = strings.TrimSpace(*req.DetailsURL)
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	endpoint, endpointErr := c.statusEndpoint(req.RepositoryOwner, req.RepositoryName, req.CommitSHA)
	if endpointErr != nil {
		return &GitHubCommitStatusError{reason: "github_status_invalid_endpoint", message: endpointErr.Error()}
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Accept", "application/vnd.github+json")
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusCreated {
		return nil
	}
	message, readErr := io.ReadAll(io.LimitReader(resp.Body, maxGitHubCommitStatusResponseBodyBytes+1))
	if readErr != nil {
		message = nil
	}
	trimmedMessage := strings.TrimSpace(string(message))
	if len(message) > maxGitHubCommitStatusResponseBodyBytes {
		trimmedMessage = truncateSCMStatusText(trimmedMessage, maxGitHubCommitStatusResponseBodyBytes)
	}
	retryable := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 || isGitHubRateLimited(resp)
	reason := "github_status_http_permanent"
	if isGitHubRateLimited(resp) {
		reason = "github_status_rate_limited"
	} else if retryable {
		reason = "github_status_http_retryable"
	}
	return &GitHubCommitStatusError{statusCode: resp.StatusCode, retryable: retryable, reason: reason, message: trimmedMessage}
}

func (c *GitHubCommitStatusClient) statusEndpoint(repositoryOwner string, repositoryName string, commitSHA string) (string, error) {
	base, err := url.Parse(strings.TrimSpace(c.baseURL))
	if err != nil {
		return "", err
	}
	base.Path = path.Join(base.Path, "repos", strings.TrimSpace(repositoryOwner), strings.TrimSpace(repositoryName), "statuses", strings.TrimSpace(commitSHA))
	return base.String(), nil
}

func validateGitHubCommitStatusRequest(req SCMCommitStatusPublishRequest) *GitHubCommitStatusError {
	if !req.State.IsValid() {
		return &GitHubCommitStatusError{reason: "github_status_invalid_input", message: "github status state is invalid"}
	}
	if strings.TrimSpace(req.RepositoryOwner) == "" || strings.TrimSpace(req.RepositoryName) == "" {
		return &GitHubCommitStatusError{reason: "github_status_invalid_input", message: "github repository owner and name are required"}
	}
	if strings.TrimSpace(req.CommitSHA) == "" {
		return &GitHubCommitStatusError{reason: "github_status_invalid_input", message: "github commit sha is required"}
	}
	if strings.TrimSpace(req.Context) == "" {
		return &GitHubCommitStatusError{reason: "github_status_invalid_input", message: "github status context is required"}
	}
	if strings.TrimSpace(req.Description) == "" {
		return &GitHubCommitStatusError{reason: "github_status_invalid_input", message: "github status description is required"}
	}
	if len([]rune(strings.TrimSpace(req.Context))) > maxSCMStatusContextLength {
		return &GitHubCommitStatusError{reason: "github_status_invalid_input", message: "github status context exceeds maximum length"}
	}
	if len([]rune(strings.TrimSpace(req.Description))) > maxSCMStatusDescriptionLength {
		return &GitHubCommitStatusError{reason: "github_status_invalid_input", message: "github status description exceeds maximum length"}
	}
	if req.DetailsURL != nil && strings.TrimSpace(*req.DetailsURL) != "" {
		if _, err := url.ParseRequestURI(strings.TrimSpace(*req.DetailsURL)); err != nil {
			return &GitHubCommitStatusError{reason: "github_status_invalid_input", message: "github status details url is invalid"}
		}
	}
	return nil
}

func isGitHubRateLimited(resp *http.Response) bool {
	if resp == nil {
		return false
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return true
	}
	if resp.StatusCode != http.StatusForbidden {
		return false
	}
	if strings.TrimSpace(resp.Header.Get("Retry-After")) != "" {
		return true
	}
	return strings.TrimSpace(resp.Header.Get("X-RateLimit-Remaining")) == "0"
}

func (e *GitHubCommitStatusError) Error() string {
	if e == nil {
		return ""
	}
	if strings.TrimSpace(e.message) == "" {
		return fmt.Sprintf("github commit status failed: status %d", e.statusCode)
	}
	return fmt.Sprintf("github commit status failed: status %d: %s", e.statusCode, e.message)
}

func (e *GitHubCommitStatusError) Retryable() bool {
	return e != nil && e.retryable
}

func (e *GitHubCommitStatusError) Reason() string {
	if e == nil {
		return ""
	}
	return e.reason
}

func (e *GitHubCommitStatusError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

var _ SCMCommitStatusPublisher = (*GitHubCommitStatusClient)(nil)
