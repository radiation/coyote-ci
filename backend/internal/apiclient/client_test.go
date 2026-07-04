package apiclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

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

func TestErrorFormattingTimeoutAndHelpers(t *testing.T) {
	wrapped := errors.New("boom")
	err := &Error{StatusCode: http.StatusUnauthorized, Message: "auth failed", RequestID: "req-1", Err: wrapped}
	if err.Error() != "auth failed (request_id=req-1)" {
		t.Fatalf("unexpected error string: %q", err.Error())
	}
	if !errors.Is(err, wrapped) {
		t.Fatalf("expected unwrap to expose wrapped error")
	}

	client, newErr := New("https://example.com", "", "agent", &http.Client{})
	if newErr != nil {
		t.Fatalf("new client: %v", newErr)
	}
	if client.httpClient.Timeout != defaultTimeout {
		t.Fatalf("expected default timeout, got %v", client.httpClient.Timeout)
	}

	customClient := &http.Client{Timeout: 3 * time.Second}
	client, newErr = New("https://example.com", "", "agent", customClient)
	if newErr != nil {
		t.Fatalf("new client with custom timeout: %v", newErr)
	}
	if client.httpClient.Timeout != 3*time.Second {
		t.Fatalf("expected preserved timeout, got %v", client.httpClient.Timeout)
	}
}

func TestClientDoJSONRequestPathTransportAndNoOutput(t *testing.T) {
	client, err := New("https://example.com/coyote", "", "agent", &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("network down")
	})})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	err = client.doJSON(context.Background(), http.MethodGet, "", nil, nil)
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Kind != ErrorKindUnexpected {
		t.Fatalf("expected invalid request path error, got %v", err)
	}

	err = client.doJSON(context.Background(), http.MethodGet, "api/info", nil, nil)
	if !errors.As(err, &apiErr) || apiErr.Kind != ErrorKindTransport {
		t.Fatalf("expected transport error, got %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client, err = New(server.URL, "", "agent", nil)
	if err != nil {
		t.Fatalf("new client no output: %v", err)
	}
	if err := client.doJSON(context.Background(), http.MethodGet, "api/info", nil, nil); err != nil {
		t.Fatalf("doJSON with nil output: %v", err)
	}
}

func TestDecodeErrorResponseAndClassifyStatus(t *testing.T) {
	response := &http.Response{
		StatusCode: http.StatusBadGateway,
		Body:       io.NopCloser(strings.NewReader("")),
	}
	err := decodeErrorResponse(response, "req-2")
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Message != http.StatusText(http.StatusBadGateway) {
		t.Fatalf("unexpected decoded empty-body error: %v", err)
	}

	tests := []struct {
		status int
		want   ErrorKind
	}{
		{status: http.StatusUnauthorized, want: ErrorKindAuthentication},
		{status: http.StatusForbidden, want: ErrorKindAuthorization},
		{status: http.StatusNotFound, want: ErrorKindNotFound},
		{status: http.StatusConflict, want: ErrorKindConflict},
		{status: http.StatusBadRequest, want: ErrorKindValidation},
		{status: http.StatusUnprocessableEntity, want: ErrorKindValidation},
		{status: http.StatusInternalServerError, want: ErrorKindServer},
		{status: http.StatusTeapot, want: ErrorKindUnexpected},
	}
	for _, tc := range tests {
		if got := classifyStatus(tc.status); got != tc.want {
			t.Fatalf("classifyStatus(%d) = %q, want %q", tc.status, got, tc.want)
		}
	}

	client, err := New("https://example.com", "", "agent", nil)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if _, err := resolveRequestURL(client.baseURL, " /api/info "); err != nil {
		t.Fatalf("resolve request url with trimmed path: %v", err)
	}

	transportErr := fmt.Errorf("wrapped transport")
	wrapped := (&Error{Err: transportErr}).Unwrap()
	if wrapped != transportErr {
		t.Fatalf("unexpected unwrap value: %v", wrapped)
	}
}

func TestErrorFallbackFormattingAndWrappers(t *testing.T) {
	if (*Error)(nil).Error() != "" {
		t.Fatal("expected nil error string")
	}
	if (*Error)(nil).Unwrap() != nil {
		t.Fatal("expected nil unwrap for nil error")
	}
	err := &Error{StatusCode: http.StatusUnauthorized}
	if err.Error() != http.StatusText(http.StatusUnauthorized) {
		t.Fatalf("expected status text fallback, got %q", err.Error())
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/me":
			_, _ = w.Write([]byte(`{"data":{"auth_mode":"header","auth_method":"api_token","email_verified":true,"user":{"id":"user-3","email":"me@example.com","global_role":"user"}}}`))
		case "/api/info":
			_, _ = w.Write([]byte(`{"data":{"version":"1.0.1","commit":"abc","build_date":"2026-07-04","api_version":"0.1"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, newErr := New(server.URL, " token ", "agent", nil)
	if newErr != nil {
		t.Fatalf("new client: %v", newErr)
	}
	me, getMeErr := client.GetMe(context.Background())
	if getMeErr != nil || me.User.Email != "me@example.com" {
		t.Fatalf("unexpected get me result: %+v err=%v", me, getMeErr)
	}
	info, infoErr := client.GetServerInfo(context.Background())
	if infoErr != nil || info.Version != "1.0.1" {
		t.Fatalf("unexpected server info result: %+v err=%v", info, infoErr)
	}
}
