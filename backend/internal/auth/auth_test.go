package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
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

func TestMiddleware_OIDCModeRequiresSession(t *testing.T) {
	userService := service.NewUserService(memory.NewUserRepository())
	sessions, err := NewCookieSessionManager(CookieSessionConfig{Secret: "test-session-secret"})
	if err != nil {
		t.Fatalf("create session manager failed: %v", err)
	}
	middleware := Middleware(MiddlewareConfig{Mode: ModeOIDC, Sessions: sessions}, userService)
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	res := httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/", nil))
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, res.Code)
	}
}

func TestMiddleware_OIDCModeLoadsSessionUser(t *testing.T) {
	userRepo := memory.NewUserRepository()
	userService := service.NewUserService(userRepo)
	created, err := userService.CreateUser(context.Background(), service.CreateUserInput{Email: "dev@example.com"})
	if err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	sessions, err := NewCookieSessionManager(CookieSessionConfig{Secret: "test-session-secret"})
	if err != nil {
		t.Fatalf("create session manager failed: %v", err)
	}
	loginRes := httptest.NewRecorder()
	if err := sessions.CreateSession(loginRes, created.ID); err != nil {
		t.Fatalf("create session failed: %v", err)
	}

	var got domain.User
	middleware := Middleware(MiddlewareConfig{Mode: ModeOIDC, Sessions: sessions}, userService)
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ok bool
		got, ok = CurrentUser(r.Context())
		if !ok {
			t.Fatalf("expected current user in context")
		}
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, cookie := range loginRes.Result().Cookies() {
		req.AddCookie(cookie)
	}
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, res.Code)
	}
	if got.ID != created.ID {
		t.Fatalf("expected session user %q, got %q", created.ID, got.ID)
	}
}

func TestCanViewProjectMembers(t *testing.T) {
	lookup := stubProjectRoleLookup{
		membership: domain.ProjectMembership{
			ProjectID: "project-1",
			UserID:    "user-1",
			Role:      domain.ProjectMemberRoleViewer,
		},
	}
	viewer := domain.User{ID: "user-1", GlobalRole: domain.GlobalRoleUser}
	admin := domain.User{ID: "admin-1", GlobalRole: domain.GlobalRoleAdmin}

	allowed, err := CanViewProjectMembers(context.Background(), lookup, ModeDisabled, domain.User{}, "project-1")
	if err != nil || !allowed {
		t.Fatalf("expected disabled mode to allow access, allowed=%v err=%v", allowed, err)
	}

	allowed, err = CanViewProjectMembers(context.Background(), lookup, ModeHeader, admin, "project-1")
	if err != nil || !allowed {
		t.Fatalf("expected admin to allow access, allowed=%v err=%v", allowed, err)
	}

	allowed, err = CanViewProjectMembers(context.Background(), lookup, ModeHeader, viewer, "project-1")
	if err != nil || !allowed {
		t.Fatalf("expected viewer membership to allow access, allowed=%v err=%v", allowed, err)
	}

	allowed, err = CanViewProjectMembers(context.Background(), stubProjectRoleLookup{err: repository.ErrProjectMembershipNotFound}, ModeHeader, viewer, "project-1")
	if err != nil || allowed {
		t.Fatalf("expected missing membership to deny without error, allowed=%v err=%v", allowed, err)
	}

	expectedErr := errors.New("lookup failed")
	allowed, err = CanViewProjectMembers(context.Background(), stubProjectRoleLookup{err: expectedErr}, ModeHeader, viewer, "project-1")
	if !errors.Is(err, expectedErr) || allowed {
		t.Fatalf("expected lookup error to bubble, allowed=%v err=%v", allowed, err)
	}
}

func TestProjectRoleCapabilities(t *testing.T) {
	maintainerLookup := stubProjectRoleLookup{membership: domain.ProjectMembership{ProjectID: "project-1", UserID: "user-1", Role: domain.ProjectMemberRoleMaintainer}}
	viewerLookup := stubProjectRoleLookup{membership: domain.ProjectMembership{ProjectID: "project-1", UserID: "user-1", Role: domain.ProjectMemberRoleViewer}}
	user := domain.User{ID: "user-1", GlobalRole: domain.GlobalRoleUser}

	allowed, err := CanManageProjectJobs(context.Background(), maintainerLookup, ModeHeader, user, "project-1")
	if err != nil || !allowed {
		t.Fatalf("expected maintainer to manage jobs, allowed=%v err=%v", allowed, err)
	}

	allowed, err = CanManageProjectJobs(context.Background(), viewerLookup, ModeHeader, user, "project-1")
	if err != nil || allowed {
		t.Fatalf("expected viewer to be denied job management, allowed=%v err=%v", allowed, err)
	}

	allowed, err = CanDownloadArtifact(context.Background(), viewerLookup, ModeHeader, user, "project-1")
	if err != nil || !allowed {
		t.Fatalf("expected viewer to download artifacts, allowed=%v err=%v", allowed, err)
	}
}

type stubProjectRoleLookup struct {
	membership domain.ProjectMembership
	err        error
}

func (s stubProjectRoleLookup) GetProjectMembership(_ context.Context, _ string, _ string) (domain.ProjectMembership, error) {
	if s.err != nil {
		return domain.ProjectMembership{}, s.err
	}
	return s.membership, nil
}
