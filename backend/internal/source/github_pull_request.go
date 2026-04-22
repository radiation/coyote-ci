package source

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

const defaultGitHubAPIBaseURL = "https://api.github.com"

type GitHubPullRequestRequest struct {
	RepositoryURL string
	HeadBranch    string
	BaseBranch    string
	Title         string
	Body          string
	Credential    domain.SourceCredential
}

type GitHubPullRequestResult struct {
	Number   int
	URL      string
	Existing bool
}

type GitHubPullRequestClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewGitHubPullRequestClient(baseURL string, httpClient *http.Client) *GitHubPullRequestClient {
	trimmedBaseURL := strings.TrimSpace(baseURL)
	if trimmedBaseURL == "" {
		trimmedBaseURL = defaultGitHubAPIBaseURL
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &GitHubPullRequestClient{baseURL: strings.TrimRight(trimmedBaseURL, "/"), httpClient: httpClient}
}

func (c *GitHubPullRequestClient) CreateOrGetPullRequest(ctx context.Context, req GitHubPullRequestRequest) (GitHubPullRequestResult, error) {
	owner, repo, supported, err := parseGitHubRepository(strings.TrimSpace(req.RepositoryURL))
	if err != nil {
		return GitHubPullRequestResult{}, err
	}
	if !supported {
		return GitHubPullRequestResult{}, nil
	}

	tokenName := strings.TrimSpace(req.Credential.SecretRef)
	if tokenName == "" {
		return GitHubPullRequestResult{}, ErrCredentialSecretMissing
	}
	token := strings.TrimSpace(os.Getenv(tokenName))
	if token == "" {
		return GitHubPullRequestResult{}, ErrCredentialSecretMissing
	}

	headBranch := strings.TrimSpace(req.HeadBranch)
	baseBranch := strings.TrimSpace(req.BaseBranch)
	if headBranch == "" || baseBranch == "" {
		return GitHubPullRequestResult{}, fmt.Errorf("head and base branches are required")
	}

	body, err := json.Marshal(map[string]string{
		"title": req.Title,
		"head":  headBranch,
		"base":  baseBranch,
		"body":  req.Body,
	})
	if err != nil {
		return GitHubPullRequestResult{}, err
	}

	createURL := fmt.Sprintf("%s/repos/%s/%s/pulls", c.baseURL, owner, repo)
	createReq, err := http.NewRequestWithContext(ctx, http.MethodPost, createURL, bytes.NewReader(body))
	if err != nil {
		return GitHubPullRequestResult{}, err
	}
	createReq.Header.Set("Accept", "application/vnd.github+json")
	createReq.Header.Set("Authorization", "Bearer "+token)
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	createResp, err := c.httpClient.Do(createReq)
	if err != nil {
		return GitHubPullRequestResult{}, err
	}
	defer func() {
		_ = createResp.Body.Close()
	}()

	if createResp.StatusCode == http.StatusCreated {
		return decodeGitHubPullRequestResponse(createResp.Body, false)
	}
	if createResp.StatusCode != http.StatusUnprocessableEntity {
		message, readErr := io.ReadAll(createResp.Body)
		if readErr != nil {
			return GitHubPullRequestResult{}, fmt.Errorf("create pull request failed: status %d", createResp.StatusCode)
		}
		return GitHubPullRequestResult{}, fmt.Errorf("create pull request failed: status %d: %s", createResp.StatusCode, strings.TrimSpace(string(message)))
	}

	query := url.Values{}
	query.Set("state", "open")
	query.Set("head", owner+":"+headBranch)
	query.Set("base", baseBranch)
	lookupURL := fmt.Sprintf("%s/repos/%s/%s/pulls?%s", c.baseURL, owner, repo, query.Encode())
	lookupReq, err := http.NewRequestWithContext(ctx, http.MethodGet, lookupURL, nil)
	if err != nil {
		return GitHubPullRequestResult{}, err
	}
	lookupReq.Header.Set("Accept", "application/vnd.github+json")
	lookupReq.Header.Set("Authorization", "Bearer "+token)
	lookupReq.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	lookupResp, err := c.httpClient.Do(lookupReq)
	if err != nil {
		return GitHubPullRequestResult{}, err
	}
	defer func() {
		_ = lookupResp.Body.Close()
	}()
	if lookupResp.StatusCode != http.StatusOK {
		message, readErr := io.ReadAll(lookupResp.Body)
		if readErr != nil {
			return GitHubPullRequestResult{}, fmt.Errorf("lookup pull request failed: status %d", lookupResp.StatusCode)
		}
		return GitHubPullRequestResult{}, fmt.Errorf("lookup pull request failed: status %d: %s", lookupResp.StatusCode, strings.TrimSpace(string(message)))
	}

	var items []struct {
		Number  int    `json:"number"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(lookupResp.Body).Decode(&items); err != nil {
		return GitHubPullRequestResult{}, err
	}
	if len(items) == 0 {
		return GitHubPullRequestResult{}, fmt.Errorf("pull request already exists but could not be located for branch %q", headBranch)
	}
	return GitHubPullRequestResult{Number: items[0].Number, URL: items[0].HTMLURL, Existing: true}, nil
}

func decodeGitHubPullRequestResponse(body io.Reader, existing bool) (GitHubPullRequestResult, error) {
	var payload struct {
		Number  int    `json:"number"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(body).Decode(&payload); err != nil {
		return GitHubPullRequestResult{}, err
	}
	return GitHubPullRequestResult{Number: payload.Number, URL: payload.HTMLURL, Existing: existing}, nil
}

func parseGitHubRepository(repoURL string) (owner string, repo string, supported bool, err error) {
	parsed, err := url.Parse(strings.TrimSpace(repoURL))
	if err != nil {
		return "", "", false, err
	}
	if !strings.EqualFold(parsed.Hostname(), "github.com") {
		return "", "", false, nil
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 2 {
		return "", "", false, fmt.Errorf("invalid GitHub repository URL %q", repoURL)
	}
	repoName := strings.TrimSuffix(parts[1], ".git")
	if strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(repoName) == "" {
		return "", "", false, fmt.Errorf("invalid GitHub repository URL %q", repoURL)
	}
	return parts[0], repoName, true, nil
}
