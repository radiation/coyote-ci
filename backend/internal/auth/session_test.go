package auth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewCookieSessionManager_DefaultsAndValidation(t *testing.T) {
	_, err := NewCookieSessionManager(CookieSessionConfig{})
	if err == nil || err.Error() != "session secret is required" {
		t.Fatalf("expected missing secret error, got %v", err)
	}

	_, err = NewCookieSessionManager(CookieSessionConfig{
		Secret:   "test-session-secret",
		Secure:   false,
		SameSite: http.SameSiteNoneMode,
	})
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if err.Error() != "SESSION_COOKIE_SECURE must be true when SESSION_COOKIE_SAME_SITE=none" {
		t.Fatalf("unexpected error: %v", err)
	}

	manager, err := NewCookieSessionManager(CookieSessionConfig{Secret: "test-session-secret", SameSite: http.SameSiteDefaultMode})
	if err != nil {
		t.Fatalf("expected manager, got %v", err)
	}
	if manager.cookieName != defaultSessionCookieName {
		t.Fatalf("expected default cookie name %q, got %q", defaultSessionCookieName, manager.cookieName)
	}
	if manager.authCookieName != defaultSessionCookieName+"_oidc" {
		t.Fatalf("unexpected auth cookie name %q", manager.authCookieName)
	}
	if manager.sameSite != http.SameSiteLaxMode {
		t.Fatalf("expected lax same-site default, got %v", manager.sameSite)
	}
}

func TestParseSameSite(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		expected  http.SameSite
		expectErr bool
	}{
		{name: "default empty", input: "", expected: http.SameSiteLaxMode},
		{name: "lax", input: " lax ", expected: http.SameSiteLaxMode},
		{name: "strict", input: "strict", expected: http.SameSiteStrictMode},
		{name: "none", input: "none", expected: http.SameSiteNoneMode},
		{name: "invalid", input: "off", expected: http.SameSiteDefaultMode, expectErr: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			actual, err := ParseSameSite(tc.input)
			if tc.expectErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
			} else if err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}
			if actual != tc.expected {
				t.Fatalf("expected same-site %v, got %v", tc.expected, actual)
			}
		})
	}
}

func TestCookieSessionManager_CreateAndReadSession(t *testing.T) {
	now := time.Date(2025, time.January, 2, 3, 4, 5, 0, time.UTC)
	manager := newCookieSessionManagerForTest(t, now)
	manager.secure = true
	manager.sameSite = http.SameSiteStrictMode

	response := httptest.NewRecorder()
	if err := manager.CreateSession(response, " user-1 "); err != nil {
		t.Fatalf("create session failed: %v", err)
	}

	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected one cookie, got %d", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != defaultSessionCookieName {
		t.Fatalf("expected cookie name %q, got %q", defaultSessionCookieName, cookie.Name)
	}
	if cookie.Path != "/" {
		t.Fatalf("expected root path, got %q", cookie.Path)
	}
	if !cookie.HttpOnly || !cookie.Secure {
		t.Fatalf("expected secure http-only cookie, got %#v", cookie)
	}
	if cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("expected strict same-site, got %v", cookie.SameSite)
	}
	if cookie.MaxAge != int(defaultSessionTTL.Seconds()) {
		t.Fatalf("expected max age %d, got %d", int(defaultSessionTTL.Seconds()), cookie.MaxAge)
	}

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(cookie)
	userID, err := manager.UserID(request)
	if err != nil {
		t.Fatalf("user id failed: %v", err)
	}
	if userID != "user-1" {
		t.Fatalf("expected trimmed user id, got %q", userID)
	}

	clearResponse := httptest.NewRecorder()
	manager.ClearSession(clearResponse)
	clearCookies := clearResponse.Result().Cookies()
	if len(clearCookies) != 1 || clearCookies[0].MaxAge != -1 {
		t.Fatalf("expected expired cookie, got %#v", clearCookies)
	}
	if clearCookies[0].Path != "/" {
		t.Fatalf("expected clear cookie path '/', got %q", clearCookies[0].Path)
	}
}

func TestCookieSessionManager_UserIDRejectsInvalidSessions(t *testing.T) {
	now := time.Date(2025, time.January, 2, 3, 4, 5, 0, time.UTC)
	manager := newCookieSessionManagerForTest(t, now)

	_, err := manager.UserID(httptest.NewRequest(http.MethodGet, "/", nil))
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound, got %v", err)
	}

	response := httptest.NewRecorder()
	createErr := manager.CreateSession(response, "user-1")
	if createErr != nil {
		t.Fatalf("create session failed: %v", createErr)
	}
	tamperedRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	tampered := *response.Result().Cookies()[0]
	tampered.Value += "tampered"
	tamperedRequest.AddCookie(&tampered)
	_, err = manager.UserID(tamperedRequest)
	if !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("expected ErrSessionInvalid for tampered cookie, got %v", err)
	}

	expiredValue, err := manager.encode(sessionPayload{UserID: "user-1", ExpiresAt: now.Add(-time.Minute).Unix()})
	if err != nil {
		t.Fatalf("encode expired session failed: %v", err)
	}
	expiredRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	expiredRequest.AddCookie(&http.Cookie{Name: manager.cookieName, Value: expiredValue})
	_, err = manager.UserID(expiredRequest)
	if !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("expected ErrSessionInvalid for expired cookie, got %v", err)
	}

	emptyValue, err := manager.encode(sessionPayload{UserID: " ", ExpiresAt: now.Add(time.Hour).Unix()})
	if err != nil {
		t.Fatalf("encode empty session failed: %v", err)
	}
	emptyRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	emptyRequest.AddCookie(&http.Cookie{Name: manager.cookieName, Value: emptyValue})
	_, err = manager.UserID(emptyRequest)
	if !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("expected ErrSessionInvalid for empty user id, got %v", err)
	}

	if err := manager.CreateSession(httptest.NewRecorder(), " "); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("expected ErrSessionInvalid for blank create session id, got %v", err)
	}
}

