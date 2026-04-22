package source

import (
	"context"
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
