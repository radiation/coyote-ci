package githubapp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

func TestClient_ProbeInstallation_SuspendedAndHealthy(t *testing.T) {
	privateKeyPEM, _ := testRSAPrivateKeyPEM(t)
	for _, tc := range []struct {
		name       string
		apiBaseURL string
		wantPath   string
	}{
		{name: "github.com", apiBaseURL: "https://api.github.com", wantPath: "/installation/repositories"},
		{name: "ghes", apiBaseURL: "/api/v3", wantPath: "/api/v3/installation/repositories"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case tc.wantPath:
					if got := r.URL.Query().Get("per_page"); got != "1" {
						t.Fatalf("expected per_page=1, got %q", got)
					}
					if got := r.Header.Get("Authorization"); got != "Bearer ghs_probe" {
						t.Fatalf("expected installation token auth, got %q", got)
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"total_count": 1, "repositories": []map[string]any{{"id": 1}}})
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
			if result.InstallationID != "999" {
				t.Fatalf("unexpected probe result: %+v", result)
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
		wantErr    error
	}{
		{name: "malformed success", statusCode: http.StatusOK, body: `{"repositories":[]}`, wantErr: ErrMalformedResponse},
		{name: "installation unavailable", statusCode: http.StatusNotFound, body: `{"message":"not found"}`, wantErr: ErrInstallationUnavailable},
		{name: "rate limited", statusCode: http.StatusForbidden, headers: map[string]string{"X-RateLimit-Remaining": "0"}, body: `{"message":"API rate limit exceeded"}`, wantErr: ErrRateLimited},
		{name: "provider unavailable", statusCode: http.StatusBadGateway, body: `{"message":"bad gateway"}`, wantErr: ErrProviderUnavailable},
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
			request := InstallationTokenRequest{AppRegistrationID: "registration-1", AppID: "12345", InstallationID: "999", APIBaseURL: server.URL, PrivateKeyPEM: privateKeyPEM}
			client.cache[installationCacheKey(request)] = cachedInstallationToken{token: InstallationToken{Value: "ghs_probe", ExpiresAt: time.Now().Add(10 * time.Minute).UTC()}}
			_, err := client.ProbeInstallation(context.Background(), request)
			if err != tc.wantErr {
				t.Fatalf("expected %v, got %v", tc.wantErr, err)
			}
		})
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

func probeBaseURL(serverURL string, apiBaseURL string) string {
	if apiBaseURL == "https://api.github.com" {
		return serverURL
	}
	return serverURL + apiBaseURL
}
