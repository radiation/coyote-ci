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

func TestExecutionJobRepository_CreateAndLookup(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sql mock: %v", err)
	}

	repo := NewExecutionJobRepository(db)
	now := time.Now().UTC()
	timeout := 30

	mock.ExpectBegin()
	mock.ExpectQuery("INSERT INTO build_jobs").WillReturnRows(
		sqlmock.NewRows(executionJobMockColumns).
			AddRow("job-1", "build-1", "step-1", nil, nil, "[]", "test", 0, 1, nil, nil, "queued", nil, "golang:1.24", ".", `["sh","-c","go test ./..."]`, `{"A":"1"}`, timeout, ".coyote/pipeline.yml", ".", "https://github.com/acme/repo.git", "abc123", "main", nil, nil, 1, "digest", `{"version":1}`, nil, nil, nil, now, nil, nil, nil, nil, nil, `[]`),
	)
	mock.ExpectCommit()

	created, err := repo.CreateJobsForBuild(context.Background(), []domain.ExecutionJob{{
		ID:               "job-1",
		BuildID:          "build-1",
		StepID:           "step-1",
		Name:             "test",
		StepIndex:        0,
		Status:           domain.ExecutionJobStatusQueued,
		Image:            "golang:1.24",
		WorkingDir:       ".",
		Command:          []string{"sh", "-c", "go test ./..."},
		Environment:      map[string]string{"A": "1"},
		TimeoutSeconds:   &timeout,
		SpecVersion:      1,
		ResolvedSpecJSON: `{"version":1}`,
		CreatedAt:        now,
		Source: domain.SourceSnapshotRef{
			RepositoryURL: "https://github.com/acme/repo.git",
			CommitSHA:     "abc123",
			RefName:       stringPtr("main"),
		},
	}})
	if err != nil {
		t.Fatalf("create jobs failed: %v", err)
	}
	if len(created) != 1 {
		t.Fatalf("expected one created job, got %d", len(created))
	}

	mock.ExpectQuery(`SELECT .* FROM build_jobs WHERE step_id = \$1`).WithArgs("step-1").WillReturnRows(
		sqlmock.NewRows(executionJobMockColumns).
			AddRow("job-1", "build-1", "step-1", nil, nil, "[]", "test", 0, 1, nil, nil, "queued", nil, "golang:1.24", ".", `["sh","-c","go test ./..."]`, `{"A":"1"}`, timeout, ".coyote/pipeline.yml", ".", "https://github.com/acme/repo.git", "abc123", "main", nil, nil, 1, "digest", `{"version":1}`, nil, nil, nil, now, nil, nil, nil, nil, nil, `[]`),
	)

	job, err := repo.GetJobByStepID(context.Background(), "step-1")
	if err != nil {
		t.Fatalf("get by step failed: %v", err)
	}
	if job.Source.CommitSHA != "abc123" {
		t.Fatalf("expected commit sha abc123, got %q", job.Source.CommitSHA)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestExecutionJobRepository_RenewAndComplete(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sql mock: %v", err)
	}

	repo := NewExecutionJobRepository(db)
	now := time.Now().UTC()
	lease := now.Add(time.Minute)
	finished := now.Add(2 * time.Minute)

	mock.ExpectQuery(`UPDATE build_jobs\s+SET claim_expires_at`).WithArgs("job-1", "claim-1", lease).WillReturnRows(
		sqlmock.NewRows(executionJobMockColumns).
			AddRow("job-1", "build-1", "step-1", nil, nil, "[]", "test", 0, 1, nil, nil, "running", nil, "golang:1.24", ".", `["sh","-c","go test ./..."]`, `{"A":"1"}`, nil, nil, nil, "https://github.com/acme/repo.git", "abc123", nil, nil, nil, 1, nil, `{}`, "claim-1", "worker-1", lease, now, now, nil, nil, nil, nil, `[]`),
	)

	_, outcome, err := repo.RenewJobLease(context.Background(), "job-1", "claim-1", lease)
	if err != nil {
		t.Fatalf("renew failed: %v", err)
	}
	if outcome != repository.StepCompletionCompleted {
		t.Fatalf("expected completed outcome, got %q", outcome)
	}

	mock.ExpectQuery(`UPDATE build_jobs\s+SET status = \$3`).WithArgs("job-1", "claim-1", "success", finished, nil, nil, 0, `[]`).WillReturnRows(
		sqlmock.NewRows(executionJobMockColumns).
			AddRow("job-1", "build-1", "step-1", nil, nil, "[]", "test", 0, 1, nil, nil, "success", nil, "golang:1.24", ".", `["sh","-c","go test ./..."]`, `{"A":"1"}`, nil, nil, nil, "https://github.com/acme/repo.git", "abc123", nil, nil, nil, 1, nil, `{}`, nil, nil, nil, now, now, finished, nil, nil, 0, `[]`),
	)

	_, outcome, err = repo.CompleteJobSuccess(context.Background(), "job-1", "claim-1", finished, 0, nil)
	if err != nil {
		t.Fatalf("complete success failed: %v", err)
	}
	if outcome != repository.StepCompletionCompleted {
		t.Fatalf("expected completed outcome, got %q", outcome)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestExecutionJobRepository_CompleteJobFailurePersistsFailureKind(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sql mock: %v", err)
	}

	repo := NewExecutionJobRepository(db)
	now := time.Now().UTC()
	finished := now.Add(time.Minute)
	exitCode := -1

	mock.ExpectQuery(`UPDATE build_jobs\s+SET status = \$3`).
		WithArgs("job-timeout", "claim-timeout", "failed", finished, "step timed out", domain.ExecutionFailureKindTimeout, &exitCode, `[]`).
		WillReturnRows(sqlmock.NewRows(executionJobMockColumns).
			AddRow("job-timeout", "build-1", "step-1", nil, nil, "[]", "test", 0, 1, nil, nil, "failed", nil, "golang:1.24", ".", `["sh","-c","go test ./..."]`, `{"A":"1"}`, nil, nil, nil, "https://github.com/acme/repo.git", "abc123", nil, nil, nil, 1, nil, `{}`, nil, nil, nil, now, now, finished, "step timed out", "timeout", -1, `[]`))
	mock.ExpectClose()

	job, outcome, completeErr := repo.CompleteJobFailure(context.Background(), "job-timeout", "claim-timeout", finished, "step timed out", domain.ExecutionFailureKindTimeout, &exitCode, nil)
	if completeErr != nil {
		t.Fatalf("complete failed job: %v", completeErr)
	}
	if outcome != repository.StepCompletionCompleted {
		t.Fatalf("expected completed outcome, got %q", outcome)
	}
	if job.FailureKind == nil || *job.FailureKind != domain.ExecutionFailureKindTimeout {
		t.Fatalf("expected timeout failure kind, got %v", job.FailureKind)
	}
	if closeErr := db.Close(); closeErr != nil {
		t.Fatalf("close sql mock database: %v", closeErr)
	}

	if expectationsErr := mock.ExpectationsWereMet(); expectationsErr != nil {
		t.Fatalf("unmet sql expectations: %v", expectationsErr)
	}
}

func TestExecutionJobRepository_ClaimNextRunnableJob(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sql mock: %v", err)
	}

	repo := NewExecutionJobRepository(db)
	now := time.Now().UTC()
	lease := now.Add(time.Minute)

	mock.ExpectQuery(`WITH candidate AS \(\s*SELECT bj\.id\s*FROM build_jobs AS bj`).WithArgs(now, "worker-1", "claim-1", lease).WillReturnRows(
		sqlmock.NewRows(executionJobMockColumns).
			AddRow("job-1", "build-1", "step-1", nil, nil, "[]", "test", 0, 1, nil, nil, "running", nil, "golang:1.24", ".", `["sh","-c","go test ./..."]`, `{"A":"1"}`, 30, ".coyote/pipeline.yml", ".", "https://github.com/acme/repo.git", "abc123", "main", nil, nil, 1, "digest", `{"version":1}`, "claim-1", "worker-1", lease, now, now, nil, nil, nil, nil, `[]`),
	)

	job, claimed, err := repo.ClaimNextRunnableJob(context.Background(), repository.StepClaim{
		WorkerID:       "worker-1",
		ClaimToken:     "claim-1",
		ClaimedAt:      now,
		LeaseExpiresAt: lease,
	})
	if err != nil {
		t.Fatalf("claim next runnable job failed: %v", err)
	}
	if !claimed {
		t.Fatal("expected claim to succeed")
	}
	if job.ID != "job-1" {
		t.Fatalf("expected claimed job-1, got %q", job.ID)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestExecutionJobRepository_ClaimNextRunnableJob_UsesBuildPriorityOrdering(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sql mock: %v", err)
	}

	repo := NewExecutionJobRepository(db)
	now := time.Now().UTC()
	lease := now.Add(time.Minute)

	mock.ExpectQuery(`ORDER BY\s+b\.priority DESC,\s+COALESCE\(b\.queued_at, b\.created_at\) ASC,\s+bj\.created_at ASC,\s+bj\.step_index ASC,\s+bj\.attempt_number ASC,\s+bj\.id ASC`).
		WithArgs(now, "worker-1", "claim-1", lease).
		WillReturnRows(sqlmock.NewRows(executionJobMockColumns).
			AddRow("job-priority", "build-priority", "step-1", nil, nil, "[]", "test", 0, 1, nil, nil, "running", nil, "golang:1.24", ".", `["sh","-c","go test ./..."]`, `{"A":"1"}`, 30, ".coyote/pipeline.yml", ".", "https://github.com/acme/repo.git", "abc123", "main", nil, nil, 1, "digest", `{"version":1}`, "claim-1", "worker-1", lease, now, now, nil, nil, nil, nil, `[]`))

	job, claimed, err := repo.ClaimNextRunnableJob(context.Background(), repository.StepClaim{
		WorkerID:       "worker-1",
		ClaimToken:     "claim-1",
		ClaimedAt:      now,
		LeaseExpiresAt: lease,
	})
	if err != nil {
		t.Fatalf("claim next runnable job failed: %v", err)
	}
	if !claimed {
		t.Fatal("expected claim to succeed")
	}
	if job.ID != "job-priority" {
		t.Fatalf("expected job-priority, got %q", job.ID)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestExecutionJobRepository_ClaimJobByStepID_RequiresRunningBuild(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sql mock: %v", err)
	}

	repo := NewExecutionJobRepository(db)
	now := time.Now().UTC()
	lease := now.Add(time.Minute)

	mock.ExpectQuery(`INNER JOIN builds AS b ON b\.id = bj\.build_id[\s\S]*AND b\.status = 'running'`).
		WithArgs("step-1", "worker-1", "claim-1", lease, now).
		WillReturnError(sql.ErrNoRows)

	_, claimed, err := repo.ClaimJobByStepID(context.Background(), "step-1", repository.StepClaim{
		WorkerID:       "worker-1",
		ClaimToken:     "claim-1",
		ClaimedAt:      now,
		LeaseExpiresAt: lease,
	})
	if err != nil {
		t.Fatalf("claim job by step failed: %v", err)
	}
	if claimed {
		t.Fatal("expected claim to be skipped when no running build candidate exists")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestExecutionJobRepository_CompleteJobSuccess_CanceledJobIsDuplicateTerminal(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sql mock: %v", err)
	}

	repo := NewExecutionJobRepository(db)
	now := time.Now().UTC()
	finished := now.Add(2 * time.Minute)

	mock.ExpectQuery(`UPDATE build_jobs\s+SET status = \$3`).
		WithArgs("job-1", "claim-1", "success", finished, nil, nil, 0, `[]`).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`SELECT .* FROM build_jobs WHERE id = \$1`).WithArgs("job-1").WillReturnRows(
		sqlmock.NewRows(executionJobMockColumns).
			AddRow("job-1", "build-1", "step-1", nil, nil, "[]", "test", 0, 1, nil, nil, "canceled", nil, "golang:1.24", ".", `["sh","-c","go test ./..."]`, `{"A":"1"}`, nil, nil, nil, "https://github.com/acme/repo.git", "abc123", nil, nil, nil, 1, nil, `{}`, nil, nil, nil, now, now, finished, "operator canceled", nil, nil, `[]`),
	)

	job, outcome, err := repo.CompleteJobSuccess(context.Background(), "job-1", "claim-1", finished, 0, nil)
	if err != nil {
		t.Fatalf("complete job success failed: %v", err)
	}
	if outcome != repository.StepCompletionDuplicateTerminal {
		t.Fatalf("expected duplicate terminal outcome, got %q", outcome)
	}
	if job.Status != domain.ExecutionJobStatusCanceled {
		t.Fatalf("expected job to remain canceled, got %q", job.Status)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestExecutionJobRepository_CompleteSuccessfulStepAndJob_CommitsStepAndJobSuccess(t *testing.T) {
	db, mock, setupErr := sqlmock.New()
	if setupErr != nil {
		t.Fatalf("failed to create sql mock: %v", setupErr)
	}

	repo := NewExecutionJobRepository(db)
	now := time.Now().UTC()
	claimExpiresAt := now.Add(time.Minute)
	finishedAt := now.Add(2 * time.Minute)
	claimToken := "claim-1"
	exitCode := 0

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .* FROM build_jobs WHERE id = \$1 FOR UPDATE`).WithArgs("job-1").WillReturnRows(atomicCompletionJobRows(now, claimExpiresAt, "running", claimToken))
	mock.ExpectQuery("UPDATE build_steps").WillReturnRows(atomicCompletionStepRows(now, "success", &exitCode))
	mock.ExpectQuery(`UPDATE build_jobs\s+SET status = 'success'`).WithArgs("job-1", finishedAt, exitCode).WillReturnRows(atomicCompletionJobRows(now, time.Time{}, "success", ""))
	mock.ExpectExec("UPDATE builds").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT\\s+COUNT\\(\\*\\)::int AS total_count").WithArgs("build-1").WillReturnRows(sqlmock.NewRows([]string{"total_count", "success_count", "failed_count", "pending_count", "running_count"}).AddRow(2, 1, 0, 1, 0))
	mock.ExpectQuery("SELECT EXISTS").WithArgs("build-1").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectCommit()

	result, job, outcome, completeErr := repo.CompleteSuccessfulStepAndJob(context.Background(), atomicCompletionRequest(claimToken, finishedAt, exitCode))
	if completeErr != nil {
		t.Fatalf("complete successful step and job: %v", completeErr)
	}
	if outcome != repository.StepCompletionCompleted || result.Step.Status != domain.BuildStepStatusSuccess || job.Status != domain.ExecutionJobStatusSuccess {
		t.Fatalf("expected committed step/job success, result=%#v job=%#v outcome=%q", result, job, outcome)
	}

	if expectationsErr := mock.ExpectationsWereMet(); expectationsErr != nil {
		t.Fatalf("unmet sql expectations: %v", expectationsErr)
	}
}

func TestExecutionJobRepository_CompleteFailedStepAndJob_CommitsStepAndJobFailure(t *testing.T) {
	db, mock, setupErr := sqlmock.New()
	if setupErr != nil {
		t.Fatalf("failed to create sql mock: %v", setupErr)
	}
	repo := NewExecutionJobRepository(db)

	now := time.Now().UTC()
	claimExpiresAt := now.Add(time.Minute)
	finishedAt := now.Add(2 * time.Minute)
	claimToken := "claim-1"
	exitCode := 1
	request := repository.CompleteFailedStepAndJobRequest{JobID: "job-1", ClaimToken: claimToken, FinishedAt: finishedAt, ErrorMessage: "step timed out", FailureKind: domain.ExecutionFailureKindTimeout, ExitCode: &exitCode, StepRequest: repository.CompleteStepRequest{BuildID: "build-1", StepIndex: 0, ClaimToken: claimToken, RequireClaim: true, Update: repository.StepUpdate{Status: domain.BuildStepStatusFailed, ExitCode: &exitCode, StartedAt: &now, FinishedAt: &finishedAt}}}

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .* FROM build_jobs WHERE id = \$1 FOR UPDATE`).WithArgs("job-1").WillReturnRows(atomicCompletionJobRows(now, claimExpiresAt, "running", claimToken))
	mock.ExpectQuery("UPDATE build_steps").WillReturnRows(atomicCompletionStepRows(now, "failed", &exitCode))
	mock.ExpectQuery(`UPDATE build_jobs\s+SET status = 'failed'`).WithArgs("job-1", finishedAt, "step timed out", domain.ExecutionFailureKindTimeout, &exitCode).WillReturnRows(atomicCompletionJobRows(now, time.Time{}, "failed", ""))
	mock.ExpectQuery("SELECT\\s+COUNT\\(\\*\\)::int AS total_count").WithArgs("build-1").WillReturnRows(sqlmock.NewRows([]string{"total_count", "success_count", "failed_count", "pending_count", "running_count"}).AddRow(2, 0, 1, 1, 0))
	mock.ExpectQuery("SELECT EXISTS").WithArgs("build-1").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectExec("UPDATE builds").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	result, job, outcome, completeErr := repo.CompleteFailedStepAndJob(context.Background(), request)
	if completeErr != nil || outcome != repository.StepCompletionCompleted || result.Step.Status != domain.BuildStepStatusFailed || job.Status != domain.ExecutionJobStatusFailed {
		t.Fatalf("expected committed step/job failure, result=%#v job=%#v outcome=%q err=%v", result, job, outcome, completeErr)
	}
	if expectationsErr := mock.ExpectationsWereMet(); expectationsErr != nil {
		t.Fatalf("unmet sql expectations: %v", expectationsErr)
	}
}

func TestExecutionJobRepository_CompleteSuccessfulStepAndJob_RollsBackMutationFailures(t *testing.T) {
	tests := []struct {
		name      string
		configure func(sqlmock.Sqlmock, time.Time, time.Time)
	}{
		{
			name: "step mutation failure",
			configure: func(mock sqlmock.Sqlmock, now time.Time, claimExpiresAt time.Time) {
				mock.ExpectQuery(`SELECT .* FROM build_jobs WHERE id = \$1 FOR UPDATE`).WithArgs("job-1").WillReturnRows(atomicCompletionJobRows(now, claimExpiresAt, "running", "claim-1"))
				mock.ExpectQuery("UPDATE build_steps").WillReturnError(errors.New("step update failed"))
			},
		},
		{
			name: "execution job mutation failure",
			configure: func(mock sqlmock.Sqlmock, now time.Time, claimExpiresAt time.Time) {
				exitCode := 0
				mock.ExpectQuery(`SELECT .* FROM build_jobs WHERE id = \$1 FOR UPDATE`).WithArgs("job-1").WillReturnRows(atomicCompletionJobRows(now, claimExpiresAt, "running", "claim-1"))
				mock.ExpectQuery("UPDATE build_steps").WillReturnRows(atomicCompletionStepRows(now, "success", &exitCode))
				mock.ExpectQuery(`UPDATE build_jobs\s+SET status = 'success'`).WillReturnError(errors.New("job update failed"))
			},
		},
	}

	for _, testCase := range tests {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			db, mock, setupErr := sqlmock.New()
			if setupErr != nil {
				t.Fatalf("failed to create sql mock: %v", setupErr)
			}
			now := time.Now().UTC()
			claimExpiresAt := now.Add(time.Minute)
			mock.ExpectBegin()
			testCase.configure(mock, now, claimExpiresAt)
			mock.ExpectRollback()

			_, _, _, completeErr := NewExecutionJobRepository(db).CompleteSuccessfulStepAndJob(context.Background(), atomicCompletionRequest("claim-1", now.Add(2*time.Minute), 0))
			if completeErr == nil {
				t.Fatal("expected completion error")
			}
			if expectationsErr := mock.ExpectationsWereMet(); expectationsErr != nil {
				t.Fatalf("unmet sql expectations: %v", expectationsErr)
			}
		})
	}
}

func TestExecutionJobRepository_CompleteSuccessfulStepAndJob_RejectsInvalidClaims(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name           string
		rows           *sqlmock.Rows
		expectNotFound bool
	}{
		{name: "missing job", rows: nil, expectNotFound: true},
		{name: "wrong token", rows: atomicCompletionJobRows(now, now.Add(time.Minute), "running", "other-claim")},
		{name: "expired claim", rows: atomicCompletionJobRows(now, now.Add(-time.Minute), "running", "claim-1")},
	}

	for _, testCase := range tests {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			db, mock, setupErr := sqlmock.New()
			if setupErr != nil {
				t.Fatalf("failed to create sql mock: %v", setupErr)
			}
			mock.ExpectBegin()
			expectation := mock.ExpectQuery(`SELECT .* FROM build_jobs WHERE id = \$1 FOR UPDATE`).WithArgs("job-1")
			if testCase.expectNotFound {
				expectation.WillReturnError(sql.ErrNoRows)
				mock.ExpectRollback()
			} else {
				expectation.WillReturnRows(testCase.rows)
				mock.ExpectCommit()
			}

			_, _, outcome, completeErr := NewExecutionJobRepository(db).CompleteSuccessfulStepAndJob(context.Background(), atomicCompletionRequest("claim-1", now.Add(2*time.Minute), 0))
			if testCase.expectNotFound {
				if !errors.Is(completeErr, repository.ErrExecutionJobNotFound) {
					t.Fatalf("expected execution job not found, got %v", completeErr)
				}
			} else if completeErr != nil || outcome != repository.StepCompletionStaleClaim {
				t.Fatalf("expected stale-claim rejection, outcome=%q err=%v", outcome, completeErr)
			}
			if expectationsErr := mock.ExpectationsWereMet(); expectationsErr != nil {
				t.Fatalf("unmet sql expectations: %v", expectationsErr)
			}
		})
	}
}

