package handler

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/radiation/coyote-ci/backend/internal/auth"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository/memory"
	"github.com/radiation/coyote-ci/backend/internal/service"
)

func TestUserHandler_HeaderModeAuthorization(t *testing.T) {
	userService := service.NewUserService(memory.NewUserRepository())
	handler := NewUserHandler(userService, auth.ModeHeader)

	nonAdmin := domain.User{ID: "user-1", Email: "user@example.com", GlobalRole: domain.GlobalRoleUser}
	admin := domain.User{ID: "admin-1", Email: "admin@example.com", GlobalRole: domain.GlobalRoleAdmin}

	forbiddenReq := httptest.NewRequest(http.MethodGet, "/users", nil).WithContext(auth.WithUser(context.Background(), nonAdmin))
	forbiddenRes := httptest.NewRecorder()
	handler.ListUsers(forbiddenRes, forbiddenReq)
	if forbiddenRes.Code != http.StatusForbidden {
		t.Fatalf("expected non-admin status %d, got %d", http.StatusForbidden, forbiddenRes.Code)
	}

	allowedReq := httptest.NewRequest(http.MethodGet, "/users", nil).WithContext(auth.WithUser(context.Background(), admin))
	allowedRes := httptest.NewRecorder()
	handler.ListUsers(allowedRes, allowedReq)
	if allowedRes.Code != http.StatusOK {
		t.Fatalf("expected admin status %d, got %d", http.StatusOK, allowedRes.Code)
	}
}

func TestProjectMembershipHandler_HeaderModeAuthorization(t *testing.T) {
	ctx := context.Background()
	projectRepo := memory.NewProjectRepository(memory.NewJobRepository())
	userRepo := memory.NewUserRepository()
	membershipRepo := memory.NewProjectMembershipRepository(projectRepo, userRepo)
	membershipService := service.NewProjectMembershipService(projectRepo, membershipRepo)
	handler := NewProjectMembershipHandler(membershipService, auth.ModeHeader)
	now := time.Now().UTC()

	project, err := projectRepo.Create(ctx, domain.Project{ID: "project-1", Name: "Platform", Slug: "platform", CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatalf("create project failed: %v", err)
	}
	owner, err := userRepo.Create(ctx, domain.User{ID: "owner-1", Email: "owner@example.com", GlobalRole: domain.GlobalRoleUser, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatalf("create owner failed: %v", err)
	}
	viewer, err := userRepo.Create(ctx, domain.User{ID: "viewer-1", Email: "viewer@example.com", GlobalRole: domain.GlobalRoleUser, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatalf("create viewer failed: %v", err)
	}
	target, err := userRepo.Create(ctx, domain.User{ID: "target-1", Email: "target@example.com", GlobalRole: domain.GlobalRoleUser, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatalf("create target failed: %v", err)
	}
	if _, err := membershipService.UpsertProjectMembership(ctx, service.UpsertProjectMembershipInput{ProjectID: project.ID, UserID: owner.ID, Role: "owner"}); err != nil {
		t.Fatalf("create owner membership failed: %v", err)
	}
	if _, err := membershipService.UpsertProjectMembership(ctx, service.UpsertProjectMembershipInput{ProjectID: project.ID, UserID: viewer.ID, Role: "viewer"}); err != nil {
		t.Fatalf("create viewer membership failed: %v", err)
	}

	viewerReq := addURLParams(httptest.NewRequest(http.MethodPut, "/projects/project-1/members/target-1", bytes.NewBufferString(`{"role":"viewer"}`)), map[string]string{"id": project.ID, "user_id": target.ID})
	viewerReq = viewerReq.WithContext(auth.WithUser(viewerReq.Context(), viewer))
	viewerRes := httptest.NewRecorder()
	handler.UpsertProjectMember(viewerRes, viewerReq)
	if viewerRes.Code != http.StatusForbidden {
		t.Fatalf("expected viewer status %d, got %d", http.StatusForbidden, viewerRes.Code)
	}

	viewerListReq := addURLParams(httptest.NewRequest(http.MethodGet, "/projects/project-1/members", nil), map[string]string{"id": project.ID})
	viewerListReq = viewerListReq.WithContext(auth.WithUser(viewerListReq.Context(), viewer))
	viewerListRes := httptest.NewRecorder()
	handler.ListProjectMembers(viewerListRes, viewerListReq)
	if viewerListRes.Code != http.StatusOK {
		t.Fatalf("expected viewer list status %d, got %d body=%s", http.StatusOK, viewerListRes.Code, viewerListRes.Body.String())
	}

	nonMember := domain.User{ID: "outsider-1", Email: "outsider@example.com", GlobalRole: domain.GlobalRoleUser}
	nonMemberListReq := addURLParams(httptest.NewRequest(http.MethodGet, "/projects/project-1/members", nil), map[string]string{"id": project.ID})
	nonMemberListReq = nonMemberListReq.WithContext(auth.WithUser(nonMemberListReq.Context(), nonMember))
	nonMemberListRes := httptest.NewRecorder()
	handler.ListProjectMembers(nonMemberListRes, nonMemberListReq)
	if nonMemberListRes.Code != http.StatusForbidden {
		t.Fatalf("expected non-member list status %d, got %d", http.StatusForbidden, nonMemberListRes.Code)
	}

	ownerReq := addURLParams(httptest.NewRequest(http.MethodPut, "/projects/project-1/members/target-1", bytes.NewBufferString(`{"role":"maintainer"}`)), map[string]string{"id": project.ID, "user_id": target.ID})
	ownerReq = ownerReq.WithContext(auth.WithUser(ownerReq.Context(), owner))
	ownerRes := httptest.NewRecorder()
	handler.UpsertProjectMember(ownerRes, ownerReq)
	if ownerRes.Code != http.StatusOK {
		t.Fatalf("expected owner status %d, got %d body=%s", http.StatusOK, ownerRes.Code, ownerRes.Body.String())
	}

	ownerListReq := addURLParams(httptest.NewRequest(http.MethodGet, "/projects/project-1/members", nil), map[string]string{"id": project.ID})
	ownerListReq = ownerListReq.WithContext(auth.WithUser(ownerListReq.Context(), owner))
	ownerListRes := httptest.NewRecorder()
	handler.ListProjectMembers(ownerListRes, ownerListReq)
	if ownerListRes.Code != http.StatusOK {
		t.Fatalf("expected owner list status %d, got %d body=%s", http.StatusOK, ownerListRes.Code, ownerListRes.Body.String())
	}
}

func addURLParams(req *http.Request, params map[string]string) *http.Request {
	rctx := chi.NewRouteContext()
	for key, value := range params {
		rctx.URLParams.Add(key, value)
	}
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	return req.WithContext(ctx)
}
