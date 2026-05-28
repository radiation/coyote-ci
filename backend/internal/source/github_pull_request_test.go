package source

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

func TestGitHubPullRequestClient_CreatePullRequest(t *testing.T) {
	t.Setenv("GITHUB_WRITE_TOKEN", "secret-token")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/example/repo/pulls" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret-token" {
			t.Fatalf("unexpected auth header: %q", got)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if !strings.Contains(string(body), `"head":"branch-name"`) {
			t.Fatalf("unexpected request body: %s", string(body))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"number":42,"html_url":"https://github.com/example/repo/pull/42"}`))
	}))
	defer server.Close()

	client := NewGitHubPullRequestClient(server.URL, server.Client())
	result, err := client.CreateOrGetPullRequest(context.Background(), GitHubPullRequestRequest{
		RepositoryURL: "https://github.com/example/repo.git",
		HeadBranch:    "branch-name",
		BaseBranch:    "main",
		Title:         "test",
		Body:          "body",
		Credential: domain.SourceCredential{
			Kind:      domain.SourceCredentialKindHTTPSToken,
			SecretRef: "GITHUB_WRITE_TOKEN",
		},
	})
	if err != nil {
		t.Fatalf("create pull request failed: %v", err)
	}
	if result.Number != 42 || result.URL != "https://github.com/example/repo/pull/42" || result.Existing {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestGitHubPullRequestClient_ReturnsExistingOpenPullRequest(t *testing.T) {
	t.Setenv("GITHUB_WRITE_TOKEN", "secret-token")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/repos/example/repo/pulls":
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"message":"A pull request already exists"}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/repos/example/repo/pulls"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"number":7,"html_url":"https://github.com/example/repo/pull/7"}]`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	client := NewGitHubPullRequestClient(server.URL, server.Client())
	result, err := client.CreateOrGetPullRequest(context.Background(), GitHubPullRequestRequest{
		RepositoryURL: "https://github.com/example/repo",
		HeadBranch:    "branch-name",
		BaseBranch:    "main",
		Title:         "test",
		Body:          "body",
		Credential: domain.SourceCredential{
			Kind:      domain.SourceCredentialKindHTTPSToken,
			SecretRef: "GITHUB_WRITE_TOKEN",
		},
	})
	if err != nil {
		t.Fatalf("lookup existing pull request failed: %v", err)
	}
	if result.Number != 7 || result.URL != "https://github.com/example/repo/pull/7" || !result.Existing {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestGitHubPullRequestClient_ValidationAndUnsupportedRepository(t *testing.T) {
	client := NewGitHubPullRequestClient(" ", nil)
	if client.baseURL != defaultGitHubAPIBaseURL {
		t.Fatalf("expected default GitHub API URL, got %q", client.baseURL)
	}
	if client.httpClient == nil {
		t.Fatal("expected default HTTP client")
	}

	result, err := client.CreateOrGetPullRequest(context.Background(), GitHubPullRequestRequest{RepositoryURL: "https://gitlab.example.com/example/repo"})
	if err != nil {
		t.Fatalf("unsupported repository should not error: %v", err)
	}
	if result != (GitHubPullRequestResult{}) {
		t.Fatalf("expected zero result for unsupported repository, got %+v", result)
	}

	_, err = client.CreateOrGetPullRequest(context.Background(), GitHubPullRequestRequest{RepositoryURL: "https://github.com/example/repo"})
	if !errors.Is(err, ErrCredentialSecretMissing) {
		t.Fatalf("expected missing credential secret error, got %v", err)
	}

	t.Setenv("GITHUB_WRITE_TOKEN", "secret-token")
	_, err = client.CreateOrGetPullRequest(context.Background(), GitHubPullRequestRequest{
		RepositoryURL: "https://github.com/example/repo",
		HeadBranch:    " ",
		BaseBranch:    "main",
		Credential: domain.SourceCredential{
			Kind:      domain.SourceCredentialKindHTTPSToken,
			SecretRef: "GITHUB_WRITE_TOKEN",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "head and base branches") {
		t.Fatalf("expected branch validation error, got %v", err)
	}
}

func TestGitHubPullRequestClient_ErrorResponses(t *testing.T) {
	t.Setenv("GITHUB_WRITE_TOKEN", "secret-token")

	t.Run("create failure includes response body", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`upstream unavailable`))
		}))
		defer server.Close()

		client := NewGitHubPullRequestClient(server.URL+"/", server.Client())
		_, err := client.CreateOrGetPullRequest(context.Background(), validPullRequestRequest())
		if err == nil || !strings.Contains(err.Error(), "status 502: upstream unavailable") {
			t.Fatalf("expected create failure body, got %v", err)
		}
	})

	t.Run("lookup failure includes response body", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodPost:
				w.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = w.Write([]byte(`{"message":"already exists"}`))
			case http.MethodGet:
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`lookup unavailable`))
			}
		}))
		defer server.Close()

		client := NewGitHubPullRequestClient(server.URL, server.Client())
		_, err := client.CreateOrGetPullRequest(context.Background(), validPullRequestRequest())
		if err == nil || !strings.Contains(err.Error(), "status 503: lookup unavailable") {
			t.Fatalf("expected lookup failure body, got %v", err)
		}
	})

	t.Run("existing pull request not found", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodPost:
				w.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = w.Write([]byte(`{"message":"already exists"}`))
			case http.MethodGet:
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`[]`))
			}
		}))
		defer server.Close()

		client := NewGitHubPullRequestClient(server.URL, server.Client())
		_, err := client.CreateOrGetPullRequest(context.Background(), validPullRequestRequest())
		if err == nil || !strings.Contains(err.Error(), "could not be located") {
			t.Fatalf("expected existing pull request lookup miss, got %v", err)
		}
	})
}

func TestParseGitHubRepository(t *testing.T) {
	tests := []struct {
		name      string
		repoURL   string
		owner     string
		repo      string
		supported bool
		wantErr   bool
	}{
		{name: "github https", repoURL: " https://github.com/example/repo.git ", owner: "example", repo: "repo", supported: true},
		{name: "github extra path", repoURL: "https://github.com/example/repo/tree/main", owner: "example", repo: "repo", supported: true},
		{name: "unsupported host", repoURL: "https://gitlab.example.com/example/repo", supported: false},
		{name: "missing repo", repoURL: "https://github.com/example", supported: false, wantErr: true},
		{name: "blank owner", repoURL: "https://github.com//repo", supported: false, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			owner, repo, supported, err := parseGitHubRepository(tc.repoURL)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected parse error")
				}
				return
			}
			if err != nil {
				t.Fatalf("parse failed: %v", err)
			}
			if owner != tc.owner || repo != tc.repo || supported != tc.supported {
				t.Fatalf("expected owner=%q repo=%q supported=%v, got owner=%q repo=%q supported=%v", tc.owner, tc.repo, tc.supported, owner, repo, supported)
			}
		})
	}
}

func validPullRequestRequest() GitHubPullRequestRequest {
	return GitHubPullRequestRequest{
		RepositoryURL: "https://github.com/example/repo.git",
		HeadBranch:    "branch-name",
		BaseBranch:    "main",
		Title:         "test",
		Body:          "body",
		Credential: domain.SourceCredential{
			Kind:      domain.SourceCredentialKindHTTPSToken,
			SecretRef: "GITHUB_WRITE_TOKEN",
		},
	}
}
