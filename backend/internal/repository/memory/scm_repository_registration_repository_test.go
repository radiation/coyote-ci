package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func TestSCMRepositoryRegistrationRepository_UniquenessAndMetadataUpdates(t *testing.T) {
	repo := NewSCMRepositoryRegistrationRepository()
	now := time.Now().UTC()
	left := domain.SCMRepositoryRegistration{ID: "repo-1", ConnectionID: "connection-1", ProviderRepositoryID: "1001", Owner: "octo", Name: "widgets", FullName: "octo/widgets", CloneURL: "https://github.com/octo/widgets.git", WebURL: "https://github.com/octo/widgets", MetadataRefreshedAt: now, CreatedAt: now, UpdatedAt: now}
	right := domain.SCMRepositoryRegistration{ID: "repo-2", ConnectionID: "connection-2", ProviderRepositoryID: "1001", Owner: "octo", Name: "widgets", FullName: "octo/widgets", CloneURL: "https://ghe.example/octo/widgets.git", WebURL: "https://ghe.example/octo/widgets", MetadataRefreshedAt: now, CreatedAt: now, UpdatedAt: now}

	if _, err := repo.Create(context.Background(), left); err != nil {
		t.Fatalf("create left failed: %v", err)
	}
	if _, err := repo.Create(context.Background(), right); err != nil {
		t.Fatalf("create right failed: %v", err)
	}

	duplicate := left
	duplicate.ID = "repo-3"
	if _, err := repo.Create(context.Background(), duplicate); !errors.Is(err, repository.ErrSCMRepositoryRegistrationDuplicate) {
		t.Fatalf("expected duplicate identity error, got %v", err)
	}

	left.Owner = "acme"
	left.Name = "platform"
	left.FullName = "acme/platform"
	left.WebURL = "https://github.com/acme/platform"
	left.CloneURL = "https://github.com/acme/platform.git"
	left.UpdatedAt = now.Add(time.Minute)
	updated, updateErr := repo.Update(context.Background(), left)
	if updateErr != nil {
		t.Fatalf("update failed: %v", updateErr)
	}
	if updated.ProviderRepositoryID != "1001" || updated.FullName != "acme/platform" {
		t.Fatalf("expected metadata update without identity change, got %+v", updated)
	}

	list, listErr := repo.List(context.Background())
	if listErr != nil {
		t.Fatalf("list failed: %v", listErr)
	}
	if len(list) != 2 {
		t.Fatalf("expected two repositories, got %d", len(list))
	}
}

func TestSCMRepositoryRegistrationRepository_NotFoundAndUpdateConflict(t *testing.T) {
	repo := NewSCMRepositoryRegistrationRepository()
	now := time.Now().UTC()
	left := domain.SCMRepositoryRegistration{ID: "repo-1", ConnectionID: "connection-1", ProviderRepositoryID: "1001", Owner: "octo", Name: "widgets", FullName: "octo/widgets", CloneURL: "https://github.com/octo/widgets.git", WebURL: "https://github.com/octo/widgets", MetadataRefreshedAt: now, CreatedAt: now, UpdatedAt: now}
	right := domain.SCMRepositoryRegistration{ID: "repo-2", ConnectionID: "connection-2", ProviderRepositoryID: "2002", Owner: "acme", Name: "platform", FullName: "acme/platform", CloneURL: "https://github.com/acme/platform.git", WebURL: "https://github.com/acme/platform", MetadataRefreshedAt: now, CreatedAt: now, UpdatedAt: now}
	if _, err := repo.Create(context.Background(), left); err != nil {
		t.Fatalf("create left failed: %v", err)
	}
	if _, err := repo.Create(context.Background(), right); err != nil {
		t.Fatalf("create right failed: %v", err)
	}
	if _, err := repo.GetByID(context.Background(), "missing"); !errors.Is(err, repository.ErrSCMRepositoryRegistrationNotFound) {
		t.Fatalf("expected missing repository error, got %v", err)
	}
	missing := left
	missing.ID = "missing"
	if _, err := repo.Update(context.Background(), missing); !errors.Is(err, repository.ErrSCMRepositoryRegistrationNotFound) {
		t.Fatalf("expected update missing repository error, got %v", err)
	}
	left.ConnectionID = right.ConnectionID
	left.ProviderRepositoryID = right.ProviderRepositoryID
	if _, err := repo.Update(context.Background(), left); !errors.Is(err, repository.ErrSCMRepositoryRegistrationDuplicate) {
		t.Fatalf("expected duplicate identity on update, got %v", err)
	}
}
