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
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer coyote_pat_ignored_in_disabled_mode")
	handler.ServeHTTP(res, req)
	if !called || res.Code != http.StatusNoContent {
		t.Fatalf("expected request to pass through, called=%v status=%d", called, res.Code)
	}
}

func TestMiddleware_BearerTokenAuthenticatesAsOwner(t *testing.T) {
	ctx := context.Background()
	userRepo := memory.NewUserRepository()
	tokenRepo := memory.NewAPITokenRepository()
	userService := service.NewUserService(userRepo)
	user, err := userService.CreateUser(ctx, service.CreateUserInput{Email: "dev@example.com"})
	if err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	tokenService := service.NewAPITokenService(tokenRepo, userRepo)
	created, err := tokenService.CreateAPIToken(ctx, service.CreateAPITokenInput{UserID: user.ID, Name: "cli"})
	if err != nil {
		t.Fatalf("create token failed: %v", err)
	}

	var got domain.User
	var method Method
	middleware := Middleware(MiddlewareConfig{Mode: ModeOIDC, APITokens: tokenService}, userService)
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ok bool
		got, ok = CurrentUser(r.Context())
		if !ok {
			t.Fatalf("expected current user in context")
		}
		method, ok = CurrentAuthMethod(r.Context())
		if !ok {
			t.Fatalf("expected auth method in context")
		}
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+created.PlaintextToken)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusOK, res.Code, res.Body.String())
	}
	if got.ID != user.ID {
		t.Fatalf("expected token owner %q, got %q", user.ID, got.ID)
	}
	if method != MethodAPIToken {
		t.Fatalf("expected api_token auth method, got %q", method)
	}
}

func TestMiddleware_InvalidBearerTokenReturnsUnauthorized(t *testing.T) {
	userService := service.NewUserService(memory.NewUserRepository())
	tokenService := service.NewAPITokenService(memory.NewAPITokenRepository(), memory.NewUserRepository())
	middleware := Middleware(MiddlewareConfig{Mode: ModeHeader, APITokens: tokenService}, userService)
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for _, authorization := range []string{"Bearer coyote_pat_missing", "Bearer"} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", authorization)
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != http.StatusUnauthorized {
			t.Fatalf("expected status %d for %q, got %d", http.StatusUnauthorized, authorization, res.Code)
		}
	}
}

func TestMiddleware_BearerTokenWithoutAuthenticatorReturnsUnauthorized(t *testing.T) {
	userService := service.NewUserService(memory.NewUserRepository())
	middleware := Middleware(MiddlewareConfig{Mode: ModeHeader}, userService)
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer coyote_pat_missing")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, res.Code)
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

func TestParseModeAndBootstrapAdminEmails(t *testing.T) {
	if ParseMode(" OIDC ") != ModeOIDC {
		t.Fatalf("expected oidc mode")
	}
	if ParseMode("header") != ModeHeader {
		t.Fatalf("expected header mode")
	}
	if ParseMode("unknown") != ModeDisabled {
		t.Fatalf("expected unknown mode to disable auth")
	}

	emails := ParseBootstrapAdminEmails(" admin@example.com, ,OWNER@example.com ")
	if _, ok := emails["admin@example.com"]; !ok {
		t.Fatalf("expected normalized admin email in bootstrap set")
	}
	if _, ok := emails["owner@example.com"]; !ok {
		t.Fatalf("expected normalized owner email in bootstrap set")
	}
}

func TestCurrentUserHelpersAndPermissionChecks(t *testing.T) {
	admin := domain.User{ID: "admin-1", GlobalRole: domain.GlobalRoleAdmin}
	viewer := domain.User{ID: "user-1", GlobalRole: domain.GlobalRoleUser}
	lookup := stubProjectRoleLookup{membership: domain.ProjectMembership{ProjectID: "project-1", UserID: "user-1", Role: domain.ProjectMemberRoleViewer}}
	maintainerLookup := stubProjectRoleLookup{membership: domain.ProjectMembership{ProjectID: "project-1", UserID: "user-1", Role: domain.ProjectMemberRoleMaintainer}}
	ownerLookup := stubProjectRoleLookup{membership: domain.ProjectMembership{ProjectID: "project-1", UserID: "user-1", Role: domain.ProjectMemberRoleOwner}}

	ctx := WithUser(context.Background(), viewer)
	current, ok := CurrentUser(ctx)
	if !ok || current.ID != viewer.ID {
		t.Fatalf("expected current user %q, got %+v ok=%v", viewer.ID, current, ok)
	}
	disabled := DisabledModeUser()
	if disabled.GlobalRole != domain.GlobalRoleAdmin {
		t.Fatalf("expected disabled mode user to be admin, got %q", disabled.GlobalRole)
	}
	if !IsGlobalAdmin(admin) || IsGlobalAdmin(viewer) {
		t.Fatalf("unexpected global admin detection")
	}
	if !CanManageUsers(ModeDisabled, viewer) || CanManageUsers(ModeHeader, viewer) {
		t.Fatalf("unexpected user management permissions")
	}
	if !CanManageCredentials(ModeHeader, admin) || CanManageCredentials(ModeHeader, viewer) {
		t.Fatalf("unexpected credential permissions")
	}

	allowed, err := CanReadProject(context.Background(), lookup, ModeHeader, viewer, "project-1")
	if err != nil || !allowed {
		t.Fatalf("expected viewer read access, allowed=%v err=%v", allowed, err)
	}
	allowed, err = CanUpdateProject(context.Background(), lookup, ModeHeader, viewer, "project-1")
	if err != nil || allowed {
		t.Fatalf("expected viewer update denied, allowed=%v err=%v", allowed, err)
	}
	allowed, err = CanTriggerBuild(context.Background(), maintainerLookup, ModeHeader, viewer, "project-1")
	if err != nil || !allowed {
		t.Fatalf("expected maintainer trigger access, allowed=%v err=%v", allowed, err)
	}
	allowed, err = CanCancelBuild(context.Background(), maintainerLookup, ModeHeader, viewer, "project-1")
	if err != nil || !allowed {
		t.Fatalf("expected maintainer cancel access, allowed=%v err=%v", allowed, err)
	}
	allowed, err = CanReadProjectResources(context.Background(), lookup, ModeHeader, viewer, "project-1")
	if err != nil || !allowed {
		t.Fatalf("expected viewer resource access, allowed=%v err=%v", allowed, err)
	}
	allowed, err = CanManageProjectMembers(context.Background(), ownerLookup, ModeHeader, viewer, "project-1")
	if err != nil || !allowed {
		t.Fatalf("expected owner membership management access, allowed=%v err=%v", allowed, err)
	}
	allowed, err = CanManageProjectMembers(context.Background(), ownerLookup, ModeHeader, admin, "project-1")
	if err != nil || !allowed {
		t.Fatalf("expected global admin membership management access, allowed=%v err=%v", allowed, err)
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
