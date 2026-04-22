package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type JobManagedImageConfigRepository struct {
	db *sql.DB
}

func NewJobManagedImageConfigRepository(db *sql.DB) *JobManagedImageConfigRepository {
	return &JobManagedImageConfigRepository{db: db}
}

func (r *JobManagedImageConfigRepository) Create(ctx context.Context, config domain.JobManagedImageConfig) (domain.JobManagedImageConfig, error) {
	const query = `
		INSERT INTO job_managed_image_configs (
			id, job_id, managed_image_name, pipeline_path, write_credential_id,
			bot_branch_prefix, commit_author_name, commit_author_email,
			enabled, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id, job_id, managed_image_name, pipeline_path, write_credential_id,
			bot_branch_prefix, commit_author_name, commit_author_email,
			enabled, created_at, updated_at
	`

	return scanJobManagedImageConfig(r.db.QueryRowContext(ctx, query,
		config.ID,
		config.JobID,
		config.ManagedImageName,
		config.PipelinePath,
		config.WriteCredentialID,
		config.BotBranchPrefix,
		config.CommitAuthorName,
		config.CommitAuthorEmail,
		config.Enabled,
		config.CreatedAt,
		config.UpdatedAt,
	))
}

func (r *JobManagedImageConfigRepository) GetByJobID(ctx context.Context, jobID string) (domain.JobManagedImageConfig, error) {
	const query = `
		SELECT id, job_id, managed_image_name, pipeline_path, write_credential_id,
			bot_branch_prefix, commit_author_name, commit_author_email,
			enabled, created_at, updated_at
		FROM job_managed_image_configs
		WHERE job_id = $1
	`

	config, err := scanJobManagedImageConfig(r.db.QueryRowContext(ctx, query, jobID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.JobManagedImageConfig{}, repository.ErrJobManagedImageConfigNotFound
		}
		return domain.JobManagedImageConfig{}, err
	}

	return config, nil
}

func (r *JobManagedImageConfigRepository) UpsertByJobID(ctx context.Context, config domain.JobManagedImageConfig) (domain.JobManagedImageConfig, error) {
	const query = `
		INSERT INTO job_managed_image_configs (
			id, job_id, managed_image_name, pipeline_path, write_credential_id,
			bot_branch_prefix, commit_author_name, commit_author_email,
			enabled, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (job_id)
		DO UPDATE SET
			managed_image_name = EXCLUDED.managed_image_name,
			pipeline_path = EXCLUDED.pipeline_path,
			write_credential_id = EXCLUDED.write_credential_id,
			bot_branch_prefix = EXCLUDED.bot_branch_prefix,
			commit_author_name = EXCLUDED.commit_author_name,
			commit_author_email = EXCLUDED.commit_author_email,
			enabled = EXCLUDED.enabled,
			updated_at = EXCLUDED.updated_at
		RETURNING id, job_id, managed_image_name, pipeline_path, write_credential_id,
			bot_branch_prefix, commit_author_name, commit_author_email,
			enabled, created_at, updated_at
	`

	return scanJobManagedImageConfig(r.db.QueryRowContext(ctx, query,
		config.ID,
		config.JobID,
		config.ManagedImageName,
		config.PipelinePath,
		config.WriteCredentialID,
		config.BotBranchPrefix,
		config.CommitAuthorName,
		config.CommitAuthorEmail,
		config.Enabled,
		config.CreatedAt,
		config.UpdatedAt,
	))
}

func (r *JobManagedImageConfigRepository) DeleteByJobID(ctx context.Context, jobID string) error {
	const query = `DELETE FROM job_managed_image_configs WHERE job_id = $1`

	res, err := r.db.ExecContext(ctx, query, jobID)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return repository.ErrJobManagedImageConfigNotFound
	}
	return nil
}

type jobManagedImageConfigScanner interface {
	Scan(dest ...any) error
}

func scanJobManagedImageConfig(scanner jobManagedImageConfigScanner) (domain.JobManagedImageConfig, error) {
	var config domain.JobManagedImageConfig
	err := scanner.Scan(
		&config.ID,
		&config.JobID,
		&config.ManagedImageName,
		&config.PipelinePath,
		&config.WriteCredentialID,
		&config.BotBranchPrefix,
		&config.CommitAuthorName,
		&config.CommitAuthorEmail,
		&config.Enabled,
		&config.CreatedAt,
		&config.UpdatedAt,
	)
	if err != nil {
		return domain.JobManagedImageConfig{}, err
	}
	return config, nil
}
