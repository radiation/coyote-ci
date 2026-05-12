package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
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

	tokenService.now = func() time.Time { return now.Add(apiTokenLastUsedUpdateInterval / 2) }
	if _, authErr := tokenService.AuthenticateAPIToken(ctx, created.PlaintextToken); authErr != nil {
		t.Fatalf("authenticate token inside touch interval failed: %v", authErr)
	}
	stored, err = tokenRepo.GetByHash(ctx, HashAPIToken(created.PlaintextToken))
	if err != nil {
		t.Fatalf("get stored token after throttled auth failed: %v", err)
	}
	if stored.LastUsedAt == nil || !stored.LastUsedAt.Equal(now) {
		t.Fatalf("expected throttled last_used_at to remain %s, got %v", now, stored.LastUsedAt)
	}

	staleAt := now.Add(apiTokenLastUsedUpdateInterval)
	tokenService.now = func() time.Time { return staleAt }
	if _, authErr := tokenService.AuthenticateAPIToken(ctx, created.PlaintextToken); authErr != nil {
		t.Fatalf("authenticate token after touch interval failed: %v", authErr)
	}
	stored, err = tokenRepo.GetByHash(ctx, HashAPIToken(created.PlaintextToken))
	if err != nil {
		t.Fatalf("get stored token after stale auth failed: %v", err)
	}
	if stored.LastUsedAt == nil || !stored.LastUsedAt.Equal(staleAt) {
		t.Fatalf("expected stale last_used_at update %s, got %v", staleAt, stored.LastUsedAt)
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

func TestAPITokenService_ValidationAndDependencyErrors(t *testing.T) {
	ctx := context.Background()
	userRepo := memory.NewUserRepository()
	userService := NewUserService(userRepo)
	user, err := userService.CreateUser(ctx, CreateUserInput{Email: "dev@example.com"})
	if err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	tokenService := NewAPITokenService(memory.NewAPITokenRepository(), userRepo)

	if _, createErr := tokenService.CreateAPIToken(ctx, CreateAPITokenInput{Name: "missing-user"}); !errors.Is(createErr, repository.ErrUserNotFound) {
		t.Fatalf("expected missing user id error, got %v", createErr)
	}
	if _, createErr := tokenService.CreateAPIToken(ctx, CreateAPITokenInput{UserID: "missing-user", Name: "cli"}); !errors.Is(createErr, repository.ErrUserNotFound) {
		t.Fatalf("expected unknown user error, got %v", createErr)
	}
	if _, createErr := tokenService.CreateAPIToken(ctx, CreateAPITokenInput{UserID: user.ID, Name: "   "}); !errors.Is(createErr, ErrAPITokenNameRequired) {
		t.Fatalf("expected name required error, got %v", createErr)
	}

	tokenService.random = strings.NewReader("short")
	if _, createErr := tokenService.CreateAPIToken(ctx, CreateAPITokenInput{UserID: user.ID, Name: "cli"}); createErr == nil {
		t.Fatal("expected random reader error, got nil")
	}

	if _, listErr := tokenService.ListAPITokens(ctx, "   "); !errors.Is(listErr, repository.ErrUserNotFound) {
		t.Fatalf("expected empty list user error, got %v", listErr)
	}
	if revokeErr := tokenService.RevokeAPIToken(ctx, user.ID, "   "); !errors.Is(revokeErr, repository.ErrAPITokenNotFound) {
		t.Fatalf("expected empty revoke token error, got %v", revokeErr)
	}

	short := APITokenPrefix + "short"
	if got := DisplayAPITokenPrefix(short); got != short {
		t.Fatalf("expected short display prefix %q, got %q", short, got)
	}
}

func TestAPITokenService_AuthenticateDependencyErrors(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC)
	plaintext := APITokenPrefix + "abcdefghijklmnopqrstuvwxyz"

	missingUserTokenRepo := memory.NewAPITokenRepository()
	if _, err := missingUserTokenRepo.Create(ctx, domain.APIToken{ID: "token-1", UserID: "missing-user", Name: "cli", TokenHash: HashAPIToken(plaintext), CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create token fixture failed: %v", err)
	}
	missingUserService := NewAPITokenService(missingUserTokenRepo, memory.NewUserRepository())
	if _, authErr := missingUserService.AuthenticateAPIToken(ctx, plaintext); !errors.Is(authErr, repository.ErrUserNotFound) {
		t.Fatalf("expected missing owner error, got %v", authErr)
	}

	userRepo := memory.NewUserRepository()
	userService := NewUserService(userRepo)
	user, err := userService.CreateUser(ctx, CreateUserInput{Email: "dev@example.com"})
	if err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	tokenService := NewAPITokenService(touchErrorAPITokenRepository{token: domain.APIToken{ID: "token-1", UserID: user.ID, Name: "cli", TokenHash: HashAPIToken(plaintext), CreatedAt: now, UpdatedAt: now}, err: errors.New("touch failed")}, userRepo)
	authenticated, authErr := tokenService.AuthenticateAPIToken(ctx, plaintext)
	if authErr != nil {
		t.Fatalf("expected touch failure to be ignored, got %v", authErr)
	}
	if authenticated.ID != user.ID {
		t.Fatalf("expected authenticated user %q, got %q", user.ID, authenticated.ID)
	}
}

type touchErrorAPITokenRepository struct {
	token domain.APIToken
	err   error
}

func (r touchErrorAPITokenRepository) Create(context.Context, domain.APIToken) (domain.APIToken, error) {
	return domain.APIToken{}, errors.New("create failed")
}

func (r touchErrorAPITokenRepository) ListByUserID(context.Context, string) ([]domain.APIToken, error) {
	return nil, errors.New("list failed")
}

func (r touchErrorAPITokenRepository) GetByHash(_ context.Context, tokenHash string) (domain.APIToken, error) {
	if tokenHash != r.token.TokenHash {
		return domain.APIToken{}, repository.ErrAPITokenNotFound
	}
	return r.token, nil
}

func (r touchErrorAPITokenRepository) RevokeByID(context.Context, string, string, time.Time) error {
	return repository.ErrAPITokenNotFound
}

func (r touchErrorAPITokenRepository) TouchLastUsed(context.Context, string, time.Time) error {
	return r.err
}
