package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/repository/memory"
)

func TestAPITokenService_CreateStoresHashAndReturnsPlaintextOnce(t *testing.T) {
	ctx := context.Background()
	userRepo := memory.NewUserRepository()
	tokenRepo := memory.NewAPITokenRepository()
	userService := NewUserService(userRepo)
	user, err := userService.CreateUser(ctx, CreateUserInput{Email: "dev@example.com"})
	if err != nil {
		t.Fatalf("create user failed: %v", err)
	}

	tokenService := NewAPITokenService(tokenRepo, userRepo)
	created, err := tokenService.CreateAPIToken(ctx, CreateAPITokenInput{UserID: user.ID, Name: "fixtures"})
	if err != nil {
		t.Fatalf("create token failed: %v", err)
	}
	if !strings.HasPrefix(created.PlaintextToken, APITokenPrefix) {
		t.Fatalf("expected token prefix %q, got %q", APITokenPrefix, created.PlaintextToken)
	}
	if created.Token.TokenHash == "" || created.Token.TokenHash == created.PlaintextToken {
		t.Fatalf("expected stored hash, got %q", created.Token.TokenHash)
	}
	if created.Token.TokenHash != HashAPIToken(created.PlaintextToken) {
		t.Fatalf("stored hash does not match plaintext token")
	}
	if created.Token.TokenPrefix == "" || created.Token.TokenPrefix == created.PlaintextToken {
		t.Fatalf("expected short display prefix, got %q", created.Token.TokenPrefix)
	}

	listed, err := tokenService.ListAPITokens(ctx, user.ID)
	if err != nil {
		t.Fatalf("list tokens failed: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("expected one token, got %d", len(listed))
	}
	if listed[0].TokenHash == created.PlaintextToken || listed[0].TokenPrefix == created.PlaintextToken {
		t.Fatalf("plaintext token leaked into stored metadata: %+v", listed[0])
	}
}

func TestAPITokenService_AuthenticateAndRejectInvalidStates(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC)
	userRepo := memory.NewUserRepository()
	tokenRepo := memory.NewAPITokenRepository()
	userService := NewUserService(userRepo)
	user, err := userService.CreateUser(ctx, CreateUserInput{Email: "dev@example.com"})
	if err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	tokenService := NewAPITokenService(tokenRepo, userRepo)
	tokenService.now = func() time.Time { return now }

	created, err := tokenService.CreateAPIToken(ctx, CreateAPITokenInput{UserID: user.ID, Name: "cli"})
	if err != nil {
		t.Fatalf("create token failed: %v", err)
	}
	authenticated, err := tokenService.AuthenticateAPIToken(ctx, created.PlaintextToken)
	if err != nil {
		t.Fatalf("authenticate token failed: %v", err)
	}
	if authenticated.ID != user.ID {
		t.Fatalf("expected owner %q, got %q", user.ID, authenticated.ID)
	}
	stored, err := tokenRepo.GetByHash(ctx, HashAPIToken(created.PlaintextToken))
	if err != nil {
		t.Fatalf("get stored token failed: %v", err)
	}
	if stored.LastUsedAt == nil || !stored.LastUsedAt.Equal(now) {
		t.Fatalf("expected last_used_at %s, got %v", now, stored.LastUsedAt)
	}

	if _, err := tokenService.AuthenticateAPIToken(ctx, APITokenPrefix+"missing"); !errors.Is(err, ErrAPITokenInvalid) {
		t.Fatalf("expected invalid token error, got %v", err)
	}
	if _, err := tokenService.AuthenticateAPIToken(ctx, "not-a-coyote-token"); !errors.Is(err, ErrAPITokenInvalid) {
		t.Fatalf("expected invalid prefix error, got %v", err)
	}
}

func TestAPITokenService_RevokedAndExpiredTokensAreRejected(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC)
	userRepo := memory.NewUserRepository()
	tokenRepo := memory.NewAPITokenRepository()
	userService := NewUserService(userRepo)
	user, err := userService.CreateUser(ctx, CreateUserInput{Email: "dev@example.com"})
	if err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	tokenService := NewAPITokenService(tokenRepo, userRepo)
	tokenService.now = func() time.Time { return now }

	created, err := tokenService.CreateAPIToken(ctx, CreateAPITokenInput{UserID: user.ID, Name: "cli"})
	if err != nil {
		t.Fatalf("create token failed: %v", err)
	}
	if revokeErr := tokenService.RevokeAPIToken(ctx, user.ID, created.Token.ID); revokeErr != nil {
		t.Fatalf("revoke token failed: %v", revokeErr)
	}
	if _, authErr := tokenService.AuthenticateAPIToken(ctx, created.PlaintextToken); !errors.Is(authErr, ErrAPITokenInvalid) {
		t.Fatalf("expected revoked token to be invalid, got %v", authErr)
	}

	expiresAt := now.Add(time.Hour)
	expiring, err := tokenService.CreateAPIToken(ctx, CreateAPITokenInput{UserID: user.ID, Name: "short-lived", ExpiresAt: &expiresAt})
	if err != nil {
		t.Fatalf("create expiring token failed: %v", err)
	}
	tokenService.now = func() time.Time { return now.Add(2 * time.Hour) }
	if _, authErr := tokenService.AuthenticateAPIToken(ctx, expiring.PlaintextToken); !errors.Is(authErr, ErrAPITokenInvalid) {
		t.Fatalf("expected expired token to be invalid, got %v", authErr)
	}

	past := now.Add(-time.Minute)
	if _, createErr := tokenService.CreateAPIToken(ctx, CreateAPITokenInput{UserID: user.ID, Name: "past", ExpiresAt: &past}); !errors.Is(createErr, ErrAPITokenExpirationInvalid) {
		t.Fatalf("expected invalid expiration error, got %v", createErr)
	}
}

func TestAPITokenService_RevokeMissingToken(t *testing.T) {
	tokenService := NewAPITokenService(memory.NewAPITokenRepository(), memory.NewUserRepository())
	if err := tokenService.RevokeAPIToken(context.Background(), "user-1", "missing"); !errors.Is(err, repository.ErrAPITokenNotFound) {
		t.Fatalf("expected ErrAPITokenNotFound, got %v", err)
	}
}
