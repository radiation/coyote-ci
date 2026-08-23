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

type bootstrapUserRepository struct {
	repository.UserRepository
	getByEmail func(context.Context, string) (domain.User, error)
	create     func(context.Context, domain.User) (domain.User, error)
	update     func(context.Context, domain.User) (domain.User, error)
}

func (r bootstrapUserRepository) GetByEmail(ctx context.Context, email string) (domain.User, error) {
	if r.getByEmail != nil {
		return r.getByEmail(ctx, email)
	}
	return r.UserRepository.GetByEmail(ctx, email)
}

func (r bootstrapUserRepository) Create(ctx context.Context, user domain.User) (domain.User, error) {
	if r.create != nil {
		return r.create(ctx, user)
	}
	return r.UserRepository.Create(ctx, user)
}

func (r bootstrapUserRepository) Update(ctx context.Context, user domain.User) (domain.User, error) {
	if r.update != nil {
		return r.update(ctx, user)
	}
	return r.UserRepository.Update(ctx, user)
}

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

func TestUserService_ResolveOIDCUserRequiresPreauthorizedUser(t *testing.T) {
	userRepo := memory.NewUserRepository()
	service := NewUserService(userRepo)

	_, err := service.ResolveOIDCUser(context.Background(), "unknown@example.com", nil)
	if !errors.Is(err, ErrUserNotPreauthorized) {
		t.Fatalf("expected ErrUserNotPreauthorized, got %v", err)
	}
	users, listErr := service.ListUsers(context.Background())
	if listErr != nil {
		t.Fatalf("list users failed: %v", listErr)
	}
	if len(users) != 0 {
		t.Fatalf("expected no provisioned users, got %+v", users)
	}
}

func TestUserService_ResolveOIDCUserUpdatesDisplayNameWithoutDemoting(t *testing.T) {
	service := NewUserService(memory.NewUserRepository())
	created, err := service.CreateUser(context.Background(), CreateUserInput{Email: "user@example.com", GlobalRole: "user"})
	if err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	displayName := "Updated User"

	resolved, err := service.ResolveOIDCUser(context.Background(), "USER@example.com", &displayName)
	if err != nil {
		t.Fatalf("resolve oidc user failed: %v", err)
	}
	if resolved.ID != created.ID {
		t.Fatalf("expected existing user %q, got %q", created.ID, resolved.ID)
	}
	if resolved.GlobalRole != domain.GlobalRoleUser {
		t.Fatalf("expected existing global role to be unchanged, got %q", resolved.GlobalRole)
	}
	if resolved.DisplayName == nil || *resolved.DisplayName != displayName {
		t.Fatalf("expected updated display name, got %v", resolved.DisplayName)
	}
}

func TestUserService_ResolveOIDCUserRequiresEmail(t *testing.T) {
	service := NewUserService(memory.NewUserRepository())

	_, err := service.ResolveOIDCUser(context.Background(), " ", nil)
	if !errors.Is(err, ErrUserEmailRequired) {
		t.Fatalf("expected ErrUserEmailRequired, got %v", err)
	}
}

