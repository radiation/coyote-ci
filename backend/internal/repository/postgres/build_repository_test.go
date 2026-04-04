package postgres

// Core Postgres BuildRepository tests.
// Step claim/completion/lease lifecycle tests live in build_repository_step_lifecycle_test.go.

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

func TestNewBuildRepository(t *testing.T) {
	repo := NewBuildRepository(&sql.DB{})
	if repo == nil {
		t.Fatal("expected repository, got nil")
	}
	if repo.db == nil {
		t.Fatal("expected db to be set")
	}
}

func TestBuildRepository_Create(t *testing.T) {
	tests := []struct {
		name      string
		expectErr bool
	}{
		{name: "success"},
		{name: "exec error", expectErr: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("failed to create sql mock: %v", err)
			}

			repo := NewBuildRepository(db)
			exec := mock.ExpectExec("INSERT INTO builds")
			if tc.expectErr {
				exec.WillReturnError(errors.New("insert failed"))
			} else {
				exec.WillReturnResult(sqlmock.NewResult(1, 1))
			}

			build := domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusPending, CreatedAt: time.Now().UTC()}
			got, err := repo.Create(context.Background(), build)
			if tc.expectErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if got.ID != build.ID {
				t.Fatalf("expected id %q, got %q", build.ID, got.ID)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("unmet sql expectations: %v", err)
			}
		})
	}
}

func TestBuildRepository_GetByID(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name      string
		err       error
		expectErr error
	}{
		{name: "success"},
		{name: "not found", err: sql.ErrNoRows, expectErr: repository.ErrBuildNotFound},
		{name: "query error", err: errors.New("query failed"), expectErr: errors.New("query failed")},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("failed to create sql mock: %v", err)
			}

			repo := NewBuildRepository(db)
			exp := mock.ExpectQuery("SELECT id, project_id, job_id, status, created_at")
			if tc.err != nil {
				exp.WillReturnError(tc.err)
			} else {
				exp.WillReturnRows(sqlmock.NewRows([]string{"id", "project_id", "job_id", "status", "created_at", "queued_at", "started_at", "finished_at", "current_step_index", "attempt_number", "rerun_of_build_id", "rerun_from_step_index", "error_message", "pipeline_config_yaml", "pipeline_name", "pipeline_source", "pipeline_path", "repo_url", "ref", "commit_sha", "trigger_kind", "scm_provider", "event_type", "trigger_repository_owner", "trigger_repository_name", "trigger_repository_url", "trigger_ref", "trigger_ref_type", "trigger_delivery_id", "trigger_actor"}).AddRow("build-1", "project-1", nil, "queued", now, now, nil, nil, 0, 1, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, "manual", nil, nil, nil, nil, nil, nil, nil, nil, nil))
			}

			got, err := repo.GetByID(context.Background(), "build-1")
			if tc.expectErr != nil {
				if tc.expectErr == repository.ErrBuildNotFound {
					if !errors.Is(err, repository.ErrBuildNotFound) {
						t.Fatalf("expected ErrBuildNotFound, got %v", err)
					}
				} else if err == nil || err.Error() != tc.expectErr.Error() {
					t.Fatalf("expected error %q, got %v", tc.expectErr.Error(), err)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if got.Status != domain.BuildStatusQueued {
				t.Fatalf("expected queued status, got %q", got.Status)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("unmet sql expectations: %v", err)
			}
		})
	}
}

func TestBuildRepository_UpdateStatus(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name      string
		err       error
		expectErr error
	}{
		{name: "success"},
		{name: "not found", err: sql.ErrNoRows, expectErr: repository.ErrBuildNotFound},
		{name: "query error", err: errors.New("update failed"), expectErr: errors.New("update failed")},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("failed to create sql mock: %v", err)
			}

			repo := NewBuildRepository(db)
			exp := mock.ExpectQuery("UPDATE builds")
			if tc.err != nil {
				exp.WillReturnError(tc.err)
			} else {
				exp.WillReturnRows(sqlmock.NewRows([]string{"id", "project_id", "job_id", "status", "created_at", "queued_at", "started_at", "finished_at", "current_step_index", "attempt_number", "rerun_of_build_id", "rerun_from_step_index", "error_message", "pipeline_config_yaml", "pipeline_name", "pipeline_source", "pipeline_path", "repo_url", "ref", "commit_sha", "trigger_kind", "scm_provider", "event_type", "trigger_repository_owner", "trigger_repository_name", "trigger_repository_url", "trigger_ref", "trigger_ref_type", "trigger_delivery_id", "trigger_actor"}).AddRow("build-1", "project-1", nil, "running", now, now, now, nil, 0, 1, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, "manual", nil, nil, nil, nil, nil, nil, nil, nil, nil))
			}

			got, err := repo.UpdateStatus(context.Background(), "build-1", domain.BuildStatusRunning, nil)
			if tc.expectErr != nil {
				if tc.expectErr == repository.ErrBuildNotFound {
					if !errors.Is(err, repository.ErrBuildNotFound) {
						t.Fatalf("expected ErrBuildNotFound, got %v", err)
					}
				} else if err == nil || err.Error() != tc.expectErr.Error() {
					t.Fatalf("expected error %q, got %v", tc.expectErr.Error(), err)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if got.Status != domain.BuildStatusRunning {
				t.Fatalf("expected running status, got %q", got.Status)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("unmet sql expectations: %v", err)
			}
		})
	}
}

