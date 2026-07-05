package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func TestNewAPITokenRepository(t *testing.T) {
	repo := NewAPITokenRepository()
	if repo == nil {
		t.Fatal("expected repository, got nil")
		return
	}
	if repo.tokens == nil {
		t.Fatal("expected token map to be initialized")
	}
}

func TestAPITokenRepository_CreateListGetRevokeAndTouch(t *testing.T) {
	ctx := context.Background()
	repo := NewAPITokenRepository()
	now := time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC)
	expiresAt := now.Add(time.Hour)

	generated, err := repo.Create(ctx, domain.APIToken{
		UserID:      "user-1",
		Name:        "generated",
		TokenHash:   "hash-generated",
		TokenPrefix: "coyote_pat_gen",
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		t.Fatalf("create generated token failed: %v", err)
	}
	if generated.ID == "" {
		t.Fatal("expected generated id")
	}

	created, err := repo.Create(ctx, domain.APIToken{
		ID:          "token-1",
		UserID:      "user-1",
		Name:        "cli",
		Scopes:      []domain.APITokenScope{domain.APITokenScopeBuildRead},
		TokenHash:   "hash-1",
		TokenPrefix: "coyote_pat_12345678",
		ExpiresAt:   &expiresAt,
		CreatedAt:   now.Add(-time.Hour),
		UpdatedAt:   now.Add(-time.Hour),
	})
	if err != nil {
		t.Fatalf("create token failed: %v", err)
	}
	if created.ID != "token-1" {
		t.Fatalf("expected provided id, got %q", created.ID)
	}

	other, err := repo.Create(ctx, domain.APIToken{
		ID:          "token-2",
		UserID:      "user-2",
		Name:        "other",
		TokenHash:   "hash-2",
		TokenPrefix: "coyote_pat_other",
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		t.Fatalf("create other token failed: %v", err)
	}

	listed, err := repo.ListByUserID(ctx, "user-1")
	if err != nil {
		t.Fatalf("list tokens failed: %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("expected 2 user tokens, got %d", len(listed))
	}
	if listed[0].ID != "token-1" {
		t.Fatalf("expected oldest token first, got %+v", listed)
	}
	for _, token := range listed {
		if token.UserID != "user-1" {
			t.Fatalf("expected only user-1 tokens, got %+v", listed)
		}
	}

	byHash, err := repo.GetByHash(ctx, "hash-1")
	if err != nil {
		t.Fatalf("get by hash failed: %v", err)
	}
	if byHash.ID != "token-1" {
		t.Fatalf("expected token-1, got %q", byHash.ID)
	}
	if len(byHash.Scopes) != 1 || byHash.Scopes[0] != domain.APITokenScopeBuildRead {
		t.Fatalf("expected build:read scope, got %v", byHash.Scopes)
	}

	revokedAt := now.Add(2 * time.Hour)
	if revokeErr := repo.RevokeByID(ctx, "user-1", "token-1", revokedAt); revokeErr != nil {
		t.Fatalf("revoke token failed: %v", revokeErr)
	}
	revoked, err := repo.GetByHash(ctx, "hash-1")
	if err != nil {
		t.Fatalf("get revoked token failed: %v", err)
	}
	if revoked.RevokedAt == nil || !revoked.RevokedAt.Equal(revokedAt) {
		t.Fatalf("expected revoked_at %s, got %v", revokedAt, revoked.RevokedAt)
	}
	if !revoked.UpdatedAt.Equal(revokedAt) {
		t.Fatalf("expected updated_at %s, got %s", revokedAt, revoked.UpdatedAt)
	}

	lastUsedAt := now.Add(3 * time.Hour)
	if touchErr := repo.TouchLastUsed(ctx, other.ID, lastUsedAt); touchErr != nil {
		t.Fatalf("touch last used failed: %v", touchErr)
	}
	touched, err := repo.GetByHash(ctx, other.TokenHash)
	if err != nil {
		t.Fatalf("get touched token failed: %v", err)
	}
	if touched.LastUsedAt == nil || !touched.LastUsedAt.Equal(lastUsedAt) {
		t.Fatalf("expected last_used_at %s, got %v", lastUsedAt, touched.LastUsedAt)
	}
	if !touched.UpdatedAt.Equal(lastUsedAt) {
		t.Fatalf("expected updated_at %s, got %s", lastUsedAt, touched.UpdatedAt)
	}
}

func TestAPITokenRepository_NotFound(t *testing.T) {
	ctx := context.Background()
	repo := NewAPITokenRepository()
	now := time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC)

	if _, err := repo.GetByHash(ctx, "missing"); !errors.Is(err, repository.ErrAPITokenNotFound) {
		t.Fatalf("expected ErrAPITokenNotFound, got %v", err)
	}
	if err := repo.RevokeByID(ctx, "user-1", "missing", now); !errors.Is(err, repository.ErrAPITokenNotFound) {
		t.Fatalf("expected missing revoke ErrAPITokenNotFound, got %v", err)
	}

	created, err := repo.Create(ctx, domain.APIToken{ID: "token-1", UserID: "user-2", TokenHash: "hash-1", CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatalf("create token failed: %v", err)
	}
	if err := repo.RevokeByID(ctx, "user-1", created.ID, now); !errors.Is(err, repository.ErrAPITokenNotFound) {
		t.Fatalf("expected wrong-user revoke ErrAPITokenNotFound, got %v", err)
	}
	if err := repo.TouchLastUsed(ctx, "missing", now); !errors.Is(err, repository.ErrAPITokenNotFound) {
		t.Fatalf("expected missing touch ErrAPITokenNotFound, got %v", err)
	}
}
