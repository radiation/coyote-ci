package build

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultGitHubCommitStatusAPIBaseURL = "https://api.github.com"
const defaultGitHubCommitStatusTimeout = 30 * time.Second

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
		return &GitHubCommitStatusError{reason: "github_status_token_missing", message: "github status token is not configured"}
	}
	body := map[string]string{
		"state":       strings.TrimSpace(string(req.State)),
		"context":     strings.TrimSpace(req.Context),
		"description": strings.TrimSpace(req.Description),
	}
	if req.DetailsURL != nil && strings.TrimSpace(*req.DetailsURL) != "" {
		body["target_url"] = strings.TrimSpace(*req.DetailsURL)
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	endpoint := fmt.Sprintf("%s/repos/%s/%s/statuses/%s", c.baseURL, req.RepositoryOwner, req.RepositoryName, req.CommitSHA)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Accept", "application/vnd.github+json")
	httpReq.Header.Set("Authorization", "Bearer "+c.token)
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
	message, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		message = nil
	}
	trimmedMessage := strings.TrimSpace(string(message))
	retryable := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
	reason := "github_status_http_permanent"
	if retryable {
		reason = "github_status_http_retryable"
	}
	return &GitHubCommitStatusError{statusCode: resp.StatusCode, retryable: retryable, reason: reason, message: trimmedMessage}
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

var _ SCMCommitStatusPublisher = (*GitHubCommitStatusClient)(nil)
