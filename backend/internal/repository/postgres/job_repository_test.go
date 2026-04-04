package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func TestJobRepository_CreateGetListUpdate(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}

	repo := NewJobRepository(db)
	now := time.Now().UTC()
	row := []string{"id", "project_id", "name", "repository_url", "default_ref", "default_commit_sha", "push_enabled", "push_branch", "trigger_mode", "branch_allowlist", "tag_allowlist", "pipeline_yaml", "pipeline_path", "enabled", "created_at", "updated_at"}
	pushBranch := "main"
	pipelinePath := ".coyote/pipeline.yml"
	branchAllowlistJSON := ` ["main"] `
	tagAllowlistJSON := ` [] `

	job := domain.Job{
		ID:              "job-1",
		ProjectID:       "project-1",
		Name:            "backend-ci",
		RepositoryURL:   "https://github.com/example/backend.git",
		DefaultRef:      "main",
		PushEnabled:     true,
		PushBranch:      &pushBranch,
		TriggerMode:     domain.JobTriggerModeBranches,
		BranchAllowlist: []string{"main"},
		PipelineYAML:    "version: 1\nsteps:\n  - name: test\n    run: go test ./...\n",
		PipelinePath:    &pipelinePath,
		Enabled:         true,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	mock.ExpectQuery("INSERT INTO jobs").WillReturnRows(sqlmock.NewRows(row).AddRow(
		job.ID, job.ProjectID, job.Name, job.RepositoryURL, job.DefaultRef, nil, job.PushEnabled, job.PushBranch, job.TriggerMode, branchAllowlistJSON, tagAllowlistJSON, job.PipelineYAML, job.PipelinePath, job.Enabled, job.CreatedAt, job.UpdatedAt,
	))
	created, err := repo.Create(context.Background(), job)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if created.ID != "job-1" {
		t.Fatalf("expected created id job-1, got %q", created.ID)
	}

	mock.ExpectQuery("SELECT id, project_id, name, repository_url").WillReturnRows(sqlmock.NewRows(row).AddRow(
		job.ID, job.ProjectID, job.Name, job.RepositoryURL, job.DefaultRef, nil, job.PushEnabled, job.PushBranch, job.TriggerMode, branchAllowlistJSON, tagAllowlistJSON, job.PipelineYAML, job.PipelinePath, job.Enabled, job.CreatedAt, job.UpdatedAt,
	))
	got, err := repo.GetByID(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if got.Name != "backend-ci" {
		t.Fatalf("expected name backend-ci, got %q", got.Name)
	}

	mock.ExpectQuery("SELECT id, project_id, name, repository_url").WillReturnRows(sqlmock.NewRows(row).AddRow(
		job.ID, job.ProjectID, job.Name, job.RepositoryURL, job.DefaultRef, nil, job.PushEnabled, job.PushBranch, job.TriggerMode, branchAllowlistJSON, tagAllowlistJSON, job.PipelineYAML, job.PipelinePath, job.Enabled, job.CreatedAt, job.UpdatedAt,
	))
	listed, err := repo.List(context.Background())
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("expected 1 listed job, got %d", len(listed))
	}

	job.Enabled = false
	job.UpdatedAt = now.Add(time.Second)
	mock.ExpectQuery("UPDATE jobs").WillReturnRows(sqlmock.NewRows(row).AddRow(
		job.ID, job.ProjectID, job.Name, job.RepositoryURL, job.DefaultRef, nil, job.PushEnabled, job.PushBranch, job.TriggerMode, branchAllowlistJSON, tagAllowlistJSON, job.PipelineYAML, job.PipelinePath, job.Enabled, job.CreatedAt, job.UpdatedAt,
	))
	updated, err := repo.Update(context.Background(), job)
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}
	if updated.Enabled {
		t.Fatal("expected enabled=false after update")
	}

	mock.ExpectQuery("SELECT id, project_id, name, repository_url").WillReturnError(sql.ErrNoRows)
	_, err = repo.GetByID(context.Background(), "missing")
	if !errors.Is(err, repository.ErrJobNotFound) {
		t.Fatalf("expected ErrJobNotFound, got %v", err)
	}

	mock.ExpectQuery("FROM jobs").WillReturnRows(sqlmock.NewRows(row).AddRow(
		"job-2", "project-1", "backend-main", "https://github.com/example/backend.git", "main", nil, true, "main", "branches", `["main"]`, `[]`, "version: 1\nsteps:\n  - name: test\n    run: go test ./...\n", ".coyote/pipeline.yml", true, now, now,
	))
	matched, err := repo.ListPushEnabledByRepository(context.Background(), "https://github.com/example/backend")
	if err != nil {
		t.Fatalf("list push-enabled jobs failed: %v", err)
	}
	if len(matched) != 1 {
		t.Fatalf("expected one matched job, got %d", len(matched))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}
