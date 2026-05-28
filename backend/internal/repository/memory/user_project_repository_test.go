package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func TestUserRepository_CRUDConflictAndOrdering(t *testing.T) {
	ctx := context.Background()
	repo := NewUserRepository()
	now := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
	aliceName := "Alice"
	bobName := "Bob"

	alice, createAliceErr := repo.Create(ctx, domain.User{ID: "user-b", Email: "Alice@example.com", DisplayName: &aliceName, CreatedAt: now.Add(time.Minute)})
	if createAliceErr != nil {
		t.Fatalf("create alice: %v", createAliceErr)
	}
	bob, createBobErr := repo.Create(ctx, domain.User{ID: "user-a", Email: "bob@example.com", DisplayName: &bobName, CreatedAt: now})
	if createBobErr != nil {
		t.Fatalf("create bob: %v", createBobErr)
	}
	generated, createGeneratedErr := repo.Create(ctx, domain.User{Email: "generated@example.com", CreatedAt: now.Add(2 * time.Minute)})
	if createGeneratedErr != nil {
		t.Fatalf("create generated user: %v", createGeneratedErr)
	}
	if generated.ID == "" {
		t.Fatal("expected generated user ID")
	}

	_, duplicateErr := repo.Create(ctx, domain.User{ID: "user-c", Email: "alice@EXAMPLE.com"})
	if !errors.Is(duplicateErr, repository.ErrUserEmailConflict) {
		t.Fatalf("expected duplicate email conflict, got %v", duplicateErr)
	}

	fetched, getErr := repo.GetByEmail(ctx, "ALICE@example.com")
	if getErr != nil {
		t.Fatalf("get by email: %v", getErr)
	}
	if fetched.ID != alice.ID {
		t.Fatalf("expected alice by case-insensitive email, got %q", fetched.ID)
	}

	users, listErr := repo.List(ctx)
	if listErr != nil {
		t.Fatalf("list users: %v", listErr)
	}
	if len(users) != 3 || users[0].ID != bob.ID || users[1].ID != alice.ID || users[2].ID != generated.ID {
		t.Fatalf("expected users ordered by created_at then id, got %+v", users)
	}

	alice.Email = "alice-updated@example.com"
	updated, updateErr := repo.Update(ctx, alice)
	if updateErr != nil {
		t.Fatalf("update alice: %v", updateErr)
	}
	if updated.Email != "alice-updated@example.com" {
		t.Fatalf("expected updated email, got %q", updated.Email)
	}

	bob.Email = "alice-updated@example.com"
	_, updateConflictErr := repo.Update(ctx, bob)
	if !errors.Is(updateConflictErr, repository.ErrUserEmailConflict) {
		t.Fatalf("expected update email conflict, got %v", updateConflictErr)
	}

	deleteErr := repo.Delete(ctx, generated.ID)
	if deleteErr != nil {
		t.Fatalf("delete generated user: %v", deleteErr)
	}
	_, deletedErr := repo.GetByID(ctx, generated.ID)
	if !errors.Is(deletedErr, repository.ErrUserNotFound) {
		t.Fatalf("expected deleted user not found, got %v", deletedErr)
	}
}

