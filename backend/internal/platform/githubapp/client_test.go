package githubapp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestClient_GetInstallationToken_ExchangesTokenForGitHubDotComAndGHES(t *testing.T) {
	privateKeyPEM, _ := testRSAPrivateKeyPEM(t)
	for _, tc := range []struct {
		name       string
		apiBaseURL string
		wantPath   string
	}{
		{name: "github.com", apiBaseURL: "https://api.github.com", wantPath: "/app/installations/999/access_tokens"},
		{name: "ghes", apiBaseURL: "/api/v3", wantPath: "/api/v3/app/installations/999/access_tokens"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tc.wantPath {
					t.Fatalf("expected path %q, got %q", tc.wantPath, r.URL.Path)
				}
				if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
					t.Fatalf("expected github accept header, got %q", got)
				}
				if got := r.Header.Get("Authorization"); got == "" {
					t.Fatal("expected authorization header")
				}
				if got := r.Header.Get("X-GitHub-Api-Version"); got != defaultAPIVersion {
					t.Fatalf("expected github api version header, got %q", got)
				}
				w.WriteHeader(http.StatusCreated)
				_ = json.NewEncoder(w).Encode(map[string]string{"token": "ghs_token", "expires_at": time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339)})
			}))
			defer server.Close()
			client := NewClient(server.Client())
			client.now = func() time.Time { return time.Date(2026, 7, 17, 16, 0, 0, 0, time.UTC) }
			apiBaseURL := server.URL + tc.apiBaseURL
			if tc.name == "github.com" {
				apiBaseURL = server.URL
			}
			token, err := client.GetInstallationToken(context.Background(), InstallationTokenRequest{AppRegistrationID: "registration-1", AppID: "12345", InstallationID: "999", APIBaseURL: apiBaseURL, PrivateKeyPEM: privateKeyPEM})
			if err != nil {
				t.Fatalf("get token: %v", err)
			}
			if token.Value != "ghs_token" {
				t.Fatalf("expected token value, got %q", token.Value)
			}
		})
	}
}

