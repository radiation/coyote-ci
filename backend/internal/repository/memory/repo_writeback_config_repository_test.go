package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func TestRepoWritebackConfigRepository_CRUDAndLookup(t *testing.T) {
	repo := NewRepoWritebackConfigRepository()
	ctx := context.Background()
	now := time.Date(2026, 5, 28, 13, 0, 0, 0, time.UTC)

	configs := []domain.RepoWritebackConfig{
		{
			ID:                "cfg-1",
			ProjectID:         "project-1",
			RepositoryURL:     "https://github.com/example/app.git",
			PipelinePath:      ".coyote/pipeline.yml",
			ManagedImageName:  "go",
			WriteCredentialID: "cred-1",
			BotBranchPrefix:   "coyote/",
			CommitAuthorName:  "Coyote Bot",
			CommitAuthorEmail: "coyote@example.test",
			Enabled:           true,
			CreatedAt:         now,
			UpdatedAt:         now,
		},
		{
			ID:            "cfg-2",
			ProjectID:     "project-2",
			RepositoryURL: "https://github.com/example/other.git",
			Enabled:       true,
			CreatedAt:     now.Add(time.Minute),
			UpdatedAt:     now.Add(time.Minute),
		},
	}

	for _, cfg := range configs {
		if _, createErr := repo.Create(ctx, cfg); createErr != nil {
			t.Fatalf("create config failed: %v", createErr)
		}
	}

	projectConfigs, err := repo.ListByProjectID(ctx, "project-1")
	if err != nil {
		t.Fatalf("list configs by project failed: %v", err)
	}
	if len(projectConfigs) != 1 || projectConfigs[0].ID != "cfg-1" {
		t.Fatalf("expected cfg-1 for project-1, got %#v", projectConfigs)
	}

	got, err := repo.GetByID(ctx, "cfg-1")
	if err != nil {
		t.Fatalf("get config failed: %v", err)
	}
	if got.RepositoryURL != configs[0].RepositoryURL {
		t.Fatalf("expected repository URL %q, got %q", configs[0].RepositoryURL, got.RepositoryURL)
	}

	got, err = repo.GetByProjectAndRepo(ctx, "project-1", "https://github.com/example/app.git")
	if err != nil {
		t.Fatalf("get config by project/repo failed: %v", err)
	}
	if got.ID != "cfg-1" {
		t.Fatalf("expected cfg-1, got %q", got.ID)
	}

	got.Enabled = false
	updated, err := repo.Update(ctx, got)
	if err != nil {
		t.Fatalf("update config failed: %v", err)
	}
	if updated.Enabled {
		t.Fatal("expected updated config to be disabled")
	}

	if deleteErr := repo.Delete(ctx, "cfg-1"); deleteErr != nil {
		t.Fatalf("delete config failed: %v", deleteErr)
	}
	if _, getErr := repo.GetByID(ctx, "cfg-1"); !errors.Is(getErr, repository.ErrRepoWritebackConfigNotFound) {
		t.Fatalf("expected ErrRepoWritebackConfigNotFound after delete, got %v", getErr)
	}
}

func TestRepoWritebackConfigRepository_NotFound(t *testing.T) {
	repo := NewRepoWritebackConfigRepository()
	ctx := context.Background()

	if _, err := repo.GetByID(ctx, "missing"); !errors.Is(err, repository.ErrRepoWritebackConfigNotFound) {
		t.Fatalf("expected ErrRepoWritebackConfigNotFound from get, got %v", err)
	}
	if _, err := repo.GetByProjectAndRepo(ctx, "project", "repo"); !errors.Is(err, repository.ErrRepoWritebackConfigNotFound) {
		t.Fatalf("expected ErrRepoWritebackConfigNotFound from project/repo lookup, got %v", err)
	}
	if _, err := repo.Update(ctx, domain.RepoWritebackConfig{ID: "missing"}); !errors.Is(err, repository.ErrRepoWritebackConfigNotFound) {
		t.Fatalf("expected ErrRepoWritebackConfigNotFound from update, got %v", err)
	}
	if err := repo.Delete(ctx, "missing"); !errors.Is(err, repository.ErrRepoWritebackConfigNotFound) {
		t.Fatalf("expected ErrRepoWritebackConfigNotFound from delete, got %v", err)
	}
}
