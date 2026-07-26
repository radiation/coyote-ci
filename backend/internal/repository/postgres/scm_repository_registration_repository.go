package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type SCMRepositoryRegistrationRepository struct {
	db *sql.DB
}

func NewSCMRepositoryRegistrationRepository(db *sql.DB) *SCMRepositoryRegistrationRepository {
	return &SCMRepositoryRegistrationRepository{db: db}
}

func (r *SCMRepositoryRegistrationRepository) Create(ctx context.Context, registration domain.SCMRepositoryRegistration) (domain.SCMRepositoryRegistration, error) {
	const query = `
		INSERT INTO scm_registered_repositories (
			id, connection_id, provider_repository_id, owner_name, repository_name, full_name, clone_url, web_url, default_branch, archived, disabled, metadata_refreshed_at, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		RETURNING id, connection_id, provider_repository_id, owner_name, repository_name, full_name, clone_url, web_url, default_branch, archived, disabled, metadata_refreshed_at, created_at, updated_at
	`
	created, err := scanSCMRepositoryRegistration(r.db.QueryRowContext(ctx, query,
		registration.ID,
		registration.ConnectionID,
		registration.ProviderRepositoryID,
		registration.Owner,
		registration.Name,
		registration.FullName,
		registration.CloneURL,
		registration.WebURL,
		registration.DefaultBranch,
		registration.Archived,
		registration.Disabled,
		registration.MetadataRefreshedAt,
		registration.CreatedAt,
		registration.UpdatedAt,
	))
	if err != nil {
		if isSCMRepositoryRegistrationUniqueViolation(err, "scm_registered_repositories_connection_id_provider_repository_id_key") {
			return domain.SCMRepositoryRegistration{}, repository.ErrSCMRepositoryRegistrationDuplicate
		}
		return domain.SCMRepositoryRegistration{}, err
	}
	return created, nil
}

func (r *SCMRepositoryRegistrationRepository) List(ctx context.Context) ([]domain.SCMRepositoryRegistration, error) {
	const query = `
		SELECT id, connection_id, provider_repository_id, owner_name, repository_name, full_name, clone_url, web_url, default_branch, archived, disabled, metadata_refreshed_at, created_at, updated_at
		FROM scm_registered_repositories
		ORDER BY created_at DESC, id DESC
	`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]domain.SCMRepositoryRegistration, 0)
	for rows.Next() {
		item, scanErr := scanSCMRepositoryRegistration(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *SCMRepositoryRegistrationRepository) GetByIDs(ctx context.Context, ids []string) ([]domain.SCMRepositoryRegistration, error) {
	ids = uniqueSCMRepositoryRegistrationIDs(ids)
	if len(ids) == 0 {
		return []domain.SCMRepositoryRegistration{}, nil
	}

	args := make([]any, 0, len(ids))
	placeholders := make([]string, 0, len(ids))
	for idx, id := range ids {
		args = append(args, id)
		placeholders = append(placeholders, fmt.Sprintf("$%d", idx+1))
	}

	query := `
		SELECT id, connection_id, provider_repository_id, owner_name, repository_name, full_name, clone_url, web_url, default_branch, archived, disabled, metadata_refreshed_at, created_at, updated_at
		FROM scm_registered_repositories
		WHERE id IN (` + strings.Join(placeholders, ", ") + `)
		ORDER BY created_at DESC, id ASC
	`
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]domain.SCMRepositoryRegistration, 0, len(ids))
	for rows.Next() {
		item, scanErr := scanSCMRepositoryRegistration(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *SCMRepositoryRegistrationRepository) GetByID(ctx context.Context, id string) (domain.SCMRepositoryRegistration, error) {
	const query = `
		SELECT id, connection_id, provider_repository_id, owner_name, repository_name, full_name, clone_url, web_url, default_branch, archived, disabled, metadata_refreshed_at, created_at, updated_at
		FROM scm_registered_repositories
		WHERE id = $1
	`
	item, err := scanSCMRepositoryRegistration(r.db.QueryRowContext(ctx, query, id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.SCMRepositoryRegistration{}, repository.ErrSCMRepositoryRegistrationNotFound
		}
		return domain.SCMRepositoryRegistration{}, err
	}
	return item, nil
}

func (r *SCMRepositoryRegistrationRepository) Update(ctx context.Context, registration domain.SCMRepositoryRegistration) (domain.SCMRepositoryRegistration, error) {
	const query = `
		UPDATE scm_registered_repositories
		SET connection_id = $2,
			provider_repository_id = $3,
			owner_name = $4,
			repository_name = $5,
			full_name = $6,
			clone_url = $7,
			web_url = $8,
			default_branch = $9,
			archived = $10,
			disabled = $11,
			metadata_refreshed_at = $12,
			updated_at = $13
		WHERE id = $1
		RETURNING id, connection_id, provider_repository_id, owner_name, repository_name, full_name, clone_url, web_url, default_branch, archived, disabled, metadata_refreshed_at, created_at, updated_at
	`
	updated, err := scanSCMRepositoryRegistration(r.db.QueryRowContext(ctx, query,
		registration.ID,
		registration.ConnectionID,
		registration.ProviderRepositoryID,
		registration.Owner,
		registration.Name,
		registration.FullName,
		registration.CloneURL,
		registration.WebURL,
		registration.DefaultBranch,
		registration.Archived,
		registration.Disabled,
		registration.MetadataRefreshedAt,
		registration.UpdatedAt,
	))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.SCMRepositoryRegistration{}, repository.ErrSCMRepositoryRegistrationNotFound
		}
		if isSCMRepositoryRegistrationUniqueViolation(err, "scm_registered_repositories_connection_id_provider_repository_id_key") {
			return domain.SCMRepositoryRegistration{}, repository.ErrSCMRepositoryRegistrationDuplicate
		}
		return domain.SCMRepositoryRegistration{}, err
	}
	return updated, nil
}

type scmRepositoryRegistrationScanner interface{ Scan(dest ...any) error }

func scanSCMRepositoryRegistration(scanner scmRepositoryRegistrationScanner) (domain.SCMRepositoryRegistration, error) {
	var registration domain.SCMRepositoryRegistration
	var defaultBranch sql.NullString
	if err := scanner.Scan(
		&registration.ID,
		&registration.ConnectionID,
		&registration.ProviderRepositoryID,
		&registration.Owner,
		&registration.Name,
		&registration.FullName,
		&registration.CloneURL,
		&registration.WebURL,
		&defaultBranch,
		&registration.Archived,
		&registration.Disabled,
		&registration.MetadataRefreshedAt,
		&registration.CreatedAt,
		&registration.UpdatedAt,
	); err != nil {
		return domain.SCMRepositoryRegistration{}, err
	}
	if defaultBranch.Valid {
		value := defaultBranch.String
		registration.DefaultBranch = &value
	}
	return registration.Normalize(), nil
}

func isSCMRepositoryRegistrationUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "23505" && pgErr.ConstraintName == constraint
}

func uniqueSCMRepositoryRegistrationIDs(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		trimmedID := strings.TrimSpace(id)
		if trimmedID == "" {
			continue
		}
		if _, ok := seen[trimmedID]; ok {
			continue
		}
		seen[trimmedID] = struct{}{}
		result = append(result, trimmedID)
	}
	return result
}
