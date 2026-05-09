package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/repository/memory"
)

func TestUserService_CreateListGetUpdateDelete(t *testing.T) {
	userRepo := memory.NewUserRepository()
	service := NewUserService(userRepo)

	created, err := service.CreateUser(context.Background(), CreateUserInput{
		Email:       "ADMIN@Example.COM ",
		DisplayName: strPtr("Admin User"),
		GlobalRole:  "admin",
	})
	if err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	if created.Email != "admin@example.com" {
		t.Fatalf("expected normalized email, got %q", created.Email)
	}
	if created.GlobalRole != domain.GlobalRoleAdmin {
		t.Fatalf("expected admin role, got %q", created.GlobalRole)
	}

	listed, err := service.ListUsers(context.Background())
	if err != nil {
		t.Fatalf("list users failed: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("expected 1 user, got %d", len(listed))
	}

	got, err := service.GetUser(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("get user failed: %v", err)
	}
	if got.Email != created.Email {
		t.Fatalf("expected email %q, got %q", created.Email, got.Email)
	}

	updated, err := service.UpdateUser(context.Background(), created.ID, UpdateUserInput{
		Email:       strPtr("owner@example.com"),
		DisplayName: OptionalStringPatch{Set: true, Value: strPtr("Owner")},
		GlobalRole:  strPtr("user"),
	})
	if err != nil {
		t.Fatalf("update user failed: %v", err)
	}
	if updated.Email != "owner@example.com" || updated.GlobalRole != domain.GlobalRoleUser {
		t.Fatalf("unexpected updated user: %+v", updated)
	}

	err = service.DeleteUser(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("delete user failed: %v", err)
	}
	_, err = service.GetUser(context.Background(), created.ID)
	if !errors.Is(err, repository.ErrUserNotFound) {
		t.Fatalf("expected ErrUserNotFound after delete, got %v", err)
	}
}

func TestUserService_DuplicateEmailConflict(t *testing.T) {
	service := NewUserService(memory.NewUserRepository())

	_, err := service.CreateUser(context.Background(), CreateUserInput{Email: "dev@example.com"})
	if err != nil {
		t.Fatalf("first create failed: %v", err)
	}
	_, err = service.CreateUser(context.Background(), CreateUserInput{Email: "DEV@example.com"})
	if !errors.Is(err, repository.ErrUserEmailConflict) {
		t.Fatalf("expected ErrUserEmailConflict, got %v", err)
	}
}

func TestUserService_ResolveOIDCUserProvisioningAndBootstrap(t *testing.T) {
	service := NewUserService(memory.NewUserRepository())
	displayName := "Admin User"

	user, err := service.ResolveOIDCUser(context.Background(), "ADMIN@example.com", &displayName, map[string]struct{}{"admin@example.com": {}})
	if err != nil {
		t.Fatalf("resolve oidc user failed: %v", err)
	}
	if user.Email != "admin@example.com" {
		t.Fatalf("expected normalized email, got %q", user.Email)
	}
	if user.DisplayName == nil || *user.DisplayName != displayName {
		t.Fatalf("expected display name %q, got %v", displayName, user.DisplayName)
	}
	if user.GlobalRole != domain.GlobalRoleAdmin {
		t.Fatalf("expected bootstrap admin, got %q", user.GlobalRole)
	}
}

func TestUserService_ResolveOIDCUserUpdatesDisplayNameWithoutDemoting(t *testing.T) {
	service := NewUserService(memory.NewUserRepository())
	created, err := service.CreateUser(context.Background(), CreateUserInput{Email: "admin@example.com", GlobalRole: "admin"})
	if err != nil {
		t.Fatalf("create admin failed: %v", err)
	}
	displayName := "Updated Admin"

	resolved, err := service.ResolveOIDCUser(context.Background(), "admin@example.com", &displayName, nil)
	if err != nil {
		t.Fatalf("resolve oidc user failed: %v", err)
	}
	if resolved.ID != created.ID {
		t.Fatalf("expected existing user %q, got %q", created.ID, resolved.ID)
	}
	if resolved.GlobalRole != domain.GlobalRoleAdmin {
		t.Fatalf("expected existing admin not to be demoted, got %q", resolved.GlobalRole)
	}
	if resolved.DisplayName == nil || *resolved.DisplayName != displayName {
		t.Fatalf("expected updated display name, got %v", resolved.DisplayName)
	}
}

func TestUserService_ResolveOIDCUserRequiresEmail(t *testing.T) {
	service := NewUserService(memory.NewUserRepository())

	_, err := service.ResolveOIDCUser(context.Background(), " ", nil, nil)
	if !errors.Is(err, ErrUserEmailRequired) {
		t.Fatalf("expected ErrUserEmailRequired, got %v", err)
	}
}

func TestUserService_Validation(t *testing.T) {
	service := NewUserService(memory.NewUserRepository())

	_, err := service.CreateUser(context.Background(), CreateUserInput{Email: "   "})
	if !errors.Is(err, ErrUserEmailRequired) {
		t.Fatalf("expected ErrUserEmailRequired, got %v", err)
	}
	_, err = service.CreateUser(context.Background(), CreateUserInput{Email: "dev@example.com", GlobalRole: "superuser"})
	if !errors.Is(err, ErrUserGlobalRoleInvalid) {
		t.Fatalf("expected ErrUserGlobalRoleInvalid, got %v", err)
	}
}

func TestProjectMembershipService_AddUpdateListDelete(t *testing.T) {
	ctx := context.Background()
	jobRepo := memory.NewJobRepository()
	projectRepo := memory.NewProjectRepository(jobRepo)
	userRepo := memory.NewUserRepository()
	membershipRepo := memory.NewProjectMembershipRepository(projectRepo, userRepo)
	projectService := NewProjectService(projectRepo)
	userService := NewUserService(userRepo)
	membershipService := NewProjectMembershipService(projectRepo, membershipRepo)

	project, err := projectService.CreateProject(ctx, CreateProjectInput{Name: "Platform", Slug: "platform"})
	if err != nil {
		t.Fatalf("create project failed: %v", err)
	}
	user, err := userService.CreateUser(ctx, CreateUserInput{Email: "maintainer@example.com"})
	if err != nil {
		t.Fatalf("create user failed: %v", err)
	}

	membership, err := membershipService.UpsertProjectMembership(ctx, UpsertProjectMembershipInput{ProjectID: project.ID, UserID: user.ID, Role: "viewer"})
	if err != nil {
		t.Fatalf("add membership failed: %v", err)
	}
	if membership.Role != domain.ProjectMemberRoleViewer {
		t.Fatalf("expected viewer role, got %q", membership.Role)
	}

	updated, err := membershipService.UpsertProjectMembership(ctx, UpsertProjectMembershipInput{ProjectID: project.ID, UserID: user.ID, Role: "maintainer"})
	if err != nil {
		t.Fatalf("update membership failed: %v", err)
	}
	if updated.Role != domain.ProjectMemberRoleMaintainer {
		t.Fatalf("expected maintainer role, got %q", updated.Role)
	}

	members, err := membershipService.ListProjectMembers(ctx, project.ID)
	if err != nil {
		t.Fatalf("list members failed: %v", err)
	}
	if len(members) != 1 || members[0].Email != "maintainer@example.com" {
		t.Fatalf("unexpected members: %+v", members)
	}

	err = membershipService.DeleteProjectMembership(ctx, project.ID, user.ID)
	if err != nil {
		t.Fatalf("delete membership failed: %v", err)
	}
	_, err = membershipService.GetProjectMembership(ctx, project.ID, user.ID)
	if !errors.Is(err, repository.ErrProjectMembershipNotFound) {
		t.Fatalf("expected ErrProjectMembershipNotFound, got %v", err)
	}
}

func TestProjectMembershipService_ValidationAndMissingReferences(t *testing.T) {
	ctx := context.Background()
	projectRepo := memory.NewProjectRepository(memory.NewJobRepository())
	userRepo := memory.NewUserRepository()
	membershipService := NewProjectMembershipService(projectRepo, memory.NewProjectMembershipRepository(projectRepo, userRepo))

	_, err := membershipService.UpsertProjectMembership(ctx, UpsertProjectMembershipInput{ProjectID: "missing", UserID: "user", Role: "admin"})
	if !errors.Is(err, ErrProjectMembershipRoleInvalid) {
		t.Fatalf("expected ErrProjectMembershipRoleInvalid, got %v", err)
	}
	_, err = membershipService.UpsertProjectMembership(ctx, UpsertProjectMembershipInput{ProjectID: "missing", UserID: "user", Role: "viewer"})
	if !errors.Is(err, repository.ErrProjectNotFound) {
		t.Fatalf("expected ErrProjectNotFound, got %v", err)
	}

	now := time.Now().UTC()
	project, err := projectRepo.Create(ctx, domain.Project{ID: "project-1", Name: "Platform", Slug: "platform", CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatalf("create project failed: %v", err)
	}
	_, err = membershipService.UpsertProjectMembership(ctx, UpsertProjectMembershipInput{ProjectID: project.ID, UserID: "missing-user", Role: "viewer"})
	if !errors.Is(err, repository.ErrUserNotFound) {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}
}
