package build

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestGitHubCommitStatusClient_ValidationAndHelpers(t *testing.T) {
	t.Run("publish success request construction", func(t *testing.T) {
		detailsURL := "https://ci.example.com/builds/1"
		client := NewGitHubCommitStatusClient("https://api.github.com", &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.Method != http.MethodPost {
				t.Fatalf("expected POST request, got %s", req.Method)
			}
			if req.URL.String() != "https://api.github.com/repos/octo/repo/statuses/deadbeef" {
				t.Fatalf("unexpected request url: %s", req.URL.String())
			}
			if req.Header.Get("Authorization") != "Bearer token" || req.Header.Get("Accept") != "application/vnd.github+json" || req.Header.Get("Content-Type") != "application/json" || req.Header.Get("X-GitHub-Api-Version") != "2022-11-28" {
				t.Fatalf("unexpected headers: %+v", req.Header)
			}
			var payload map[string]string
			if decodeErr := json.NewDecoder(req.Body).Decode(&payload); decodeErr != nil {
				t.Fatalf("decode request body: %v", decodeErr)
			}
			if payload["state"] != "pending" || payload["context"] != "ctx" || payload["description"] != "desc" || payload["target_url"] != detailsURL {
				t.Fatalf("unexpected payload: %+v", payload)
			}
			return &http.Response{StatusCode: http.StatusCreated, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
		})}, "token")

		err := client.PublishCommitStatus(context.Background(), SCMCommitStatusPublishRequest{RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "deadbeef", Context: "ctx", State: domain.SCMCommitStatusStatePending, Description: "desc", DetailsURL: &detailsURL})
		if err != nil {
			t.Fatalf("expected publish success, got %v", err)
		}
	})

	t.Run("publish validation branches", func(t *testing.T) {
		client := NewGitHubCommitStatusClient("", nil, "")
		err := client.PublishCommitStatus(context.Background(), SCMCommitStatusPublishRequest{})
		var statusErr *GitHubCommitStatusError
		if !errors.As(err, &statusErr) || statusErr.Reason() != "github_status_token_missing" {
			t.Fatalf("expected missing token error, got %v", err)
		}
		if statusErr.statusCode != http.StatusUnauthorized {
			t.Fatalf("expected unauthorized status for missing token, got %d", statusErr.statusCode)
		}

		detailsURL := "http://[::1"
		inputErr := validateGitHubCommitStatusRequest(SCMCommitStatusPublishRequest{RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "deadbeef", Context: "ctx", State: domain.SCMCommitStatusStatePending, Description: "desc", DetailsURL: &detailsURL})
		if inputErr == nil || inputErr.Reason() != "github_status_invalid_input" {
			t.Fatalf("expected invalid details url error, got %v", inputErr)
		}

		badEndpointClient := NewGitHubCommitStatusClient("://bad url", &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return nil, errors.New("unexpected transport call")
		})}, "token")
		err = badEndpointClient.PublishCommitStatus(context.Background(), SCMCommitStatusPublishRequest{RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "deadbeef", Context: "ctx", State: domain.SCMCommitStatusStatePending, Description: "desc"})
		if !errors.As(err, &statusErr) || statusErr.Reason() != "github_status_invalid_endpoint" {
			t.Fatalf("expected invalid endpoint error, got %v", err)
		}

		tooLongContext := strings.Repeat("c", maxSCMStatusContextLength+1)
		tooLongDescription := strings.Repeat("d", maxSCMStatusDescriptionLength+1)
		cases := []struct {
			name string
			req  SCMCommitStatusPublishRequest
		}{
			{name: "invalid state", req: SCMCommitStatusPublishRequest{RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "deadbeef", Context: "ctx", State: domain.SCMCommitStatusState("bad"), Description: "desc"}},
			{name: "missing repo", req: SCMCommitStatusPublishRequest{CommitSHA: "deadbeef", Context: "ctx", State: domain.SCMCommitStatusStatePending, Description: "desc"}},
			{name: "missing sha", req: SCMCommitStatusPublishRequest{RepositoryOwner: "octo", RepositoryName: "repo", Context: "ctx", State: domain.SCMCommitStatusStatePending, Description: "desc"}},
			{name: "missing context", req: SCMCommitStatusPublishRequest{RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "deadbeef", State: domain.SCMCommitStatusStatePending, Description: "desc"}},
			{name: "missing description", req: SCMCommitStatusPublishRequest{RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "deadbeef", Context: "ctx", State: domain.SCMCommitStatusStatePending}},
			{name: "context too long", req: SCMCommitStatusPublishRequest{RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "deadbeef", Context: tooLongContext, State: domain.SCMCommitStatusStatePending, Description: "desc"}},
			{name: "description too long", req: SCMCommitStatusPublishRequest{RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "deadbeef", Context: "ctx", State: domain.SCMCommitStatusStatePending, Description: tooLongDescription}},
		}
		for _, test := range cases {
			t.Run(test.name, func(t *testing.T) {
				inputErr := validateGitHubCommitStatusRequest(test.req)
				if inputErr == nil || inputErr.Reason() != "github_status_invalid_input" {
					t.Fatalf("expected invalid input error, got %v", inputErr)
				}
			})
		}
	})

	t.Run("http classification", func(t *testing.T) {
		cases := []struct {
			name       string
			statusCode int
			headers    map[string]string
			wantReason string
			wantRetry  bool
		}{
			{name: "server retryable", statusCode: http.StatusBadGateway, wantReason: "github_status_http_retryable", wantRetry: true},
			{name: "too many requests", statusCode: http.StatusTooManyRequests, wantReason: "github_status_rate_limited", wantRetry: true},
			{name: "forbidden permanent", statusCode: http.StatusForbidden, wantReason: "github_status_http_permanent", wantRetry: false},
			{name: "forbidden retry after", statusCode: http.StatusForbidden, headers: map[string]string{"Retry-After": "30"}, wantReason: "github_status_rate_limited", wantRetry: true},
		}
		for _, test := range cases {
			t.Run(test.name, func(t *testing.T) {
				client := NewGitHubCommitStatusClient("https://api.github.com", &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					resp := &http.Response{StatusCode: test.statusCode, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(strings.Repeat("x", maxGitHubCommitStatusResponseBodyBytes+64)))}
					for key, value := range test.headers {
						resp.Header.Set(key, value)
					}
					return resp, nil
				})}, "token")
				err := client.PublishCommitStatus(context.Background(), SCMCommitStatusPublishRequest{RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "deadbeef", Context: "ctx", State: domain.SCMCommitStatusStatePending, Description: "desc"})
				var statusErr *GitHubCommitStatusError
				if !errors.As(err, &statusErr) {
					t.Fatalf("expected github status error, got %v", err)
				}
				if statusErr.Reason() != test.wantReason || statusErr.Retryable() != test.wantRetry {
					t.Fatalf("expected reason=%s retry=%v, got reason=%s retry=%v", test.wantReason, test.wantRetry, statusErr.Reason(), statusErr.Retryable())
				}
				if len(statusErr.message) > maxGitHubCommitStatusResponseBodyBytes {
					t.Fatalf("expected bounded error message, got %d", len(statusErr.message))
				}
			})
		}
	})

	t.Run("helper coverage", func(t *testing.T) {
		client := NewGitHubCommitStatusClient("https://api.example.com/root/", nil, "token")
		endpoint, err := client.statusEndpoint("octo", "repo", "deadbeef")
		if err != nil || endpoint != "https://api.example.com/root/repos/octo/repo/statuses/deadbeef" {
			t.Fatalf("unexpected endpoint result: endpoint=%q err=%v", endpoint, err)
		}
		defaulted := NewGitHubCommitStatusClient(" ", nil, " token ")
		if defaulted.baseURL != defaultGitHubCommitStatusAPIBaseURL || defaulted.httpClient == nil || defaulted.httpClient.Timeout != defaultGitHubCommitStatusTimeout || defaulted.token != "token" {
			t.Fatalf("unexpected defaulted client: %+v", defaulted)
		}

		if isGitHubRateLimited(nil) {
			t.Fatal("nil response should not be rate limited")
		}
		if !isGitHubRateLimited(&http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{}}) {
			t.Fatal("expected 429 response to be rate limited")
		}
		forbiddenRetryAfterHeader := make(http.Header)
		forbiddenRetryAfterHeader.Set("Retry-After", "1")
		forbiddenRetryAfter := &http.Response{StatusCode: http.StatusForbidden, Header: forbiddenRetryAfterHeader}
		if !isGitHubRateLimited(forbiddenRetryAfter) {
			t.Fatal("expected forbidden response with retry-after to be rate limited")
		}
		forbiddenRemainingZeroHeader := make(http.Header)
		forbiddenRemainingZeroHeader.Set("X-RateLimit-Remaining", "0")
		forbiddenRemainingZero := &http.Response{StatusCode: http.StatusForbidden, Header: forbiddenRemainingZeroHeader}
		if !isGitHubRateLimited(forbiddenRemainingZero) {
			t.Fatal("expected forbidden response with zero remaining quota to be rate limited")
		}
		nonRateLimitedForbiddenHeader := make(http.Header)
		nonRateLimitedForbiddenHeader.Set("X-RateLimit-Remaining", "1")
		if isGitHubRateLimited(&http.Response{StatusCode: http.StatusForbidden, Header: nonRateLimitedForbiddenHeader}) {
			t.Fatal("did not expect non-rate-limited forbidden response to be rate limited")
		}

		if (*GitHubCommitStatusError)(nil).Error() != "" || (*GitHubCommitStatusError)(nil).Reason() != "" {
			t.Fatal("nil GitHubCommitStatusError methods should be empty")
		}
		errText := (&GitHubCommitStatusError{statusCode: 500}).Error()
		if !strings.Contains(errText, "status 500") {
			t.Fatalf("expected status-only error text, got %q", errText)
		}
		if msgText := (&GitHubCommitStatusError{statusCode: 422, message: "bad payload"}).Error(); !strings.Contains(msgText, "bad payload") {
			t.Fatalf("expected message-bearing error text, got %q", msgText)
		}
		if !(&GitHubCommitStatusError{retryable: true, reason: "retry_me"}).Retryable() || (&GitHubCommitStatusError{reason: "retry_me"}).Reason() != "retry_me" {
			t.Fatal("expected retryable and reason helpers to reflect error fields")
		}
	})
}
