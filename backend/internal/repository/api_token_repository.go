package repository

import (
	"context"
	"errors"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

var ErrAPITokenNotFound = errors.New("api token not found")

type APITokenRepository interface {
	Create(ctx context.Context, token domain.APIToken) (domain.APIToken, error)
	ListByUserID(ctx context.Context, userID string) ([]domain.APIToken, error)
	GetByHash(ctx context.Context, tokenHash string) (domain.APIToken, error)
	RevokeByID(ctx context.Context, userID string, tokenID string, revokedAt time.Time) error
	TouchLastUsed(ctx context.Context, tokenID string, lastUsedAt time.Time) error
}
