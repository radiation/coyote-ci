package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type ExternalImageBuildRepository struct{ db *sql.DB }

func NewExternalImageBuildRepository(db *sql.DB) *ExternalImageBuildRepository {
	return &ExternalImageBuildRepository{db: db}
}

const externalImageBuildColumns = `execution_job_id, provider, submission_state, external_build_id, external_resource_name, source_bucket, source_object, source_generation, target_image_reference, submitted_at, last_provider_status, terminal_result, image_digest, external_log_url, failure_detail, created_at, updated_at`

func (r *ExternalImageBuildRepository) CreateIntent(ctx context.Context, build domain.ExternalImageBuild) (domain.ExternalImageBuild, error) {
	now := time.Now().UTC()
	const query = `INSERT INTO external_image_builds (` + externalImageBuildColumns + `) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17) ON CONFLICT (execution_job_id) DO NOTHING RETURNING ` + externalImageBuildColumns
	created, err := scanExternalImageBuild(r.db.QueryRowContext(ctx, query, build.ExecutionJobID, build.Provider, domain.ExternalImageBuildSubmissionIntent, nil, nil, nil, nil, nil, build.TargetImageReference, nil, nil, nil, nil, nil, nil, now, now))
	if err == nil {
		return created, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return domain.ExternalImageBuild{}, err
	}
	return r.GetByExecutionJobID(ctx, build.ExecutionJobID)
}

func (r *ExternalImageBuildRepository) GetByExecutionJobID(ctx context.Context, executionJobID string) (domain.ExternalImageBuild, error) {
	build, err := scanExternalImageBuild(r.db.QueryRowContext(ctx, `SELECT `+externalImageBuildColumns+` FROM external_image_builds WHERE execution_job_id = $1`, executionJobID))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ExternalImageBuild{}, repository.ErrExternalImageBuildNotFound
	}
	return build, err
}

func (r *ExternalImageBuildRepository) Update(ctx context.Context, build domain.ExternalImageBuild) (domain.ExternalImageBuild, error) {
	const query = `UPDATE external_image_builds SET provider=$2, submission_state=$3, external_build_id=$4, external_resource_name=$5, source_bucket=$6, source_object=$7, source_generation=$8, target_image_reference=$9, submitted_at=$10, last_provider_status=$11, terminal_result=$12, image_digest=$13, external_log_url=$14, failure_detail=$15, updated_at=NOW() WHERE execution_job_id=$1 RETURNING ` + externalImageBuildColumns
	updated, err := scanExternalImageBuild(r.db.QueryRowContext(ctx, query, build.ExecutionJobID, build.Provider, build.SubmissionState, nullableString(build.ExternalBuildID), nullableString(build.ExternalResourceName), nullableString(build.Source.Bucket), nullableString(build.Source.Object), nullableString(build.Source.Generation), build.TargetImageReference, build.SubmittedAt, nullableString(build.LastProviderStatus), nullableString(build.TerminalResult), nullableString(build.ImageDigest), nullableString(build.ExternalLogURL), nullableString(build.FailureDetail)))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ExternalImageBuild{}, repository.ErrExternalImageBuildNotFound
	}
	return updated, err
}

func scanExternalImageBuild(row interface{ Scan(...any) error }) (domain.ExternalImageBuild, error) {
	var build domain.ExternalImageBuild
	var provider, state string
	var externalBuildID, resourceName, bucket, object, generation, status, terminal, digest, logURL, failure sql.NullString
	err := row.Scan(&build.ExecutionJobID, &provider, &state, &externalBuildID, &resourceName, &bucket, &object, &generation, &build.TargetImageReference, &build.SubmittedAt, &status, &terminal, &digest, &logURL, &failure, &build.CreatedAt, &build.UpdatedAt)
	if err != nil {
		return domain.ExternalImageBuild{}, err
	}
	build.Provider, build.SubmissionState = domain.ImageBuildProvider(provider), domain.ExternalImageBuildSubmissionState(state)
	build.ExternalBuildID, build.ExternalResourceName = externalBuildID.String, resourceName.String
	build.Source = domain.ImageBuildSource{Bucket: bucket.String, Object: object.String, Generation: generation.String}
	build.LastProviderStatus, build.TerminalResult, build.ImageDigest, build.ExternalLogURL, build.FailureDetail = status.String, terminal.String, digest.String, logURL.String, failure.String
	return build, nil
}

var _ repository.ExternalImageBuildRepository = (*ExternalImageBuildRepository)(nil)