func TestClient_GetInstallationToken_ClassifiesResponses(t *testing.T) {
	privateKeyPEM, _ := testRSAPrivateKeyPEM(t)
	for _, tc := range []struct {
		name       string
		statusCode int
		headers    map[string]string
		body       string
		wantErr    error
	}{
		{name: "malformed success", statusCode: http.StatusCreated, body: `{"token":"","expires_at":"bad"}`, wantErr: ErrMalformedResponse},
		{name: "auth", statusCode: http.StatusUnauthorized, body: `{"message":"bad credentials"}`, wantErr: ErrAuthentication},
		{name: "installation unavailable", statusCode: http.StatusNotFound, body: `{"message":"not found"}`, wantErr: ErrInstallationUnavailable},
		{name: "rate limited", statusCode: http.StatusForbidden, headers: map[string]string{"X-RateLimit-Remaining": "0"}, body: `{"message":"API rate limit exceeded"}`, wantErr: ErrRateLimited},
		{name: "server", statusCode: http.StatusBadGateway, body: `{"message":"bad gateway"}`, wantErr: ErrProviderUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				for key, value := range tc.headers {
					w.Header().Set(key, value)
				}
				w.WriteHeader(tc.statusCode)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			client := NewClient(server.Client())
			_, err := client.GetInstallationToken(context.Background(), InstallationTokenRequest{AppRegistrationID: "registration-1", AppID: "12345", InstallationID: "999", APIBaseURL: server.URL, PrivateKeyPEM: privateKeyPEM})
			if err != tc.wantErr {
				t.Fatalf("expected %v, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestClient_ProbeInstallation_UsesDocumentedEndpointsAndPopulatesIdentity(t *testing.T) {
	privateKeyPEM, _ := testRSAPrivateKeyPEM(t)
	for _, tc := range []struct {
		name       string
		apiBaseURL string
		wantRepo   string
		wantDetail string
	}{
		{name: "github.com", apiBaseURL: "https://api.github.com", wantRepo: "/installation/repositories", wantDetail: "/app/installations/999"},
		{name: "ghes", apiBaseURL: "/api/v3", wantRepo: "/api/v3/installation/repositories", wantDetail: "/api/v3/app/installations/999"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case tc.wantRepo:
					if got := r.URL.Query().Get("per_page"); got != "1" {
						t.Fatalf("expected per_page=1, got %q", got)
					}
					if got := r.Header.Get("Authorization"); got != "Bearer ghs_probe" {
						t.Fatalf("expected installation token auth, got %q", got)
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"total_count": 1, "repositories": []map[string]any{{"id": 1}}})
				case tc.wantDetail:
					if got := r.Header.Get("Authorization"); got == "" || got == "Bearer ghs_probe" {
						t.Fatalf("expected app jwt auth, got %q", got)
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"id": 999, "account": map[string]any{"login": "octo"}})
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			client := NewClient(server.Client())
			client.cache = map[string]cachedInstallationToken{
				installationCacheKey(InstallationTokenRequest{AppRegistrationID: "registration-1", AppID: "12345", InstallationID: "999", APIBaseURL: probeBaseURL(server.URL, tc.apiBaseURL), PrivateKeyPEM: privateKeyPEM}): {
					token: InstallationToken{Value: "ghs_probe", ExpiresAt: time.Now().Add(10 * time.Minute).UTC()},
				},
			}
			result, err := client.ProbeInstallation(context.Background(), InstallationTokenRequest{AppRegistrationID: "registration-1", AppID: "12345", InstallationID: "999", APIBaseURL: probeBaseURL(server.URL, tc.apiBaseURL), PrivateKeyPEM: privateKeyPEM})
			if err != nil {
				t.Fatalf("probe installation: %v", err)
			}
			if result.InstallationID != "999" || result.AccountLogin != "octo" || result.Suspended {
				t.Fatalf("unexpected probe result: %+v", result)
			}
		})
	}
}

func TestClient_ProbeInstallation_RejectsSuspendedAndMismatchedInstallation(t *testing.T) {
	privateKeyPEM, _ := testRSAPrivateKeyPEM(t)
	for _, tc := range []struct {
		name            string
		installationDoc map[string]any
		wantErr         error
	}{
		{name: "suspended", installationDoc: map[string]any{"id": 999, "suspended_at": time.Now().UTC().Format(time.RFC3339), "account": map[string]any{"login": "octo"}}, wantErr: ErrInstallationUnavailable},
		{name: "mismatched id", installationDoc: map[string]any{"id": 1000, "account": map[string]any{"login": "octo"}}, wantErr: ErrInstallationUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/installation/repositories":
					_ = json.NewEncoder(w).Encode(map[string]any{"total_count": 1, "repositories": []map[string]any{{"id": 1}}})
				case "/app/installations/999":
					_ = json.NewEncoder(w).Encode(tc.installationDoc)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			client := NewClient(server.Client())
			request := InstallationTokenRequest{AppRegistrationID: "registration-1", AppID: "12345", InstallationID: "999", APIBaseURL: server.URL, PrivateKeyPEM: privateKeyPEM}
			client.cache[installationCacheKey(request)] = cachedInstallationToken{token: InstallationToken{Value: "ghs_probe", ExpiresAt: time.Now().Add(10 * time.Minute).UTC()}}
			_, err := client.ProbeInstallation(context.Background(), request)
			if err != tc.wantErr {
				t.Fatalf("expected %v, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestClient_ProbeInstallation_Cached401InvalidatesRefreshesAndRetriesOnce(t *testing.T) {
	privateKeyPEM, _ := testRSAPrivateKeyPEM(t)
	var exchangeCalls atomic.Int32
	var probeCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/app/installations/999/access_tokens":
			exchangeCalls.Add(1)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{"token": "ghs_fresh", "expires_at": time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339)})
		case "/installation/repositories":
			probeCalls.Add(1)
			switch r.Header.Get("Authorization") {
			case "Bearer ghs_cached":
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"message":"bad credentials"}`))
			case "Bearer ghs_fresh":
				_ = json.NewEncoder(w).Encode(map[string]any{"total_count": 0, "repositories": []map[string]any{}})
			default:
				t.Fatalf("unexpected auth header %q", r.Header.Get("Authorization"))
			}
		case "/app/installations/999":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 999, "account": map[string]any{"login": "octo"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := NewClient(server.Client())
	request := InstallationTokenRequest{AppRegistrationID: "registration-1", AppID: "12345", InstallationID: "999", APIBaseURL: server.URL, PrivateKeyPEM: privateKeyPEM}
	client.cache[installationCacheKey(request)] = cachedInstallationToken{token: InstallationToken{Value: "ghs_cached", ExpiresAt: time.Now().Add(10 * time.Minute).UTC()}}

	result, err := client.ProbeInstallation(context.Background(), request)
	if err != nil {
		t.Fatalf("probe installation: %v", err)
	}
	if result.InstallationID != "999" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if exchangeCalls.Load() != 1 || probeCalls.Load() != 2 {
		t.Fatalf("expected 1 exchange and 2 probe calls, got exchanges=%d probes=%d", exchangeCalls.Load(), probeCalls.Load())
	}
}

func TestClient_ProbeInstallation_Second401ReturnsFailureWithoutExtraRetry(t *testing.T) {
	privateKeyPEM, _ := testRSAPrivateKeyPEM(t)
	var exchangeCalls atomic.Int32
	var probeCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/app/installations/999/access_tokens":
			exchangeCalls.Add(1)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{"token": "ghs_fresh", "expires_at": time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339)})
		case "/installation/repositories":
			probeCalls.Add(1)
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"bad credentials"}`))
		case "/app/installations/999":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 999, "account": map[string]any{"login": "octo"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := NewClient(server.Client())
	request := InstallationTokenRequest{AppRegistrationID: "registration-1", AppID: "12345", InstallationID: "999", APIBaseURL: server.URL, PrivateKeyPEM: privateKeyPEM}
	client.cache[installationCacheKey(request)] = cachedInstallationToken{token: InstallationToken{Value: "ghs_cached", ExpiresAt: time.Now().Add(10 * time.Minute).UTC()}}

	_, err := client.ProbeInstallation(context.Background(), request)
	if err != ErrAuthentication {
		t.Fatalf("expected auth failure, got %v", err)
	}
	if exchangeCalls.Load() != 1 || probeCalls.Load() != 2 {
		t.Fatalf("expected one refresh retry, got exchanges=%d probes=%d", exchangeCalls.Load(), probeCalls.Load())
	}
}

func TestClient_ProbeInstallation_ConditionalInvalidationKeepsNewerToken(t *testing.T) {
	privateKeyPEM, _ := testRSAPrivateKeyPEM(t)
	request := InstallationTokenRequest{AppRegistrationID: "registration-1", AppID: "12345", InstallationID: "999", APIBaseURL: "https://api.github.com", PrivateKeyPEM: privateKeyPEM}
	cacheKey := installationCacheKey(request)
	client := NewClient(nil)
	oldToken := InstallationToken{Value: "ghs_old", ExpiresAt: time.Now().Add(10 * time.Minute).UTC()}
	newToken := InstallationToken{Value: "ghs_new", ExpiresAt: time.Now().Add(20 * time.Minute).UTC()}
	client.cache[cacheKey] = cachedInstallationToken{token: newToken}
	client.invalidateCachedToken(request, oldToken)
	entry, ok := client.cache[cacheKey]
	if !ok || entry.token != newToken {
		t.Fatalf("expected newer token to remain cached, got %+v exists=%t", entry.token, ok)
	}
}

func TestClient_ProbeInstallation_MalformedAndFailureClassification(t *testing.T) {
	privateKeyPEM, _ := testRSAPrivateKeyPEM(t)
	for _, tc := range []struct {
		name       string
		statusCode int
		headers    map[string]string
		body       string
		detailBody map[string]any
		wantErr    error
	}{
		{name: "malformed repositories", statusCode: http.StatusOK, body: `{"repositories":[]}`, wantErr: ErrMalformedResponse},
		{name: "installation unavailable", statusCode: http.StatusNotFound, body: `{"message":"not found"}`, wantErr: ErrInstallationUnavailable},
		{name: "rate limited", statusCode: http.StatusForbidden, headers: map[string]string{"X-RateLimit-Remaining": "0"}, body: `{"message":"API rate limit exceeded"}`, wantErr: ErrRateLimited},
		{name: "provider unavailable", statusCode: http.StatusBadGateway, body: `{"message":"bad gateway"}`, wantErr: ErrProviderUnavailable},
		{name: "malformed installation details", statusCode: http.StatusOK, body: `{"total_count":0,"repositories":[]}`, detailBody: map[string]any{"id": 999}, wantErr: ErrMalformedResponse},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/installation/repositories":
					for key, value := range tc.headers {
						w.Header().Set(key, value)
					}
					w.WriteHeader(tc.statusCode)
					_, _ = w.Write([]byte(tc.body))
				case "/app/installations/999":
					if tc.detailBody == nil {
						_ = json.NewEncoder(w).Encode(map[string]any{"id": 999, "account": map[string]any{"login": "octo"}})
						return
					}
					_ = json.NewEncoder(w).Encode(tc.detailBody)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			client := NewClient(server.Client())
			request := InstallationTokenRequest{AppRegistrationID: "registration-1", AppID: "12345", InstallationID: "999", APIBaseURL: server.URL, PrivateKeyPEM: privateKeyPEM}
			client.cache[installationCacheKey(request)] = cachedInstallationToken{token: InstallationToken{Value: "ghs_probe", ExpiresAt: time.Now().Add(10 * time.Minute).UTC()}}
			_, err := client.ProbeInstallation(context.Background(), request)
			if err != tc.wantErr {
				t.Fatalf("expected %v, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestClient_ProbeInstallation_BoundsLargeSuccessBodyReads(t *testing.T) {
	privateKeyPEM, _ := testRSAPrivateKeyPEM(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/installation/repositories":
			_, _ = w.Write([]byte(`{"total_count":1,"repositories":[{"id":1,"name":"` + strings.Repeat("a", maxGitHubProbeResponseBytes) + `"}]}`))
		case "/app/installations/999":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 999, "account": map[string]any{"login": "octo"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := NewClient(server.Client())
	request := InstallationTokenRequest{AppRegistrationID: "registration-1", AppID: "12345", InstallationID: "999", APIBaseURL: server.URL, PrivateKeyPEM: privateKeyPEM}
	client.cache[installationCacheKey(request)] = cachedInstallationToken{token: InstallationToken{Value: "ghs_probe", ExpiresAt: time.Now().Add(10 * time.Minute).UTC()}}
	_, err := client.ProbeInstallation(context.Background(), request)
	if err != ErrMalformedResponse {
		t.Fatalf("expected malformed response for oversized success body, got %v", err)
	}
}

func TestClient_GetRepositoryByID_UsesProviderPathAndParsesCanonicalMetadata(t *testing.T) {
	privateKeyPEM, _ := testRSAPrivateKeyPEM(t)
	for _, tc := range []struct {
		name       string
		apiBaseURL string
		wantPath   string
	}{
		{name: "github.com", apiBaseURL: "https://api.github.com", wantPath: "/repositories/1001"},
		{name: "ghes", apiBaseURL: "/api/v3", wantPath: "/api/v3/repositories/1001"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tc.wantPath {
					t.Fatalf("expected path %q, got %q", tc.wantPath, r.URL.Path)
				}
				if got := r.Header.Get("Authorization"); got != "Bearer ghs_repo" {
					t.Fatalf("expected installation token auth, got %q", got)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{
					"id":             1001,
					"name":           "widgets",
					"full_name":      "octo/widgets",
					"clone_url":      "https://github.com/octo/widgets.git",
					"html_url":       "https://github.com/octo/widgets",
					"default_branch": "main",
					"archived":       true,
					"disabled":       false,
					"owner": map[string]any{
						"login": "octo",
					},
				})
			}))
			defer server.Close()
			client := NewClient(server.Client())
			request := InstallationTokenRequest{AppRegistrationID: "registration-1", AppID: "12345", InstallationID: "999", APIBaseURL: probeBaseURL(server.URL, tc.apiBaseURL), PrivateKeyPEM: privateKeyPEM}
			client.cache[installationCacheKey(request)] = cachedInstallationToken{token: InstallationToken{Value: "ghs_repo", ExpiresAt: time.Now().Add(10 * time.Minute).UTC()}}

			repository, err := client.GetRepositoryByID(context.Background(), request, "1001")
			if err != nil {
				t.Fatalf("get repository by id: %v", err)
			}
			if repository.ID != "1001" || repository.Owner != "octo" || repository.Name != "widgets" || repository.FullName != "octo/widgets" {
				t.Fatalf("unexpected repository identity: %+v", repository)
			}
			if repository.DefaultBranch == nil || *repository.DefaultBranch != "main" || !repository.Archived || repository.Disabled {
				t.Fatalf("unexpected repository metadata: %+v", repository)
			}
		})
	}
}

func TestClient_GetRepositoryByOwnerAndName_EscapesPathAndClassifiesFailures(t *testing.T) {
	privateKeyPEM, _ := testRSAPrivateKeyPEM(t)
	for _, tc := range []struct {
		name       string
		statusCode int
		headers    map[string]string
		body       string
		wantErr    error
	}{
		{name: "forbidden inaccessible", statusCode: http.StatusForbidden, body: `{"message":"forbidden"}`, wantErr: ErrRepositoryInaccessible},
		{name: "not found inaccessible", statusCode: http.StatusNotFound, body: `{"message":"not found"}`, wantErr: ErrRepositoryInaccessible},
		{name: "rate limited", statusCode: http.StatusForbidden, headers: map[string]string{"X-RateLimit-Remaining": "0"}, body: `{"message":"API rate limit exceeded"}`, wantErr: ErrRateLimited},
		{name: "server unavailable", statusCode: http.StatusBadGateway, body: `{"message":"bad gateway"}`, wantErr: ErrProviderUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.EscapedPath() != "/repos/acme%20org/widgets%2Fcore" {
					t.Fatalf("expected escaped owner/name path, got path=%q escaped=%q", r.URL.Path, r.URL.EscapedPath())
				}
				for key, value := range tc.headers {
					w.Header().Set(key, value)
				}
				w.WriteHeader(tc.statusCode)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			client := NewClient(server.Client())
			request := InstallationTokenRequest{AppRegistrationID: "registration-1", AppID: "12345", InstallationID: "999", APIBaseURL: server.URL, PrivateKeyPEM: privateKeyPEM}
			client.cache[installationCacheKey(request)] = cachedInstallationToken{token: InstallationToken{Value: "ghs_repo", ExpiresAt: time.Now().Add(10 * time.Minute).UTC()}}

			_, err := client.GetRepositoryByOwnerAndName(context.Background(), request, "acme org", "widgets/core")
			if err != tc.wantErr {
				t.Fatalf("expected %v, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestClient_GetRepositoryByID_MalformedResponseBranches(t *testing.T) {
	privateKeyPEM, _ := testRSAPrivateKeyPEM(t)
	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "missing owner", body: `{"id":1001,"name":"widgets","full_name":"octo/widgets","clone_url":"https://github.com/octo/widgets.git","html_url":"https://github.com/octo/widgets"}`},
		{name: "bad id", body: `{"id":"not-a-number","name":"widgets","full_name":"octo/widgets","clone_url":"https://github.com/octo/widgets.git","html_url":"https://github.com/octo/widgets","owner":{"login":"octo"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			client := NewClient(server.Client())
			request := InstallationTokenRequest{AppRegistrationID: "registration-1", AppID: "12345", InstallationID: "999", APIBaseURL: server.URL, PrivateKeyPEM: privateKeyPEM}
			client.cache[installationCacheKey(request)] = cachedInstallationToken{token: InstallationToken{Value: "ghs_repo", ExpiresAt: time.Now().Add(10 * time.Minute).UTC()}}

			_, err := client.GetRepositoryByID(context.Background(), request, "1001")
			if err != ErrMalformedResponse {
				t.Fatalf("expected malformed response, got %v", err)
			}
		})
	}
}

func TestClient_GetRepositoryByID_Cached401InvalidatesRefreshesAndRetriesOnce(t *testing.T) {
	privateKeyPEM, _ := testRSAPrivateKeyPEM(t)
	var exchangeCalls atomic.Int32
	var lookupCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/app/installations/999/access_tokens":
			exchangeCalls.Add(1)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{"token": "ghs_repo_fresh", "expires_at": time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339)})
		case "/repositories/1001":
			lookupCalls.Add(1)
			switch r.Header.Get("Authorization") {
			case "Bearer ghs_repo_cached":
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"message":"bad credentials"}`))
			case "Bearer ghs_repo_fresh":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"id":             1001,
					"name":           "widgets",
					"full_name":      "octo/widgets",
					"clone_url":      "https://github.com/octo/widgets.git",
					"html_url":       "https://github.com/octo/widgets",
					"default_branch": "main",
					"owner": map[string]any{
						"login": "octo",
					},
				})
			default:
				t.Fatalf("unexpected auth header %q", r.Header.Get("Authorization"))
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := NewClient(server.Client())
	request := InstallationTokenRequest{AppRegistrationID: "registration-1", AppID: "12345", InstallationID: "999", APIBaseURL: server.URL, PrivateKeyPEM: privateKeyPEM}
	client.cache[installationCacheKey(request)] = cachedInstallationToken{token: InstallationToken{Value: "ghs_repo_cached", ExpiresAt: time.Now().Add(10 * time.Minute).UTC()}}

	repository, err := client.GetRepositoryByID(context.Background(), request, "1001")
	if err != nil {
		t.Fatalf("get repository by id: %v", err)
	}
	if repository.ID != "1001" || repository.FullName != "octo/widgets" {
		t.Fatalf("unexpected repository: %+v", repository)
	}
	if exchangeCalls.Load() != 1 || lookupCalls.Load() != 2 {
		t.Fatalf("expected 1 exchange and 2 lookup calls, got exchanges=%d lookups=%d", exchangeCalls.Load(), lookupCalls.Load())
	}
}

func TestClient_GetRepositoryByID_Cached401ReturnsRefreshFailure(t *testing.T) {
	privateKeyPEM, _ := testRSAPrivateKeyPEM(t)
	var exchangeCalls atomic.Int32
	var lookupCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/app/installations/999/access_tokens":
			exchangeCalls.Add(1)
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"bad credentials"}`))
		case "/repositories/1001":
			lookupCalls.Add(1)
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"bad credentials"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := NewClient(server.Client())
	request := InstallationTokenRequest{AppRegistrationID: "registration-1", AppID: "12345", InstallationID: "999", APIBaseURL: server.URL, PrivateKeyPEM: privateKeyPEM}
	client.cache[installationCacheKey(request)] = cachedInstallationToken{token: InstallationToken{Value: "ghs_repo_cached", ExpiresAt: time.Now().Add(10 * time.Minute).UTC()}}

	_, err := client.GetRepositoryByID(context.Background(), request, "1001")
	if err != ErrAuthentication {
		t.Fatalf("expected auth failure, got %v", err)
	}
	if exchangeCalls.Load() != 1 || lookupCalls.Load() != 1 {
		t.Fatalf("expected 1 exchange and 1 lookup call, got exchanges=%d lookups=%d", exchangeCalls.Load(), lookupCalls.Load())
	}
}

func TestClient_GetRepositoryByOwnerAndName_Cached401InvalidatesRefreshesAndRetriesOnce(t *testing.T) {
	privateKeyPEM, _ := testRSAPrivateKeyPEM(t)
	var exchangeCalls atomic.Int32
	var lookupCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.EscapedPath() {
		case "/app/installations/999/access_tokens":
			exchangeCalls.Add(1)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{"token": "ghs_repo_fresh", "expires_at": time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339)})
		case "/repos/acme/widgets":
			lookupCalls.Add(1)
			switch r.Header.Get("Authorization") {
			case "Bearer ghs_repo_cached":
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"message":"bad credentials"}`))
			case "Bearer ghs_repo_fresh":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"id":        1001,
					"name":      "widgets",
					"full_name": "acme/widgets",
					"clone_url": "https://github.com/acme/widgets.git",
					"html_url":  "https://github.com/acme/widgets",
					"owner": map[string]any{
						"login": "acme",
					},
				})
			default:
				t.Fatalf("unexpected auth header %q", r.Header.Get("Authorization"))
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := NewClient(server.Client())
	request := InstallationTokenRequest{AppRegistrationID: "registration-1", AppID: "12345", InstallationID: "999", APIBaseURL: server.URL, PrivateKeyPEM: privateKeyPEM}
	client.cache[installationCacheKey(request)] = cachedInstallationToken{token: InstallationToken{Value: "ghs_repo_cached", ExpiresAt: time.Now().Add(10 * time.Minute).UTC()}}

	repository, err := client.GetRepositoryByOwnerAndName(context.Background(), request, "acme", "widgets")
	if err != nil {
		t.Fatalf("get repository by owner and name: %v", err)
	}
	if repository.ID != "1001" || repository.FullName != "acme/widgets" {
		t.Fatalf("unexpected repository: %+v", repository)
	}
	if exchangeCalls.Load() != 1 || lookupCalls.Load() != 2 {
		t.Fatalf("expected 1 exchange and 2 lookup calls, got exchanges=%d lookups=%d", exchangeCalls.Load(), lookupCalls.Load())
	}
}

