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

func (r *ProjectRepository) GetByIDs(ctx context.Context, ids []string) (projects []domain.Project, err error) {
	ids = uniqueProjectIDs(ids)
	if len(ids) == 0 {
		return []domain.Project{}, nil
	}

	args := make([]any, 0, len(ids))
	placeholders := make([]string, 0, len(ids))
	for idx, id := range ids {
		args = append(args, id)
		placeholders = append(placeholders, fmt.Sprintf("$%d", idx+1))
	}

	query := `
		SELECT id, name, slug, description, created_at, updated_at
		FROM projects
		WHERE id IN (` + strings.Join(placeholders, ", ") + `)
		ORDER BY created_at ASC, id ASC
	`

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	projects = make([]domain.Project, 0, len(ids))
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
	const deleteQuery = `
		DELETE FROM projects
		WHERE id = $1
	`
	result, err := r.db.ExecContext(ctx, deleteQuery, id)
	if err != nil {
		if isProjectDeleteBlockedByJobs(err) {
			return repository.ErrProjectHasJobs
		}
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

func isProjectDeleteBlockedByJobs(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23503" && pgErr.ConstraintName == "fk_jobs_project_id"
	}
	return false
}

func uniqueProjectIDs(ids []string) []string {
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
