package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

var ErrBuildNotFound = errors.New("build not found")

type BuildRepository struct {
	db *sql.DB
}

func NewBuildRepository(db *sql.DB) *BuildRepository {
	return &BuildRepository{db: db}
}

func (r *BuildRepository) Create(ctx context.Context, build domain.Build) (domain.Build, error) {
	const query = `
		INSERT INTO builds (id, project_id, status, created_at)
		VALUES ($1, $2, $3, $4)
	`

	_, err := r.db.ExecContext(
		ctx,
		query,
		build.ID,
		build.ProjectID,
		string(build.Status),
		build.CreatedAt,
	)
	if err != nil {
		return domain.Build{}, err
	}

	return build, nil
}

func (r *BuildRepository) GetByID(ctx context.Context, id string) (domain.Build, error) {
	const query = `
		SELECT id, project_id, status, created_at
		FROM builds
		WHERE id = $1
	`

	var build domain.Build
	var status string

	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&build.ID,
		&build.ProjectID,
		&status,
		&build.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Build{}, ErrBuildNotFound
		}
		return domain.Build{}, err
	}

	build.Status = domain.BuildStatus(status)
	return build, nil
}
