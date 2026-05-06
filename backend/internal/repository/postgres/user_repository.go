package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type UserRepository struct {
	db *sql.DB
}

func NewUserRepository(db *sql.DB) *UserRepository {
	return &UserRepository{db: db}
}

func (r *UserRepository) Create(ctx context.Context, user domain.User) (domain.User, error) {
	const query = `
		INSERT INTO users (id, email, display_name, global_role, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, email, display_name, global_role, created_at, updated_at
	`

	created, err := scanUser(r.db.QueryRowContext(ctx, query,
		user.ID,
		user.Email,
		user.DisplayName,
		user.GlobalRole,
		user.CreatedAt,
		user.UpdatedAt,
	))
	if err != nil {
		if isUserEmailConflict(err) {
			return domain.User{}, repository.ErrUserEmailConflict
		}
		return domain.User{}, err
	}
	return created, nil
}

func (r *UserRepository) GetByID(ctx context.Context, id string) (domain.User, error) {
	const query = `
		SELECT id, email, display_name, global_role, created_at, updated_at
		FROM users
		WHERE id = $1
	`

	user, err := scanUser(r.db.QueryRowContext(ctx, query, id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.User{}, repository.ErrUserNotFound
		}
		return domain.User{}, err
	}
	return user, nil
}

func (r *UserRepository) GetByEmail(ctx context.Context, email string) (domain.User, error) {
	const query = `
		SELECT id, email, display_name, global_role, created_at, updated_at
		FROM users
		WHERE email = $1
	`

	user, err := scanUser(r.db.QueryRowContext(ctx, query, email))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.User{}, repository.ErrUserNotFound
		}
		return domain.User{}, err
	}
	return user, nil
}

func (r *UserRepository) List(ctx context.Context) (users []domain.User, err error) {
	const query = `
		SELECT id, email, display_name, global_role, created_at, updated_at
		FROM users
		ORDER BY created_at ASC, id ASC
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

	users = make([]domain.User, 0)
	for rows.Next() {
		user, scanErr := scanUser(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return users, nil
}

func (r *UserRepository) Update(ctx context.Context, user domain.User) (domain.User, error) {
	const query = `
		UPDATE users
		SET email = $2,
			display_name = $3,
			global_role = $4,
			updated_at = $5
		WHERE id = $1
		RETURNING id, email, display_name, global_role, created_at, updated_at
	`

	updated, err := scanUser(r.db.QueryRowContext(ctx, query,
		user.ID,
		user.Email,
		user.DisplayName,
		user.GlobalRole,
		user.UpdatedAt,
	))
	if err != nil {
		if isUserEmailConflict(err) {
			return domain.User{}, repository.ErrUserEmailConflict
		}
		if errors.Is(err, sql.ErrNoRows) {
			return domain.User{}, repository.ErrUserNotFound
		}
		return domain.User{}, err
	}
	return updated, nil
}

func (r *UserRepository) Delete(ctx context.Context, id string) error {
	const query = `
		DELETE FROM users
		WHERE id = $1
	`
	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return repository.ErrUserNotFound
	}
	return nil
}

func scanUser(scanner rowScanner) (domain.User, error) {
	var user domain.User
	var displayName sql.NullString
	var globalRole string
	err := scanner.Scan(
		&user.ID,
		&user.Email,
		&displayName,
		&globalRole,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if err != nil {
		return domain.User{}, err
	}
	if displayName.Valid {
		user.DisplayName = &displayName.String
	}
	user.GlobalRole = domain.GlobalRole(globalRole)
	return user, nil
}

func isUserEmailConflict(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}
