package postgres

import (
	"context"
	"crypto/md5"
	"database/sql"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type ArtifactRepository struct {
	db *sql.DB
}

func NewArtifactRepository(db *sql.DB) *ArtifactRepository {
	return &ArtifactRepository{db: db}
}

const artifactColumns = `id, build_id, package_id, step_id, artifact_name, logical_path, artifact_type, storage_key, storage_provider, size_bytes, content_type, checksum_sha256, created_at`

func (r *ArtifactRepository) Create(ctx context.Context, artifact domain.BuildArtifact) (domain.BuildArtifact, error) {
	const buildScopeQuery = `
		SELECT project_id::text, job_id
		FROM builds
		WHERE id = $1
	`
	const insertPackageQuery = `
		INSERT INTO artifact_packages (
			id,
			project_id,
			job_id,
			scope_build_id,
			logical_path,
			created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT DO NOTHING
	`
	const insertArtifactQuery = `
		INSERT INTO build_artifacts (
			id,
			build_id,
			package_id,
			step_id,
			artifact_name,
			logical_path,
			artifact_type,
			storage_key,
			storage_provider,
			size_bytes,
			content_type,
			checksum_sha256,
			created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, COALESCE($13, NOW()))
		RETURNING ` + artifactColumns

	var createdAt any
	if !artifact.CreatedAt.IsZero() {
		createdAt = artifact.CreatedAt
	}

	provider := string(artifact.StorageProvider)
	if provider == "" {
		provider = string(domain.StorageProviderFilesystem)
	}

	var artifactType any
	if artifact.ArtifactType != "" {
		artifactType = string(artifact.ArtifactType)
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.BuildArtifact{}, err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var projectID string
	var jobID sql.NullString
	if queryErr := tx.QueryRowContext(ctx, buildScopeQuery, artifact.BuildID).Scan(&projectID, &jobID); queryErr != nil {
		if errors.Is(queryErr, sql.ErrNoRows) {
			return domain.BuildArtifact{}, repository.ErrArtifactNotFound
		}
		return domain.BuildArtifact{}, queryErr
	}

	scopeID := artifact.BuildID
	var packageJobID any
	var scopeBuildID any = artifact.BuildID
	if jobID.Valid && strings.TrimSpace(jobID.String) != "" {
		scopeID = strings.TrimSpace(jobID.String)
		packageJobID = scopeID
		scopeBuildID = nil
	}
	packageID := md5UUID(scopeID + "::" + artifact.LogicalPath)
	artifact.PackageID = packageID

	if _, insertErr := tx.ExecContext(ctx, insertPackageQuery, packageID, projectID, packageJobID, scopeBuildID, artifact.LogicalPath, createdAt); insertErr != nil {
		return domain.BuildArtifact{}, insertErr
	}

	created, err := scanArtifact(tx.QueryRowContext(
		ctx,
		insertArtifactQuery,
		artifact.ID,
		artifact.BuildID,
		artifact.PackageID,
		artifact.StepID,
		nullableTrimmedString(artifact.Name),
		artifact.LogicalPath,
		artifactType,
		artifact.StorageKey,
		provider,
		artifact.SizeBytes,
		artifact.ContentType,
		artifact.ChecksumSHA256,
		createdAt,
	))
	if err != nil {
		return domain.BuildArtifact{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.BuildArtifact{}, err
	}
	return created, nil
}

func (r *ArtifactRepository) ListByBuildID(ctx context.Context, buildID string) ([]domain.BuildArtifact, error) {
	const query = `
		SELECT ` + artifactColumns + `
		FROM build_artifacts
		WHERE build_id = $1
		ORDER BY created_at ASC, logical_path ASC
	`

	return scanArtifactRows(r.db.QueryContext(ctx, query, buildID))
}

func (r *ArtifactRepository) ListByStepID(ctx context.Context, stepID string) ([]domain.BuildArtifact, error) {
	const query = `
		SELECT ` + artifactColumns + `
		FROM build_artifacts
		WHERE step_id = $1
		ORDER BY created_at ASC, logical_path ASC
	`

	return scanArtifactRows(r.db.QueryContext(ctx, query, stepID))
}

func (r *ArtifactRepository) GetByID(ctx context.Context, buildID string, artifactID string) (domain.BuildArtifact, error) {
	const query = `
		SELECT ` + artifactColumns + `
		FROM build_artifacts
		WHERE build_id = $1 AND id = $2
	`

	artifact, err := scanArtifact(r.db.QueryRowContext(ctx, query, buildID, artifactID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.BuildArtifact{}, repository.ErrArtifactNotFound
		}
		return domain.BuildArtifact{}, err
	}

	return artifact, nil
}

func scanArtifactRows(rows *sql.Rows, queryErr error) ([]domain.BuildArtifact, error) {
	if queryErr != nil {
		return nil, queryErr
	}
	defer func() {
		_ = rows.Close()
	}()

	artifacts := make([]domain.BuildArtifact, 0)
	for rows.Next() {
		artifact, scanErr := scanArtifact(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		artifacts = append(artifacts, artifact)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return artifacts, nil
}

func scanArtifact(scanner rowScanner) (domain.BuildArtifact, error) {
	var artifact domain.BuildArtifact
	var stepID sql.NullString
	var artifactName sql.NullString
	var artifactType sql.NullString
	var storageProvider string
	var contentType sql.NullString
	var checksum sql.NullString

	err := scanner.Scan(
		&artifact.ID,
		&artifact.BuildID,
		&artifact.PackageID,
		&stepID,
		&artifactName,
		&artifact.LogicalPath,
		&artifactType,
		&artifact.StorageKey,
		&storageProvider,
		&artifact.SizeBytes,
		&contentType,
		&checksum,
		&artifact.CreatedAt,
	)
	if err != nil {
		return domain.BuildArtifact{}, err
	}

	if stepID.Valid {
		v := stepID.String
		artifact.StepID = &v
	}
	if artifactName.Valid {
		artifact.Name = artifactName.String
	}
	if artifactType.Valid {
		artifact.ArtifactType = domain.ArtifactType(artifactType.String)
	}
	artifact.StorageProvider = domain.StorageProvider(storageProvider)
	if contentType.Valid {
		v := contentType.String
		artifact.ContentType = &v
	}
	if checksum.Valid {
		v := checksum.String
		artifact.ChecksumSHA256 = &v
	}

	return artifact, nil
}

func scanArtifactBrowseRecord(scanner rowScanner) (domain.ArtifactBrowseRecord, error) {
	var record domain.ArtifactBrowseRecord
	var artifactStepID sql.NullString
	var artifactName sql.NullString
	var artifactType sql.NullString
	var artifactStorageProvider string
	var artifactContentType sql.NullString
	var artifactChecksum sql.NullString
	var buildNulls buildNullFields
	var stepID sql.NullString
	var stepIndex sql.NullInt64
	var stepName sql.NullString

	err := scanner.Scan(
		&record.Artifact.ID,
		&record.Artifact.BuildID,
		&record.Artifact.PackageID,
		&artifactStepID,
		&artifactName,
		&record.Artifact.LogicalPath,
		&artifactType,
		&record.Artifact.StorageKey,
		&artifactStorageProvider,
		&record.Artifact.SizeBytes,
		&artifactContentType,
		&artifactChecksum,
		&record.Artifact.CreatedAt,
		&record.Build.ID,
		&record.Build.BuildNumber,
		&record.Build.ProjectID,
		&buildNulls.jobID,
		&record.Build.Priority,
		&buildNulls.status,
		&record.Build.CreatedAt,
		&buildNulls.queuedAt,
		&buildNulls.startedAt,
		&buildNulls.finishedAt,
		&record.Build.CurrentStepIndex,
		&record.Build.AttemptNumber,
		&buildNulls.rerunOfBuildID,
		&buildNulls.rerunFromStepIdx,
		&buildNulls.errorMessage,
		&buildNulls.pipelineName,
		&buildNulls.pipelineSource,
		&buildNulls.pipelinePath,
		&buildNulls.repoURL,
		&buildNulls.ref,
		&buildNulls.commitSHA,
		&buildNulls.sourceAuthorName,
		&buildNulls.sourceAuthorEmail,
		&buildNulls.sourceCommitterName,
		&buildNulls.sourceCommitterEmail,
		&buildNulls.triggerKind,
		&buildNulls.scmProvider,
		&buildNulls.eventType,
		&buildNulls.triggerRepositoryOwner,
		&buildNulls.triggerRepositoryName,
		&buildNulls.triggerRepositoryURL,
		&buildNulls.triggerRawRef,
		&buildNulls.triggerRef,
		&buildNulls.triggerRefType,
		&buildNulls.triggerRefName,
		&buildNulls.triggerDeleted,
		&buildNulls.triggerCommitSHA,
		&buildNulls.triggerDeliveryID,
		&buildNulls.triggerActor,
		&buildNulls.triggerProducerProjectID,
		&buildNulls.triggerProducerJobID,
		&buildNulls.triggerProducerBuildID,
		&buildNulls.triggerArtifactID,
		&buildNulls.triggerArtifactPath,
		&buildNulls.triggerArtifactName,
		&buildNulls.triggerArtifactSizeBytes,
		&buildNulls.triggerArtifactChecksumSHA256,
		&buildNulls.requestedImageRef,
		&buildNulls.resolvedImageRef,
		&buildNulls.imageSourceKind,
		&buildNulls.managedImageID,
		&buildNulls.managedImageVersionID,
		&buildNulls.pullRequestNumber,
		&buildNulls.pullRequestAction,
		&buildNulls.pullRequestURL,
		&buildNulls.pullRequestBaseRef,
		&buildNulls.pullRequestBaseSHA,
		&buildNulls.pullRequestHeadRef,
		&buildNulls.pullRequestHeadSHA,
		&buildNulls.pullRequestSourceMode,
		&stepID,
		&stepIndex,
		&stepName,
	)
	if err != nil {
		return domain.ArtifactBrowseRecord{}, err
	}

	if artifactStepID.Valid {
		v := artifactStepID.String
		record.Artifact.StepID = &v
	}
	if artifactName.Valid {
		record.Artifact.Name = artifactName.String
	}
	if artifactType.Valid {
		record.Artifact.ArtifactType = domain.ArtifactType(artifactType.String)
	}
	record.Artifact.StorageProvider = domain.StorageProvider(artifactStorageProvider)
	if artifactContentType.Valid {
		v := artifactContentType.String
		record.Artifact.ContentType = &v
	}
	if artifactChecksum.Valid {
		v := artifactChecksum.String
		record.Artifact.ChecksumSHA256 = &v
	}
	buildNulls.applyTo(&record.Build)
	if stepID.Valid {
		record.Step = &domain.BuildStep{
			ID:        stepID.String,
			BuildID:   record.Build.ID,
			StepIndex: int(stepIndex.Int64),
			Name:      stepName.String,
		}
	}

	return record, nil
}

func md5UUID(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "00000000-0000-4000-a000-000000000000"
	}
	hash := md5.Sum([]byte(trimmed))
	hash[6] = (hash[6] & 0x0f) | 0x40
	hash[8] = (hash[8] & 0x0f) | 0xa0
	generated, err := uuid.FromBytes(hash[:])
	if err != nil {
		return "00000000-0000-4000-a000-000000000000"
	}
	return generated.String()
}

func nullableTrimmedString(value string) any {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return trimmed
}
