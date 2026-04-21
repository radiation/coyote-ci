package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type SourceCredentialRepository struct {
	db *sql.DB
}

func NewSourceCredentialRepository(db *sql.DB) *SourceCredentialRepository {
	return &SourceCredentialRepository{db: db}
}

func (r *SourceCredentialRepository) Create(ctx context.Context, credential domain.SourceCredential) (domain.SourceCredential, error) {
	const query = `
		INSERT INTO source_credentials (id, project_id, name, kind, username, secret_ref, created_at, updated_at)
		VALUES ($1, NULL, $2, $3, $4, $5, $6, $7)
		RETURNING id, name, kind, username, secret_ref, created_at, updated_at
	`

	return scanSourceCredential(r.db.QueryRowContext(ctx, query,
		credential.ID,
		credential.Name,
		string(credential.Kind),
		credential.Username,
		credential.SecretRef,
		credential.CreatedAt,
		credential.UpdatedAt,
	))
}

func (r *SourceCredentialRepository) List(ctx context.Context) (credentials []domain.SourceCredential, err error) {
	const query = `
		SELECT id, name, kind, username, secret_ref, created_at, updated_at
		FROM source_credentials
		ORDER BY created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	credentials = make([]domain.SourceCredential, 0)
	for rows.Next() {
		credential, scanErr := scanSourceCredential(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		credentials = append(credentials, credential)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return credentials, nil
}

func (r *SourceCredentialRepository) GetByID(ctx context.Context, id string) (domain.SourceCredential, error) {
	const query = `
		SELECT id, name, kind, username, secret_ref, created_at, updated_at
		FROM source_credentials
		WHERE id = $1
	`

	credential, err := scanSourceCredential(r.db.QueryRowContext(ctx, query, id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.SourceCredential{}, repository.ErrSourceCredentialNotFound
		}
		return domain.SourceCredential{}, err
	}
	return credential, nil
}

func (r *SourceCredentialRepository) Update(ctx context.Context, credential domain.SourceCredential) (domain.SourceCredential, error) {
	const query = `
		UPDATE source_credentials
		SET name = $2, kind = $3, username = $4, secret_ref = $5, updated_at = $6
		WHERE id = $1
		RETURNING id, name, kind, username, secret_ref, created_at, updated_at
	`

	updated, err := scanSourceCredential(r.db.QueryRowContext(ctx, query,
		credential.ID,
		credential.Name,
		string(credential.Kind),
		credential.Username,
		credential.SecretRef,
		credential.UpdatedAt,
	))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.SourceCredential{}, repository.ErrSourceCredentialNotFound
		}
		return domain.SourceCredential{}, err
	}
	return updated, nil
}

func (r *SourceCredentialRepository) Delete(ctx context.Context, id string) error {
	const query = `DELETE FROM source_credentials WHERE id = $1`

	res, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return repository.ErrSourceCredentialNotFound
	}
	return nil
}

type sourceCredentialScanner interface {
	Scan(dest ...any) error
}

func scanSourceCredential(scanner sourceCredentialScanner) (domain.SourceCredential, error) {
	var credential domain.SourceCredential
	var kind string
	var username sql.NullString
	if err := scanner.Scan(
		&credential.ID,
		&credential.Name,
		&kind,
		&username,
		&credential.SecretRef,
		&credential.CreatedAt,
		&credential.UpdatedAt,
	); err != nil {
		return domain.SourceCredential{}, err
	}
	credential.Kind = domain.SourceCredentialKind(kind)
	if username.Valid {
		v := username.String
		credential.Username = &v
	}
	return credential, nil
}
