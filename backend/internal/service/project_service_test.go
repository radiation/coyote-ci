package service

import (
	"context"
	"errors"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/repository/memory"
	buildsvc "github.com/radiation/coyote-ci/backend/internal/service/build"
)

func TestProjectService_CreateListGetUpdateDelete(t *testing.T) {
	jobRepo := memory.NewJobRepository()
	projectRepo := memory.NewProjectRepository(jobRepo)
	service := NewProjectService(projectRepo)

	created, err := service.CreateProject(context.Background(), CreateProjectInput{
		Name:        "Platform",
		Slug:        "platform",
		Description: strPtr("Core platform pipelines"),
	})
	if err != nil {
		t.Fatalf("create project failed: %v", err)
	}
	if created.Slug != "platform" {
		t.Fatalf("expected slug platform, got %q", created.Slug)
	}
	if created.IsPublic {
		t.Fatal("expected newly created project to be private by default")
	}

	listed, err := service.ListProjects(context.Background())
	if err != nil {
		t.Fatalf("list projects failed: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("expected 1 project, got %d", len(listed))
	}

	got, err := service.GetProject(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("get project failed: %v", err)
	}
	if got.Name != "Platform" {
		t.Fatalf("expected project name Platform, got %q", got.Name)
	}

	updated, err := service.UpdateProject(context.Background(), created.ID, UpdateProjectInput{
		Name:        strPtr("Platform CI"),
		Slug:        strPtr("platform-ci"),
		Description: OptionalStringPatch{Set: true, Value: strPtr("Updated description")},
		IsPublic:    boolPtr(true),
	})
	if err != nil {
		t.Fatalf("update project failed: %v", err)
	}
	if updated.Name != "Platform CI" {
		t.Fatalf("expected updated name, got %q", updated.Name)
	}
	if updated.Slug != "platform-ci" {
		t.Fatalf("expected updated slug, got %q", updated.Slug)
	}
	if !updated.IsPublic {
		t.Fatal("expected updated project to be public")
	}

	deleteErr := service.DeleteProject(context.Background(), created.ID)
	if deleteErr != nil {
		t.Fatalf("delete project failed: %v", deleteErr)
	}

	_, err = service.GetProject(context.Background(), created.ID)
	if !errors.Is(err, repository.ErrProjectNotFound) {
		t.Fatalf("expected ErrProjectNotFound after delete, got %v", err)
	}
}

func TestProjectService_DuplicateSlugConflict(t *testing.T) {
	projectRepo := memory.NewProjectRepository(memory.NewJobRepository())
	service := NewProjectService(projectRepo)

	_, err := service.CreateProject(context.Background(), CreateProjectInput{Name: "Platform", Slug: "platform"})
	if err != nil {
		t.Fatalf("first create failed: %v", err)
	}

	_, err = service.CreateProject(context.Background(), CreateProjectInput{Name: "Platform 2", Slug: "platform"})
	if !errors.Is(err, repository.ErrProjectSlugConflict) {
		t.Fatalf("expected ErrProjectSlugConflict, got %v", err)
	}
}

func TestProjectService_DeleteProjectWithJobsReturnsConflict(t *testing.T) {
	jobRepo := memory.NewJobRepository()
	projectRepo := memory.NewProjectRepository(jobRepo)
	projectService := NewProjectService(projectRepo)
	jobService := NewJobService(jobRepo, buildsvc.NewBuildService(memory.NewBuildRepository(), nil, nil)).WithProjectRepository(projectRepo)

	project, err := projectService.CreateProject(context.Background(), CreateProjectInput{Name: "Platform", Slug: "platform"})
	if err != nil {
		t.Fatalf("create project failed: %v", err)
	}

	_, err = jobService.CreateJob(context.Background(), CreateJobInput{
		ProjectID:     project.ID,
		Name:          "backend-ci",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PipelineYAML:  "version: 1\nsteps:\n  - name: test\n    run: go test ./...\n",
	})
	if err != nil {
		t.Fatalf("create job failed: %v", err)
	}

	err = projectService.DeleteProject(context.Background(), project.ID)
	if !errors.Is(err, repository.ErrProjectHasJobs) {
		t.Fatalf("expected ErrProjectHasJobs, got %v", err)
	}
}

func TestProjectService_DeleteDefaultProjectReturnsConflict(t *testing.T) {
	jobRepo := memory.NewJobRepository()
	projectRepo := memory.NewProjectRepository(jobRepo)
	projectService := NewProjectService(projectRepo)

	defaultProject, err := projectRepo.Create(context.Background(), domain.Project{
		ID:        "00000000-0000-0000-0000-000000000001",
		Name:      "Default Project",
		Slug:      domain.DefaultProjectSlug,
		CreatedAt: projectService.now().UTC(),
		UpdatedAt: projectService.now().UTC(),
	})
	if err != nil {
		t.Fatalf("create default project failed: %v", err)
	}

	err = projectService.DeleteProject(context.Background(), defaultProject.ID)
	if !errors.Is(err, ErrDefaultProjectDeleteForbidden) {
		t.Fatalf("expected ErrDefaultProjectDeleteForbidden, got %v", err)
	}
}

func TestJobService_CreateJobAssociatesProject(t *testing.T) {
	jobRepo := memory.NewJobRepository()
	projectRepo := memory.NewProjectRepository(jobRepo)
	buildRepo := memory.NewBuildRepository()
	projectService := NewProjectService(projectRepo)
	jobService := NewJobService(jobRepo, buildsvc.NewBuildService(buildRepo, nil, nil)).WithProjectRepository(projectRepo)

	project, err := projectService.CreateProject(context.Background(), CreateProjectInput{Name: "Platform", Slug: "platform"})
	if err != nil {
		t.Fatalf("create project failed: %v", err)
	}

	job, err := jobService.CreateJob(context.Background(), CreateJobInput{
		ProjectID:     project.ID,
		Name:          "backend-ci",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PipelineYAML:  "version: 1\nsteps:\n  - name: test\n    run: go test ./...\n",
	})
	if err != nil {
		t.Fatalf("create job failed: %v", err)
	}
	if job.ProjectID != project.ID {
		t.Fatalf("expected project id %q, got %q", project.ID, job.ProjectID)
	}

	builds, err := buildRepo.ListByJobID(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("list builds failed: %v", err)
	}
	if len(builds) != 1 {
		t.Fatalf("expected 1 build, got %d", len(builds))
	}
	if builds[0].ProjectID != project.ID {
		t.Fatalf("expected build project id %q, got %q", project.ID, builds[0].ProjectID)
	}
}

func TestJobService_CreateJobDefaultsToDefaultProject(t *testing.T) {
	jobRepo := memory.NewJobRepository()
	projectRepo := memory.NewProjectRepository(jobRepo)
	buildRepo := memory.NewBuildRepository()
	projectService := NewProjectService(projectRepo)
	jobService := NewJobService(jobRepo, buildsvc.NewBuildService(buildRepo, nil, nil)).WithProjectRepository(projectRepo)

	defaultProject, err := projectRepo.Create(context.Background(), domain.Project{
		ID:        "00000000-0000-0000-0000-000000000001",
		Name:      "Default Project",
		Slug:      domain.DefaultProjectSlug,
		CreatedAt: projectService.now().UTC(),
		UpdatedAt: projectService.now().UTC(),
	})
	if err != nil {
		t.Fatalf("create default project failed: %v", err)
	}

	job, err := jobService.CreateJob(context.Background(), CreateJobInput{
		Name:          "compat-job",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PipelineYAML:  "version: 1\nsteps:\n  - name: test\n    run: go test ./...\n",
	})
	if err != nil {
		t.Fatalf("create job failed: %v", err)
	}
	if job.ProjectID != defaultProject.ID {
		t.Fatalf("expected default project id %q, got %q", defaultProject.ID, job.ProjectID)
	}
}
