package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func TestJobRepository_CreateGetListUpdate(t *testing.T) {
	repo := NewJobRepository()
	now := time.Now().UTC()

	created, err := repo.Create(context.Background(), domain.Job{
		ID:            "job-1",
		ProjectID:     "project-1",
		Name:          "backend-ci",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PushEnabled:   true,
		PushBranch:    strPtr("main"),
		PipelineYAML:  "version: 1\nsteps:\n  - name: test\n    run: go test ./...\n",
		Enabled:       true,
		CreatedAt:     now,
		UpdatedAt:     now,
	})
	if err != nil {
		t.Fatalf("create job failed: %v", err)
	}

	got, err := repo.GetByID(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("get job failed: %v", err)
	}
	if got.Name != "backend-ci" {
		t.Fatalf("expected name backend-ci, got %q", got.Name)
	}

	list, err := repo.List(context.Background())
	if err != nil {
		t.Fatalf("list jobs failed: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 job, got %d", len(list))
	}

	got.Enabled = false
	updated, err := repo.Update(context.Background(), got)
	if err != nil {
		t.Fatalf("update job failed: %v", err)
	}
	if updated.Enabled {
		t.Fatal("expected enabled=false after update")
	}
	if !updated.PushEnabled {
		t.Fatal("expected push_enabled to remain true")
	}

	matched, err := repo.ListPushEnabledByRepository(context.Background(), "https://github.com/example/backend")
	if err != nil {
		t.Fatalf("list push enabled jobs failed: %v", err)
	}
	if len(matched) != 0 {
		t.Fatalf("expected 0 matched jobs when enabled=false, got %d", len(matched))
	}

	updated.Enabled = true
	if _, updateErr := repo.Update(context.Background(), updated); updateErr != nil {
		t.Fatalf("re-enable job failed: %v", updateErr)
	}

	matched, err = repo.ListPushEnabledByRepository(context.Background(), "https://github.com/example/backend")
	if err != nil {
		t.Fatalf("list push enabled jobs failed: %v", err)
	}
	if len(matched) != 1 {
		t.Fatalf("expected 1 matched job, got %d", len(matched))
	}

	_, err = repo.GetByID(context.Background(), "missing")
	if !errors.Is(err, repository.ErrJobNotFound) {
		t.Fatalf("expected ErrJobNotFound, got %v", err)
	}
}

func strPtr(v string) *string { return &v }
