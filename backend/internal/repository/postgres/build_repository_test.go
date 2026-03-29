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
			exp := mock.ExpectQuery("SELECT id, project_id, status, created_at")
			if tc.err != nil {
				exp.WillReturnError(tc.err)
			} else {
				exp.WillReturnRows(sqlmock.NewRows([]string{"id", "project_id", "status", "created_at", "queued_at", "started_at", "finished_at", "current_step_index", "error_message", "pipeline_config_yaml", "pipeline_name", "pipeline_source", "repo_url", "ref", "commit_sha"}).AddRow("build-1", "project-1", "queued", now, now, nil, nil, 0, nil, nil, nil, nil, nil, nil, nil))
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
				exp.WillReturnRows(sqlmock.NewRows([]string{"id", "project_id", "status", "created_at", "queued_at", "started_at", "finished_at", "current_step_index", "error_message", "pipeline_config_yaml", "pipeline_name", "pipeline_source", "repo_url", "ref", "commit_sha"}).AddRow("build-1", "project-1", "running", now, now, now, nil, 0, nil, nil, nil, nil, nil, nil, nil))
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
		sqlmock.NewRows([]string{"id", "project_id", "status", "created_at", "queued_at", "started_at", "finished_at", "current_step_index", "error_message", "pipeline_config_yaml", "pipeline_name", "pipeline_source", "repo_url", "ref", "commit_sha"}).
			AddRow("build-1", "project-1", "queued", now, now, nil, nil, 0, nil, nil, nil, nil, nil, nil, nil),
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

	mock.ExpectQuery("SELECT id, build_id, step_index, name, command").WillReturnRows(
		sqlmock.NewRows([]string{"id", "build_id", "step_index", "name", "command", "args", "env", "working_dir", "timeout_seconds", "status", "worker_id", "claim_token", "claimed_at", "lease_expires_at", "started_at", "finished_at", "exit_code", "stdout", "stderr", "error_message"}).
			AddRow("step-1", "build-1", 0, "lint", "go", "[\"test\"]", "{}", "/workspace", 60, "success", nil, nil, nil, nil, now, now, 0, "ok", "", nil).
			AddRow("step-2", "build-1", 1, "test", "go", "[\"test\",\"./...\"]", "{}", "/workspace", 60, "pending", nil, nil, nil, nil, nil, nil, nil, nil, nil, nil),
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
		sqlmock.NewRows([]string{"id", "build_id", "step_index", "name", "command", "args", "env", "working_dir", "timeout_seconds", "status", "worker_id", "claim_token", "claimed_at", "lease_expires_at", "started_at", "finished_at", "exit_code", "stdout", "stderr", "error_message"}).
			AddRow("step-1", "build-1", 0, "lint", "go", "[\"test\",\"./...\"]", "{}", "/workspace", 60, "failed", "worker-1", nil, nil, nil, now, now, exitCode, stdout, stderr, errMsg),
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

func TestBuildRepository_ClaimStepIfPending(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name        string
		queryErr    error
		rows        *sqlmock.Rows
		expectErr   bool
		expectClaim bool
	}{
		{
			name: "success",
			rows: sqlmock.NewRows([]string{"id", "build_id", "step_index", "name", "command", "args", "env", "working_dir", "timeout_seconds", "status", "worker_id", "claim_token", "claimed_at", "lease_expires_at", "started_at", "finished_at", "exit_code", "stdout", "stderr", "error_message"}).
				AddRow("step-1", "build-1", 0, "default", "sh", "[\"-c\",\"echo ok\"]", "{}", ".", 30, "running", nil, nil, nil, nil, now, nil, nil, nil, nil, nil),
			expectClaim: true,
		},
		{
			name:        "no rows means not pending",
			queryErr:    sql.ErrNoRows,
			expectClaim: false,
		},
		{
			name:      "query error",
			queryErr:  errors.New("query failed"),
			expectErr: true,
		},
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
			exp := mock.ExpectQuery("UPDATE build_steps")
			if tc.queryErr != nil {
				exp.WillReturnError(tc.queryErr)
			} else {
				exp.WillReturnRows(tc.rows)
			}

			step, claimed, err := repo.ClaimStepIfPending(context.Background(), "build-1", 0, nil, now)
			if tc.expectErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if claimed != tc.expectClaim {
				t.Fatalf("expected claimed=%v, got %v", tc.expectClaim, claimed)
			}
			if claimed && step.Status != domain.BuildStepStatusRunning {
				t.Fatalf("expected running status, got %q", step.Status)
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("unmet sql expectations: %v", err)
			}
		})
	}
}

