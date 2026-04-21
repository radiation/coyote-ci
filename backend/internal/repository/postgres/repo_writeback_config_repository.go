package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type RepoWritebackConfigRepository struct {
	db *sql.DB
}

func NewRepoWritebackConfigRepository(db *sql.DB) *RepoWritebackConfigRepository {
	return &RepoWritebackConfigRepository{db: db}
}

func (r *RepoWritebackConfigRepository) Create(ctx context.Context, cfg domain.RepoWritebackConfig) (domain.RepoWritebackConfig, error) {
	const query = `
		INSERT INTO repo_writeback_configs (
			id, project_id, repository_url, pipeline_path, managed_image_name, write_credential_id,
			bot_branch_prefix, commit_author_name, commit_author_email, enabled, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING id, project_id, repository_url, pipeline_path, managed_image_name, write_credential_id,
			bot_branch_prefix, commit_author_name, commit_author_email, enabled, created_at, updated_at
	`

	return scanRepoWritebackConfig(r.db.QueryRowContext(ctx, query,
		cfg.ID,
		cfg.ProjectID,
		cfg.RepositoryURL,
		cfg.PipelinePath,
		cfg.ManagedImageName,
		cfg.WriteCredentialID,
		cfg.BotBranchPrefix,
		cfg.CommitAuthorName,
		cfg.CommitAuthorEmail,
		cfg.Enabled,
		cfg.CreatedAt,
		cfg.UpdatedAt,
	))
}

func (r *RepoWritebackConfigRepository) ListByProjectID(ctx context.Context, projectID string) (configs []domain.RepoWritebackConfig, err error) {
	const query = `
		SELECT id, project_id, repository_url, pipeline_path, managed_image_name, write_credential_id,
			bot_branch_prefix, commit_author_name, commit_author_email, enabled, created_at, updated_at
		FROM repo_writeback_configs
		WHERE project_id = $1
		ORDER BY created_at DESC
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

	configs = make([]domain.RepoWritebackConfig, 0)
	for rows.Next() {
		cfg, scanErr := scanRepoWritebackConfig(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		configs = append(configs, cfg)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return configs, nil
}

func (r *RepoWritebackConfigRepository) GetByID(ctx context.Context, id string) (domain.RepoWritebackConfig, error) {
	const query = `
		SELECT id, project_id, repository_url, pipeline_path, managed_image_name, write_credential_id,
			bot_branch_prefix, commit_author_name, commit_author_email, enabled, created_at, updated_at
		FROM repo_writeback_configs
		WHERE id = $1
	`

	cfg, err := scanRepoWritebackConfig(r.db.QueryRowContext(ctx, query, id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.RepoWritebackConfig{}, repository.ErrRepoWritebackConfigNotFound
		}
		return domain.RepoWritebackConfig{}, err
	}
	return cfg, nil
}

func (r *RepoWritebackConfigRepository) GetByProjectAndRepo(ctx context.Context, projectID string, repositoryURL string) (domain.RepoWritebackConfig, error) {
	const query = `
		SELECT id, project_id, repository_url, pipeline_path, managed_image_name, write_credential_id,
			bot_branch_prefix, commit_author_name, commit_author_email, enabled, created_at, updated_at
		FROM repo_writeback_configs
		WHERE project_id = $1 AND repository_url = $2
	`

	cfg, err := scanRepoWritebackConfig(r.db.QueryRowContext(ctx, query, projectID, repositoryURL))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.RepoWritebackConfig{}, repository.ErrRepoWritebackConfigNotFound
		}
		return domain.RepoWritebackConfig{}, err
	}
	return cfg, nil
}

func (r *RepoWritebackConfigRepository) Update(ctx context.Context, cfg domain.RepoWritebackConfig) (domain.RepoWritebackConfig, error) {
	const query = `
		UPDATE repo_writeback_configs
		SET repository_url = $2, pipeline_path = $3, managed_image_name = $4, write_credential_id = $5,
			bot_branch_prefix = $6, commit_author_name = $7, commit_author_email = $8, enabled = $9, updated_at = $10
		WHERE id = $1
		RETURNING id, project_id, repository_url, pipeline_path, managed_image_name, write_credential_id,
			bot_branch_prefix, commit_author_name, commit_author_email, enabled, created_at, updated_at
	`

	updated, err := scanRepoWritebackConfig(r.db.QueryRowContext(ctx, query,
		cfg.ID,
		cfg.RepositoryURL,
		cfg.PipelinePath,
		cfg.ManagedImageName,
		cfg.WriteCredentialID,
		cfg.BotBranchPrefix,
		cfg.CommitAuthorName,
		cfg.CommitAuthorEmail,
		cfg.Enabled,
		cfg.UpdatedAt,
	))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.RepoWritebackConfig{}, repository.ErrRepoWritebackConfigNotFound
		}
		return domain.RepoWritebackConfig{}, err
	}
	return updated, nil
}

func (r *RepoWritebackConfigRepository) Delete(ctx context.Context, id string) error {
	const query = `DELETE FROM repo_writeback_configs WHERE id = $1`

	res, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return repository.ErrRepoWritebackConfigNotFound
	}
	return nil
}

type repoWritebackConfigScanner interface {
	Scan(dest ...any) error
}

func scanRepoWritebackConfig(scanner repoWritebackConfigScanner) (domain.RepoWritebackConfig, error) {
	var cfg domain.RepoWritebackConfig
	if err := scanner.Scan(
		&cfg.ID,
		&cfg.ProjectID,
		&cfg.RepositoryURL,
		&cfg.PipelinePath,
		&cfg.ManagedImageName,
		&cfg.WriteCredentialID,
		&cfg.BotBranchPrefix,
		&cfg.CommitAuthorName,
		&cfg.CommitAuthorEmail,
		&cfg.Enabled,
		&cfg.CreatedAt,
		&cfg.UpdatedAt,
	); err != nil {
		return domain.RepoWritebackConfig{}, err
	}
	return cfg, nil
}
