package postgres

import (
	"context"
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
		sqlmock.NewRows([]string{"id", "build_id", "step_id", "name", "step_index", "status", "queue_name", "image", "working_dir", "command_json", "env_json", "timeout_seconds", "pipeline_file_path", "context_dir", "source_repo_url", "source_commit_sha", "source_ref_name", "source_archive_uri", "source_archive_digest", "spec_version", "spec_digest", "resolved_spec_json", "claim_token", "claimed_by", "claim_expires_at", "created_at", "started_at", "finished_at", "error_message", "exit_code", "output_refs_json"}).
			AddRow("job-1", "build-1", "step-1", "test", 0, "queued", nil, "golang:1.24", ".", `["sh","-c","go test ./..."]`, `{"A":"1"}`, timeout, ".coyote/pipeline.yml", ".", "https://github.com/acme/repo.git", "abc123", "main", nil, nil, 1, "digest", `{"version":1}`, nil, nil, nil, now, nil, nil, nil, nil, `[]`),
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
		sqlmock.NewRows([]string{"id", "build_id", "step_id", "name", "step_index", "status", "queue_name", "image", "working_dir", "command_json", "env_json", "timeout_seconds", "pipeline_file_path", "context_dir", "source_repo_url", "source_commit_sha", "source_ref_name", "source_archive_uri", "source_archive_digest", "spec_version", "spec_digest", "resolved_spec_json", "claim_token", "claimed_by", "claim_expires_at", "created_at", "started_at", "finished_at", "error_message", "exit_code", "output_refs_json"}).
			AddRow("job-1", "build-1", "step-1", "test", 0, "queued", nil, "golang:1.24", ".", `["sh","-c","go test ./..."]`, `{"A":"1"}`, timeout, ".coyote/pipeline.yml", ".", "https://github.com/acme/repo.git", "abc123", "main", nil, nil, 1, "digest", `{"version":1}`, nil, nil, nil, now, nil, nil, nil, nil, `[]`),
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
		sqlmock.NewRows([]string{"id", "build_id", "step_id", "name", "step_index", "status", "queue_name", "image", "working_dir", "command_json", "env_json", "timeout_seconds", "pipeline_file_path", "context_dir", "source_repo_url", "source_commit_sha", "source_ref_name", "source_archive_uri", "source_archive_digest", "spec_version", "spec_digest", "resolved_spec_json", "claim_token", "claimed_by", "claim_expires_at", "created_at", "started_at", "finished_at", "error_message", "exit_code", "output_refs_json"}).
			AddRow("job-1", "build-1", "step-1", "test", 0, "running", nil, "golang:1.24", ".", `["sh","-c","go test ./..."]`, `{"A":"1"}`, nil, nil, nil, "https://github.com/acme/repo.git", "abc123", nil, nil, nil, 1, nil, `{}`, "claim-1", "worker-1", lease, now, now, nil, nil, nil, `[]`),
	)

	_, outcome, err := repo.RenewJobLease(context.Background(), "job-1", "claim-1", lease)
	if err != nil {
		t.Fatalf("renew failed: %v", err)
	}
	if outcome != repository.StepCompletionCompleted {
		t.Fatalf("expected completed outcome, got %q", outcome)
	}

	mock.ExpectQuery(`UPDATE build_jobs\s+SET status = \$3`).WithArgs("job-1", "claim-1", "success", finished, nil, 0, `[]`).WillReturnRows(
		sqlmock.NewRows([]string{"id", "build_id", "step_id", "name", "step_index", "status", "queue_name", "image", "working_dir", "command_json", "env_json", "timeout_seconds", "pipeline_file_path", "context_dir", "source_repo_url", "source_commit_sha", "source_ref_name", "source_archive_uri", "source_archive_digest", "spec_version", "spec_digest", "resolved_spec_json", "claim_token", "claimed_by", "claim_expires_at", "created_at", "started_at", "finished_at", "error_message", "exit_code", "output_refs_json"}).
			AddRow("job-1", "build-1", "step-1", "test", 0, "success", nil, "golang:1.24", ".", `["sh","-c","go test ./..."]`, `{"A":"1"}`, nil, nil, nil, "https://github.com/acme/repo.git", "abc123", nil, nil, nil, 1, nil, `{}`, nil, nil, nil, now, now, finished, nil, 0, `[]`),
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

func stringPtr(value string) *string {
	return &value
}