func TestBuildRepository_CompleteStepIfRunning(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name            string
		queryErr        error
		rows            *sqlmock.Rows
		expectErr       bool
		expectCompleted bool
	}{
		{
			name: "success",
			rows: sqlmock.NewRows([]string{"id", "build_id", "step_index", "name", "command", "args", "env", "working_dir", "timeout_seconds", "status", "worker_id", "claim_token", "claimed_at", "lease_expires_at", "started_at", "finished_at", "exit_code", "stdout", "stderr", "error_message"}).
				AddRow("step-1", "build-1", 0, "default", "sh", "[\"-c\",\"echo ok\"]", "{}", ".", 30, "success", nil, nil, nil, nil, now, now, 0, "ok", "", nil),
			expectCompleted: true,
		},
		{
			name:            "no rows means no-op",
			queryErr:        sql.ErrNoRows,
			expectCompleted: false,
		},
		{
			name:      "query error",
			queryErr:  errors.New("query failed"),
			expectErr: true,
		},
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
			exp := mock.ExpectQuery("UPDATE build_steps")
			if tc.queryErr != nil {
				exp.WillReturnError(tc.queryErr)
			} else {
				exp.WillReturnRows(tc.rows)
			}

			exitCode := 0
			stdout := "ok"
			step, completed, err := repo.CompleteStepIfRunning(context.Background(), "build-1", 0, repository.StepUpdate{
				Status:     domain.BuildStepStatusSuccess,
				ExitCode:   &exitCode,
				Stdout:     &stdout,
				StartedAt:  &now,
				FinishedAt: &now,
			})
			if tc.expectErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if completed != tc.expectCompleted {
				t.Fatalf("expected completed=%v, got %v", tc.expectCompleted, completed)
			}
			if completed && step.Status != domain.BuildStepStatusSuccess {
				t.Fatalf("expected success status, got %q", step.Status)
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("unmet sql expectations: %v", err)
			}
		})
	}
}

