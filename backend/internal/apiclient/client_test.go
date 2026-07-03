package apiclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestClient_GetMeSendsBearerAndUserAgent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/me" {
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("expected bearer header, got %q", got)
		}
		if got := r.Header.Get("User-Agent"); got != "coyote/dev" {
			t.Fatalf("expected user agent, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"auth_mode":"oidc","auth_method":"api_token","email_verified":true,"user":{"id":"user-1","email":"dev@example.com","global_role":"admin"}}}`))
	}))
	defer server.Close()

	client, err := New(server.URL, "test-token", "coyote/dev", nil)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	me, err := client.GetMe(context.Background())
	if err != nil {
		t.Fatalf("get me: %v", err)
	}
	if me.AuthMethod != "api_token" || me.User.ID != "user-1" {
		t.Fatalf("unexpected response: %+v", me)
	}
}

func TestClient_RequestURLPreservesPathPrefixAndQuery(t *testing.T) {
	baseURL, err := normalizeBaseURL("https://example.com/platform/coyote/")
	if err != nil {
		t.Fatalf("normalize base url: %v", err)
	}

	resolved, err := resolveRequestURL(baseURL, "api/builds?status=running")
	if err != nil {
		t.Fatalf("resolve request url: %v", err)
	}
	if resolved.String() != "https://example.com/platform/coyote/api/builds?status=running" {
		t.Fatalf("unexpected resolved url: %s", resolved.String())
	}
}

func TestNormalizeBaseURLValidation(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "root host", input: "https://ci.example.com", want: "https://ci.example.com"},
		{name: "one level prefix", input: "https://example.com/coyote", want: "https://example.com/coyote"},
		{name: "multi level prefix", input: "https://example.com/platform/coyote/", want: "https://example.com/platform/coyote"},
		{name: "embedded credentials", input: "https://user:pass@example.com/coyote", wantErr: true},
		{name: "fragment", input: "https://example.com/coyote#frag", wantErr: true},
		{name: "query", input: "https://example.com/coyote?x=1", wantErr: true},
		{name: "malformed", input: "://bad", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeBaseURL(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("normalize base url: %v", err)
			}
			if got.String() != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got.String())
			}
		})
	}
}

func TestClient_GetServerInfoReturnsTypedValidationError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-Id", "req-123")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":"invalid_request","message":"bad input"}}`))
	}))
	defer server.Close()

	client, err := New(server.URL, "", "coyote/dev", nil)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	_, err = client.GetServerInfo(context.Background())
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected api error, got %T", err)
	}
	if apiErr.Kind != ErrorKindValidation || apiErr.RequestID != "req-123" || apiErr.Code != "invalid_request" {
		t.Fatalf("unexpected api error: %+v", apiErr)
	}
}

func TestClient_GetServerInfoHandlesNonJSONServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer server.Close()

	client, err := New(server.URL, "", "coyote/dev", nil)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	_, err = client.GetServerInfo(context.Background())
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected api error, got %T", err)
	}
	if apiErr.Kind != ErrorKindServer || apiErr.Message != "boom" {
		t.Fatalf("unexpected api error: %+v", apiErr)
	}
}

func TestClient_UsesContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	client, err := New(server.URL, "", "coyote/dev", &http.Client{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = client.GetServerInfo(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
}

func TestClient_InvalidJSONResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":`))
	}))
	defer server.Close()

	client, err := New(server.URL, "", "coyote/dev", nil)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	_, err = client.GetServerInfo(context.Background())
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected api error, got %T", err)
	}
	if apiErr.Kind != ErrorKindUnexpected || !strings.Contains(apiErr.Error(), "invalid json response") {
		t.Fatalf("unexpected error: %+v", apiErr)
	}
}

func TestResolveRequestURLRejectsAbsoluteRequestURLs(t *testing.T) {
	baseURL := &url.URL{Scheme: "https", Host: "example.com", Path: "/coyote"}
	_, err := resolveRequestURL(baseURL, "https://other.example.com/api/me")
	if err == nil {
		t.Fatal("expected error")
	}
}