func TestProjectRepository_CRUDConflictOrderingAndDeleteGuard(t *testing.T) {
	ctx := context.Background()
	jobRepo := NewJobRepository()
	repo := NewProjectRepository(jobRepo)
	now := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)

	api, createAPIErr := repo.Create(ctx, domain.Project{ID: "project-b", Name: "API", Slug: "api", CreatedAt: now.Add(time.Minute)})
	if createAPIErr != nil {
		t.Fatalf("create api project: %v", createAPIErr)
	}
	web, createWebErr := repo.Create(ctx, domain.Project{ID: "project-a", Name: "Web", Slug: "web", CreatedAt: now})
	if createWebErr != nil {
		t.Fatalf("create web project: %v", createWebErr)
	}
	generated, createGeneratedErr := repo.Create(ctx, domain.Project{Name: "Generated", Slug: "generated", CreatedAt: now.Add(2 * time.Minute)})
	if createGeneratedErr != nil {
		t.Fatalf("create generated project: %v", createGeneratedErr)
	}
	if generated.ID == "" {
		t.Fatal("expected generated project ID")
	}

	_, duplicateErr := repo.Create(ctx, domain.Project{ID: "project-c", Name: "API Duplicate", Slug: "API"})
	if !errors.Is(duplicateErr, repository.ErrProjectSlugConflict) {
		t.Fatalf("expected duplicate slug conflict, got %v", duplicateErr)
	}
	bySlug, bySlugErr := repo.GetBySlug(ctx, "api")
	if bySlugErr != nil {
		t.Fatalf("get project by slug: %v", bySlugErr)
	}
	if bySlug.ID != api.ID {
		t.Fatalf("expected project %q by slug, got %q", api.ID, bySlug.ID)
	}

	byIDs, byIDsResultErr := repo.GetByIDs(ctx, []string{" missing ", api.ID, "", web.ID, api.ID})
	if byIDsResultErr != nil {
		t.Fatalf("get by ids: %v", byIDsResultErr)
	}
	if len(byIDs) != 2 || byIDs[0].ID != web.ID || byIDs[1].ID != api.ID {
		t.Fatalf("expected unique existing projects ordered by created_at, got %+v", byIDs)
	}

	listed, listErr := repo.List(ctx)
	if listErr != nil {
		t.Fatalf("list projects: %v", listErr)
	}
	if len(listed) != 3 || listed[0].ID != web.ID || listed[1].ID != api.ID || listed[2].ID != generated.ID {
		t.Fatalf("expected projects ordered by created_at then id, got %+v", listed)
	}

	api.Name = "API Updated"
	updated, updateErr := repo.Update(ctx, api)
	if updateErr != nil {
		t.Fatalf("update api project: %v", updateErr)
	}
	if updated.Name != "API Updated" {
		t.Fatalf("expected updated project name, got %q", updated.Name)
	}

	web.Slug = "api"
	_, updateConflictErr := repo.Update(ctx, web)
	if !errors.Is(updateConflictErr, repository.ErrProjectSlugConflict) {
		t.Fatalf("expected update slug conflict, got %v", updateConflictErr)
	}

	_, createJobErr := jobRepo.Create(ctx, domain.Job{ID: "job-1", ProjectID: api.ID, CreatedAt: now})
	if createJobErr != nil {
		t.Fatalf("create project job: %v", createJobErr)
	}
	guardErr := repo.Delete(ctx, api.ID)
	if !errors.Is(guardErr, repository.ErrProjectHasJobs) {
		t.Fatalf("expected project has jobs guard, got %v", guardErr)
	}

	deleteErr := repo.Delete(ctx, generated.ID)
	if deleteErr != nil {
		t.Fatalf("delete generated project: %v", deleteErr)
	}
	_, deletedErr := repo.GetByID(ctx, generated.ID)
	if !errors.Is(deletedErr, repository.ErrProjectNotFound) {
		t.Fatalf("expected deleted project not found, got %v", deletedErr)
	}
}

