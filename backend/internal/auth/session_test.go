package auth

import (
	"net/http"
	"testing"
)

func TestNewCookieSessionManager_RejectsInsecureSameSiteNone(t *testing.T) {
	_, err := NewCookieSessionManager(CookieSessionConfig{
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
}
