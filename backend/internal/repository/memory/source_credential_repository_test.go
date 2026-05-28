package memory

import (
	"context"
	"errors"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func TestSourceCredentialRepository_CRUD(t *testing.T) {
	ctx := context.Background()
	repo := NewSourceCredentialRepository()
	credential := domain.SourceCredential{
		ID:        "credential-1",
		Name:      "github-token",
		Kind:      domain.SourceCredentialKindHTTPSToken,
		SecretRef: "secret/github-token",
	}

	created, createErr := repo.Create(ctx, credential)
	if createErr != nil {
		t.Fatalf("create credential: %v", createErr)
	}
	if created.ID != credential.ID {
		t.Fatalf("expected created credential %q, got %q", credential.ID, created.ID)
	}

	listed, listErr := repo.List(ctx)
	if listErr != nil {
		t.Fatalf("list credentials: %v", listErr)
	}
	if len(listed) != 1 || listed[0].ID != credential.ID {
		t.Fatalf("expected one credential, got %+v", listed)
	}

	fetched, getErr := repo.GetByID(ctx, credential.ID)
	if getErr != nil {
		t.Fatalf("get credential: %v", getErr)
	}
	if fetched.SecretRef != credential.SecretRef {
		t.Fatalf("expected secret ref %q, got %q", credential.SecretRef, fetched.SecretRef)
	}

	fetched.SecretRef = "secret/github-token-v2"
	updated, updateErr := repo.Update(ctx, fetched)
	if updateErr != nil {
		t.Fatalf("update credential: %v", updateErr)
	}
	if updated.SecretRef != "secret/github-token-v2" {
		t.Fatalf("expected updated secret ref, got %q", updated.SecretRef)
	}

	deleteErr := repo.Delete(ctx, credential.ID)
	if deleteErr != nil {
		t.Fatalf("delete credential: %v", deleteErr)
	}
	_, deletedErr := repo.GetByID(ctx, credential.ID)
	if !errors.Is(deletedErr, repository.ErrSourceCredentialNotFound) {
		t.Fatalf("expected deleted credential not found, got %v", deletedErr)
	}
}

func TestSourceCredentialRepository_NotFound(t *testing.T) {
	ctx := context.Background()
	repo := NewSourceCredentialRepository()

	_, getErr := repo.GetByID(ctx, "missing")
	if !errors.Is(getErr, repository.ErrSourceCredentialNotFound) {
		t.Fatalf("expected missing get to be not found, got %v", getErr)
	}
	_, updateErr := repo.Update(ctx, domain.SourceCredential{ID: "missing"})
	if !errors.Is(updateErr, repository.ErrSourceCredentialNotFound) {
		t.Fatalf("expected missing update to be not found, got %v", updateErr)
	}
	deleteErr := repo.Delete(ctx, "missing")
	if !errors.Is(deleteErr, repository.ErrSourceCredentialNotFound) {
		t.Fatalf("expected missing delete to be not found, got %v", deleteErr)
	}
}