func TestClient_GetRepositoryByOwnerAndName_Second401ReturnsFailureWithoutExtraRetry(t *testing.T) {
	privateKeyPEM, _ := testRSAPrivateKeyPEM(t)
	var exchangeCalls atomic.Int32
	var lookupCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.EscapedPath() {
		case "/app/installations/999/access_tokens":
			exchangeCalls.Add(1)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{"token": "ghs_repo_fresh", "expires_at": time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339)})
		case "/repos/acme/widgets":
			lookupCalls.Add(1)
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"bad credentials"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := NewClient(server.Client())
	request := InstallationTokenRequest{AppRegistrationID: "registration-1", AppID: "12345", InstallationID: "999", APIBaseURL: server.URL, PrivateKeyPEM: privateKeyPEM}
	client.cache[installationCacheKey(request)] = cachedInstallationToken{token: InstallationToken{Value: "ghs_repo_cached", ExpiresAt: time.Now().Add(10 * time.Minute).UTC()}}

	_, err := client.GetRepositoryByOwnerAndName(context.Background(), request, "acme", "widgets")
	if err != ErrAuthentication {
		t.Fatalf("expected auth failure, got %v", err)
	}
	if exchangeCalls.Load() != 1 || lookupCalls.Load() != 2 {
		t.Fatalf("expected one refresh retry, got exchanges=%d lookups=%d", exchangeCalls.Load(), lookupCalls.Load())
	}
}

