package memory

import (
	"context"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

func TestExecutionJobOutputRepository_CreateAndList(t *testing.T) {
	repo := NewExecutionJobOutputRepository()
	now := time.Now().UTC()

	_, err := repo.CreateMany(context.Background(), []domain.ExecutionJobOutput{
		{
			ID:           "out-1",
			JobID:        "job-1",
			BuildID:      "build-1",
			Name:         "dist",
			Kind:         "artifact",
			DeclaredPath: "dist/**",
			Status:       domain.ExecutionJobOutputStatusDeclared,
			CreatedAt:    now,
		},
		{
			ID:           "out-2",
			JobID:        "job-1",
			BuildID:      "build-1",
			Name:         "report",
			Kind:         "artifact",
			DeclaredPath: "reports/*.xml",
			Status:       domain.ExecutionJobOutputStatusDeclared,
			CreatedAt:    now.Add(time.Second),
		},
	})
	if err != nil {
		t.Fatalf("create outputs failed: %v", err)
	}

	byBuild, err := repo.ListByBuildID(context.Background(), "build-1")
	if err != nil {
		t.Fatalf("list by build failed: %v", err)
	}
	if len(byBuild) != 2 {
		t.Fatalf("expected two outputs by build, got %d", len(byBuild))
	}

	byJob, err := repo.ListByJobID(context.Background(), "job-1")
	if err != nil {
		t.Fatalf("list by job failed: %v", err)
	}
	if len(byJob) != 2 {
		t.Fatalf("expected two outputs by job, got %d", len(byJob))
	}
}
