package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type ProjectMembershipRepository struct {
	db *sql.DB
}

func NewProjectMembershipRepository(db *sql.DB) *ProjectMembershipRepository {
	return &ProjectMembershipRepository{db: db}
}

func (r *ProjectMembershipRepository) Upsert(ctx context.Context, membership domain.ProjectMembership) (domain.ProjectMembership, error) {
	const query = `
		INSERT INTO project_memberships (project_id, user_id, role, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (project_id, user_id) DO UPDATE
		SET role = EXCLUDED.role,
			updated_at = EXCLUDED.updated_at
		RETURNING project_id, user_id, role, created_at, updated_at
	`

	upserted, err := scanProjectMembership(r.db.QueryRowContext(ctx, query,
		membership.ProjectID,
		membership.UserID,
		membership.Role,
		membership.CreatedAt,
		membership.UpdatedAt,
	))
	if err != nil {
		return domain.ProjectMembership{}, mapProjectMembershipWriteError(err)
	}
	return upserted, nil
}

func (r *ProjectMembershipRepository) Get(ctx context.Context, projectID string, userID string) (domain.ProjectMembership, error) {
	const query = `
		SELECT project_id, user_id, role, created_at, updated_at
		FROM project_memberships
		WHERE project_id = $1 AND user_id = $2
	`

	membership, err := scanProjectMembership(r.db.QueryRowContext(ctx, query, projectID, userID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ProjectMembership{}, repository.ErrProjectMembershipNotFound
		}
		return domain.ProjectMembership{}, err
	}
	return membership, nil
}

func (r *ProjectMembershipRepository) ListByUserID(ctx context.Context, userID string) (memberships []domain.ProjectMembership, err error) {
	const query = `
		SELECT project_id, user_id, role, created_at, updated_at
		FROM project_memberships
		WHERE user_id = $1
		ORDER BY project_id ASC
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

	memberships = make([]domain.ProjectMembership, 0)
	for rows.Next() {
		membership, scanErr := scanProjectMembership(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		memberships = append(memberships, membership)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return memberships, nil
}

func (r *ProjectMembershipRepository) ListByProjectID(ctx context.Context, projectID string) (memberships []domain.ProjectMembershipWithUser, err error) {
	const query = `
		SELECT pm.project_id, pm.user_id, pm.role, pm.created_at, pm.updated_at, u.email, u.display_name
		FROM project_memberships pm
		JOIN users u ON u.id = pm.user_id
		WHERE pm.project_id = $1
		ORDER BY u.email ASC, pm.user_id ASC
	`

	rows, err := r.db.QueryContext(ctx, query, projectID)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	memberships = make([]domain.ProjectMembershipWithUser, 0)
	for rows.Next() {
		membership, scanErr := scanProjectMembershipWithUser(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		memberships = append(memberships, membership)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return memberships, nil
}

func (r *ProjectMembershipRepository) Delete(ctx context.Context, projectID string, userID string) error {
	const query = `
		DELETE FROM project_memberships
		WHERE project_id = $1 AND user_id = $2
	`
	result, err := r.db.ExecContext(ctx, query, projectID, userID)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return repository.ErrProjectMembershipNotFound
	}
	return nil
}

func scanProjectMembership(scanner rowScanner) (domain.ProjectMembership, error) {
	var membership domain.ProjectMembership
	var role string
	err := scanner.Scan(
		&membership.ProjectID,
		&membership.UserID,
		&role,
		&membership.CreatedAt,
		&membership.UpdatedAt,
	)
	if err != nil {
		return domain.ProjectMembership{}, err
	}
	membership.Role = domain.ProjectMemberRole(role)
	return membership, nil
}

func scanProjectMembershipWithUser(scanner rowScanner) (domain.ProjectMembershipWithUser, error) {
	var membership domain.ProjectMembershipWithUser
	var role string
	var displayName sql.NullString
	err := scanner.Scan(
		&membership.ProjectID,
		&membership.UserID,
		&role,
		&membership.CreatedAt,
		&membership.UpdatedAt,
		&membership.Email,
		&displayName,
	)
	if err != nil {
		return domain.ProjectMembershipWithUser{}, err
	}
	membership.Role = domain.ProjectMemberRole(role)
	if displayName.Valid {
		membership.DisplayName = &displayName.String
	}
	return membership, nil
}

func mapProjectMembershipWriteError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.ConstraintName {
		case "project_memberships_project_id_fkey":
			return repository.ErrProjectNotFound
		case "project_memberships_user_id_fkey":
			return repository.ErrUserNotFound
		}
	}
	return err
}
