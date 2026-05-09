package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/auth"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository/memory"
	"github.com/radiation/coyote-ci/backend/internal/service"
)

type fakeOIDCAuthenticator struct {
	email       string
	displayName *string
	err         error
	lastState   string
	lastNonce   string
}

func (f *fakeOIDCAuthenticator) AuthCodeURL(state string, nonce string) string {
	f.lastState = state
	f.lastNonce = nonce
	return "/provider/auth?state=" + state
}

func (f *fakeOIDCAuthenticator) Exchange(_ context.Context, _ string, nonce string) (auth.OIDCIdentity, error) {
	if f.err != nil {
		return auth.OIDCIdentity{}, f.err
	}
	if nonce != f.lastNonce {
		return auth.OIDCIdentity{}, errors.New("nonce mismatch")
	}
	return auth.OIDCIdentity{Email: f.email, DisplayName: f.displayName}, nil
}

func TestAuthHandler_CallbackProvisionsUserAndSession(t *testing.T) {
	userService := service.NewUserService(memory.NewUserRepository())
	sessions := newTestSessionManager(t)
	displayName := "Admin User"
	fakeOIDC := &fakeOIDCAuthenticator{email: "ADMIN@example.com", displayName: &displayName}
	h := NewAuthHandler(fakeOIDC, sessions, userService, AuthHandlerConfig{
		BootstrapAdminEmails: map[string]struct{}{"admin@example.com": {}},
	})

	loginRes := httptest.NewRecorder()
	h.Login(loginRes, httptest.NewRequest(http.MethodGet, "/auth/login", nil))
	if loginRes.Code != http.StatusFound {
		t.Fatalf("expected login redirect, got %d", loginRes.Code)
	}

	callbackReq := httptest.NewRequest(http.MethodGet, "/auth/callback?state="+fakeOIDC.lastState+"&code=code", nil)
	for _, cookie := range loginRes.Result().Cookies() {
		callbackReq.AddCookie(cookie)
	}
	callbackRes := httptest.NewRecorder()
	h.Callback(callbackRes, callbackReq)
	if callbackRes.Code != http.StatusFound {
		t.Fatalf("expected callback redirect, got %d body=%s", callbackRes.Code, callbackRes.Body.String())
	}

	users, err := userService.ListUsers(context.Background())
	if err != nil {
		t.Fatalf("list users failed: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("expected one provisioned user, got %d", len(users))
	}
	if users[0].Email != "admin@example.com" || users[0].GlobalRole != domain.GlobalRoleAdmin {
		t.Fatalf("expected bootstrap admin, got %+v", users[0])
	}

	sessionReq := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, cookie := range callbackRes.Result().Cookies() {
		sessionReq.AddCookie(cookie)
	}
	userID, sessionErr := sessions.UserID(sessionReq)
	if sessionErr != nil {
		t.Fatalf("expected session cookie, got %v", sessionErr)
	}
	if userID != users[0].ID {
		t.Fatalf("expected session user %q, got %q", users[0].ID, userID)
	}
}

func TestAuthHandler_CallbackRejectsInvalidState(t *testing.T) {
	userService := service.NewUserService(memory.NewUserRepository())
	sessions := newTestSessionManager(t)
	fakeOIDC := &fakeOIDCAuthenticator{email: "user@example.com"}
	h := NewAuthHandler(fakeOIDC, sessions, userService, AuthHandlerConfig{})

	loginRes := httptest.NewRecorder()
	h.Login(loginRes, httptest.NewRequest(http.MethodGet, "/auth/login", nil))

	callbackReq := httptest.NewRequest(http.MethodGet, "/auth/callback?state=wrong&code=code", nil)
	for _, cookie := range loginRes.Result().Cookies() {
		callbackReq.AddCookie(cookie)
	}
	callbackRes := httptest.NewRecorder()
	h.Callback(callbackRes, callbackReq)
	if callbackRes.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid state status %d, got %d", http.StatusBadRequest, callbackRes.Code)
	}
}

func TestAuthHandler_CallbackRejectsMissingEmail(t *testing.T) {
	userService := service.NewUserService(memory.NewUserRepository())
	sessions := newTestSessionManager(t)
	fakeOIDC := &fakeOIDCAuthenticator{}
	h := NewAuthHandler(fakeOIDC, sessions, userService, AuthHandlerConfig{})

	loginRes := httptest.NewRecorder()
	h.Login(loginRes, httptest.NewRequest(http.MethodGet, "/auth/login", nil))

	callbackReq := httptest.NewRequest(http.MethodGet, "/auth/callback?state="+fakeOIDC.lastState+"&code=code", nil)
	for _, cookie := range loginRes.Result().Cookies() {
		callbackReq.AddCookie(cookie)
	}
	callbackRes := httptest.NewRecorder()
	h.Callback(callbackRes, callbackReq)
	if callbackRes.Code != http.StatusUnauthorized {
		t.Fatalf("expected missing email status %d, got %d", http.StatusUnauthorized, callbackRes.Code)
	}
}

func TestAuthHandler_LogoutClearsSession(t *testing.T) {
	sessions := newTestSessionManager(t)
	h := NewAuthHandler(&fakeOIDCAuthenticator{}, sessions, service.NewUserService(memory.NewUserRepository()), AuthHandlerConfig{})

	res := httptest.NewRecorder()
	h.Logout(res, httptest.NewRequest(http.MethodPost, "/auth/logout", nil))
	if res.Code != http.StatusOK {
		t.Fatalf("expected logout status %d, got %d", http.StatusOK, res.Code)
	}
	if len(res.Result().Cookies()) == 0 || res.Result().Cookies()[0].MaxAge != -1 {
		t.Fatalf("expected clearing session cookie, got %#v", res.Result().Cookies())
	}
}

func newTestSessionManager(t *testing.T) *auth.CookieSessionManager {
	t.Helper()
	sessions, err := auth.NewCookieSessionManager(auth.CookieSessionConfig{Secret: "test-session-secret", Secure: false})
	if err != nil {
		t.Fatalf("create session manager failed: %v", err)
	}
	return sessions
}
