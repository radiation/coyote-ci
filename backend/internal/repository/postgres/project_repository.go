package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type ProjectRepository struct {
	db *sql.DB
}

func NewProjectRepository(db *sql.DB) *ProjectRepository {
	return &ProjectRepository{db: db}
}

func (r *ProjectRepository) Create(ctx context.Context, project domain.Project) (domain.Project, error) {
	const query = `
		INSERT INTO projects (id, name, slug, description, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, name, slug, description, created_at, updated_at
	`

	created, err := scanProject(r.db.QueryRowContext(ctx, query,
		project.ID,
		project.Name,
		project.Slug,
		project.Description,
		project.CreatedAt,
		project.UpdatedAt,
	))
	if err != nil {
		if isProjectSlugConflict(err) {
			return domain.Project{}, repository.ErrProjectSlugConflict
		}
		return domain.Project{}, err
	}

	return created, nil
}

func (r *ProjectRepository) GetByID(ctx context.Context, id string) (domain.Project, error) {
	const query = `
		SELECT id, name, slug, description, created_at, updated_at
		FROM projects
		WHERE id = $1
	`

	project, err := scanProject(r.db.QueryRowContext(ctx, query, id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Project{}, repository.ErrProjectNotFound
		}
		return domain.Project{}, err
	}
	return project, nil
}

func (r *ProjectRepository) GetBySlug(ctx context.Context, slug string) (domain.Project, error) {
	const query = `
		SELECT id, name, slug, description, created_at, updated_at
		FROM projects
		WHERE slug = $1
	`

	project, err := scanProject(r.db.QueryRowContext(ctx, query, slug))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Project{}, repository.ErrProjectNotFound
		}
		return domain.Project{}, err
	}
	return project, nil
}

func (r *ProjectRepository) List(ctx context.Context) (projects []domain.Project, err error) {
	const query = `
		SELECT id, name, slug, description, created_at, updated_at
		FROM projects
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

	projects = make([]domain.Project, 0)
	for rows.Next() {
		project, scanErr := scanProject(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		projects = append(projects, project)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return projects, nil
}

func (r *ProjectRepository) Update(ctx context.Context, project domain.Project) (domain.Project, error) {
	const query = `
		UPDATE projects
		SET name = $2,
			slug = $3,
			description = $4,
			updated_at = $5
		WHERE id = $1
		RETURNING id, name, slug, description, created_at, updated_at
	`

	updated, err := scanProject(r.db.QueryRowContext(ctx, query,
		project.ID,
		project.Name,
		project.Slug,
		project.Description,
		project.UpdatedAt,
	))
	if err != nil {
		if isProjectSlugConflict(err) {
			return domain.Project{}, repository.ErrProjectSlugConflict
		}
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Project{}, repository.ErrProjectNotFound
		}
		return domain.Project{}, err
	}

	return updated, nil
}

func (r *ProjectRepository) Delete(ctx context.Context, id string) error {
	const countQuery = `
		SELECT COUNT(1)
		FROM jobs
		WHERE project_id = $1
	`
	var jobCount int
	if err := r.db.QueryRowContext(ctx, countQuery, id).Scan(&jobCount); err != nil {
		return err
	}
	if jobCount > 0 {
		return repository.ErrProjectHasJobs
	}

	const deleteQuery = `
		DELETE FROM projects
		WHERE id = $1
	`
	result, err := r.db.ExecContext(ctx, deleteQuery, id)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return repository.ErrProjectNotFound
	}
	return nil
}

func scanProject(scanner rowScanner) (domain.Project, error) {
	var project domain.Project
	var description sql.NullString
	err := scanner.Scan(
		&project.ID,
		&project.Name,
		&project.Slug,
		&description,
		&project.CreatedAt,
		&project.UpdatedAt,
	)
	if err != nil {
		return domain.Project{}, err
	}
	if description.Valid {
		project.Description = &description.String
	}
	return project, nil
}

func isProjectSlugConflict(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}
