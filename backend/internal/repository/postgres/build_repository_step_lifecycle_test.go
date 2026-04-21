package postgres

// Postgres BuildRepository step lifecycle tests:
// - claiming
// - completion transitions
// - lease renewal outcomes

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

// Claim and complete entry-point behavior.
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
			rows: sqlmock.NewRows(stepMockColumns).
				AddRow("step-1", "build-1", 0, nil, nil, "[]", "default", "", "sh", "[\"-c\",\"echo ok\"]", "{}", ".", 30, "running", nil, nil, nil, nil, now, nil, nil, nil, nil, nil, "[]", nil, nil, nil, "external", nil, nil),
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
			rows: sqlmock.NewRows(stepMockColumns).
				AddRow("step-1", "build-1", 0, nil, nil, "[]", "default", "", "sh", "[\"-c\",\"echo ok\"]", "{}", ".", 30, "success", nil, nil, nil, nil, now, now, 0, "ok", "", nil, "[]", nil, nil, nil, "external", nil, nil),
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
		sqlmock.NewRows(stepMockColumns).
			AddRow("step-1", "build-1", 0, nil, nil, "[]", "first", "", "sh", "[\"-c\",\"echo ok\"]", "{}", ".", 0, "success", nil, nil, nil, nil, now, now, 0, "ok", "", nil, "[]", nil, nil, nil, "external", nil, nil),
	)
	mock.ExpectExec("UPDATE builds").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT\\s+COUNT\\(\\*\\)::int AS total_count").WithArgs("build-1").WillReturnRows(
		sqlmock.NewRows([]string{"total_count", "success_count", "failed_count", "pending_count", "running_count"}).
			AddRow(2, 1, 0, 1, 0),
	)
	mock.ExpectQuery("SELECT EXISTS").WithArgs("build-1").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
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
		sqlmock.NewRows(stepMockColumns).
			AddRow("step-2", "build-1", 1, nil, nil, "[]", "second", "", "sh", "[\"-c\",\"echo ok\"]", "{}", ".", 0, "success", nil, nil, nil, nil, now, now, 0, "ok", "", nil, "[]", nil, nil, nil, "external", nil, nil),
	)
	mock.ExpectExec("UPDATE builds").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT\\s+COUNT\\(\\*\\)::int AS total_count").WithArgs("build-1").WillReturnRows(
		sqlmock.NewRows([]string{"total_count", "success_count", "failed_count", "pending_count", "running_count"}).
			AddRow(2, 2, 0, 0, 0),
	)
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
		sqlmock.NewRows(stepMockColumns).
			AddRow("step-1", "build-1", 0, nil, nil, "[]", "first", "", "sh", "[\"-c\",\"echo boom\"]", "{}", ".", 0, "failed", nil, nil, nil, nil, now, now, 7, "", "boom", "boom", "[]", nil, nil, nil, "external", nil, nil),
	)
	mock.ExpectQuery("SELECT\\s+COUNT\\(\\*\\)::int AS total_count").WithArgs("build-1").WillReturnRows(
		sqlmock.NewRows([]string{"total_count", "success_count", "failed_count", "pending_count", "running_count"}).
			AddRow(2, 0, 1, 1, 0),
	)
	mock.ExpectQuery("SELECT EXISTS").WithArgs("build-1").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
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
		sqlmock.NewRows(stepMockColumns).
			AddRow("step-1", "build-1", 0, nil, nil, "[]", "first", "", "sh", "[\"-c\",\"exit 1\"]", "{}", ".", 0, "failed", nil, nil, nil, nil, now, now, 1, "", "command failed", nil, "[]", nil, nil, nil, "external", nil, nil),
	)
	mock.ExpectQuery("SELECT\\s+COUNT\\(\\*\\)::int AS total_count").WithArgs("build-1").WillReturnRows(
		sqlmock.NewRows([]string{"total_count", "success_count", "failed_count", "pending_count", "running_count"}).
			AddRow(2, 0, 1, 1, 0),
	)
	mock.ExpectQuery("SELECT EXISTS").WithArgs("build-1").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectExec("error_message = COALESCE\\(\\$2::text, error_message\\)").WithArgs("build-1", "build cannot make further progress toward success").WillReturnResult(sqlmock.NewResult(0, 1))
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
		sqlmock.NewRows(stepMockColumns).
			AddRow("step-1", "build-1", 0, nil, nil, "[]", "first", "", "sh", "[\"-c\",\"echo ok\"]", "{}", ".", 0, "success", nil, nil, nil, nil, now, now, 0, "ok", "", nil, "[]", nil, nil, nil, "external", nil, nil),
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
		sqlmock.NewRows(stepMockColumns).
			AddRow("step-1", "build-1", 0, nil, nil, "[]", "first", "", "sh", "[\"-c\",\"echo ok\"]", "{}", ".", 0, "pending", nil, nil, nil, nil, now, nil, nil, nil, nil, nil, "[]", nil, nil, nil, "external", nil, nil),
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
		sqlmock.NewRows(stepMockColumns).
			AddRow("step-1", "build-1", 0, nil, nil, "[]", "first", "", "sh", "[\"-c\",\"echo ok\"]", "{}", ".", 0, "success", nil, nil, nil, nil, now, now, 0, "ok", "", nil, "[]", nil, nil, nil, "external", nil, nil),
	)
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
		sqlmock.NewRows([]string{"id", "project_id", "job_id", "status", "created_at", "queued_at", "started_at", "finished_at", "current_step_index", "attempt_number", "rerun_of_build_id", "rerun_from_step_index", "error_message", "pipeline_config_yaml", "pipeline_name", "pipeline_source", "pipeline_path", "repo_url", "ref", "commit_sha", "trigger_kind", "scm_provider", "event_type", "trigger_repository_owner", "trigger_repository_name", "trigger_repository_url", "trigger_raw_ref", "trigger_ref", "trigger_ref_type", "trigger_ref_name", "trigger_deleted", "trigger_commit_sha", "trigger_delivery_id", "trigger_actor", "requested_image_ref", "resolved_image_ref", "image_source_kind", "managed_image_id", "managed_image_version_id"}).
			AddRow("build-1", "project-1", nil, "queued", now, now, nil, nil, 0, 1, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, "manual", nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, "external", nil, nil),
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
		sqlmock.NewRows(stepMockColumns).
			AddRow("step-1", "build-1", 0, nil, nil, "[]", "default", "", "sh", "[\"-c\",\"echo ok\"]", "{}", ".", 30, "running", "worker-a", "claim-a", now, lease, now, nil, nil, nil, nil, nil, "[]", nil, nil, nil, "external", nil, nil),
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
		sqlmock.NewRows(stepMockColumns).
			AddRow("step-1", "build-1", 0, nil, nil, "[]", "default", "", "sh", "[\"-c\",\"echo ok\"]", "{}", ".", 30, "running", "worker-b", "claim-b", reclaimBefore, lease, reclaimBefore.Add(-time.Minute), nil, nil, nil, nil, nil, "[]", nil, nil, nil, "external", nil, nil),
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
		sqlmock.NewRows(stepMockColumns).
			AddRow("step-1", "build-1", 0, nil, nil, "[]", "first", "", "sh", "[\"-c\",\"echo ok\"]", "{}", ".", 0, "running", "worker-b", "claim-b", now, now.Add(time.Minute), now, nil, nil, nil, nil, nil, "[]", nil, nil, nil, "external", nil, nil),
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
		sqlmock.NewRows(stepMockColumns).
			AddRow("step-1", "build-1", 0, nil, nil, "[]", "first", "", "sh", "[\"-c\",\"echo ok\"]", "{}", ".", 0, "running", "worker-a", "claim-a", now, extended, now, nil, nil, nil, nil, nil, "[]", nil, nil, nil, "external", nil, nil),
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
		sqlmock.NewRows(stepMockColumns).
			AddRow("step-1", "build-1", 0, nil, nil, "[]", "first", "", "sh", "[\"-c\",\"echo ok\"]", "{}", ".", 0, "running", "worker-b", "claim-b", now, now.Add(time.Minute), now, nil, nil, nil, nil, nil, "[]", nil, nil, nil, "external", nil, nil),
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
		sqlmock.NewRows(stepMockColumns).
			AddRow("step-1", "build-1", 0, nil, nil, "[]", "first", "", "sh", "[\"-c\",\"echo ok\"]", "{}", ".", 0, "success", "worker-b", nil, nil, nil, now, now, 0, "ok", "", nil, "[]", nil, nil, nil, "external", nil, nil),
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