func TestUserService_BootstrapAdmins(t *testing.T) {
	ctx := context.Background()
	userRepo := memory.NewUserRepository()
	service := NewUserService(userRepo)
	normalUser, err := service.CreateUser(ctx, CreateUserInput{Email: "member@example.com"})
	if err != nil {
		t.Fatalf("create normal user failed: %v", err)
	}
	existingAdmin, err := service.CreateUser(ctx, CreateUserInput{Email: "admin@example.com", GlobalRole: "admin"})
	if err != nil {
		t.Fatalf("create existing admin failed: %v", err)
	}

	bootstrapAdmins := map[string]struct{}{
		"NEW-ADMIN@example.com": {},
		"member@example.com":    {},
		"admin@example.com":     {},
	}
	if bootstrapErr := service.BootstrapAdmins(ctx, bootstrapAdmins); bootstrapErr != nil {
		t.Fatalf("bootstrap admins failed: %v", bootstrapErr)
	}
	if bootstrapErr := service.BootstrapAdmins(ctx, bootstrapAdmins); bootstrapErr != nil {
		t.Fatalf("repeated bootstrap admins failed: %v", bootstrapErr)
	}

	newAdmin, getErr := userRepo.GetByEmail(ctx, "new-admin@example.com")
	if getErr != nil || newAdmin.GlobalRole != domain.GlobalRoleAdmin {
		t.Fatalf("expected newly provisioned admin, user=%+v err=%v", newAdmin, getErr)
	}
	promotedUser, getErr := userRepo.GetByEmail(ctx, normalUser.Email)
	if getErr != nil || promotedUser.GlobalRole != domain.GlobalRoleAdmin {
		t.Fatalf("expected normal user promotion, user=%+v err=%v", promotedUser, getErr)
	}
	unchangedAdmin, getErr := userRepo.GetByEmail(ctx, existingAdmin.Email)
	if getErr != nil || unchangedAdmin.ID != existingAdmin.ID || unchangedAdmin.GlobalRole != domain.GlobalRoleAdmin {
		t.Fatalf("expected existing admin unchanged, user=%+v err=%v", unchangedAdmin, getErr)
	}
}

func TestUserService_BootstrapAdminsRepositoryErrors(t *testing.T) {
	ctx := context.Background()
	base := memory.NewUserRepository()
	tests := []struct {
		name string
		repo repository.UserRepository
		want error
	}{
		{
			name: "lookup failure",
			repo: bootstrapUserRepository{
				UserRepository: base,
				getByEmail: func(context.Context, string) (domain.User, error) {
					return domain.User{}, errors.New("lookup failed")
				},
			},
			want: errors.New("lookup failed"),
		},
		{
			name: "create failure",
			repo: bootstrapUserRepository{
				UserRepository: base,
				getByEmail: func(context.Context, string) (domain.User, error) {
					return domain.User{}, repository.ErrUserNotFound
				},
				create: func(context.Context, domain.User) (domain.User, error) {
					return domain.User{}, errors.New("create failed")
				},
			},
			want: errors.New("create failed"),
		},
		{
			name: "update failure",
			repo: bootstrapUserRepository{
				UserRepository: base,
				getByEmail: func(context.Context, string) (domain.User, error) {
					return domain.User{ID: "user-1", Email: "admin@example.com", GlobalRole: domain.GlobalRoleUser}, nil
				},
				update: func(context.Context, domain.User) (domain.User, error) {
					return domain.User{}, errors.New("update failed")
				},
			},
			want: errors.New("update failed"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := NewUserService(test.repo).BootstrapAdmins(ctx, map[string]struct{}{"admin@example.com": {}})
			if err == nil || err.Error() != test.want.Error() {
				t.Fatalf("expected %v, got %v", test.want, err)
			}
		})
	}
}

func TestUserService_BootstrapAdminsSkipsBlankEmailsAndRecoversCreateConflict(t *testing.T) {
	ctx := context.Background()
	lookupCalls := 0
	repo := bootstrapUserRepository{
		UserRepository: memory.NewUserRepository(),
		getByEmail: func(_ context.Context, email string) (domain.User, error) {
			lookupCalls++
			if lookupCalls == 1 {
				if email != "admin@example.com" {
					t.Fatalf("expected normalized email, got %q", email)
				}
				return domain.User{}, repository.ErrUserNotFound
			}
			return domain.User{ID: "admin-1", Email: email, GlobalRole: domain.GlobalRoleAdmin}, nil
		},
		create: func(context.Context, domain.User) (domain.User, error) {
			return domain.User{}, repository.ErrUserEmailConflict
		},
	}

	if err := NewUserService(repo).BootstrapAdmins(ctx, map[string]struct{}{
		" ":                 {},
		"ADMIN@example.com": {},
	}); err != nil {
		t.Fatalf("bootstrap conflict recovery failed: %v", err)
	}
	if lookupCalls != 2 {
		t.Fatalf("expected two lookups after skipping blank email, got %d", lookupCalls)
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
