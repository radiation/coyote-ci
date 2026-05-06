package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository/memory"
	"github.com/radiation/coyote-ci/backend/internal/service"
)

func TestMiddleware_HeaderModeMissingEmailReturnsUnauthorized(t *testing.T) {
	userService := service.NewUserService(memory.NewUserRepository())
	middleware := Middleware(MiddlewareConfig{Mode: ModeHeader}, userService)
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	res := httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/", nil))
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, res.Code)
	}
}

func TestMiddleware_HeaderModeLoadsExistingUser(t *testing.T) {
	userRepo := memory.NewUserRepository()
	userService := service.NewUserService(userRepo)
	created, err := userService.CreateUser(context.Background(), service.CreateUserInput{Email: "dev@example.com"})
	if err != nil {
		t.Fatalf("create user failed: %v", err)
	}

	var got domain.User
	middleware := Middleware(MiddlewareConfig{Mode: ModeHeader}, userService)
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ok bool
		got, ok = CurrentUser(r.Context())
		if !ok {
			t.Fatalf("expected current user in context")
		}
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Coyote-User-Email", "DEV@example.com")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, res.Code)
	}
	if got.ID != created.ID {
		t.Fatalf("expected existing user %q, got %q", created.ID, got.ID)
	}
}

func TestMiddleware_HeaderModeAutoProvisionsUserAndBootstrapAdmin(t *testing.T) {
	userService := service.NewUserService(memory.NewUserRepository())
	middleware := Middleware(MiddlewareConfig{
		Mode:                 ModeHeader,
		BootstrapAdminEmails: ParseBootstrapAdminEmails("admin@example.com"),
	}, userService)

	var got domain.User
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = CurrentUser(r.Context())
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Coyote-User-Email", "ADMIN@example.com")
	req.Header.Set("X-Coyote-User-Name", "Admin")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, res.Code)
	}
	if got.Email != "admin@example.com" || got.GlobalRole != domain.GlobalRoleAdmin {
		t.Fatalf("expected bootstrap admin user, got %+v", got)
	}
}

func TestMiddleware_DisabledModePreservesRequest(t *testing.T) {
	userService := service.NewUserService(memory.NewUserRepository())
	middleware := Middleware(MiddlewareConfig{Mode: ModeDisabled}, userService)
	called := false
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if _, ok := CurrentUser(r.Context()); ok {
			t.Fatalf("did not expect disabled mode to inject user")
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	res := httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/", nil))
	if !called || res.Code != http.StatusNoContent {
		t.Fatalf("expected request to pass through, called=%v status=%d", called, res.Code)
	}
}