func TestExecutionJobRepository_CompleteSuccessfulStepAndJob_DuplicateTerminalAndCommitFailure(t *testing.T) {
	t.Run("duplicate terminal", func(t *testing.T) {
		db, mock, setupErr := sqlmock.New()
		if setupErr != nil {
			t.Fatalf("failed to create sql mock: %v", setupErr)
		}
		now := time.Now().UTC()
		mock.ExpectBegin()
		mock.ExpectQuery(`SELECT .* FROM build_jobs WHERE id = \$1 FOR UPDATE`).WithArgs("job-1").WillReturnRows(atomicCompletionJobRows(now, time.Time{}, "success", ""))
		mock.ExpectCommit()

		_, job, outcome, completeErr := NewExecutionJobRepository(db).CompleteSuccessfulStepAndJob(context.Background(), atomicCompletionRequest("claim-1", now, 0))
		if completeErr != nil || outcome != repository.StepCompletionDuplicateTerminal || job.Status != domain.ExecutionJobStatusSuccess {
			t.Fatalf("expected duplicate terminal success, job=%#v outcome=%q err=%v", job, outcome, completeErr)
		}
		if expectationsErr := mock.ExpectationsWereMet(); expectationsErr != nil {
			t.Fatalf("unmet sql expectations: %v", expectationsErr)
		}
	})

	t.Run("commit failure", func(t *testing.T) {
		db, mock, setupErr := sqlmock.New()
		if setupErr != nil {
			t.Fatalf("failed to create sql mock: %v", setupErr)
		}
		now := time.Now().UTC()
		claimExpiresAt := now.Add(time.Minute)
		exitCode := 0
		mock.ExpectBegin()
		mock.ExpectQuery(`SELECT .* FROM build_jobs WHERE id = \$1 FOR UPDATE`).WithArgs("job-1").WillReturnRows(atomicCompletionJobRows(now, claimExpiresAt, "running", "claim-1"))
		mock.ExpectQuery("UPDATE build_steps").WillReturnRows(atomicCompletionStepRows(now, "success", &exitCode))
		mock.ExpectQuery(`UPDATE build_jobs\s+SET status = 'success'`).WillReturnRows(atomicCompletionJobRows(now, time.Time{}, "success", ""))
		mock.ExpectExec("UPDATE builds").WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectQuery("SELECT\\s+COUNT\\(\\*\\)::int AS total_count").WithArgs("build-1").WillReturnRows(sqlmock.NewRows([]string{"total_count", "success_count", "failed_count", "pending_count", "running_count"}).AddRow(2, 1, 0, 1, 0))
		mock.ExpectQuery("SELECT EXISTS").WithArgs("build-1").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
		mock.ExpectCommit().WillReturnError(errors.New("commit failed"))

		_, _, _, completeErr := NewExecutionJobRepository(db).CompleteSuccessfulStepAndJob(context.Background(), atomicCompletionRequest("claim-1", now.Add(2*time.Minute), exitCode))
		if completeErr == nil || completeErr.Error() != "commit failed" {
			t.Fatalf("expected commit failure, got %v", completeErr)
		}
		if expectationsErr := mock.ExpectationsWereMet(); expectationsErr != nil {
			t.Fatalf("unmet sql expectations: %v", expectationsErr)
		}
	})
}