func TestClient_TokenCacheBehavior(t *testing.T) {
	privateKeyPEM, _ := testRSAPrivateKeyPEM(t)
	var callCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"token": installationIDString(int64(callCount.Load())), "expires_at": time.Date(2026, 7, 17, 17, 0, 0, 0, time.UTC).Format(time.RFC3339)})
	}))
	defer server.Close()
	client := NewClient(server.Client())
	clock := time.Date(2026, 7, 17, 16, 0, 0, 0, time.UTC)
	client.now = func() time.Time { return clock }
	client.refreshSkew = time.Minute
	request := InstallationTokenRequest{AppRegistrationID: "registration-1", AppID: "12345", InstallationID: "999", APIBaseURL: server.URL, PrivateKeyPEM: privateKeyPEM}

	first, err := client.GetInstallationToken(context.Background(), request)
	if err != nil {
		t.Fatalf("first token: %v", err)
	}
	second, err := client.GetInstallationToken(context.Background(), request)
	if err != nil {
		t.Fatalf("second token: %v", err)
	}
	if first.Value != second.Value || callCount.Load() != 1 {
		t.Fatalf("expected cached token reuse, got first=%q second=%q calls=%d", first.Value, second.Value, callCount.Load())
	}

	clock = time.Date(2026, 7, 17, 16, 59, 30, 0, time.UTC)
	third, err := client.GetInstallationToken(context.Background(), request)
	if err != nil {
		t.Fatalf("third token: %v", err)
	}
	if third.Value == first.Value || callCount.Load() != 2 {
		t.Fatalf("expected refresh after skew, got first=%q third=%q calls=%d", first.Value, third.Value, callCount.Load())
	}
}

