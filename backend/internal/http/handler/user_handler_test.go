package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/api"
	"github.com/radiation/coyote-ci/backend/internal/auth"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository/memory"
	"github.com/radiation/coyote-ci/backend/internal/service"
)

func TestUserHandler_GetMeUsesCurrentUser(t *testing.T) {
	handler := NewUserHandler(service.NewUserService(memory.NewUserRepository()), auth.ModeOIDC)
	user := domain.User{ID: "user-1", Email: "user@example.com", GlobalRole: domain.GlobalRoleUser}
	ctx := auth.WithUser(httptest.NewRequest(http.MethodGet, "/api/me", nil).Context(), user)
	ctx = auth.WithAuthMethod(ctx, auth.MethodOIDC)
	request := httptest.NewRequest(http.MethodGet, "/api/me", nil).WithContext(ctx)
	response := httptest.NewRecorder()

	handler.GetMe(response, request)

	var payload api.MeEnvelope
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	if payload.Data.AuthMode != string(auth.ModeOIDC) {
		t.Fatalf("expected auth mode oidc, got %q", payload.Data.AuthMode)
	}
	if payload.Data.User.Email != user.Email {
		t.Fatalf("expected current user email %q, got %q", user.Email, payload.Data.User.Email)
	}
	if payload.Data.AuthMethod != string(auth.MethodOIDC) {
		t.Fatalf("expected auth method oidc, got %q", payload.Data.AuthMethod)
	}
	if payload.Data.EmailVerified != nil {
		t.Fatalf("expected unknown email verification state, got %v", payload.Data.EmailVerified)
	}
}

func TestUserHandler_GetMeFallsBackToDisabledModeUser(t *testing.T) {
	handler := NewUserHandler(service.NewUserService(memory.NewUserRepository()), auth.ModeDisabled)
	response := httptest.NewRecorder()

	handler.GetMe(response, httptest.NewRequest(http.MethodGet, "/api/me", nil))

	var payload api.MeEnvelope
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	if payload.Data.User.ID != auth.DisabledModeUser().ID {
		t.Fatalf("expected disabled mode fallback user, got %+v", payload.Data.User)
	}
	if payload.Data.AuthMode != string(auth.ModeDisabled) {
		t.Fatalf("expected disabled auth mode, got %q", payload.Data.AuthMode)
	}
	if payload.Data.EmailVerified != nil {
		t.Fatalf("expected unknown email verification state, got %v", payload.Data.EmailVerified)
	}
}

func TestUserHandler_GetAuthConfig(t *testing.T) {
	tests := []struct {
		name             string
		mode             auth.Mode
		expectedLoginURL *string
	}{
		{name: "disabled mode", mode: auth.ModeDisabled},
		{name: "header mode", mode: auth.ModeHeader},
		{name: "oidc mode", mode: auth.ModeOIDC, expectedLoginURL: stringPtr("/auth/login")},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			handler := NewUserHandler(service.NewUserService(memory.NewUserRepository()), tc.mode)
			response := httptest.NewRecorder()

			handler.GetAuthConfig(response, httptest.NewRequest(http.MethodGet, "/api/auth/config", nil))

			var payload api.AuthConfigEnvelope
			if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
				t.Fatalf("decode response failed: %v", err)
			}
			if payload.Data.AuthMode != string(tc.mode) {
				t.Fatalf("expected auth mode %q, got %q", tc.mode, payload.Data.AuthMode)
			}
			if !equalOptionalString(payload.Data.LoginURL, tc.expectedLoginURL) {
				t.Fatalf("expected login url %v, got %v", tc.expectedLoginURL, payload.Data.LoginURL)
			}
		})
	}
}

func stringPtr(value string) *string {
	return &value
}

func equalOptionalString(left *string, right *string) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}
