package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

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
		INSERT INTO jobs (id, project_id, name, repository_url, default_ref, default_commit_sha, push_enabled, push_branch, trigger_mode, branch_allowlist, tag_allowlist, pipeline_yaml, pipeline_path, enabled, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb, $11::jsonb, $12, $13, $14, $15, $16)
		RETURNING id, project_id, name, repository_url, default_ref, default_commit_sha, push_enabled, push_branch, trigger_mode, branch_allowlist, tag_allowlist, pipeline_yaml, pipeline_path, enabled, created_at, updated_at
	`

	branchAllowlistJSON, err := json.Marshal(job.BranchAllowlist)
	if err != nil {
		return domain.Job{}, err
	}
	tagAllowlistJSON, err := json.Marshal(job.TagAllowlist)
	if err != nil {
		return domain.Job{}, err
	}

	return scanJob(r.db.QueryRowContext(ctx, query,
		job.ID,
		job.ProjectID,
		job.Name,
		job.RepositoryURL,
		nilIfBlank(job.DefaultRef),
		job.DefaultCommitSHA,
		job.PushEnabled,
		job.PushBranch,
		nilIfBlank(string(job.TriggerMode)),
		string(branchAllowlistJSON),
		string(tagAllowlistJSON),
		nilIfBlank(job.PipelineYAML),
		job.PipelinePath,
		job.Enabled,
		job.CreatedAt,
		job.UpdatedAt,
	))
}

func (r *JobRepository) List(ctx context.Context) (jobs []domain.Job, err error) {
	const query = `
		SELECT id, project_id, name, repository_url, default_ref, default_commit_sha, push_enabled, push_branch, trigger_mode, branch_allowlist, tag_allowlist, pipeline_yaml, pipeline_path, enabled, created_at, updated_at
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

func (r *JobRepository) ListPushEnabledByRepository(ctx context.Context, repositoryURL string) (jobs []domain.Job, err error) {
	normalizedRepo := normalizeRepositoryURLForMatch(repositoryURL)
	if normalizedRepo == "" {
		return []domain.Job{}, nil
	}

	// Normalize the stored repository_url in SQL the same way normalizeRepositoryURLForMatch does
	// in Go: lowercase, trim whitespace, strip trailing '/', strip trailing '.git'.
	const query = `
		SELECT id, project_id, name, repository_url, default_ref, default_commit_sha, push_enabled, push_branch, trigger_mode, branch_allowlist, tag_allowlist, pipeline_yaml, pipeline_path, enabled, created_at, updated_at
		FROM jobs
		WHERE enabled = TRUE
		  AND push_enabled = TRUE
		  AND REGEXP_REPLACE(REGEXP_REPLACE(LOWER(TRIM(repository_url)), '/$', ''), '\.git$', '') = $1
		ORDER BY created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, normalizedRepo)
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

func normalizeRepositoryURLForMatch(value string) string {
	trimmed := strings.TrimSpace(strings.ToLower(value))
	if trimmed == "" {
		return ""
	}
	trimmed = strings.TrimSuffix(trimmed, "/")
	trimmed = strings.TrimSuffix(trimmed, ".git")
	return trimmed
}

func (r *JobRepository) GetByID(ctx context.Context, id string) (domain.Job, error) {
	const query = `
		SELECT id, project_id, name, repository_url, default_ref, default_commit_sha, push_enabled, push_branch, trigger_mode, branch_allowlist, tag_allowlist, pipeline_yaml, pipeline_path, enabled, created_at, updated_at
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
			default_commit_sha = $6,
			push_enabled = $7,
			push_branch = $8,
			trigger_mode = $9,
			branch_allowlist = $10::jsonb,
			tag_allowlist = $11::jsonb,
			pipeline_yaml = $12,
			pipeline_path = $13,
			enabled = $14,
			updated_at = $15
		WHERE id = $1
		RETURNING id, project_id, name, repository_url, default_ref, default_commit_sha, push_enabled, push_branch, trigger_mode, branch_allowlist, tag_allowlist, pipeline_yaml, pipeline_path, enabled, created_at, updated_at
	`

	branchAllowlistJSON, err := json.Marshal(job.BranchAllowlist)
	if err != nil {
		return domain.Job{}, err
	}
	tagAllowlistJSON, err := json.Marshal(job.TagAllowlist)
	if err != nil {
		return domain.Job{}, err
	}

	updated, err := scanJob(r.db.QueryRowContext(ctx, query,
		job.ID,
		job.ProjectID,
		job.Name,
		job.RepositoryURL,
		nilIfBlank(job.DefaultRef),
		job.DefaultCommitSHA,
		job.PushEnabled,
		job.PushBranch,
		nilIfBlank(string(job.TriggerMode)),
		string(branchAllowlistJSON),
		string(tagAllowlistJSON),
		nilIfBlank(job.PipelineYAML),
		job.PipelinePath,
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
	var defaultRef sql.NullString
	var defaultCommitSHA sql.NullString
	var triggerMode sql.NullString
	var branchAllowlistRaw []byte
	var tagAllowlistRaw []byte
	var pipelineYAML sql.NullString
	var pipelinePath sql.NullString
	err := scanner.Scan(
		&job.ID,
		&job.ProjectID,
		&job.Name,
		&job.RepositoryURL,
		&defaultRef,
		&defaultCommitSHA,
		&job.PushEnabled,
		&job.PushBranch,
		&triggerMode,
		&branchAllowlistRaw,
		&tagAllowlistRaw,
		&pipelineYAML,
		&pipelinePath,
		&job.Enabled,
		&job.CreatedAt,
		&job.UpdatedAt,
	)
	if err != nil {
		return domain.Job{}, err
	}
	if defaultRef.Valid {
		job.DefaultRef = defaultRef.String
	}
	if defaultCommitSHA.Valid {
		v := defaultCommitSHA.String
		job.DefaultCommitSHA = &v
	}
	if triggerMode.Valid {
		job.TriggerMode = domain.JobTriggerMode(strings.TrimSpace(triggerMode.String))
	}
	if len(branchAllowlistRaw) > 0 {
		if err := json.Unmarshal(branchAllowlistRaw, &job.BranchAllowlist); err != nil {
			return domain.Job{}, err
		}
	}
	if len(tagAllowlistRaw) > 0 {
		if err := json.Unmarshal(tagAllowlistRaw, &job.TagAllowlist); err != nil {
			return domain.Job{}, err
		}
	}
	if pipelineYAML.Valid {
		job.PipelineYAML = pipelineYAML.String
	}
	if pipelinePath.Valid {
		v := pipelinePath.String
		job.PipelinePath = &v
	}
	return job, nil
}

func nilIfBlank(value string) *string {
	v := strings.TrimSpace(value)
	if v == "" {
		return nil
	}
	return &v
}
