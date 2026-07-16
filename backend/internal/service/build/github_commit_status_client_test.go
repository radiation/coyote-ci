package build

import (
	"context"
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
	t.Run("publish validation branches", func(t *testing.T) {
		client := NewGitHubCommitStatusClient("", nil, "")
		err := client.PublishCommitStatus(context.Background(), SCMCommitStatusPublishRequest{})
		var statusErr *GitHubCommitStatusError
		if !errors.As(err, &statusErr) || statusErr.Reason() != "github_status_token_missing" {
			t.Fatalf("expected missing token error, got %v", err)
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

		if isGitHubRateLimited(nil) {
			t.Fatal("nil response should not be rate limited")
		}
		forbiddenRetryAfter := &http.Response{StatusCode: http.StatusForbidden, Header: http.Header{"Retry-After": []string{"1"}}}
		if !isGitHubRateLimited(forbiddenRetryAfter) {
			t.Fatal("expected forbidden response with retry-after to be rate limited")
		}

		if (*GitHubCommitStatusError)(nil).Error() != "" || (*GitHubCommitStatusError)(nil).Reason() != "" {
			t.Fatal("nil GitHubCommitStatusError methods should be empty")
		}
		errText := (&GitHubCommitStatusError{statusCode: 500}).Error()
		if !strings.Contains(errText, "status 500") {
			t.Fatalf("expected status-only error text, got %q", errText)
		}
	})
}
