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

func TestJobRepository_ListFiltersAndPaging(t *testing.T) {
	repo := NewJobRepository()
	ctx := context.Background()
	baseTime := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)

	jobs := []domain.Job{
		{
			ID:            "job-old",
			ProjectID:     "project-1",
			Name:          "old",
			RepositoryURL: "https://github.com/example/app.git",
			PushEnabled:   true,
			Enabled:       true,
			CreatedAt:     baseTime,
		},
		{
			ID:            "job-new-b",
			ProjectID:     "project-2",
			Name:          "new-b",
			RepositoryURL: "https://github.com/example/app.git/",
			PushEnabled:   true,
			Enabled:       true,
			CreatedAt:     baseTime.Add(time.Hour),
		},
		{
			ID:            "job-new-a",
			ProjectID:     "project-1",
			Name:          "new-a",
			RepositoryURL: "https://github.com/example/app",
			PushEnabled:   false,
			Enabled:       true,
			CreatedAt:     baseTime.Add(time.Hour),
		},
	}

	for _, job := range jobs {
		if _, createErr := repo.Create(ctx, job); createErr != nil {
			t.Fatalf("create job failed: %v", createErr)
		}
	}
	generated, err := repo.Create(ctx, domain.Job{Name: "generated", CreatedAt: baseTime.Add(-time.Hour)})
	if err != nil {
		t.Fatalf("create generated job failed: %v", err)
	}
	if generated.ID == "" {
		t.Fatal("expected generated job ID")
	}

	list, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("list jobs failed: %v", err)
	}
	assertJobIDs(t, list[:3], []string{"job-new-a", "job-new-b", "job-old"})

	byIDs, err := repo.GetByIDs(ctx, []string{" missing ", "job-old", "job-new-b", "job-old", ""})
	if err != nil {
		t.Fatalf("get jobs by ids failed: %v", err)
	}
	assertJobIDs(t, byIDs, []string{"job-new-b", "job-old"})

	projectJobs, err := repo.ListByProjectID(ctx, "project-1")
	if err != nil {
		t.Fatalf("list jobs by project failed: %v", err)
	}
	assertJobIDs(t, projectJobs, []string{"job-new-a", "job-old"})

	page, err := repo.ListPaged(ctx, repository.ListParams{Limit: 2, Offset: 1})
	if err != nil {
		t.Fatalf("list paged jobs failed: %v", err)
	}
	assertJobIDs(t, page, []string{"job-new-b", "job-old"})

	emptyPage, err := repo.ListPaged(ctx, repository.ListParams{Limit: 2, Offset: 99})
	if err != nil {
		t.Fatalf("list empty page failed: %v", err)
	}
	if len(emptyPage) != 0 {
		t.Fatalf("expected empty page, got %d jobs", len(emptyPage))
	}

	pushMatches, err := repo.ListPushEnabledByRepository(ctx, " HTTPS://github.com/example/app.git ")
	if err != nil {
		t.Fatalf("list push-enabled jobs failed: %v", err)
	}
	assertJobIDs(t, pushMatches, []string{"job-new-b", "job-old"})

	blankMatches, err := repo.ListPushEnabledByRepository(ctx, "  ")
	if err != nil {
		t.Fatalf("list blank push-enabled jobs failed: %v", err)
	}
	if len(blankMatches) != 0 {
		t.Fatalf("expected no blank-repository matches, got %d", len(blankMatches))
	}

	if deleteErr := repo.Delete(ctx, "job-old"); deleteErr != nil {
		t.Fatalf("delete job failed: %v", deleteErr)
	}
	if deleteErr := repo.Delete(ctx, "job-old"); !errors.Is(deleteErr, repository.ErrJobNotFound) {
		t.Fatalf("expected ErrJobNotFound on second delete, got %v", deleteErr)
	}
	if _, updateErr := repo.Update(ctx, domain.Job{ID: "missing"}); !errors.Is(updateErr, repository.ErrJobNotFound) {
		t.Fatalf("expected ErrJobNotFound on missing update, got %v", updateErr)
	}
}

func assertJobIDs(t *testing.T, jobs []domain.Job, want []string) {
	t.Helper()
	if len(jobs) != len(want) {
		t.Fatalf("expected %d jobs, got %d: %#v", len(want), len(jobs), jobs)
	}
	for i, job := range jobs {
		if job.ID != want[i] {
			t.Fatalf("job[%d]: expected ID %q, got %q", i, want[i], job.ID)
		}
	}
}

func strPtr(v string) *string { return &v }