func TestExecutionJobRepository_CompleteSuccessfulStepAndJob_ReturnsPreMutationTransactionErrors(t *testing.T) {
	tests := []struct {
		name      string
		configure func(sqlmock.Sqlmock)
	}{
		{
			name: "begin failure",
			configure: func(mock sqlmock.Sqlmock) {
				mock.ExpectBegin().WillReturnError(errors.New("begin failed"))
			},
		},
		{
			name: "authoritative job lookup failure",
			configure: func(mock sqlmock.Sqlmock) {
				mock.ExpectBegin()
				mock.ExpectQuery(`SELECT .* FROM build_jobs WHERE id = \$1 FOR UPDATE`).WithArgs("job-1").WillReturnError(errors.New("job lookup failed"))
				mock.ExpectRollback()
			},
		},
	}

	for _, testCase := range tests {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			db, mock, setupErr := sqlmock.New()
			if setupErr != nil {
				t.Fatalf("failed to create sql mock: %v", setupErr)
			}
			testCase.configure(mock)

			_, _, _, completeErr := NewExecutionJobRepository(db).CompleteSuccessfulStepAndJob(context.Background(), atomicCompletionRequest("claim-1", time.Now().UTC(), 0))
			if completeErr == nil {
				t.Fatal("expected transaction error")
			}
			if expectationsErr := mock.ExpectationsWereMet(); expectationsErr != nil {
				t.Fatalf("unmet sql expectations: %v", expectationsErr)
			}
		})
	}
}

