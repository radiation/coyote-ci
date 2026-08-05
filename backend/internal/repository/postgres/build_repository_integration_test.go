package postgres

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestBuildRepository_PostgresRejectsDuplicateBuildNumberWithinJob(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("COYOTE_TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("set COYOTE_TEST_DATABASE_URL to run Postgres integration tests")
	}

	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("open postgres connection: %v", err)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			t.Fatalf("close postgres connection: %v", closeErr)
		}
	}()

	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	now := time.Now().UTC()
	projectID := uuid.NewString()
	jobA := uuid.NewString()
	jobB := uuid.NewString()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO projects (id, name, slug, description, created_at, updated_at)
		VALUES ($1, $2, $3, NULL, $4, $4)
	`, projectID, "Project "+projectID, "project-"+uuid.NewString(), now)
	if err != nil {
		t.Fatalf("insert project %s: %v", projectID, err)
	}

	for _, jobID := range []string{jobA, jobB} {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO jobs (id, project_id, name, priority, repository_url, push_enabled, trigger_mode, branch_allowlist, tag_allowlist, enabled, created_at, updated_at)
			VALUES ($1, $2, $3, 5, 'https://example.invalid/repo.git', FALSE, 'branches', '[]'::jsonb, '[]'::jsonb, TRUE, $4, $4)
		`, jobID, projectID, "job-"+jobID, now)
		if err != nil {
			t.Fatalf("insert job %s: %v", jobID, err)
		}
	}

	insertBuild := func(buildID string, jobID string, buildNumber int64) error {
		_, execErr := tx.ExecContext(ctx, `
			INSERT INTO builds (id, build_number, project_id, job_id, priority, status, created_at, current_step_index, attempt_number, trigger_kind, image_source_kind)
			VALUES ($1, $2, $3, $4, 5, 'pending', $5, 0, 1, 'manual', 'external')
		`, buildID, buildNumber, projectID, jobID, now)
		return execErr
	}

	insertAErr := insertBuild(uuid.NewString(), jobA, 1)
	if insertAErr != nil {
		t.Fatalf("insert first build for job A: %v", insertAErr)
	}
	insertBErr := insertBuild(uuid.NewString(), jobB, 1)
	if insertBErr != nil {
		t.Fatalf("insert first build for job B: %v", insertBErr)
	}

	err = insertBuild(uuid.NewString(), jobA, 1)
	if err == nil {
		t.Fatal("expected duplicate (job_id, build_number) insert to fail")
	}

	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("expected pg error, got %T: %v", err, err)
	}
	if pgErr.Code != "23505" {
		t.Fatalf("expected unique violation 23505, got %s", pgErr.Code)
	}
	if pgErr.ConstraintName != "idx_builds_job_id_build_number" {
		t.Fatalf("expected idx_builds_job_id_build_number violation, got %q", pgErr.ConstraintName)
	}
}

func TestBuildRepository_PostgresRejectsPartialPullRequestSnapshot(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("COYOTE_TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("set COYOTE_TEST_DATABASE_URL to run Postgres integration tests")
	}

	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("open postgres connection: %v", err)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			t.Fatalf("close postgres connection: %v", closeErr)
		}
	}()

	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	now := time.Now().UTC()
	projectID := uuid.NewString()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO projects (id, name, slug, description, created_at, updated_at)
		VALUES ($1, $2, $3, NULL, $4, $4)
	`, projectID, "Project "+projectID, "project-"+uuid.NewString(), now)
	if err != nil {
		t.Fatalf("insert project %s: %v", projectID, err)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO builds (id, build_number, project_id, priority, status, created_at, current_step_index, attempt_number, trigger_kind, image_source_kind, pull_request_number)
		VALUES ($1, 1, $2, 5, 'pending', $3, 0, 1, 'manual', 'external', 42)
	`, uuid.NewString(), projectID, now)
	if err == nil {
		t.Fatal("expected partial pull-request snapshot insert to fail")
	}

	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("expected pg error, got %T: %v", err, err)
	}
	if pgErr.Code != "23514" {
		t.Fatalf("expected check violation 23514, got %s", pgErr.Code)
	}
	if pgErr.ConstraintName != "builds_pull_request_snapshot_check" {
		t.Fatalf("expected builds_pull_request_snapshot_check violation, got %q", pgErr.ConstraintName)
	}
}
