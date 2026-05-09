package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

var ErrSessionNotFound = errors.New("session not found")
var ErrSessionInvalid = errors.New("session invalid")

const (
	defaultSessionCookieName = "coyote_session"
	defaultSessionTTL        = 24 * time.Hour
	defaultAuthRequestTTL    = 10 * time.Minute
)

type SessionManager interface {
	CreateSession(w http.ResponseWriter, userID string) error
	ClearSession(w http.ResponseWriter)
	UserID(r *http.Request) (string, error)
	CreateAuthRequest(w http.ResponseWriter, state string, nonce string) error
	VerifyAuthRequest(w http.ResponseWriter, r *http.Request, receivedState string) (string, error)
}

type CookieSessionConfig struct {
	Secret     string
	CookieName string
	Secure     bool
	SameSite   http.SameSite
	Now        func() time.Time
}

type CookieSessionManager struct {
	secret         []byte
	cookieName     string
	authCookieName string
	secure         bool
	sameSite       http.SameSite
	now            func() time.Time
	sessionTTL     time.Duration
	authRequestTTL time.Duration
}

type sessionPayload struct {
	UserID    string `json:"user_id"`
	ExpiresAt int64  `json:"expires_at"`
}

type authRequestPayload struct {
	State     string `json:"state"`
	Nonce     string `json:"nonce"`
	ExpiresAt int64  `json:"expires_at"`
}

func NewCookieSessionManager(cfg CookieSessionConfig) (*CookieSessionManager, error) {
	secret := strings.TrimSpace(cfg.Secret)
	if secret == "" {
		return nil, errors.New("session secret is required")
	}
	cookieName := strings.TrimSpace(cfg.CookieName)
	if cookieName == "" {
		cookieName = defaultSessionCookieName
	}
	sameSite := cfg.SameSite
	if sameSite == http.SameSiteDefaultMode {
		sameSite = http.SameSiteLaxMode
	}
	if sameSite == http.SameSiteNoneMode && !cfg.Secure {
		return nil, errors.New("SESSION_COOKIE_SECURE must be true when SESSION_COOKIE_SAME_SITE=none")
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}

	return &CookieSessionManager{
		secret:         []byte(secret),
		cookieName:     cookieName,
		authCookieName: cookieName + "_oidc",
		secure:         cfg.Secure,
		sameSite:       sameSite,
		now:            now,
		sessionTTL:     defaultSessionTTL,
		authRequestTTL: defaultAuthRequestTTL,
	}, nil
}

func ParseSameSite(value string) (http.SameSite, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "lax":
		return http.SameSiteLaxMode, nil
	case "strict":
		return http.SameSiteStrictMode, nil
	case "none":
		return http.SameSiteNoneMode, nil
	default:
		return http.SameSiteDefaultMode, errors.New("SESSION_COOKIE_SAME_SITE must be one of lax, strict, none")
	}
}

func (m *CookieSessionManager) CreateSession(w http.ResponseWriter, userID string) error {
	trimmedID := strings.TrimSpace(userID)
	if trimmedID == "" {
		return ErrSessionInvalid
	}
	value, encodeErr := m.encode(sessionPayload{
		UserID:    trimmedID,
		ExpiresAt: m.now().UTC().Add(m.sessionTTL).Unix(),
	})
	if encodeErr != nil {
		return encodeErr
	}
	http.SetCookie(w, m.cookie(value, m.sessionTTL, "/"))
	return nil
}

func (m *CookieSessionManager) ClearSession(w http.ResponseWriter) {
	http.SetCookie(w, m.expiredCookie(m.cookieName, "/"))
}

func (m *CookieSessionManager) UserID(r *http.Request) (string, error) {
	cookie, cookieErr := r.Cookie(m.cookieName)
	if cookieErr != nil {
		return "", ErrSessionNotFound
	}
	var payload sessionPayload
	if decodeErr := m.decode(cookie.Value, &payload); decodeErr != nil {
		return "", decodeErr
	}
	if strings.TrimSpace(payload.UserID) == "" || payload.ExpiresAt <= m.now().UTC().Unix() {
		return "", ErrSessionInvalid
	}
	return payload.UserID, nil
}

func (m *CookieSessionManager) CreateAuthRequest(w http.ResponseWriter, state string, nonce string) error {
	trimmedState := strings.TrimSpace(state)
	trimmedNonce := strings.TrimSpace(nonce)
	if trimmedState == "" || trimmedNonce == "" {
		return ErrSessionInvalid
	}
	value, encodeErr := m.encode(authRequestPayload{
		State:     trimmedState,
		Nonce:     trimmedNonce,
		ExpiresAt: m.now().UTC().Add(m.authRequestTTL).Unix(),
	})
	if encodeErr != nil {
		return encodeErr
	}
	http.SetCookie(w, m.cookieWithName(m.authCookieName, value, m.authRequestTTL, "/auth"))
	return nil
}

func (m *CookieSessionManager) VerifyAuthRequest(w http.ResponseWriter, r *http.Request, receivedState string) (string, error) {
	defer http.SetCookie(w, m.expiredCookie(m.authCookieName, "/auth"))
	cookie, cookieErr := r.Cookie(m.authCookieName)
	if cookieErr != nil {
		return "", ErrSessionNotFound
	}
	var payload authRequestPayload
	if decodeErr := m.decode(cookie.Value, &payload); decodeErr != nil {
		return "", decodeErr
	}
	if payload.ExpiresAt <= m.now().UTC().Unix() {
		return "", ErrSessionInvalid
	}
	if !constantTimeStringEqual(payload.State, strings.TrimSpace(receivedState)) {
		return "", ErrSessionInvalid
	}
	if strings.TrimSpace(payload.Nonce) == "" {
		return "", ErrSessionInvalid
	}
	return payload.Nonce, nil
}

func (m *CookieSessionManager) cookie(value string, maxAge time.Duration, path string) *http.Cookie {
	return m.cookieWithName(m.cookieName, value, maxAge, path)
}

func (m *CookieSessionManager) cookieWithName(name string, value string, maxAge time.Duration, path string) *http.Cookie {
	return &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     path,
		MaxAge:   int(maxAge.Seconds()),
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: m.sameSite,
	}
}

func (m *CookieSessionManager) expiredCookie(name string, path string) *http.Cookie {
	return &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     path,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: m.sameSite,
	}
}

func (m *CookieSessionManager) encode(payload any) (string, error) {
	raw, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		return "", marshalErr
	}
	payloadPart := base64.RawURLEncoding.EncodeToString(raw)
	signaturePart := m.sign(payloadPart)
	return payloadPart + "." + signaturePart, nil
}

func (m *CookieSessionManager) decode(value string, target any) error {
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return ErrSessionInvalid
	}
	expectedSignature := m.sign(parts[0])
	if !constantTimeStringEqual(expectedSignature, parts[1]) {
		return ErrSessionInvalid
	}
	raw, decodeErr := base64.RawURLEncoding.DecodeString(parts[0])
	if decodeErr != nil {
		return ErrSessionInvalid
	}
	if unmarshalErr := json.Unmarshal(raw, target); unmarshalErr != nil {
		return ErrSessionInvalid
	}
	return nil
}

func (m *CookieSessionManager) sign(payloadPart string) string {
	mac := hmac.New(sha256.New, m.secret)
	_, _ = mac.Write([]byte(payloadPart))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func constantTimeStringEqual(left string, right string) bool {
	leftBytes := []byte(left)
	rightBytes := []byte(right)
	if len(leftBytes) != len(rightBytes) {
		return false
	}
	return hmac.Equal(leftBytes, rightBytes)
}
