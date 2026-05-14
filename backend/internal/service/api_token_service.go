package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

const APITokenPrefix = "coyote_pat_"

const apiTokenLastUsedUpdateInterval = 5 * time.Minute

var ErrAPITokenNameRequired = errors.New("api token name is required")
var ErrAPITokenInvalid = errors.New("api token is invalid")
var ErrAPITokenExpirationInvalid = errors.New("expires_at must be in the future")

type APITokenService struct {
	tokens repository.APITokenRepository
	users  repository.UserRepository
	now    func() time.Time
	random io.Reader
}

func NewAPITokenService(tokens repository.APITokenRepository, users repository.UserRepository) *APITokenService {
	return &APITokenService{tokens: tokens, users: users, now: time.Now, random: rand.Reader}
}

type CreateAPITokenInput struct {
	UserID    string
	Name      string
	ExpiresAt *time.Time
}

type CreatedAPIToken struct {
	Token          domain.APIToken
	PlaintextToken string
}

func (s *APITokenService) CreateAPIToken(ctx context.Context, input CreateAPITokenInput) (CreatedAPIToken, error) {
	userID := strings.TrimSpace(input.UserID)
	if userID == "" {
		return CreatedAPIToken{}, repository.ErrUserNotFound
	}
	if _, err := s.users.GetByID(ctx, userID); err != nil {
		return CreatedAPIToken{}, err
	}

	name := strings.TrimSpace(input.Name)
	if name == "" {
		return CreatedAPIToken{}, ErrAPITokenNameRequired
	}
	now := s.now().UTC()
	var expiresAt *time.Time
	if input.ExpiresAt != nil {
		normalizedExpiresAt := input.ExpiresAt.UTC()
		if !normalizedExpiresAt.After(now) {
			return CreatedAPIToken{}, ErrAPITokenExpirationInvalid
		}
		expiresAt = &normalizedExpiresAt
	}

	plaintext, tokenErr := s.generatePlaintextToken()
	if tokenErr != nil {
		return CreatedAPIToken{}, tokenErr
	}
	created, err := s.tokens.Create(ctx, domain.APIToken{
		ID:          uuid.NewString(),
		UserID:      userID,
		Name:        name,
		TokenHash:   HashAPIToken(plaintext),
		TokenPrefix: DisplayAPITokenPrefix(plaintext),
		ExpiresAt:   expiresAt,
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		return CreatedAPIToken{}, err
	}
	return CreatedAPIToken{Token: created, PlaintextToken: plaintext}, nil
}

func (s *APITokenService) ListAPITokens(ctx context.Context, userID string) ([]domain.APIToken, error) {
	trimmedUserID := strings.TrimSpace(userID)
	if trimmedUserID == "" {
		return nil, repository.ErrUserNotFound
	}
	return s.tokens.ListByUserID(ctx, trimmedUserID)
}

func (s *APITokenService) RevokeAPIToken(ctx context.Context, userID string, tokenID string) error {
	trimmedUserID := strings.TrimSpace(userID)
	trimmedTokenID := strings.TrimSpace(tokenID)
	if trimmedUserID == "" || trimmedTokenID == "" {
		return repository.ErrAPITokenNotFound
	}
	return s.tokens.RevokeByID(ctx, trimmedUserID, trimmedTokenID, s.now().UTC())
}

func (s *APITokenService) AuthenticateAPIToken(ctx context.Context, plaintext string) (domain.User, error) {
	trimmed := strings.TrimSpace(plaintext)
	if !strings.HasPrefix(trimmed, APITokenPrefix) {
		return domain.User{}, ErrAPITokenInvalid
	}
	token, err := s.tokens.GetByHash(ctx, HashAPIToken(trimmed))
	if err != nil {
		if errors.Is(err, repository.ErrAPITokenNotFound) {
			return domain.User{}, ErrAPITokenInvalid
		}
		return domain.User{}, err
	}
	now := s.now().UTC()
	if token.RevokedAt != nil {
		return domain.User{}, ErrAPITokenInvalid
	}
	if token.ExpiresAt != nil && !token.ExpiresAt.After(now) {
		return domain.User{}, ErrAPITokenInvalid
	}
	user, err := s.users.GetByID(ctx, token.UserID)
	if err != nil {
		return domain.User{}, err
	}
	if shouldTouchAPITokenLastUsed(token, now) {
		_ = s.tokens.TouchLastUsed(ctx, token.ID, now)
	}
	return user, nil
}

func shouldTouchAPITokenLastUsed(token domain.APIToken, now time.Time) bool {
	return token.LastUsedAt == nil || !token.LastUsedAt.After(now.Add(-apiTokenLastUsedUpdateInterval))
}

func HashAPIToken(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

func DisplayAPITokenPrefix(plaintext string) string {
	if len(plaintext) <= len(APITokenPrefix)+8 {
		return plaintext
	}
	return plaintext[:len(APITokenPrefix)+8]
}

func (s *APITokenService) generatePlaintextToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := io.ReadFull(s.random, buf); err != nil {
		return "", err
	}
	return APITokenPrefix + base64.RawURLEncoding.EncodeToString(buf), nil
}
