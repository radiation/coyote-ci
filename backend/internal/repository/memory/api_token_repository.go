package memory

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type APITokenRepository struct {
	mu     sync.RWMutex
	tokens map[string]domain.APIToken
}

func NewAPITokenRepository() *APITokenRepository {
	return &APITokenRepository{tokens: map[string]domain.APIToken{}}
}

func (r *APITokenRepository) Create(_ context.Context, token domain.APIToken) (domain.APIToken, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if token.ID == "" {
		token.ID = uuid.NewString()
	}
	token.Scopes = domain.CloneAPITokenScopes(token.Scopes)
	r.tokens[token.ID] = token
	return cloneAPIToken(token), nil
}

func (r *APITokenRepository) ListByUserID(_ context.Context, userID string) ([]domain.APIToken, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]domain.APIToken, 0)
	for _, token := range r.tokens {
		if token.UserID == userID {
			out = append(out, cloneAPIToken(token))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

func (r *APITokenRepository) GetByHash(_ context.Context, tokenHash string) (domain.APIToken, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, token := range r.tokens {
		if token.TokenHash == tokenHash {
			return cloneAPIToken(token), nil
		}
	}
	return domain.APIToken{}, repository.ErrAPITokenNotFound
}

func (r *APITokenRepository) RevokeByID(_ context.Context, userID string, tokenID string, revokedAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	token, ok := r.tokens[tokenID]
	if !ok || token.UserID != userID {
		return repository.ErrAPITokenNotFound
	}
	token.RevokedAt = &revokedAt
	token.UpdatedAt = revokedAt
	r.tokens[tokenID] = token
	return nil
}

func (r *APITokenRepository) TouchLastUsed(_ context.Context, tokenID string, lastUsedAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	token, ok := r.tokens[tokenID]
	if !ok {
		return repository.ErrAPITokenNotFound
	}
	token.LastUsedAt = &lastUsedAt
	token.UpdatedAt = lastUsedAt
	r.tokens[tokenID] = token
	return nil
}

func cloneAPIToken(token domain.APIToken) domain.APIToken {
	token.Scopes = domain.CloneAPITokenScopes(token.Scopes)
	return token
}
