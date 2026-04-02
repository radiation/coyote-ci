package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

func TestExecutionJobOutputRepository_CreateManyAndList(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sql mock: %v", err)
	}

	repo := NewExecutionJobOutputRepository(db)
	now := time.Now().UTC()

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO build_job_outputs").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	_, err = repo.CreateMany(context.Background(), []domain.ExecutionJobOutput{{
		ID:           "out-1",
		JobID:        "job-1",
		BuildID:      "build-1",
		Name:         "dist",
		Kind:         "artifact",
		DeclaredPath: "dist/**",
		Status:       domain.ExecutionJobOutputStatusDeclared,
		CreatedAt:    now,
	}})
	if err != nil {
		t.Fatalf("create many failed: %v", err)
	}

	mock.ExpectQuery("SELECT id, job_id, build_id, name, kind, declared_path").WithArgs("build-1").WillReturnRows(
		sqlmock.NewRows([]string{"id", "job_id", "build_id", "name", "kind", "declared_path", "destination_uri", "content_type", "size_bytes", "digest", "status", "created_at"}).
			AddRow("out-1", "job-1", "build-1", "dist", "artifact", "dist/**", nil, nil, nil, nil, "declared", now),
	)
	outputs, err := repo.ListByBuildID(context.Background(), "build-1")
	if err != nil {
		t.Fatalf("list by build failed: %v", err)
	}
	if len(outputs) != 1 {
		t.Fatalf("expected one output, got %d", len(outputs))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}
