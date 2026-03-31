package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type JobRepository struct {
	db *sql.DB
}

func NewJobRepository(db *sql.DB) *JobRepository {
	return &JobRepository{db: db}
}

func (r *JobRepository) Create(ctx context.Context, job domain.Job) (domain.Job, error) {
	const query = `
		INSERT INTO jobs (id, project_id, name, repository_url, default_ref, pipeline_yaml, enabled, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, project_id, name, repository_url, default_ref, pipeline_yaml, enabled, created_at, updated_at
	`

	return scanJob(r.db.QueryRowContext(ctx, query,
		job.ID,
		job.ProjectID,
		job.Name,
		job.RepositoryURL,
		job.DefaultRef,
		job.PipelineYAML,
		job.Enabled,
		job.CreatedAt,
		job.UpdatedAt,
	))
}

func (r *JobRepository) List(ctx context.Context) (jobs []domain.Job, err error) {
	const query = `
		SELECT id, project_id, name, repository_url, default_ref, pipeline_yaml, enabled, created_at, updated_at
		FROM jobs
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

	jobs = make([]domain.Job, 0)
	for rows.Next() {
		job, scanErr := scanJob(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		jobs = append(jobs, job)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return jobs, nil
}

func (r *JobRepository) GetByID(ctx context.Context, id string) (domain.Job, error) {
	const query = `
		SELECT id, project_id, name, repository_url, default_ref, pipeline_yaml, enabled, created_at, updated_at
		FROM jobs
		WHERE id = $1
	`

	job, err := scanJob(r.db.QueryRowContext(ctx, query, id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Job{}, repository.ErrJobNotFound
		}
		return domain.Job{}, err
	}

	return job, nil
}

func (r *JobRepository) Update(ctx context.Context, job domain.Job) (domain.Job, error) {
	const query = `
		UPDATE jobs
		SET project_id = $2,
			name = $3,
			repository_url = $4,
			default_ref = $5,
			pipeline_yaml = $6,
			enabled = $7,
			updated_at = $8
		WHERE id = $1
		RETURNING id, project_id, name, repository_url, default_ref, pipeline_yaml, enabled, created_at, updated_at
	`

	updated, err := scanJob(r.db.QueryRowContext(ctx, query,
		job.ID,
		job.ProjectID,
		job.Name,
		job.RepositoryURL,
		job.DefaultRef,
		job.PipelineYAML,
		job.Enabled,
		job.UpdatedAt,
	))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Job{}, repository.ErrJobNotFound
		}
		return domain.Job{}, err
	}

	return updated, nil
}

func scanJob(scanner rowScanner) (domain.Job, error) {
	var job domain.Job
	err := scanner.Scan(
		&job.ID,
		&job.ProjectID,
		&job.Name,
		&job.RepositoryURL,
		&job.DefaultRef,
		&job.PipelineYAML,
		&job.Enabled,
		&job.CreatedAt,
		&job.UpdatedAt,
	)
	if err != nil {
		return domain.Job{}, err
	}
	return job, nil
}
