package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type APITokenRepository struct {
	db *sql.DB
}

func NewAPITokenRepository(db *sql.DB) *APITokenRepository {
	return &APITokenRepository{db: db}
}

func (r *APITokenRepository) Create(ctx context.Context, token domain.APIToken) (domain.APIToken, error) {
	const query = `
		INSERT INTO api_tokens (id, user_id, name, token_hash, token_prefix, expires_at, last_used_at, revoked_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id, user_id, name, token_hash, token_prefix, expires_at, last_used_at, revoked_at, created_at, updated_at
	`

	created, err := scanAPIToken(r.db.QueryRowContext(ctx, query,
		token.ID,
		token.UserID,
		token.Name,
		token.TokenHash,
		token.TokenPrefix,
		token.ExpiresAt,
		token.LastUsedAt,
		token.RevokedAt,
		token.CreatedAt,
		token.UpdatedAt,
	))
	if err != nil {
		return domain.APIToken{}, mapAPITokenWriteError(err)
	}
	return created, nil
}

func (r *APITokenRepository) ListByUserID(ctx context.Context, userID string) (tokens []domain.APIToken, err error) {
	const query = `
		SELECT id, user_id, name, token_hash, token_prefix, expires_at, last_used_at, revoked_at, created_at, updated_at
		FROM api_tokens
		WHERE user_id = $1
		ORDER BY created_at ASC, id ASC
	`

	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	tokens = make([]domain.APIToken, 0)
	for rows.Next() {
		token, scanErr := scanAPIToken(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		tokens = append(tokens, token)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return tokens, nil
}

func (r *APITokenRepository) GetByHash(ctx context.Context, tokenHash string) (domain.APIToken, error) {
	const query = `
		SELECT id, user_id, name, token_hash, token_prefix, expires_at, last_used_at, revoked_at, created_at, updated_at
		FROM api_tokens
		WHERE token_hash = $1
	`

	token, err := scanAPIToken(r.db.QueryRowContext(ctx, query, tokenHash))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.APIToken{}, repository.ErrAPITokenNotFound
		}
		return domain.APIToken{}, err
	}
	return token, nil
}

func (r *APITokenRepository) RevokeByID(ctx context.Context, userID string, tokenID string, revokedAt time.Time) error {
	const query = `
		UPDATE api_tokens
		SET revoked_at = $3,
			updated_at = $3
		WHERE id = $1 AND user_id = $2
	`
	result, err := r.db.ExecContext(ctx, query, tokenID, userID, revokedAt)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return repository.ErrAPITokenNotFound
	}
	return nil
}

func (r *APITokenRepository) TouchLastUsed(ctx context.Context, tokenID string, lastUsedAt time.Time) error {
	const query = `
		UPDATE api_tokens
		SET last_used_at = $2,
			updated_at = $2
		WHERE id = $1
	`
	result, err := r.db.ExecContext(ctx, query, tokenID, lastUsedAt)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return repository.ErrAPITokenNotFound
	}
	return nil
}

func scanAPIToken(scanner rowScanner) (domain.APIToken, error) {
	var token domain.APIToken
	var expiresAt sql.NullTime
	var lastUsedAt sql.NullTime
	var revokedAt sql.NullTime
	err := scanner.Scan(
		&token.ID,
		&token.UserID,
		&token.Name,
		&token.TokenHash,
		&token.TokenPrefix,
		&expiresAt,
		&lastUsedAt,
		&revokedAt,
		&token.CreatedAt,
		&token.UpdatedAt,
	)
	if err != nil {
		return domain.APIToken{}, err
	}
	if expiresAt.Valid {
		token.ExpiresAt = &expiresAt.Time
	}
	if lastUsedAt.Valid {
		token.LastUsedAt = &lastUsedAt.Time
	}
	if revokedAt.Valid {
		token.RevokedAt = &revokedAt.Time
	}
	return token, nil
}

func mapAPITokenWriteError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.ConstraintName == "api_tokens_user_id_fkey" {
		return repository.ErrUserNotFound
	}
	return err
}