func TestBuildRepository_QueueBuild_PersistsBuildAndSteps(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sql mock: %v", err)
	}

	repo := NewBuildRepository(db)
	now := time.Now().UTC()

	mock.ExpectBegin()
	mock.ExpectQuery("UPDATE builds").WillReturnRows(
		sqlmock.NewRows([]string{"id", "project_id", "job_id", "status", "created_at", "queued_at", "started_at", "finished_at", "current_step_index", "attempt_number", "rerun_of_build_id", "rerun_from_step_index", "error_message", "pipeline_config_yaml", "pipeline_name", "pipeline_source", "pipeline_path", "repo_url", "ref", "commit_sha", "trigger_kind", "scm_provider", "event_type", "trigger_repository_owner", "trigger_repository_name", "trigger_repository_url", "trigger_ref", "trigger_ref_type", "trigger_delivery_id", "trigger_actor"}).
			AddRow("build-1", "project-1", nil, "queued", now, now, nil, nil, 0, 1, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, "manual", nil, nil, nil, nil, nil, nil, nil, nil, nil),
	)
	mock.ExpectExec("DELETE FROM build_steps").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO build_steps").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO build_steps").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	build, err := repo.QueueBuild(context.Background(), "build-1", []domain.BuildStep{
		{ID: "step-1", BuildID: "build-1", StepIndex: 0, Name: "lint", Status: domain.BuildStepStatusPending},
		{ID: "step-2", BuildID: "build-1", StepIndex: 1, Name: "test", Status: domain.BuildStepStatusPending},
	})
	if err != nil {
		t.Fatalf("queue build failed: %v", err)
	}
	if build.Status != domain.BuildStatusQueued {
		t.Fatalf("expected queued status, got %q", build.Status)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestBuildRepository_GetStepsByBuildID_Ordered(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sql mock: %v", err)
	}

	repo := NewBuildRepository(db)
	now := time.Now().UTC()

	mock.ExpectQuery("SELECT id, build_id, step_index, name, image, command").WillReturnRows(
		sqlmock.NewRows([]string{"id", "build_id", "step_index", "name", "image", "command", "args", "env", "working_dir", "timeout_seconds", "status", "worker_id", "claim_token", "claimed_at", "lease_expires_at", "started_at", "finished_at", "exit_code", "stdout", "stderr", "error_message"}).
			AddRow("step-1", "build-1", 0, "lint", "", "go", "[\"test\"]", "{}", "/workspace", 60, "success", nil, nil, nil, nil, now, now, 0, "ok", "", nil).
			AddRow("step-2", "build-1", 1, "test", "", "go", "[\"test\",\"./...\"]", "{}", "/workspace", 60, "pending", nil, nil, nil, nil, nil, nil, nil, nil, nil, nil),
	)

	steps, err := repo.GetStepsByBuildID(context.Background(), "build-1")
	if err != nil {
		t.Fatalf("get steps failed: %v", err)
	}
	if len(steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(steps))
	}
	if steps[0].StepIndex != 0 || steps[0].Name != "lint" {
		t.Fatalf("expected first step lint@0, got %s@%d", steps[0].Name, steps[0].StepIndex)
	}
	if steps[1].StepIndex != 1 || steps[1].Name != "test" {
		t.Fatalf("expected second step test@1, got %s@%d", steps[1].Name, steps[1].StepIndex)
	}
	if steps[0].Command != "go" || steps[0].WorkingDir != "/workspace" {
		t.Fatalf("expected persisted command and working dir, got command=%q working_dir=%q", steps[0].Command, steps[0].WorkingDir)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestBuildRepository_UpdateStepByIndex(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sql mock: %v", err)
	}

	repo := NewBuildRepository(db)
	now := time.Now().UTC()
	exitCode := 1
	stdout := "partial output"
	stderr := "step failed"
	errMsg := "step failed"

	mock.ExpectQuery("UPDATE build_steps").WillReturnRows(
		sqlmock.NewRows([]string{"id", "build_id", "step_index", "name", "image", "command", "args", "env", "working_dir", "timeout_seconds", "status", "worker_id", "claim_token", "claimed_at", "lease_expires_at", "started_at", "finished_at", "exit_code", "stdout", "stderr", "error_message"}).
			AddRow("step-1", "build-1", 0, "lint", "", "go", "[\"test\",\"./...\"]", "{}", "/workspace", 60, "failed", "worker-1", nil, nil, nil, now, now, exitCode, stdout, stderr, errMsg),
	)

	workerID := "worker-1"
	step, err := repo.UpdateStepByIndex(context.Background(), "build-1", 0, repository.StepUpdate{
		Status:       domain.BuildStepStatusFailed,
		WorkerID:     &workerID,
		ExitCode:     &exitCode,
		Stdout:       &stdout,
		Stderr:       &stderr,
		ErrorMessage: &errMsg,
		StartedAt:    &now,
		FinishedAt:   &now,
	})
	if err != nil {
		t.Fatalf("update step failed: %v", err)
	}
	if step.Status != domain.BuildStepStatusFailed {
		t.Fatalf("expected failed step status, got %q", step.Status)
	}
	if step.ExitCode == nil || *step.ExitCode != exitCode {
		t.Fatalf("expected exit code %d, got %v", exitCode, step.ExitCode)
	}
	if step.Stdout == nil || *step.Stdout != stdout {
		t.Fatalf("expected stdout %q, got %v", stdout, step.Stdout)
	}
	if step.Stderr == nil || *step.Stderr != stderr {
		t.Fatalf("expected stderr %q, got %v", stderr, step.Stderr)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}