func TestClient_TokenCacheSingleflightAndFailureRecovery(t *testing.T) {
	privateKeyPEM, _ := testRSAPrivateKeyPEM(t)
	var callCount atomic.Int32
	start := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := callCount.Add(1)
		if current == 1 {
			close(start)
			<-release
		}
		if current == 2 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"boom"}`))
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"token": installationIDString(int64(current)), "expires_at": time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339)})
	}))
	defer server.Close()
	client := NewClient(server.Client())
	request := InstallationTokenRequest{AppRegistrationID: "registration-1", AppID: "12345", InstallationID: "999", APIBaseURL: server.URL, PrivateKeyPEM: privateKeyPEM}

	var wg sync.WaitGroup
	errs := make(chan error, 4)
	results := make(chan InstallationToken, 4)
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			token, err := client.GetInstallationToken(context.Background(), request)
			errs <- err
			results <- token
		}()
	}
	<-start
	close(release)
	wg.Wait()
	close(errs)
	close(results)
	for err := range errs {
		if err != nil {
			t.Fatalf("expected no concurrent refresh errors, got %v", err)
		}
	}
	if callCount.Load() != 1 {
		t.Fatalf("expected one refresh call, got %d", callCount.Load())
	}

	client.cache = map[string]cachedInstallationToken{}
	_, err := client.GetInstallationToken(context.Background(), request)
	if err != ErrProviderUnavailable {
		t.Fatalf("expected provider unavailable on failed refresh, got %v", err)
	}
	token, err := client.GetInstallationToken(context.Background(), request)
	if err != nil {
		t.Fatalf("expected refresh recovery, got %v", err)
	}
	if token.Value != "3" {
		t.Fatalf("expected recovered token value 3, got %q", token.Value)
	}
}

func TestClient_TokenCacheKeyIsolation(t *testing.T) {
	privateKeyPEM, _ := testRSAPrivateKeyPEM(t)
	var callCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"token": installationIDString(int64(callCount.Load())), "expires_at": time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339)})
	}))
	defer server.Close()
	client := NewClient(server.Client())
	requests := []InstallationTokenRequest{
		{AppRegistrationID: "registration-1", AppID: "12345", InstallationID: "999", APIBaseURL: server.URL, PrivateKeyPEM: privateKeyPEM},
		{AppRegistrationID: "registration-2", AppID: "12345", InstallationID: "999", APIBaseURL: server.URL, PrivateKeyPEM: privateKeyPEM},
		{AppRegistrationID: "registration-1", AppID: "12345", InstallationID: "1000", APIBaseURL: server.URL, PrivateKeyPEM: privateKeyPEM},
	}
	for _, request := range requests {
		if _, err := client.GetInstallationToken(context.Background(), request); err != nil {
			t.Fatalf("get token: %v", err)
		}
	}
	if callCount.Load() != int32(len(requests)) {
		t.Fatalf("expected isolated cache keys, got %d calls", callCount.Load())
	}
}

func TestInstallationCacheKey_NormalizesRepositoryRestrictionOrder(t *testing.T) {
	base := InstallationTokenRequest{AppRegistrationID: "registration-1", InstallationID: "999", APIBaseURL: "https://api.github.com"}
	first := base
	first.RepositoryIDs = []string{" 200 ", "100", "200"}
	second := base
	second.RepositoryIDs = []string{"100", "200"}

	if firstKey, secondKey := installationCacheKey(first), installationCacheKey(second); firstKey != secondKey {
		t.Fatalf("expected equivalent repository restriction keys, got %q and %q", firstKey, secondKey)
	}
	if got := normalizedRepositoryIDs(first.RepositoryIDs); len(got) != 2 || got[0] != "100" || got[1] != "200" {
		t.Fatalf("expected sorted unique repository IDs, got %#v", got)
	}
}

func TestClient_GetInstallationToken_RejectsMalformedRepositoryRestriction(t *testing.T) {
	privateKeyPEM, _ := testRSAPrivateKeyPEM(t)
	for _, repositoryIDs := range [][]string{{"not-a-number"}, {"0"}, {"-1"}} {
		client := NewClient(nil)
		_, err := client.GetInstallationToken(context.Background(), InstallationTokenRequest{AppRegistrationID: "registration-1", AppID: "12345", InstallationID: "999", APIBaseURL: "https://api.github.com", PrivateKeyPEM: privateKeyPEM, RepositoryIDs: repositoryIDs})
		if err != ErrMalformedResponse {
			t.Fatalf("expected malformed restriction %q to fail, got %v", repositoryIDs, err)
		}
	}
}

func TestClient_GetFreshInstallationToken_WaitsForNormalExchange(t *testing.T) {
	privateKeyPEM, _ := testRSAPrivateKeyPEM(t)
	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			close(started)
			<-release
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"token": installationIDString(int64(calls.Load())), "expires_at": time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339)})
	}))
	defer server.Close()
	client := NewClient(server.Client())
	request := InstallationTokenRequest{AppRegistrationID: "registration-1", AppID: "12345", InstallationID: "999", APIBaseURL: server.URL, PrivateKeyPEM: privateKeyPEM, RepositoryIDs: []string{"1001"}}
	normalResult := make(chan error, 1)
	go func() { _, err := client.GetInstallationToken(context.Background(), request); normalResult <- err }()
	<-started
	freshResult := make(chan InstallationToken, 1)
	freshErr := make(chan error, 1)
	go func() {
		token, err := client.GetFreshInstallationToken(context.Background(), request)
		freshResult <- token
		freshErr <- err
	}()
	close(release)
	if err := <-normalResult; err != nil {
		t.Fatalf("normal exchange: %v", err)
	}
	if err := <-freshErr; err != nil {
		t.Fatalf("forced refresh: %v", err)
	}
	if token := <-freshResult; token.Value != "2" || calls.Load() != 2 {
		t.Fatalf("expected forced exchange after normal exchange, token=%+v calls=%d", token, calls.Load())
	}
}

func TestClient_GetInstallationToken_WaitsForFreshExchange(t *testing.T) {
	privateKeyPEM, _ := testRSAPrivateKeyPEM(t)
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"token": "fresh-token", "expires_at": time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339)})
	}))
	defer server.Close()
	client := NewClient(server.Client())
	request := InstallationTokenRequest{AppRegistrationID: "registration-1", AppID: "12345", InstallationID: "999", APIBaseURL: server.URL, PrivateKeyPEM: privateKeyPEM, RepositoryIDs: []string{"1001"}}
	freshErr := make(chan error, 1)
	go func() { _, err := client.GetFreshInstallationToken(context.Background(), request); freshErr <- err }()
	<-started
	normalResult := make(chan InstallationToken, 1)
	normalErr := make(chan error, 1)
	go func() {
		token, err := client.GetInstallationToken(context.Background(), request)
		normalResult <- token
		normalErr <- err
	}()
	close(release)
	if err := <-freshErr; err != nil {
		t.Fatalf("fresh exchange: %v", err)
	}
	if err := <-normalErr; err != nil {
		t.Fatalf("normal cache lookup: %v", err)
	}
	if token := <-normalResult; token.Value != "fresh-token" {
		t.Fatalf("expected fresh cached token, got %+v", token)
	}
}

func TestClient_GetFreshInstallationToken_BypassesOnlyExactScopedCacheEntry(t *testing.T) {
	privateKeyPEM, _ := testRSAPrivateKeyPEM(t)
	var callCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/app/installations/999/access_tokens" {
			http.NotFound(w, r)
			return
		}
		var body struct {
			RepositoryIDs []int64 `json:"repository_ids"`
		}
		if decodeErr := json.NewDecoder(r.Body).Decode(&body); decodeErr != nil || len(body.RepositoryIDs) != 1 || body.RepositoryIDs[0] != 1001 {
			t.Fatalf("expected numeric repository restriction [1001], body=%+v err=%v", body, decodeErr)
		}
		current := callCount.Add(1)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"token": "token-" + installationIDString(int64(current)), "expires_at": time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339)})
	}))
	defer server.Close()
	client := NewClient(server.Client())
	request := InstallationTokenRequest{AppRegistrationID: "registration-1", AppID: "12345", InstallationID: "999", APIBaseURL: server.URL, PrivateKeyPEM: privateKeyPEM, RepositoryIDs: []string{"1001"}}
	unrelated := InstallationTokenRequest{AppRegistrationID: "registration-2", AppID: "12345", InstallationID: "999", APIBaseURL: server.URL, PrivateKeyPEM: privateKeyPEM, RepositoryIDs: []string{"1002"}}
	client.cache[installationCacheKey(unrelated)] = cachedInstallationToken{token: InstallationToken{Value: "unrelated-token", ExpiresAt: time.Now().Add(10 * time.Minute).UTC()}}

	normal, err := client.GetInstallationToken(context.Background(), request)
	if err != nil {
		t.Fatalf("normal token: %v", err)
	}
	fresh, err := client.GetFreshInstallationToken(context.Background(), request)
	if err != nil {
		t.Fatalf("fresh token: %v", err)
	}
	again, err := client.GetInstallationToken(context.Background(), request)
	if err != nil {
		t.Fatalf("cached fresh token: %v", err)
	}
	if normal.Value == fresh.Value || fresh.Value != again.Value || callCount.Load() != 2 {
		t.Fatalf("expected one normal exchange and one forced exchange, normal=%q fresh=%q cached=%q calls=%d", normal.Value, fresh.Value, again.Value, callCount.Load())
	}
	if entry := client.cache[installationCacheKey(unrelated)].token; entry.Value != "unrelated-token" {
		t.Fatalf("expected unrelated cache entry to remain untouched, got %+v", entry)
	}
}

func TestClient_GetFreshInstallationToken_ConcurrentRequestsShareOneExchange(t *testing.T) {
	privateKeyPEM, _ := testRSAPrivateKeyPEM(t)
	var callCount atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if callCount.Add(1) == 1 {
			close(started)
			<-release
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"token": "fresh-token", "expires_at": time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339)})
	}))
	defer server.Close()
	client := NewClient(server.Client())
	request := InstallationTokenRequest{AppRegistrationID: "registration-1", AppID: "12345", InstallationID: "999", APIBaseURL: server.URL, PrivateKeyPEM: privateKeyPEM, RepositoryIDs: []string{"1001"}}
	results := make(chan InstallationToken, 2)
	errs := make(chan error, 2)
	for range 2 {
		go func() {
			token, freshErr := client.GetFreshInstallationToken(context.Background(), request)
			results <- token
			errs <- freshErr
		}()
	}
	<-started
	close(release)
	for range 2 {
		if freshErr := <-errs; freshErr != nil {
			t.Fatalf("fresh token: %v", freshErr)
		}
		if token := <-results; token.Value != "fresh-token" {
			t.Fatalf("unexpected token: %+v", token)
		}
	}
	if callCount.Load() != 1 {
		t.Fatalf("expected one forced exchange, got %d", callCount.Load())
	}
}

func TestClient_GetFreshInstallationToken_FailurePreservesCachedToken(t *testing.T) {
	privateKeyPEM, _ := testRSAPrivateKeyPEM(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"bad credentials"}`))
	}))
	defer server.Close()
	client := NewClient(server.Client())
	request := InstallationTokenRequest{AppRegistrationID: "registration-1", AppID: "12345", InstallationID: "999", APIBaseURL: server.URL, PrivateKeyPEM: privateKeyPEM, RepositoryIDs: []string{"1001"}}
	client.cache[installationCacheKey(request)] = cachedInstallationToken{token: InstallationToken{Value: "cached-token", ExpiresAt: time.Now().Add(10 * time.Minute).UTC()}}
	if _, err := client.GetFreshInstallationToken(context.Background(), request); err != ErrAuthentication {
		t.Fatalf("expected classified authentication error, got %v", err)
	}
	token, err := client.GetInstallationToken(context.Background(), request)
	if err != nil || token.Value != "cached-token" {
		t.Fatalf("expected intact cached token after refresh failure, token=%+v err=%v", token, err)
	}
}