func TestExecutionJobRepository_CompleteSuccessfulStepAndJob_StepConflictDoesNotFinalizeJob(t *testing.T) {
	db, mock, setupErr := sqlmock.New()
	if setupErr != nil {
		t.Fatalf("failed to create sql mock: %v", setupErr)
	}
	now := time.Now().UTC()
	claimExpiresAt := now.Add(time.Minute)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .* FROM build_jobs WHERE id = \$1 FOR UPDATE`).WithArgs("job-1").WillReturnRows(atomicCompletionJobRows(now, claimExpiresAt, "running", "claim-1"))
	mock.ExpectQuery("UPDATE build_steps").WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("SELECT id, build_id, step_index").WithArgs("build-1", 0).WillReturnRows(atomicCompletionStepRows(now, "running", nil))
	mock.ExpectCommit()

	result, job, outcome, completeErr := NewExecutionJobRepository(db).CompleteSuccessfulStepAndJob(context.Background(), atomicCompletionRequest("claim-1", now.Add(time.Minute), 0))
	if completeErr != nil || outcome != repository.StepCompletionStaleClaim || result.Step.Status != domain.BuildStepStatusRunning || job.Status != domain.ExecutionJobStatusRunning {
		t.Fatalf("expected stale step conflict without job finalization, result=%#v job=%#v outcome=%q err=%v", result, job, outcome, completeErr)
	}
	if expectationsErr := mock.ExpectationsWereMet(); expectationsErr != nil {
		t.Fatalf("unmet sql expectations: %v", expectationsErr)
	}
}

func atomicCompletionRequest(claimToken string, finishedAt time.Time, exitCode int) repository.CompleteSuccessfulStepAndJobRequest {
	return repository.CompleteSuccessfulStepAndJobRequest{
		JobID: "job-1", ClaimToken: claimToken, FinishedAt: finishedAt, ExitCode: exitCode,
		StepRequest: repository.CompleteStepRequest{BuildID: "build-1", StepIndex: 0, ClaimToken: claimToken, RequireClaim: true, Update: repository.StepUpdate{Status: domain.BuildStepStatusSuccess, StartedAt: &finishedAt, FinishedAt: &finishedAt, ExitCode: &exitCode}},
	}
}

func atomicCompletionJobRows(now time.Time, claimExpiresAt time.Time, status string, claimToken string) *sqlmock.Rows {
	var claimTokenValue any
	var claimedBy any
	var leaseExpiresAt any
	if claimToken != "" {
		claimTokenValue = claimToken
		claimedBy = "worker-1"
		leaseExpiresAt = claimExpiresAt
	}
	return sqlmock.NewRows(executionJobMockColumns).AddRow("job-1", "build-1", "step-1", nil, nil, "[]", "test", 0, 1, nil, nil, status, nil, "golang:1.24", ".", `["sh","-c","go test ./..."]`, `{"A":"1"}`, nil, nil, nil, "https://github.com/acme/repo.git", "abc123", nil, nil, nil, 1, nil, `{}`, claimTokenValue, claimedBy, leaseExpiresAt, now, now, nil, nil, nil, nil, `[]`)
}

func atomicCompletionStepRows(now time.Time, status string, exitCode *int) *sqlmock.Rows {
	return sqlmock.NewRows(stepMockColumns).AddRow("step-1", "build-1", 0, nil, nil, "[]", "test", "", "sh", `["-c","go test ./..."]`, `{}`, ".", 0, status, nil, nil, nil, nil, now, now, exitCode, nil, nil, nil, "[]", nil, nil, nil, "external", nil, nil)
}

func stringPtr(value string) *string {
	return &value
}
