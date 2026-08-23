package handler

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/auth"
	"github.com/radiation/coyote-ci/backend/internal/service"
)

type AuthHandler struct {
	authenticator      auth.OIDCAuthenticator
	sessions           auth.SessionManager
	users              *service.UserService
	postLoginRedirect  string
	postLogoutRedirect string
}

type AuthHandlerConfig struct {
	PostLoginRedirectURL  string
	PostLogoutRedirectURL string
}

func NewAuthHandler(authenticator auth.OIDCAuthenticator, sessions auth.SessionManager, users *service.UserService, cfg AuthHandlerConfig) *AuthHandler {
	postLoginRedirect := strings.TrimSpace(cfg.PostLoginRedirectURL)
	if postLoginRedirect == "" {
		postLoginRedirect = "/"
	}
	postLogoutRedirect := strings.TrimSpace(cfg.PostLogoutRedirectURL)
	if postLogoutRedirect == "" {
		postLogoutRedirect = "/"
	}
	return &AuthHandler{
		authenticator:      authenticator,
		sessions:           sessions,
		users:              users,
		postLoginRedirect:  postLoginRedirect,
		postLogoutRedirect: postLogoutRedirect,
	}
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	if h.authenticator == nil || h.sessions == nil {
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "oidc auth is not configured")
		return
	}
	state, stateErr := randomURLToken()
	if stateErr != nil {
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "unable to create login state")
		return
	}
	nonce, nonceErr := randomURLToken()
	if nonceErr != nil {
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "unable to create login nonce")
		return
	}
	if sessionErr := h.sessions.CreateAuthRequest(w, state, nonce); sessionErr != nil {
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "unable to start login")
		return
	}
	http.Redirect(w, r, h.authenticator.AuthCodeURL(state, nonce), http.StatusFound)
}

func (h *AuthHandler) Callback(w http.ResponseWriter, r *http.Request) {
	if h.authenticator == nil || h.sessions == nil || h.users == nil {
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "oidc auth is not configured")
		return
	}
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if state == "" || code == "" {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "state and code are required")
		return
	}
	nonce, nonceErr := h.sessions.VerifyAuthRequest(w, r, state)
	if nonceErr != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid oidc state")
		return
	}
	identity, exchangeErr := h.authenticator.Exchange(r.Context(), code, nonce)
	if exchangeErr != nil {
		status := http.StatusUnauthorized
		message := "oidc login failed"
		if errors.Is(exchangeErr, auth.ErrOIDCEmailRequired) || errors.Is(exchangeErr, service.ErrUserEmailRequired) {
			message = "email claim is required"
		} else if errors.Is(exchangeErr, auth.ErrOIDCEmailNotVerified) {
			message = "email must be verified by the oidc provider"
		}
		writeErrorJSON(w, status, "unauthorized", message)
		return
	}
	user, resolveErr := h.users.ResolveOIDCUser(r.Context(), identity.Email, identity.DisplayName)
	if resolveErr != nil {
		status := http.StatusInternalServerError
		code := "internal_error"
		message := "internal server error"
		if errors.Is(resolveErr, service.ErrUserEmailRequired) {
			status = http.StatusUnauthorized
			code = "unauthorized"
			message = "email claim is required"
		} else if errors.Is(resolveErr, service.ErrUserNotPreauthorized) {
			status = http.StatusForbidden
			code = "invite_only"
			message = "sign-in is invite-only"
		}
		writeErrorJSON(w, status, code, message)
		return
	}
	if sessionErr := h.sessions.CreateSession(w, user.ID); sessionErr != nil {
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "unable to create session")
		return
	}
	http.Redirect(w, r, h.postLoginRedirect, http.StatusFound)
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	if h.sessions != nil {
		h.sessions.ClearSession(w)
	}
	if wantsHTMLRedirect(r) {
		http.Redirect(w, r, h.postLogoutRedirect, http.StatusFound)
		return
	}
	writeDataJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func randomURLToken() (string, error) {
	buf := make([]byte, 32)
	if _, readErr := rand.Read(buf); readErr != nil {
		return "", readErr
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func wantsHTMLRedirect(r *http.Request) bool {
	accept := strings.ToLower(r.Header.Get("Accept"))
	return strings.Contains(accept, "text/html") && !strings.Contains(accept, "application/json")
}