func TestClient_GetFreshInstallationToken_ConcurrentFailureSharesRefreshErrorWithoutReturningStaleToken(t *testing.T) {
	privateKeyPEM, _ := testRSAPrivateKeyPEM(t)
	var exchangeCalls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		exchangeCalls.Add(1)
		close(started)
		<-release
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"bad credentials"}`))
	}))
	defer server.Close()
	client := NewClient(server.Client())
	request := InstallationTokenRequest{AppRegistrationID: "registration-1", AppID: "12345", InstallationID: "999", APIBaseURL: server.URL, PrivateKeyPEM: privateKeyPEM, RepositoryIDs: []string{"1001"}}
	client.cache[installationCacheKey(request)] = cachedInstallationToken{token: InstallationToken{Value: "stale-token", ExpiresAt: time.Now().Add(10 * time.Minute).UTC()}}

	results := make(chan struct {
		token InstallationToken
		err   error
	}, 3)
	for range 3 {
		go func() {
			token, refreshErr := client.GetFreshInstallationToken(context.Background(), request)
			results <- struct {
				token InstallationToken
				err   error
			}{token: token, err: refreshErr}
		}()
	}
	<-started
	close(release)
	for range 3 {
		result := <-results
		if result.err != ErrAuthentication || result.token.Value != "" {
			t.Fatalf("expected shared refresh authentication error without token, result=%+v", result)
		}
	}
	if exchangeCalls.Load() != 1 {
		t.Fatalf("expected one forced refresh exchange, got %d", exchangeCalls.Load())
	}
	cached, err := client.GetInstallationToken(context.Background(), request)
	if err != nil || cached.Value != "stale-token" {
		t.Fatalf("expected ordinary lookup to retain stale cached token, token=%+v err=%v", cached, err)
	}
}

func TestClient_GetFreshInstallationToken_WaitingCallerContextCancellation(t *testing.T) {
	privateKeyPEM, _ := testRSAPrivateKeyPEM(t)
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"token": "fresh-token", "expires_at": time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339)})
	}))
	defer server.Close()
	client := NewClient(server.Client())
	request := InstallationTokenRequest{AppRegistrationID: "registration-1", AppID: "12345", InstallationID: "999", APIBaseURL: server.URL, PrivateKeyPEM: privateKeyPEM, RepositoryIDs: []string{"1001"}}
	leaderResult := make(chan error, 1)
	go func() {
		_, leaderErr := client.GetFreshInstallationToken(context.Background(), request)
		leaderResult <- leaderErr
	}()
	<-started
	waiterCtx, cancel := context.WithCancel(context.Background())
	waiterResult := make(chan error, 1)
	go func() {
		_, waiterErr := client.GetFreshInstallationToken(waiterCtx, request)
		waiterResult <- waiterErr
	}()
	cancel()
	if waiterErr := <-waiterResult; waiterErr != context.Canceled {
		t.Fatalf("expected waiting caller cancellation, got %v", waiterErr)
	}
	close(release)
	if leaderErr := <-leaderResult; leaderErr != nil {
		t.Fatalf("expected leader refresh success, got %v", leaderErr)
	}
}

func TestClient_ResponseHelpers(t *testing.T) {
	if got := readGitHubMessage(strings.NewReader(`{"message":"Bad Credentials"}`)); got != "bad credentials" {
		t.Fatalf("expected lower-cased github message, got %q", got)
	}
	if got := readGitHubMessage(strings.NewReader(`{"other":"value"}`)); got != "" {
		t.Fatalf("expected empty message for valid json without message field, got %q", got)
	}
	if got := readGitHubMessage(strings.NewReader("  Plain Failure  ")); got != "plain failure" {
		t.Fatalf("expected lower-cased raw body, got %q", got)
	}

	var payload map[string]any
	decodeErr := decodeGitHubJSON(strings.NewReader(`{"message":"abcdefghijklmnopqrstuvwxyz"}`), 8, &payload)
	if decodeErr == nil {
		t.Fatal("expected bounded decode failure")
	}

	if !isRateLimited(&http.Response{StatusCode: http.StatusTooManyRequests, Header: make(http.Header)}, "") {
		t.Fatal("expected 429 to be treated as rate limited")
	}
	resp := &http.Response{StatusCode: http.StatusForbidden, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"message":"Installation suspended"}`))}
	if got := classifyGitHubResponse(resp); got != ErrInstallationUnavailable {
		t.Fatalf("expected suspended installation classification, got %v", got)
	}
	resp = &http.Response{StatusCode: http.StatusForbidden, Header: http.Header{"Retry-After": []string{"60"}}, Body: io.NopCloser(strings.NewReader(`{"message":"slow down"}`))}
	if got := classifyGitHubResponse(resp); got != ErrRateLimited {
		t.Fatalf("expected retry-after classification, got %v", got)
	}
	resp = &http.Response{StatusCode: http.StatusForbidden, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"message":"Bad credentials"}`))}
	if got := classifyGitHubResponse(resp); got != ErrAuthentication {
		t.Fatalf("expected auth classification, got %v", got)
	}
}

func TestClient_GetInstallationToken_TransportErrorBranches(t *testing.T) {
	privateKeyPEM, _ := testRSAPrivateKeyPEM(t)
	request := InstallationTokenRequest{AppRegistrationID: "registration-1", AppID: "12345", InstallationID: "999", APIBaseURL: "https://api.github.com", PrivateKeyPEM: privateKeyPEM}

	client := NewClient(nil)
	client.httpClient = stubHTTPDoer(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("dial tcp: connection refused")
	})
	_, err := client.GetInstallationToken(context.Background(), request)
	if err != ErrProviderUnavailable {
		t.Fatalf("expected provider unavailable, got %v", err)
	}

	canceledClient := NewClient(nil)
	canceledClient.httpClient = stubHTTPDoer(func(req *http.Request) (*http.Response, error) {
		return nil, req.Context().Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = canceledClient.GetInstallationToken(ctx, request)
	if err != context.Canceled {
		t.Fatalf("expected context canceled, got %v", err)
	}
}

func probeBaseURL(serverURL string, apiBaseURL string) string {
	if apiBaseURL == "https://api.github.com" {
		return serverURL
	}
	return serverURL + apiBaseURL
}

type stubHTTPDoer func(req *http.Request) (*http.Response, error)

func (f stubHTTPDoer) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}