func TestProjectMembershipRepository_ListsAndDeletesMemberships(t *testing.T) {
	ctx := context.Background()
	projectRepo := NewProjectRepository(nil)
	userRepo := NewUserRepository()
	membershipRepo := NewProjectMembershipRepository(projectRepo, userRepo)
	now := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
	aliceName := "Alice"
	bobName := "Bob"

	projectA, createProjectAErr := projectRepo.Create(ctx, domain.Project{ID: "project-a", Name: "Project A", Slug: "project-a", CreatedAt: now})
	if createProjectAErr != nil {
		t.Fatalf("create project a: %v", createProjectAErr)
	}
	projectB, createProjectBErr := projectRepo.Create(ctx, domain.Project{ID: "project-b", Name: "Project B", Slug: "project-b", CreatedAt: now.Add(time.Minute)})
	if createProjectBErr != nil {
		t.Fatalf("create project b: %v", createProjectBErr)
	}
	alice, createAliceErr := userRepo.Create(ctx, domain.User{ID: "user-a", Email: "alice@example.com", DisplayName: &aliceName, CreatedAt: now})
	if createAliceErr != nil {
		t.Fatalf("create alice: %v", createAliceErr)
	}
	bob, createBobErr := userRepo.Create(ctx, domain.User{ID: "user-b", Email: "bob@example.com", DisplayName: &bobName, CreatedAt: now.Add(time.Minute)})
	if createBobErr != nil {
		t.Fatalf("create bob: %v", createBobErr)
	}

	first, upsertFirstErr := membershipRepo.Upsert(ctx, domain.ProjectMembership{ProjectID: projectB.ID, UserID: alice.ID, Role: domain.ProjectMemberRoleViewer, CreatedAt: now.Add(time.Minute), UpdatedAt: now.Add(time.Minute)})
	if upsertFirstErr != nil {
		t.Fatalf("upsert first membership: %v", upsertFirstErr)
	}
	_, upsertSecondErr := membershipRepo.Upsert(ctx, domain.ProjectMembership{ProjectID: projectA.ID, UserID: alice.ID, Role: domain.ProjectMemberRoleMaintainer, CreatedAt: now, UpdatedAt: now})
	if upsertSecondErr != nil {
		t.Fatalf("upsert second membership: %v", upsertSecondErr)
	}
	_, upsertBobErr := membershipRepo.Upsert(ctx, domain.ProjectMembership{ProjectID: projectA.ID, UserID: bob.ID, Role: domain.ProjectMemberRoleViewer, CreatedAt: now.Add(2 * time.Minute), UpdatedAt: now.Add(2 * time.Minute)})
	if upsertBobErr != nil {
		t.Fatalf("upsert bob membership: %v", upsertBobErr)
	}

	updatedFirst, updateFirstErr := membershipRepo.Upsert(ctx, domain.ProjectMembership{ProjectID: projectB.ID, UserID: alice.ID, Role: domain.ProjectMemberRoleOwner, CreatedAt: now.Add(5 * time.Minute), UpdatedAt: now.Add(5 * time.Minute)})
	if updateFirstErr != nil {
		t.Fatalf("update first membership: %v", updateFirstErr)
	}
	if updatedFirst.CreatedAt != first.CreatedAt || updatedFirst.Role != domain.ProjectMemberRoleOwner {
		t.Fatalf("expected preserved created_at and updated role, got %+v", updatedFirst)
	}

	byUser, byUserErr := membershipRepo.ListByUserID(ctx, alice.ID)
	if byUserErr != nil {
		t.Fatalf("list by user: %v", byUserErr)
	}
	if len(byUser) != 2 || byUser[0].ProjectID != projectA.ID || byUser[1].ProjectID != projectB.ID {
		t.Fatalf("expected memberships ordered by project id, got %+v", byUser)
	}

	byProject, byProjectErr := membershipRepo.ListByProjectID(ctx, projectA.ID)
	if byProjectErr != nil {
		t.Fatalf("list by project: %v", byProjectErr)
	}
	if len(byProject) != 2 || byProject[0].Email != "alice@example.com" || byProject[1].Email != "bob@example.com" {
		t.Fatalf("expected memberships ordered by user email, got %+v", byProject)
	}

	deleteErr := membershipRepo.Delete(ctx, projectA.ID, bob.ID)
	if deleteErr != nil {
		t.Fatalf("delete membership: %v", deleteErr)
	}
	_, deletedErr := membershipRepo.Get(ctx, projectA.ID, bob.ID)
	if !errors.Is(deletedErr, repository.ErrProjectMembershipNotFound) {
		t.Fatalf("expected deleted membership not found, got %v", deletedErr)
	}

	deleteUserErr := userRepo.Delete(ctx, alice.ID)
	if deleteUserErr != nil {
		t.Fatalf("delete alice: %v", deleteUserErr)
	}
	afterCascade, afterCascadeErr := membershipRepo.ListByUserID(ctx, alice.ID)
	if afterCascadeErr != nil {
		t.Fatalf("list alice memberships after delete: %v", afterCascadeErr)
	}
	if len(afterCascade) != 0 {
		t.Fatalf("expected user delete to remove memberships, got %+v", afterCascade)
	}

	_, missingProjectErr := membershipRepo.Upsert(ctx, domain.ProjectMembership{ProjectID: "missing", UserID: bob.ID})
	if !errors.Is(missingProjectErr, repository.ErrProjectNotFound) {
		t.Fatalf("expected missing project error, got %v", missingProjectErr)
	}
	_, missingUserErr := membershipRepo.Upsert(ctx, domain.ProjectMembership{ProjectID: projectA.ID, UserID: "missing"})
	if !errors.Is(missingUserErr, repository.ErrUserNotFound) {
		t.Fatalf("expected missing user error, got %v", missingUserErr)
	}
}