func TestCookieSessionManager_CreateAndVerifyAuthRequest(t *testing.T) {
	now := time.Date(2025, time.January, 2, 3, 4, 5, 0, time.UTC)
	manager := newCookieSessionManagerForTest(t, now)
	manager.sameSite = http.SameSiteStrictMode

	response := httptest.NewRecorder()
	if err := manager.CreateAuthRequest(response, " state-1 ", " nonce-1 "); err != nil {
		t.Fatalf("create auth request failed: %v", err)
	}

	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected one auth cookie, got %d", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != manager.authCookieName {
		t.Fatalf("expected auth cookie name %q, got %q", manager.authCookieName, cookie.Name)
	}
	if cookie.Path != "/auth" {
		t.Fatalf("expected auth cookie path '/auth', got %q", cookie.Path)
	}
	if cookie.MaxAge != int(defaultAuthRequestTTL.Seconds()) {
		t.Fatalf("expected auth max age %d, got %d", int(defaultAuthRequestTTL.Seconds()), cookie.MaxAge)
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("expected auth request cookie to relax strict same-site to lax, got %v", cookie.SameSite)
	}

	request := httptest.NewRequest(http.MethodGet, "/auth/callback?state=state-1", nil)
	request.AddCookie(cookie)
	verifyResponse := httptest.NewRecorder()
	nonce, err := manager.VerifyAuthRequest(verifyResponse, request, " state-1 ")
	if err != nil {
		t.Fatalf("verify auth request failed: %v", err)
	}
	if nonce != "nonce-1" {
		t.Fatalf("expected nonce %q, got %q", "nonce-1", nonce)
	}
	clearingCookies := verifyResponse.Result().Cookies()
	if len(clearingCookies) != 1 || clearingCookies[0].Name != manager.authCookieName || clearingCookies[0].MaxAge != -1 {
		t.Fatalf("expected expired auth cookie, got %#v", clearingCookies)
	}
	if clearingCookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatalf("expected auth cleanup cookie to use lax same-site, got %v", clearingCookies[0].SameSite)
	}

	if err := manager.CreateAuthRequest(httptest.NewRecorder(), " ", "nonce"); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("expected ErrSessionInvalid for blank state, got %v", err)
	}
	if err := manager.CreateAuthRequest(httptest.NewRecorder(), "state", " "); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("expected ErrSessionInvalid for blank nonce, got %v", err)
	}
}

func TestCookieSessionManager_VerifyAuthRequestRejectsInvalidValues(t *testing.T) {
	now := time.Date(2025, time.January, 2, 3, 4, 5, 0, time.UTC)
	manager := newCookieSessionManagerForTest(t, now)

	tests := []struct {
		name        string
		cookieValue string
		state       string
		expectedErr error
	}{
		{name: "missing cookie", expectedErr: ErrSessionNotFound},
		{name: "tampered cookie", cookieValue: "invalid.value", state: "state-1", expectedErr: ErrSessionInvalid},
		{name: "expired auth request", cookieValue: mustEncodeAuthRequest(t, manager, authRequestPayload{State: "state-1", Nonce: "nonce-1", ExpiresAt: now.Add(-time.Minute).Unix()}), state: "state-1", expectedErr: ErrSessionInvalid},
		{name: "wrong state", cookieValue: mustEncodeAuthRequest(t, manager, authRequestPayload{State: "state-1", Nonce: "nonce-1", ExpiresAt: now.Add(time.Minute).Unix()}), state: "other-state", expectedErr: ErrSessionInvalid},
		{name: "blank nonce", cookieValue: mustEncodeAuthRequest(t, manager, authRequestPayload{State: "state-1", Nonce: " ", ExpiresAt: now.Add(time.Minute).Unix()}), state: "state-1", expectedErr: ErrSessionInvalid},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/auth/callback", nil)
			if tc.cookieValue != "" {
				request.AddCookie(&http.Cookie{Name: manager.authCookieName, Value: tc.cookieValue})
			}
			response := httptest.NewRecorder()
			_, err := manager.VerifyAuthRequest(response, request, tc.state)
			if !errors.Is(err, tc.expectedErr) {
				t.Fatalf("expected %v, got %v", tc.expectedErr, err)
			}
			clearingCookies := response.Result().Cookies()
			if len(clearingCookies) != 1 || clearingCookies[0].Name != manager.authCookieName || clearingCookies[0].MaxAge != -1 {
				t.Fatalf("expected auth cookie cleanup, got %#v", clearingCookies)
			}
		})
	}
}

func newCookieSessionManagerForTest(t *testing.T, now time.Time) *CookieSessionManager {
	t.Helper()
	manager, err := NewCookieSessionManager(CookieSessionConfig{
		Secret: "test-session-secret",
		Now: func() time.Time {
			return now
		},
	})
	if err != nil {
		t.Fatalf("create session manager failed: %v", err)
	}
	return manager
}

func mustEncodeAuthRequest(t *testing.T, manager *CookieSessionManager, payload authRequestPayload) string {
	t.Helper()
	value, err := manager.encode(payload)
	if err != nil {
		t.Fatalf("encode auth request failed: %v", err)
	}
	return value
}