func TestBuildRepository_CompleteStep_NonFinalSuccess(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sql mock: %v", err)
	}

	repo := NewBuildRepository(db)
	now := time.Now().UTC()
	exitCode := 0
	stdout := "ok"

	mock.ExpectBegin()
	mock.ExpectQuery("UPDATE build_steps").WillReturnRows(
		sqlmock.NewRows([]string{"id", "build_id", "step_index", "name", "command", "args", "env", "working_dir", "timeout_seconds", "status", "worker_id", "claim_token", "claimed_at", "lease_expires_at", "started_at", "finished_at", "exit_code", "stdout", "stderr", "error_message"}).
			AddRow("step-1", "build-1", 0, "first", "sh", "[\"-c\",\"echo ok\"]", "{}", ".", 0, "success", nil, nil, nil, nil, now, now, 0, "ok", "", nil),
	)
	mock.ExpectQuery("SELECT EXISTS").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectExec("UPDATE builds").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	result, err := repo.CompleteStep(context.Background(), repository.CompleteStepRequest{
		BuildID:   "build-1",
		StepIndex: 0,
		Update:    repository.StepUpdate{Status: domain.BuildStepStatusSuccess, ExitCode: &exitCode, Stdout: &stdout, StartedAt: &now, FinishedAt: &now},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.Outcome != repository.StepCompletionCompleted {
		t.Fatal("expected completion")
	}
	if result.Step.Status != domain.BuildStepStatusSuccess {
		t.Fatalf("expected success status, got %q", result.Step.Status)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestBuildRepository_CompleteStep_FinalSuccess(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sql mock: %v", err)
	}

	repo := NewBuildRepository(db)
	now := time.Now().UTC()
	exitCode := 0

	mock.ExpectBegin()
	mock.ExpectQuery("UPDATE build_steps").WillReturnRows(
		sqlmock.NewRows([]string{"id", "build_id", "step_index", "name", "command", "args", "env", "working_dir", "timeout_seconds", "status", "worker_id", "claim_token", "claimed_at", "lease_expires_at", "started_at", "finished_at", "exit_code", "stdout", "stderr", "error_message"}).
			AddRow("step-2", "build-1", 1, "second", "sh", "[\"-c\",\"echo ok\"]", "{}", ".", 0, "success", nil, nil, nil, nil, now, now, 0, "ok", "", nil),
	)
	mock.ExpectQuery("SELECT EXISTS").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectExec("UPDATE builds").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	result, err := repo.CompleteStep(context.Background(), repository.CompleteStepRequest{
		BuildID:   "build-1",
		StepIndex: 1,
		Update:    repository.StepUpdate{Status: domain.BuildStepStatusSuccess, ExitCode: &exitCode, StartedAt: &now, FinishedAt: &now},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.Outcome != repository.StepCompletionCompleted {
		t.Fatal("expected completion")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestBuildRepository_CompleteStep_FailedStep(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sql mock: %v", err)
	}

	repo := NewBuildRepository(db)
	now := time.Now().UTC()
	exitCode := 7
	stderr := "boom"
	errMsg := "boom"

	mock.ExpectBegin()
	mock.ExpectQuery("UPDATE build_steps").WillReturnRows(
		sqlmock.NewRows([]string{"id", "build_id", "step_index", "name", "command", "args", "env", "working_dir", "timeout_seconds", "status", "worker_id", "claim_token", "claimed_at", "lease_expires_at", "started_at", "finished_at", "exit_code", "stdout", "stderr", "error_message"}).
			AddRow("step-1", "build-1", 0, "first", "sh", "[\"-c\",\"echo boom\"]", "{}", ".", 0, "failed", nil, nil, nil, nil, now, now, 7, "", "boom", "boom"),
	)
	mock.ExpectExec("UPDATE builds").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	result, err := repo.CompleteStep(context.Background(), repository.CompleteStepRequest{
		BuildID:   "build-1",
		StepIndex: 0,
		Update:    repository.StepUpdate{Status: domain.BuildStepStatusFailed, ExitCode: &exitCode, Stderr: &stderr, ErrorMessage: &errMsg, StartedAt: &now, FinishedAt: &now},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.Outcome != repository.StepCompletionCompleted {
		t.Fatal("expected completion")
	}
	if result.Step.Status != domain.BuildStepStatusFailed {
		t.Fatalf("expected failed status, got %q", result.Step.Status)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestBuildRepository_CompleteStep_FailedStepNilErrorMessage_UsesTypedBuildUpdate(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sql mock: %v", err)
	}

	repo := NewBuildRepository(db)
	now := time.Now().UTC()
	exitCode := 1
	stderr := "command failed"

	mock.ExpectBegin()
	mock.ExpectQuery("UPDATE build_steps").WillReturnRows(
		sqlmock.NewRows([]string{"id", "build_id", "step_index", "name", "command", "args", "env", "working_dir", "timeout_seconds", "status", "worker_id", "claim_token", "claimed_at", "lease_expires_at", "started_at", "finished_at", "exit_code", "stdout", "stderr", "error_message"}).
			AddRow("step-1", "build-1", 0, "first", "sh", "[\"-c\",\"exit 1\"]", "{}", ".", 0, "failed", nil, nil, nil, nil, now, now, 1, "", "command failed", nil),
	)
	mock.ExpectExec("error_message = COALESCE\\(\\$2::text, error_message\\)").WithArgs("build-1", nil).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	result, err := repo.CompleteStep(context.Background(), repository.CompleteStepRequest{
		BuildID:   "build-1",
		StepIndex: 0,
		Update: repository.StepUpdate{
			Status:     domain.BuildStepStatusFailed,
			ExitCode:   &exitCode,
			Stderr:     &stderr,
			StartedAt:  &now,
			FinishedAt: &now,
		},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.Outcome != repository.StepCompletionCompleted {
		t.Fatalf("expected completion, got %q", result.Outcome)
	}
	if result.Step.Status != domain.BuildStepStatusFailed {
		t.Fatalf("expected failed status, got %q", result.Step.Status)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestBuildRepository_CompleteStep_DuplicateNoOp(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sql mock: %v", err)
	}

	repo := NewBuildRepository(db)
	now := time.Now().UTC()
	exitCode := 0

	mock.ExpectBegin()
	mock.ExpectQuery("UPDATE build_steps").WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("SELECT id, build_id, step_index").WillReturnRows(
		sqlmock.NewRows([]string{"id", "build_id", "step_index", "name", "command", "args", "env", "working_dir", "timeout_seconds", "status", "worker_id", "claim_token", "claimed_at", "lease_expires_at", "started_at", "finished_at", "exit_code", "stdout", "stderr", "error_message"}).
			AddRow("step-1", "build-1", 0, "first", "sh", "[\"-c\",\"echo ok\"]", "{}", ".", 0, "success", nil, nil, nil, nil, now, now, 0, "ok", "", nil),
	)
	mock.ExpectCommit()

	result, err := repo.CompleteStep(context.Background(), repository.CompleteStepRequest{
		BuildID:   "build-1",
		StepIndex: 0,
		Update:    repository.StepUpdate{Status: domain.BuildStepStatusSuccess, ExitCode: &exitCode, StartedAt: &now, FinishedAt: &now},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.Outcome != repository.StepCompletionDuplicateTerminal {
		t.Fatal("expected duplicate completion to be no-op")
	}
	if result.Step.Status != domain.BuildStepStatusSuccess {
		t.Fatalf("expected terminal step state, got %q", result.Step.Status)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestBuildRepository_CompleteStep_InvalidTransition(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sql mock: %v", err)
	}

	repo := NewBuildRepository(db)
	now := time.Now().UTC()
	exitCode := 0

	mock.ExpectBegin()
	mock.ExpectQuery("UPDATE build_steps").WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("SELECT id, build_id, step_index").WillReturnRows(
		sqlmock.NewRows([]string{"id", "build_id", "step_index", "name", "command", "args", "env", "working_dir", "timeout_seconds", "status", "worker_id", "claim_token", "claimed_at", "lease_expires_at", "started_at", "finished_at", "exit_code", "stdout", "stderr", "error_message"}).
			AddRow("step-1", "build-1", 0, "first", "sh", "[\"-c\",\"echo ok\"]", "{}", ".", 0, "pending", nil, nil, nil, nil, now, nil, nil, nil, nil, nil),
	)
	mock.ExpectRollback()

	result, err := repo.CompleteStep(context.Background(), repository.CompleteStepRequest{
		BuildID:   "build-1",
		StepIndex: 0,
		Update:    repository.StepUpdate{Status: domain.BuildStepStatusSuccess, ExitCode: &exitCode, StartedAt: &now, FinishedAt: &now},
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if result.Outcome != repository.StepCompletionInvalidTransition {
		t.Fatal("expected no completion on invalid transition")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestBuildRepository_CompleteStep_RollsBackOnAdvanceError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sql mock: %v", err)
	}

	repo := NewBuildRepository(db)
	now := time.Now().UTC()
	exitCode := 0

	mock.ExpectBegin()
	mock.ExpectQuery("UPDATE build_steps").WillReturnRows(
		sqlmock.NewRows([]string{"id", "build_id", "step_index", "name", "command", "args", "env", "working_dir", "timeout_seconds", "status", "worker_id", "claim_token", "claimed_at", "lease_expires_at", "started_at", "finished_at", "exit_code", "stdout", "stderr", "error_message"}).
			AddRow("step-1", "build-1", 0, "first", "sh", "[\"-c\",\"echo ok\"]", "{}", ".", 0, "success", nil, nil, nil, nil, now, now, 0, "ok", "", nil),
	)
	mock.ExpectQuery("SELECT EXISTS").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectExec("UPDATE builds").WillReturnError(errors.New("update current step failed"))
	mock.ExpectRollback()

	result, err := repo.CompleteStep(context.Background(), repository.CompleteStepRequest{
		BuildID:   "build-1",
		StepIndex: 0,
		Update:    repository.StepUpdate{Status: domain.BuildStepStatusSuccess, ExitCode: &exitCode, StartedAt: &now, FinishedAt: &now},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if result.Outcome != "" {
		t.Fatalf("expected empty outcome when completion returns error, got %q", result.Outcome)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestBuildRepository_CreateQueuedBuild(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sql mock: %v", err)
	}

	repo := NewBuildRepository(db)
	now := time.Now().UTC()

	mock.ExpectBegin()
	mock.ExpectQuery("INSERT INTO builds").WillReturnRows(
		sqlmock.NewRows([]string{"id", "project_id", "status", "created_at", "queued_at", "started_at", "finished_at", "current_step_index", "error_message", "pipeline_config_yaml", "pipeline_name", "pipeline_source", "repo_url", "ref", "commit_sha"}).
			AddRow("build-1", "project-1", "queued", now, now, nil, nil, 0, nil, nil, nil, nil, nil, nil, nil),
	)
	mock.ExpectExec("INSERT INTO build_steps").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO build_steps").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	build, err := repo.CreateQueuedBuild(context.Background(), domain.Build{
		ID:        "build-1",
		ProjectID: "project-1",
		Status:    domain.BuildStatusPending,
		CreatedAt: now,
	}, []domain.BuildStep{
		{ID: "step-1", BuildID: "build-1", StepIndex: 0, Name: "checkout", Status: domain.BuildStepStatusPending},
		{ID: "step-2", BuildID: "build-1", StepIndex: 1, Name: "test", Status: domain.BuildStepStatusPending},
	})
	if err != nil {
		t.Fatalf("create queued build failed: %v", err)
	}
	if build.Status != domain.BuildStatusQueued {
		t.Fatalf("expected queued status, got %q", build.Status)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestBuildRepository_ClaimPendingStep(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sql mock: %v", err)
	}

	repo := NewBuildRepository(db)
	now := time.Now().UTC()
	lease := now.Add(45 * time.Second)

	mock.ExpectQuery("UPDATE build_steps").WillReturnRows(
		sqlmock.NewRows([]string{"id", "build_id", "step_index", "name", "command", "args", "env", "working_dir", "timeout_seconds", "status", "worker_id", "claim_token", "claimed_at", "lease_expires_at", "started_at", "finished_at", "exit_code", "stdout", "stderr", "error_message"}).
			AddRow("step-1", "build-1", 0, "default", "sh", "[\"-c\",\"echo ok\"]", "{}", ".", 30, "running", "worker-a", "claim-a", now, lease, now, nil, nil, nil, nil, nil),
	)

	step, claimed, err := repo.ClaimPendingStep(context.Background(), "build-1", 0, repository.StepClaim{WorkerID: "worker-a", ClaimToken: "claim-a", ClaimedAt: now, LeaseExpiresAt: lease})
	if err != nil {
		t.Fatalf("claim pending step failed: %v", err)
	}
	if !claimed {
		t.Fatal("expected claim to succeed")
	}
	if step.ClaimToken == nil || *step.ClaimToken != "claim-a" {
		t.Fatalf("expected claim token claim-a, got %v", step.ClaimToken)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestBuildRepository_ReclaimExpiredStep(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sql mock: %v", err)
	}

	repo := NewBuildRepository(db)
	reclaimBefore := time.Now().UTC()
	lease := reclaimBefore.Add(45 * time.Second)

	mock.ExpectQuery("UPDATE build_steps").WillReturnRows(
		sqlmock.NewRows([]string{"id", "build_id", "step_index", "name", "command", "args", "env", "working_dir", "timeout_seconds", "status", "worker_id", "claim_token", "claimed_at", "lease_expires_at", "started_at", "finished_at", "exit_code", "stdout", "stderr", "error_message"}).
			AddRow("step-1", "build-1", 0, "default", "sh", "[\"-c\",\"echo ok\"]", "{}", ".", 30, "running", "worker-b", "claim-b", reclaimBefore, lease, reclaimBefore.Add(-time.Minute), nil, nil, nil, nil, nil),
	)

	step, reclaimed, err := repo.ReclaimExpiredStep(context.Background(), "build-1", 0, reclaimBefore, repository.StepClaim{WorkerID: "worker-b", ClaimToken: "claim-b", ClaimedAt: reclaimBefore, LeaseExpiresAt: lease})
	if err != nil {
		t.Fatalf("reclaim expired step failed: %v", err)
	}
	if !reclaimed {
		t.Fatal("expected reclaim to succeed")
	}
	if step.WorkerID == nil || *step.WorkerID != "worker-b" {
		t.Fatalf("expected worker-b owner, got %v", step.WorkerID)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestBuildRepository_ReclaimExpiredStep_NoMatchReturnsFalse(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sql mock: %v", err)
	}

	repo := NewBuildRepository(db)
	reclaimBefore := time.Now().UTC()
	lease := reclaimBefore.Add(45 * time.Second)

	// Simulates a terminal (failed/success) or otherwise non-running step where reclaim cannot apply.
	mock.ExpectQuery("UPDATE build_steps").WillReturnError(sql.ErrNoRows)

	_, reclaimed, err := repo.ReclaimExpiredStep(context.Background(), "build-1", 0, reclaimBefore, repository.StepClaim{WorkerID: "worker-b", ClaimToken: "claim-b", ClaimedAt: reclaimBefore, LeaseExpiresAt: lease})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if reclaimed {
		t.Fatal("expected reclaim to fail when step is no longer eligible")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestBuildRepository_CompleteStep_StaleClaim(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sql mock: %v", err)
	}

	repo := NewBuildRepository(db)
	now := time.Now().UTC()
	exitCode := 0

	mock.ExpectBegin()
	mock.ExpectQuery("UPDATE build_steps").WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("SELECT id, build_id, step_index").WillReturnRows(
		sqlmock.NewRows([]string{"id", "build_id", "step_index", "name", "command", "args", "env", "working_dir", "timeout_seconds", "status", "worker_id", "claim_token", "claimed_at", "lease_expires_at", "started_at", "finished_at", "exit_code", "stdout", "stderr", "error_message"}).
			AddRow("step-1", "build-1", 0, "first", "sh", "[\"-c\",\"echo ok\"]", "{}", ".", 0, "running", "worker-b", "claim-b", now, now.Add(time.Minute), now, nil, nil, nil, nil, nil),
	)
	mock.ExpectCommit()

	result, err := repo.CompleteStep(context.Background(), repository.CompleteStepRequest{
		BuildID:      "build-1",
		StepIndex:    0,
		ClaimToken:   "claim-a",
		RequireClaim: true,
		Update:       repository.StepUpdate{Status: domain.BuildStepStatusSuccess, ExitCode: &exitCode, StartedAt: &now, FinishedAt: &now},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.Outcome != repository.StepCompletionStaleClaim {
		t.Fatalf("expected stale claim outcome, got %q", result.Outcome)
	}
	if result.Step.Status != domain.BuildStepStatusRunning {
		t.Fatalf("expected running step to remain unchanged, got %q", result.Step.Status)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestBuildRepository_RenewStepLease_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sql mock: %v", err)
	}

	repo := NewBuildRepository(db)
	now := time.Now().UTC()
	extended := now.Add(time.Minute)

	mock.ExpectQuery("UPDATE build_steps").WillReturnRows(
		sqlmock.NewRows([]string{"id", "build_id", "step_index", "name", "command", "args", "env", "working_dir", "timeout_seconds", "status", "worker_id", "claim_token", "claimed_at", "lease_expires_at", "started_at", "finished_at", "exit_code", "stdout", "stderr", "error_message"}).
			AddRow("step-1", "build-1", 0, "first", "sh", "[\"-c\",\"echo ok\"]", "{}", ".", 0, "running", "worker-a", "claim-a", now, extended, now, nil, nil, nil, nil, nil),
	)

	step, outcome, err := repo.RenewStepLease(context.Background(), "build-1", 0, "claim-a", extended)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if outcome != repository.StepCompletionCompleted {
		t.Fatalf("expected completed outcome, got %q", outcome)
	}
	if step.LeaseExpiresAt == nil || !step.LeaseExpiresAt.Equal(extended) {
		t.Fatalf("expected extended lease %s, got %v", extended, step.LeaseExpiresAt)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestBuildRepository_RenewStepLease_StaleAndTerminal(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sql mock: %v", err)
	}

	repo := NewBuildRepository(db)
	now := time.Now().UTC()

	// stale claim path
	mock.ExpectQuery("UPDATE build_steps").WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("SELECT id, build_id, step_index").WillReturnRows(
		sqlmock.NewRows([]string{"id", "build_id", "step_index", "name", "command", "args", "env", "working_dir", "timeout_seconds", "status", "worker_id", "claim_token", "claimed_at", "lease_expires_at", "started_at", "finished_at", "exit_code", "stdout", "stderr", "error_message"}).
			AddRow("step-1", "build-1", 0, "first", "sh", "[\"-c\",\"echo ok\"]", "{}", ".", 0, "running", "worker-b", "claim-b", now, now.Add(time.Minute), now, nil, nil, nil, nil, nil),
	)

	_, outcome, err := repo.RenewStepLease(context.Background(), "build-1", 0, "claim-a", now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if outcome != repository.StepCompletionStaleClaim {
		t.Fatalf("expected stale outcome, got %q", outcome)
	}

	// terminal step path
	mock.ExpectQuery("UPDATE build_steps").WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("SELECT id, build_id, step_index").WillReturnRows(
		sqlmock.NewRows([]string{"id", "build_id", "step_index", "name", "command", "args", "env", "working_dir", "timeout_seconds", "status", "worker_id", "claim_token", "claimed_at", "lease_expires_at", "started_at", "finished_at", "exit_code", "stdout", "stderr", "error_message"}).
			AddRow("step-1", "build-1", 0, "first", "sh", "[\"-c\",\"echo ok\"]", "{}", ".", 0, "success", "worker-b", nil, nil, nil, now, now, 0, "ok", "", nil),
	)

	_, outcome, err = repo.RenewStepLease(context.Background(), "build-1", 0, "claim-b", now.Add(3*time.Minute))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if outcome != repository.StepCompletionDuplicateTerminal {
		t.Fatalf("expected duplicate terminal outcome, got %q", outcome)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}
